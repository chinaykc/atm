package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LockFile struct {
	file *os.File
	path string
}

func Lock(filePath string) (*LockFile, error) {
	lockPath := filepath.Join(filepath.Dir(filePath), "."+filepath.Base(filePath)+".lock")
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
		if err == nil {
			fmt.Fprintf(file, "pid=%d time=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
			return &LockFile{file: file, path: lockPath}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("open lock file: %w", err)
		}
		removeStaleLock(lockPath, 2*time.Second)
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock todo file %q: timed out waiting for existing lock", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func removeStaleLock(lockPath string, maxAge time.Duration) {
	info, err := os.Stat(lockPath)
	if err != nil {
		return
	}
	if time.Since(info.ModTime()) > maxAge {
		_ = os.Remove(lockPath)
	}
}

func (l *LockFile) Close() error {
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}
