# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Duplicates is a command-line tool for finding duplicate files by calculating MD5 hashes. It supports concurrent processing, pattern matching, and optional deletion of duplicates.

## Common Commands

### Build
```bash
make build              # Build binary to ./bin/duplicates
go build -o ./bin/duplicates duplicates.go progress.go  # Direct build
```

### Code Quality
```bash
make verification       # Run vet, golangci-lint, and errcheck
go vet ./...           # Run go vet only
```

### Testing
```bash
make test              # Run tests with coverage (note: no tests currently exist)
```

## Architecture

The codebase consists of two main components:

1. **duplicates.go**: Core application logic
   - Uses worker pool pattern for concurrent file processing (default: NumCPU workers)
   - Implements MD5 hashing with 1MB buffered reading for performance
   - Thread-safe duplicate tracking using sync.RWMutex
   - Supports file filtering by pattern and minimum size

2. **progress.go**: Progress indicator
   - Atomic counters for thread-safe progress tracking
   - Can be disabled with -nostats flag

## Key Implementation Details

- **Concurrency Model**: Worker pool with channels for file distribution
- **Hash Calculation**: MD5 with buffered I/O (1MB buffer size)
- **Thread Safety**: RWMutex for concurrent map access, atomic operations for counters
- **Error Handling**: Structured logging with github.com/sirupsen/logrus
- **Command Flags**: Uses standard library flag package for CLI parsing

## Development Notes

- No test files currently exist - consider adding tests before major changes
- Binary output goes to ./bin/ directory (gitignored)
- Uses Go 1.19 with modules
- Main dependency: logrus for structured logging