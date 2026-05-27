package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/gofrs/flock"
)

const lockTimeout = 5 * time.Second

type LockFile struct {
	lock *flock.Flock
	path string
}

type LockSet struct {
	locks []*LockFile
}

func Lock(filePath string) (*LockFile, error) {
	lockDir := filepath.Join(filepath.Dir(filePath), ".atm")
	return LockPath(filepath.Join(lockDir, "lock"))
}

func LockManyPaths(lockPaths []string) (*LockSet, error) {
	seen := map[string]bool{}
	deduped := make([]string, 0, len(lockPaths))
	for _, lockPath := range lockPaths {
		clean := filepath.Clean(lockPath)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		deduped = append(deduped, clean)
	}
	slices.Sort(deduped)
	set := &LockSet{}
	for _, lockPath := range deduped {
		lock, err := LockPath(lockPath)
		if err != nil {
			_ = set.Close()
			return nil, err
		}
		set.locks = append(set.locks, lock)
	}
	return set, nil
}

func LockPath(lockPath string) (*LockFile, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	lock := flock.New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}
	if !ok {
		return nil, fmt.Errorf("lock %s: timed out waiting for existing lock", lockPath)
	}
	return &LockFile{lock: lock, path: lockPath}, nil
}

func (l *LockFile) Close() error {
	if l == nil || l.lock == nil {
		return nil
	}
	unlockErr := l.lock.Unlock()
	closeErr := l.lock.Close()
	l.lock = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func (s *LockSet) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	for i := len(s.locks) - 1; i >= 0; i-- {
		if err := s.locks[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.locks = nil
	return firstErr
}
