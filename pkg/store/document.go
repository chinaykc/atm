package store

import (
	"atm/pkg/dsl"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

type BlockLease struct {
	Index    int
	BodyHash string
}

func NewBlockLease(index int, body string) BlockLease {
	return BlockLease{Index: index, BodyHash: HashBody(body)}
}

func HashBody(body string) string {
	clean, _, err := dsl.StripRunning(body)
	if err == nil {
		body = clean
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func ReadBlocks(filePath string) ([]dsl.Block, error) {
	content, err := ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	blocks := dsl.ParseBlocks(string(content))
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no tasks found in todo file %q", filePath)
	}
	return blocks, nil
}

func ReadFile(filePath string) ([]byte, error) {
	lock, err := Lock(filePath)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, ReadError{Path: filePath, Err: err}
	}
	return content, nil
}

func SaveRunning(filePath string, lease BlockLease, info dsl.RunningInfo) (BlockLease, error) {
	lock, err := Lock(filePath)
	if err != nil {
		return BlockLease{}, err
	}
	defer lock.Close()

	blocks, err := readBlocksUnlocked(filePath)
	if err != nil {
		return BlockLease{}, err
	}
	if !leaseValid(blocks, lease) {
		return BlockLease{}, ErrObsolete
	}
	blocks[lease.Index].Body = dsl.AppendRunning(blocks[lease.Index].Body, info)
	if err := writeBlocksLocked(filePath, blocks); err != nil {
		return BlockLease{}, err
	}
	return NewBlockLease(lease.Index, blocks[lease.Index].Body), nil
}

func MarkDone(filePath string, lease BlockLease, info dsl.DoneInfo) error {
	lock, err := Lock(filePath)
	if err != nil {
		return err
	}
	defer lock.Close()

	blocks, err := readBlocksUnlocked(filePath)
	if err != nil {
		return err
	}
	if !leaseValid(blocks, lease) {
		return ErrObsolete
	}
	blocks[lease.Index].Body = dsl.AppendDone(blocks[lease.Index].Body, info)
	return writeBlocksLocked(filePath, blocks)
}

func MarkSkipped(filePath string, lease BlockLease, info dsl.SkippedInfo) error {
	lock, err := Lock(filePath)
	if err != nil {
		return err
	}
	defer lock.Close()

	blocks, err := readBlocksUnlocked(filePath)
	if err != nil {
		return err
	}
	if !leaseValid(blocks, lease) {
		return ErrObsolete
	}
	blocks[lease.Index].Body = dsl.AppendSkipped(blocks[lease.Index].Body, info)
	return writeBlocksLocked(filePath, blocks)
}

func RewriteBlocks(filePath string, mutate func([]dsl.Block) error) error {
	lock, err := Lock(filePath)
	if err != nil {
		return err
	}
	defer lock.Close()

	blocks, err := readBlocksUnlocked(filePath)
	if err != nil {
		return err
	}
	if err := mutate(blocks); err != nil {
		return err
	}
	return writeBlocksLocked(filePath, blocks)
}

func AppendContent(filePath string, content []byte) error {
	lock, err := Lock(filePath)
	if err != nil {
		return err
	}
	defer lock.Close()

	current, err := os.ReadFile(filePath)
	if err != nil {
		return ReadError{Path: filePath, Err: err}
	}
	var buf bytes.Buffer
	buf.Write(current)
	buf.Write(content)
	return WriteContentLocked(filePath, buf.Bytes())
}

func readBlocksUnlocked(filePath string) ([]dsl.Block, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, ReadError{Path: filePath, Err: err}
	}
	blocks := dsl.ParseBlocks(string(content))
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no tasks found in todo file %q", filePath)
	}
	return blocks, nil
}

func leaseValid(blocks []dsl.Block, lease BlockLease) bool {
	return lease.Index >= 0 && lease.Index < len(blocks) && HashBody(blocks[lease.Index].Body) == lease.BodyHash
}

func writeBlocksLocked(filePath string, blocks []dsl.Block) error {
	var buf bytes.Buffer
	for _, b := range blocks {
		buf.WriteString(b.Prefix)
		buf.WriteString(b.Body)
		buf.WriteString(b.Sep)
	}
	return WriteContentLocked(filePath, buf.Bytes())
}

func WriteContentLocked(filePath string, content []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(filePath); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", filePath, err)
	}

	tmp, err := os.CreateTemp(filepathDir(filePath), ".todo-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("set temp file mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace %s: %w", filePath, err)
	}
	return nil
}

func filepathDir(path string) string {
	if i := len(path) - 1; i >= 0 {
		for ; i >= 0; i-- {
			if os.IsPathSeparator(path[i]) {
				if i == 0 {
					return string(path[0])
				}
				return path[:i]
			}
		}
	}
	return "."
}
