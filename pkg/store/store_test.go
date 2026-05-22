package store

import (
	"atm/pkg/dsl"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBlockLeaseRejectsUserEdits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("first prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lease := NewBlockLease(0, "first prompt\n")
	if err := os.WriteFile(file, []byte("edited prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := MarkDone(file, lease, dsl.DoneInfo{Start: time.Now(), End: time.Now(), Runs: 1})
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

func TestSaveRunningIgnoresExistingRunningMarkerInLeaseHash(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	body := "prompt\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lease := NewBlockLease(0, body)
	updated, err := SaveRunning(file, lease, dsl.RunningInfo{Active: true, Start: time.Now(), StepRuns: 1, TotalRuns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRunning(file, updated, dsl.RunningInfo{Active: true, Start: time.Now(), StepRuns: 2, TotalRuns: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedATMBlockReplacementPreservesUserQuote(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	body := "prompt\n> user quote\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lease := NewBlockLease(0, body)
	updated, err := SaveRunning(file, lease, dsl.RunningInfo{Active: true, Start: time.Now(), StepRuns: 1, TotalRuns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRunning(file, updated, dsl.RunningInfo{Active: true, Start: time.Now(), StepRuns: 2, TotalRuns: 2}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "> [!ATM]") != 1 {
		t.Fatalf("expected exactly one generated ATM block:\n%s", content)
	}
	if !strings.Contains(string(content), "> user quote") {
		t.Fatalf("expected user quote preserved:\n%s", content)
	}
}

func TestMarkdownTaskWritebackPreservesOrdinarySections(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.md")
	content := "# Notes\n\nordinary docs\n\n## /verify\n\nRun tests.\n\n## After\n\nkeep this\n"
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
	if err := MarkDone(file, lease, dsl.DoneInfo{Start: time.Now(), End: time.Now(), Runs: 1}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, "# Notes\n\nordinary docs") || !strings.Contains(text, "## /verify") || !strings.Contains(text, "## After\n\nkeep this") {
		t.Fatalf("ordinary markdown was not preserved:\n%s", text)
	}
	if !strings.Contains(text, "> runs: 1x\n\n## After") {
		t.Fatalf("expected result block separated from following markdown heading:\n%s", text)
	}
	if !strings.Contains(text, "> [!ATM]") {
		t.Fatalf("expected result block:\n%s", text)
	}
}
