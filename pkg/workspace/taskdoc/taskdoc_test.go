package taskdoc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chinaykc/atm/pkg/lang/compiler"
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
		"/task\n\nfirst\n[done]",
		"/go\n\nsecond\n[done]",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("formatted content missing %q:\n%s", want, updated)
		}
	}
}

func TestFormatContentNormalizesComposedHeadersWithoutChangingTaskIR(t *testing.T) {
	input := "/task branch /fork base /bash echo setup /for 2 /go workers\nReview the change.\n"
	before, err := compiler.ParseTask(0, input, nil, compiler.CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}

	formatted, count := FormatContent(input)
	if count != 1 {
		t.Fatalf("block count = %d, want 1", count)
	}
	want := "/task branch\n\n/fork base\n\n/bash echo setup\n\n/for 2\n\n/go workers\n\nReview the change.\n"
	if formatted != want {
		t.Fatalf("formatted content = %q, want %q", formatted, want)
	}
	if again, _ := FormatContent(formatted); again != formatted {
		t.Fatalf("format is not idempotent:\n%s", again)
	}

	after, err := compiler.ParseTask(0, formatted, nil, compiler.CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("formatted task IR changed\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestFormatContentPreservesNestedProvidersAndFencedPayloads(t *testing.T) {
	input := "/let value /bash echo \"/task\"\n/output result\n```schema\nreason:string:why\n```\nReport.\n"
	formatted, _ := FormatContent(input)
	if !strings.Contains(formatted, `/let value /bash echo "/task"`) {
		t.Fatalf("nested provider was split: %q", formatted)
	}
	if !strings.Contains(formatted, "/output result\n```schema\nreason:string:why\n```\n") {
		t.Fatalf("fenced payload was rewritten: %q", formatted)
	}
	if !strings.Contains(formatted, `/let value /bash echo "/task"`+"\n\n/output result") {
		t.Fatalf("expected Markdown spacing between header commands: %q", formatted)
	}
	if !strings.Contains(formatted, "```\n\nReport.") {
		t.Fatalf("expected Markdown spacing after fenced header payload: %q", formatted)
	}
}

func TestFormatContentAddsMarkdownSpacingWithoutChangingTaskIR(t *testing.T) {
	cases := []string{
		"/pool reviewer 2\n\n/for area in [api docs] /go reviewer\nReview {{area}}.\n/wait reviewer\nSummarize results.\n",
		"/output result\n```schema\nok:boolean:true when done\n```\nReturn JSON.\n",
		"/bash <<'SH'\nprintf ok\nSH\nRun after setup.\n",
	}
	for _, input := range cases {
		beforePlan, err := compiler.CompileProgram("todo.md", input)
		if err != nil {
			t.Fatalf("compile before format: %v\n%s", err, input)
		}
		formatted, count := FormatContent(input)
		if count == 0 {
			t.Fatalf("expected formatted blocks for:\n%s", input)
		}
		afterPlan, err := compiler.CompileProgram("todo.md", formatted)
		if err != nil {
			t.Fatalf("compile after format: %v\n%s", err, formatted)
		}
		beforeTasks := normalizeTasksForSemanticCompare(beforePlan.Tasks)
		afterTasks := normalizeTasksForSemanticCompare(afterPlan.Tasks)
		if !reflect.DeepEqual(beforeTasks, afterTasks) {
			t.Fatalf("formatted task IR changed\ninput:\n%s\nformatted:\n%s\nbefore: %#v\nafter:  %#v", input, formatted, beforeTasks, afterTasks)
		}
		if again, _ := FormatContent(formatted); again != formatted {
			t.Fatalf("format is not idempotent:\n%s", again)
		}
	}
}

func TestFormatContentUsesTwoBlankLinesBetweenTasks(t *testing.T) {
	formatted, count := FormatContent("/task\none\n/task\ntwo\n")
	if count != 2 {
		t.Fatalf("block count = %d, want 2", count)
	}
	if formatted != "/task\n\none\n\n\n/task\n\ntwo\n" {
		t.Fatalf("unexpected task spacing:\n%q", formatted)
	}
}

func normalizeTasksForSemanticCompare(tasks []compiler.Task) []compiler.Task {
	out := make([]compiler.Task, len(tasks))
	copy(out, tasks)
	for i := range out {
		out[i].Line = 0
	}
	return out
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
	if formatted != "/go\n\nnew\n[done]\n" {
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
	if string(updated) != "/task\nexisting\n\n/go\n\nnew\n[done]\n" {
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
