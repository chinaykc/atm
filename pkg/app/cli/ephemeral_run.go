package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chinaykc/atm/pkg/runtime/engine"
)

func runEphemeralFile(ctx context.Context, opts engine.Options, source, runDir string) error {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(runDir, "source.todo.md")
	if err := copyFileForRun(source, target); err != nil {
		return err
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(runDir, "result")
	}
	opts.TaskDir = filepath.Join(runDir, "tasks")
	opts.FilePath = target
	return engine.Run(ctx, opts)
}

func runEphemeralFileCapture(ctx context.Context, opts engine.Options, source, runDir string) (engine.Result, error) {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return engine.Result{}, err
	}
	target := filepath.Join(runDir, "source.todo.md")
	if err := copyFileForRun(source, target); err != nil {
		return engine.Result{}, err
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(runDir, "result")
	}
	opts.TaskDir = filepath.Join(runDir, "tasks")
	opts.FilePath = target
	return engine.RunCapture(ctx, opts)
}

func copyFileForRun(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("read source todo: %w", err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".source-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, target)
}
