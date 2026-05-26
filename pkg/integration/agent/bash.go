package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

func RunBash(ctx context.Context, todoPath, script, workdir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Env = toolEnv(todoPath)
	cmd.Dir = workdir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bash command failed: %w", err)
	}
	return nil
}

func CaptureBash(ctx context.Context, todoPath, script, workdir string, stderr io.Writer) (string, error) {
	var stdout bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Env = toolEnv(todoPath)
	cmd.Dir = workdir
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail != "" {
			return "", fmt.Errorf("bash command failed: %w: %s", err, detail)
		}
		return "", fmt.Errorf("bash command failed: %w", err)
	}
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}
