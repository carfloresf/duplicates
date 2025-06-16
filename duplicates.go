package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
)

// WalkedFile a type of struct
type WalkedFile struct {
	path string
	file os.FileInfo
}

var (
	singleThread  = false
	delete        = false
	visitCount    int64
	fileCount     int64
	dupCount      int64
	minSize       int64
	filenameMatch = "*"
	filenameRegex *regexp.Regexp
	duplicates    = struct {
		sync.RWMutex
		m map[string][]string
	}{m: make(map[string][]string)}
	noStats      bool
	walkProgress *Progress
	walkFiles    []*WalkedFile
)

func scanAndHashFile(path string, f os.FileInfo, progress *Progress) {
	// Early return if basic conditions are not met
	if f.IsDir() || f.Size() <= minSize || (filenameMatch != "*" && !filenameRegex.MatchString(f.Name())) {
		return
	}

	// Increment file count atomically
	atomic.AddInt64(&fileCount, 1)

	// Open the file
	file, err := os.Open(path)
	if err != nil {
		log.WithFields(log.Fields{
			"path":  path,
			"error": err,
		}).Error("Failed to open file")
		return
	}
	defer file.Close()

	// Create a buffered reader for better performance
	bufReader := bufio.NewReaderSize(file, 1024*1024) // 1MB buffer

	// Calculate MD5 hash
	md5Hash := md5.New()
	if _, err := io.Copy(md5Hash, bufReader); err != nil {
		log.WithFields(log.Fields{
			"path":  path,
			"error": err,
		}).Error("Failed to calculate hash")
		return
	}

	// Generate hash string
	hash := fmt.Sprintf("%x", md5Hash.Sum(nil))

	// Update duplicates map with proper locking
	duplicates.Lock()
	duplicates.m[hash] = append(duplicates.m[hash], path)
	duplicates.Unlock()

	// Update progress
	if progress != nil {
		progress.increment()
	}
}

type workerStats struct {
	processedFiles int64
	totalBytes    int64
	errors        int64
}

func worker(ctx context.Context, workerID int, jobs <-chan *WalkedFile, results chan<- error, progress *Progress) {
	stats := &workerStats{}
	defer func() {
		log.WithFields(log.Fields{
			"workerID":       workerID,
			"processedFiles": stats.processedFiles,
			"totalBytes":    stats.totalBytes,
			"errors":        stats.errors,
		}).Debug("Worker finished")
	}()

	for {
		select {
		case <-ctx.Done():
			results <- ctx.Err()
			return
		case file, ok := <-jobs:
			if !ok {
				// Channel closed, worker can exit
				results <- nil
				return
			}

			// Process the file
			if file == nil || file.file == nil {
				atomic.AddInt64(&stats.errors, 1)
				results <- fmt.Errorf("received invalid file data")
				continue
			}

			// Log file processing at debug level
			log.WithFields(log.Fields{
				"workerID": workerID,
				"file":     file.path,
				"size":     file.file.Size(),
			}).Debug("Processing file")

			// Process the file
			scanAndHashFile(file.path, file.file, progress)

			// Update statistics
			atomic.AddInt64(&stats.processedFiles, 1)
			atomic.AddInt64(&stats.totalBytes, file.file.Size())

			// Signal completion
			results <- nil
		}
	}
}

func computeHashes() error {
	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize progress bar
	walkProgress := creatProgress("Scanning %d files ...", &noStats)
	defer walkProgress.delete()

	// Create buffered channels for jobs and results
	jobs := make(chan *WalkedFile, visitCount)
	results := make(chan error, visitCount)

	// Calculate number of workers
	numWorkers := 1
	if !singleThread {
		numWorkers = runtime.NumCPU()
	}

	// Start workers
	log.WithField("workers", numWorkers).Info("Starting workers")
	for w := 1; w <= numWorkers; w++ {
		go worker(ctx, w, jobs, results, walkProgress)
	}

	// Send jobs to workers
	go func() {
		defer close(jobs)
		for _, file := range walkFiles {
			select {
			case <-ctx.Done():
				return
			case jobs <- file:
			}
		}
	}()

	// Collect results and handle errors
	var firstErr error
	for i := 0; i < len(walkFiles); i++ {
		if err := <-results; err != nil {
			if firstErr == nil {
				firstErr = err
				// Cancel context to stop other workers
				cancel()
			}
			log.WithError(err).Error("Error processing file")
		}
	}

	return firstErr
}

func visitFile(path string, f os.FileInfo, err error) error {
	visitCount++
	if !f.IsDir() && f.Size() > minSize && (filenameMatch == "*" || filenameRegex.MatchString(f.Name())) {
		walkFiles = append(walkFiles, &WalkedFile{path: path, file: f})
		walkProgress.increment()
	}
	return nil
}

func deleteFile(path string) {
	fmt.Println("Deleting " + path)
	err := os.Remove(path)
	if err != nil {
		fmt.Printf("Error deleting file: %s \n", path)
	}
}

func main() {
	flag.Int64Var(&minSize, "size", 1, "Minimum size in bytes for a file")
	flag.StringVar(&filenameMatch, "name", "*", "Filename pattern")
	flag.BoolVar(&noStats, "nostats", false, "Do no output stats")
	flag.BoolVar(&singleThread, "singleThread", false, "Work on only one thread")
	flag.BoolVar(&delete, "delete", false, "Delete duplicate files")
	var help = flag.Bool("h", false, "Display this message")
	flag.Parse()
	if *help {
		fmt.Println("duplicates is a command line tool to find duplicate files in a folder")
		fmt.Println("usage: duplicates [options...] path")
		flag.PrintDefaults()
		os.Exit(0)
	}
	if len(flag.Args()) < 1 {
		fmt.Fprintf(os.Stderr, "You have to specify at least a directory to explore ...\n")
		os.Exit(-1)
	}
	root := flag.Arg(0)
	walkProgress = creatProgress("Walking through %d files ...", &noStats)
	if !noStats {
		fmt.Printf("\nSearching duplicates in '%s' with name that match '%s' and minimum size '%d' bytes\n\n", root, filenameMatch, minSize)
	}
	r, _ := regexp.Compile(filenameMatch)
	filenameRegex = r
	err := filepath.Walk(root, visitFile)
	if err != nil {
		log.Errorln(err)
	}
	walkProgress.delete()
	computeHashes()
	for _, v := range duplicates.m {
		if len(v) > 1 {
			dupCount++
		}
	}
	if !noStats {
		fmt.Printf("\nFound %d duplicates from %d files in %s with options { size: '%d', name: '%s' }\n", dupCount, fileCount, root, minSize, filenameMatch)
	}
	fmt.Printf("/n /n /n")
	for _, v := range duplicates.m {
		if len(v) > 1 {
			for i, file := range v {
				if i > 0 && delete {
					deleteFile(file)
				} else {
					fmt.Printf("%s\n", file)
				}
			}
			fmt.Println("---------")
		}
	}

	if !noStats {
		fmt.Printf("\nFound %d duplicates from %d files in %s with options { size: '%d', name: '%s' }\n", dupCount, fileCount, root, minSize, filenameMatch)
	}
	os.Exit(0)
}
