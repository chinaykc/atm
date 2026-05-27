package store

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveActiveTodoPath(filePath string) (string, error) {
	absOriginal, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("resolve ATM file path: %w", err)
	}
	return resolveActiveTodoPath(absOriginal, 0)
}

func resolveActiveTodoPath(path string, depth int) (string, error) {
	if depth > 8 {
		return path, nil
	}
	marker := ActiveMarkerPath(filepath.Join(os.TempDir(), "atm"), path)
	data, markerErr := os.ReadFile(marker)
	if markerErr == nil {
		active := strings.TrimSpace(string(data))
		if active != "" && filepath.Clean(active) != filepath.Clean(path) {
			if _, err := os.Stat(active); err == nil {
				return resolveActiveTodoPath(active, depth+1)
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			nextMarker := ActiveMarkerPath(filepath.Join(os.TempDir(), "atm"), active)
			if _, err := os.Stat(nextMarker); err == nil {
				return resolveActiveTodoPath(active, depth+1)
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
	} else if !errors.Is(markerErr, os.ErrNotExist) {
		return "", markerErr
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return path, nil
}

func ActiveMarkerPath(tmpDir, original string) string {
	sum := sha1.Sum([]byte(original))
	return filepath.Join(tmpDir, "active-"+hex.EncodeToString(sum[:])+".path")
}
