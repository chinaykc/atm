package cli

import (
	"atm/pkg/store"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var doneBlockForTest = regexp.MustCompile(`(?s)\n> \[!ATM\]\n> status: done\n> started: [^\n]+\n> finished: [^\n]+\n> duration: [^\n]+\n> runs: [0-9]+x`)

func TestRunSubcommandExecutesPendingTasks(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/args --yolo\nhello {{name}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'args:%s\\n' \"$*\" >> \"$CODEX_LOG\"\ncat >> \"$CODEX_LOG\"\nprintf '\\n--\\n' >> \"$CODEX_LOG\"\n"
	if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if err := Run([]string{"run", "-file", file, "-codex", codex, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeDoneMarkersForTest(string(updated)) != "/args --yolo\nhello {{name}}\n[done]\n" {
		t.Fatalf("unexpected todo content:\n%s", updated)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "args:exec --json --yolo -") || !strings.Contains(string(log), "hello {{name}}") {
		t.Fatalf("unexpected codex log:\n%s", log)
	}
}

func TestDefaultRunStillExecutesPendingTasks(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("default prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >> \"$CODEX_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if err := Run([]string{"-file", file, "-codex", codex, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(log)) != "default prompt" {
		t.Fatalf("unexpected codex log: %q", log)
	}
}

func TestDefaultRunDiscoversTodoMarkdown(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile("todo.md", []byte("markdown prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >> \"$CODEX_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if err := Run([]string{"-codex", codex, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(log)) != "markdown prompt" {
		t.Fatalf("unexpected codex log: %q", log)
	}
}

func TestRunQueuesMultipleInputFiles(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.todo.txt")
	second := filepath.Join(dir, "second.todo.md")
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	outDir := filepath.Join(dir, "out")
	if err := os.WriteFile(first, []byte("first prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >> \"$CODEX_LOG\"\nprintf '\\n--\\n' >> \"$CODEX_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if err := Run([]string{"run", "-codex", codex, "-output", outDir, first, second}, &out, &out); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "first prompt") || !strings.Contains(string(log), "second prompt") {
		t.Fatalf("unexpected codex log:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(outDir, "001-first-todo-txt", "result.md")); err != nil {
		t.Fatalf("expected first result document: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "002-second-todo-md", "result.md")); err != nil {
		t.Fatalf("expected second result document: %v", err)
	}
}

func TestRunQueuesRepeatedFileFlags(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.txt")
	second := filepath.Join(dir, "two.txt")
	if err := os.WriteFile(first, []byte("[done]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("[done]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"run", "-file", first, "-file", second, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "atm run file 1/2") || !strings.Contains(out.String(), "atm run file 2/2") {
		t.Fatalf("expected queued file progress, got:\n%s", out.String())
	}
}

func TestPlanSubcommandPrintsDryRun(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/for 2 /go\nreview {{N}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "flow: For(N in [1 2]) -> Go -> Execute") {
		t.Fatalf("unexpected plan output:\n%s", out.String())
	}
}

func TestPlanSubcommandPrintsTaskToolingAndControls(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.md")
	content := strings.Join([]string{
		"## //setup",
		"",
		"/import lib.todo.md",
		"",
		"/db new decisions scope:global persist:run access:write",
		"Decision blackboard.",
		"",
		"/db new scratch scope:local persist:run access:append",
		"Task scratch.",
		"",
		"/skill new reviewer from skills/reviewer",
		"",
		"/mcp new helper",
		"```json",
		`{"command":"helper"}`,
		"```",
		"",
		"## /def inspect",
		"",
		"/return ok",
		"",
		"## //gate",
		"",
		"/if (existsOutput(\"gate.json\"))",
		"Check gate.",
		"",
		"/else",
		"Stop.",
		"",
		"",
		"/resume",
		"/args --yolo",
		"/db use scratch access:append",
		"/db access decisions read",
		"/skill use reviewer",
		"/mcp use helper",
		"/mcp def use inspect",
		"Review.",
		"",
		"/db ignore",
		"Clean room.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(file), "lib.todo.md"), []byte("## /def lib\n\n/return ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"import block 1: lib.todo.md",
		"db block 2: decisions scope=global persist=run access=write",
		`cel("existsOutput(\"gate.json\")")`,
		"flow: Execute [resume, args=--yolo]",
		"db: use scratch access=append; access decisions read",
		"skill: use reviewer",
		"mcp: use helper; def use inspect",
		"else block",
		"db: ignore all",
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("expected plan to contain %q, got:\n%s", want, text.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"controls"`, `"conditionKind": "cel"`, `"db": {`, `"defUse": [`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON plan to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanSubcommandWritesHTMLFlowchart(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	htmlFile := filepath.Join(dir, "plan.html")
	if err := os.WriteFile(file, []byte("/pool review 2\n\n/db new board scope:global persist:run access:append\nReview board.\n\n/db access board read\n/for area in [api docs] /go review\nreview {{area}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", "-file", file, "-html", htmlFile}, &out, &out); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(htmlFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{"ATM Plan", "For(area in [api docs])", "plan-data", "workspace", "splitter", "Execution Map", "Databases", "access board read"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected HTML plan to contain %q, got:\n%s", want, text)
		}
	}
	if !strings.Contains(out.String(), "atm plan HTML:") {
		t.Fatalf("expected html path output, got:\n%s", out.String())
	}
}

func TestPlanSubcommandHTMLShowsStructureAndDocumentContent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	htmlFile := filepath.Join(dir, "plan.html")
	content := strings.Join([]string{
		"# Release Runbook",
		"",
		"Keep this prose visible in the plan HTML.",
		"",
		"## /def maker",
		"",
		"/return made",
		"",
		"## //main",
		"",
		"/if (true)",
		"",
		"then branch",
		"",
		"/else",
		"else branch",
		"",
		"/for area in [api docs] /go review",
		"Review {{area}}.",
		"",
		"/wait review",
		"",
		"/call maker",
		"Use definition output.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", "-file", file, "-html", htmlFile}, &out, &out); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(htmlFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, want := range []string{
		"branch-box",
		"branch-grid",
		"true",
		"false",
		"joins",
		"Definitions",
		"def-maker",
		"Call(maker)",
		"Release Runbook",
		"Keep this prose visible in the plan HTML.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected HTML plan to contain %q, got:\n%s", want, text)
		}
	}
}

func TestFormatSubcommandMovesMarkersToOwnLine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("prompt [done]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"format", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "prompt\n[done]\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestAppendFormatsAndTargetsTodoFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := RunAppend(file, "/go\nnew [done]\n", &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "existing\n\n/go\nnew\n[done]\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestAppendTargetsActiveTodoFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("running\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.PrepareWorkspace(file, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Restore()

	var out strings.Builder
	if err := RunAppend(file, "new work\n", &out); err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(workspace.Active)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(active), "new work") {
		t.Fatalf("expected append to target active file, got %q", active)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("expected original file to remain moved while active, stat err=%v", err)
	}
}

func TestUntagSubcommandRemovesMarkers(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("done [done]\n\nrunning [running|20260508-14:32|1x]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"untag", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "done\n\nrunning\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestUntagSubcommandRemovesATMQuoteBlocks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := "done\n> [!ATM]\n> status: done\n> started: 2026-05-18 10:00\n> finished: 2026-05-18 10:01\n> duration: 1m\n> runs: 1x\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"untag", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "done\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func normalizeDoneMarkersForTest(content string) string {
	return doneBlockForTest.ReplaceAllString(content, "\n[done]")
}

func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell fake executable")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("test requires sh")
	}
}
