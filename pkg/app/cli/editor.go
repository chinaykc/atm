package cli

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/document"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func editPromptBlock(env commandEnv) (string, error) {
	editor, err := findEditor()
	if err != nil {
		return "", err
	}
	tmpDir := filepath.Join(os.TempDir(), "atm")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("create append editor directory: %w", err)
	}
	tmp, err := os.CreateTemp(tmpDir, "append-*.txt")
	if err != nil {
		return "", fmt.Errorf("create append editor file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString("# Write todo block below. Lines starting with # are ignored.\n"); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("write append editor template: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("close append editor file: %w", err)
	}
	defer os.Remove(tmpName)

	fmt.Fprintf(env.Stderr, "opening editor for append block: %s\n", tmpName)
	cmd := editorCommand(editor, tmpName)
	cmd.Stdin = env.Stdin
	cmd.Stdout = env.Stdout
	cmd.Stderr = env.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run editor %q: %w", editor, err)
	}
	data, err := os.ReadFile(tmpName)
	if err != nil {
		return "", fmt.Errorf("read append editor file: %w", err)
	}
	return stripEditorComments(string(data)), nil
}

func findEditor() (string, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value, nil
		}
	}
	for _, candidate := range []string{"nano", "vi", "notepad"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no editor found; set VISUAL or EDITOR")
}

func editorCommand(editor, filePath string) *exec.Cmd {
	if path, err := exec.LookPath(editor); err == nil {
		return exec.Command(path, filePath)
	}
	if strings.ContainsAny(editor, " \t\"'") {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/C", editor+" "+quoteWindowsArg(filePath))
		}
		return exec.Command("sh", "-c", editor+" \"$1\"", "atm-editor", filePath)
	}
	return exec.Command(editor, filePath)
}

func quoteWindowsArg(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func stripEditorComments(content string) string {
	lines := document.SplitLines(content)
	var b strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}
