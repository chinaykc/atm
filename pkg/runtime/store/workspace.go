package store

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Workspace struct {
	Original string
	Active   string
	Marker   string

	mu       sync.Mutex
	restored bool
}

func PrepareWorkspace(filePath string, stderr io.Writer) (*Workspace, error) {
	absOriginal, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve todo file path: %w", err)
	}
	tmpDir := filepath.Join(os.TempDir(), "atm")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("create temp todo directory: %w", err)
	}

	active, err := nextTempTodoPath(tmpDir, absOriginal)
	if err != nil {
		return nil, err
	}
	if err := moveFile(absOriginal, active); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ReadError{Path: filePath, Err: err}
		}
		return nil, fmt.Errorf("move todo file to temp path: %w", err)
	}
	marker := ActiveMarkerPath(tmpDir, absOriginal)
	if err := os.WriteFile(marker, []byte(active), 0o600); err != nil {
		_ = moveFile(active, absOriginal)
		return nil, fmt.Errorf("write active todo marker: %w", err)
	}

	fmt.Fprintf(stderr, "todo file moved to %s\n", active)
	fmt.Fprintln(stderr, "edit this temporary todo file while atm is running; it will be moved back on exit")
	return &Workspace{Original: absOriginal, Active: active, Marker: marker}, nil
}

func ResolveActiveTodoPath(filePath string) (string, error) {
	absOriginal, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("resolve todo file path: %w", err)
	}
	if _, err := os.Stat(absOriginal); err == nil {
		return absOriginal, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	marker := ActiveMarkerPath(filepath.Join(os.TempDir(), "atm"), absOriginal)
	data, err := os.ReadFile(marker)
	if err != nil {
		return absOriginal, nil
	}
	active := strings.TrimSpace(string(data))
	if active == "" {
		return absOriginal, nil
	}
	if _, err := os.Stat(active); err == nil {
		return active, nil
	}
	return absOriginal, nil
}

func ActiveMarkerPath(tmpDir, original string) string {
	sum := sha1.Sum([]byte(original))
	return filepath.Join(tmpDir, "active-"+hex.EncodeToString(sum[:])+".path")
}

func (w *Workspace) Restore() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.restored {
		return nil
	}
	w.restored = true

	if _, err := os.Stat(w.Active); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(w.Marker)
			return nil
		}
		return err
	}
	err := moveFile(w.Active, w.Original)
	if err == nil {
		_ = os.Remove(w.Marker)
	}
	return err
}

func nextTempTodoPath(tmpDir, original string) (string, error) {
	dirName := sanitizePathPart(filepath.Base(filepath.Dir(original)))
	baseName := fmt.Sprintf("todo-%s", dirName)
	for i := 0; ; i++ {
		name := baseName + ".txt"
		if i > 0 {
			name = fmt.Sprintf("%s%d.txt", baseName, i)
		}
		candidate := filepath.Join(tmpDir, name)
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(candidate)
				return "", closeErr
			}
			if err := os.Remove(candidate); err != nil {
				return "", err
			}
			return candidate, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("reserve temp todo path: %w", err)
		}
	}
}

func sanitizePathPart(s string) string {
	if s == "" || s == "." || s == string(filepath.Separator) {
		return "todo"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "todo"
	}
	return b.String()
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceLinkError(err) {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".todo-restore-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
