package taskdoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatContentFormatsAllTaskBlocks(t *testing.T) {
	input := strings.Join([]string{
		"# Notes",
		"",
		"/task",
		"first [done]",
		"",
		"middle prose",
		"",
		"/go",
		"second [done]",
		"",
	}, "\n")

	updated, count := FormatContent(input)

	if count != 2 {
		t.Fatalf("block count = %d, want 2", count)
	}
	for _, want := range []string{
		"# Notes",
		"middle prose",
		"/task\nfirst\n[done]",
		"/go\nsecond\n[done]",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("formatted content missing %q:\n%s", want, updated)
		}
	}
}

func TestUntagContentCanRemoveDoneAndRunningMarkers(t *testing.T) {
	input := "/task\ndone [done]\n\n/task\nrunning [running|20260508-14:32|1x]\n"

	updated, result := UntagContent(input, UntagOptions{Done: true, Running: true})

	if result.DoneRemoved != 1 || result.RunningRemoved != 1 {
		t.Fatalf("result = %+v, want one done and one running marker removed", result)
	}
	if updated != "/task\ndone\n\n/task\nrunning\n" {
		t.Fatalf("updated content = %q", updated)
	}
}

func TestFormatAppendPromptFormatsBlocksAndRequiresTask(t *testing.T) {
	formatted, count, err := FormatAppendPrompt("/go\nnew [done]\n")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("block count = %d, want 1", count)
	}
	if formatted != "/go\nnew\n[done]\n" {
		t.Fatalf("formatted prompt = %q", formatted)
	}

	if _, _, err := FormatAppendPrompt("plain prose only\n"); err == nil {
		t.Fatal("expected empty prompt block error")
	}
}

func TestAppendFileAddsBlankLineBeforeFormattedPrompt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/task\nexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := AppendFile(file, "/go\nnew [done]\n")
	if err != nil {
		t.Fatal(err)
	}
	if result.BlockCount != 1 || result.Target != file {
		t.Fatalf("result = %+v, want one appended block targeting %s", result, file)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "/task\nexisting\n\n/go\nnew\n[done]\n" {
		t.Fatalf("updated content = %q", updated)
	}
}

func TestRepairIDsContentRewritesDuplicateReportIdentity(t *testing.T) {
	content := strings.Join([]string{
		"/task",
		"one",
		"<!-- atm:report v=2 id=dup source=sha256:a report=.atm/reports/dup.md status=done -->",
		"> [!ATM]",
		"> status: done",
		"> id: dup",
		"> source: sha256:a",
		"> report: .atm/reports/dup.md",
		"<!-- /atm:report -->",
		"",
		"/task",
		"two",
		"<!-- atm:report v=2 id=dup source=sha256:b report=.atm/reports/dup.md status=done -->",
		"> [!ATM]",
		"> status: done",
		"> id: dup",
		"> source: sha256:b",
		"> report: .atm/reports/dup.md",
		"<!-- /atm:report -->",
		"",
	}, "\n")

	updated, result := RepairIDsContent(content)

	if result.Repaired != 1 || len(result.Changes) != 1 {
		t.Fatalf("result = %+v, want one repaired id", result)
	}
	change := result.Changes[0]
	if change.Block != 2 || change.OldID != "dup" || !strings.HasPrefix(change.NewID, "two-") {
		t.Fatalf("change = %+v, want duplicate block rewritten from dup to two-*", change)
	}
	if change.NewReport != ".atm/reports/"+change.NewID+".md" {
		t.Fatalf("new report = %q, want path for %q", change.NewReport, change.NewID)
	}
	if strings.Count(updated, "id=dup") != 1 || strings.Count(updated, "> id: dup") != 1 {
		t.Fatalf("expected only first duplicate id to remain:\n%s", updated)
	}
	if !strings.Contains(updated, "id="+change.NewID) || !strings.Contains(updated, "> id: "+change.NewID) {
		t.Fatalf("expected updated report identity %q in:\n%s", change.NewID, updated)
	}
}
