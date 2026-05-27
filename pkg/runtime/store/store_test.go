package store

import (
	"errors"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBlockLeaseRejectsUserEdits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	body := "/task\nfirst prompt\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lease := NewBlockLease(0, body)
	if err := os.WriteFile(file, []byte("/task\nedited prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := MarkDone(file, lease, compiler.DoneInfo{Start: time.Now(), End: time.Now(), Runs: 1})
	if !errors.Is(err, ErrObsolete) {
		t.Fatalf("expected obsolete lease, got %v", err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "[done") {
		t.Fatalf("did not expect done marker after conflict: %s", updated)
	}
}

func TestLockUsesATMDirectoryLockFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, err := Lock(file)
	if err != nil {
		t.Fatal(err)
	}
	if lock.path != filepath.Join(dir, ".atm", "lock") {
		t.Fatalf("expected .atm lock path, got %s", lock.path)
	}
	if _, err := os.Stat(lock.path); err != nil {
		t.Fatalf("expected lock file: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Lock(file)
	if err != nil {
		t.Fatalf("expected released lock to be available: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRunningIgnoresExistingRunningMarkerInLeaseHash(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	body := "/task\nprompt\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lease := NewBlockLease(0, body)
	updated, err := SaveRunning(file, lease, compiler.RunningInfo{Active: true, Start: time.Now(), StepRuns: 1, TotalRuns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRunning(file, updated, compiler.RunningInfo{Active: true, Start: time.Now(), StepRuns: 2, TotalRuns: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedATMBlockReplacementPreservesUserQuote(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	body := "/task\nprompt\n> user quote\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lease := NewBlockLease(0, body)
	updated, err := SaveRunning(file, lease, compiler.RunningInfo{Active: true, Start: time.Now(), StepRuns: 1, TotalRuns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRunning(file, updated, compiler.RunningInfo{Active: true, Start: time.Now(), StepRuns: 2, TotalRuns: 2}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "> [!ATM]") != 1 {
		t.Fatalf("expected exactly one generated ATM block:\n%s", content)
	}
	if !strings.Contains(string(content), "> id: prompt-") || !strings.Contains(string(content), "> source: sha256:") {
		t.Fatalf("expected stable report identity:\n%s", content)
	}
	if !strings.Contains(string(content), "> user quote") {
		t.Fatalf("expected user quote preserved:\n%s", content)
	}
}

func TestGeneratedATMReportEnvelopeSurvivesLaterWriteback(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	first := "/task\nfirst prompt\n"
	second := "/task\nsecond prompt\n"
	if err := os.WriteFile(file, []byte(first+"\n"+second), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MarkDone(file, NewBlockLease(0, first), compiler.DoneInfo{Start: time.Now(), End: time.Now(), Runs: 1}); err != nil {
		t.Fatal(err)
	}
	blocks, err := ReadBlocks(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected two blocks, got %#v", blocks)
	}
	if err := MarkDone(file, NewBlockLease(1, blocks[1].Body), compiler.DoneInfo{Start: time.Now(), End: time.Now(), Runs: 1}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "<!-- atm:report ") != 2 || strings.Count(string(content), "<!-- /atm:report -->") != 2 {
		t.Fatalf("expected both report envelopes preserved:\n%s", content)
	}
}

func TestMarkdownTaskWritebackPreservesOrdinarySections(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.md")
	content := "# Notes\n\nordinary docs\n\n## Verify\n\n/task\nRun tests.\n\n## After\n\nkeep this\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	blocks, err := ReadBlocks(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected one runnable block, got %#v", blocks)
	}
	lease := NewBlockLease(0, blocks[0].Body)
	if err := MarkDone(file, lease, compiler.DoneInfo{Start: time.Now(), End: time.Now(), Runs: 1}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, "# Notes\n\nordinary docs") || !strings.Contains(text, "## Verify") || !strings.Contains(text, "## After\n\nkeep this") {
		t.Fatalf("ordinary markdown was not preserved:\n%s", text)
	}
	if !strings.Contains(text, "> source: sha256:") || !strings.Contains(text, "\n\n## After") {
		t.Fatalf("expected result block separated from following markdown heading:\n%s", text)
	}
	if !strings.Contains(text, "> [!ATM]") {
		t.Fatalf("expected result block:\n%s", text)
	}
}
