package cli

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/runtime/store"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExitCodeClassifiesErrors(t *testing.T) {
	if got := ExitCode(nil); got != ExitOK {
		t.Fatalf("nil error exit code = %d", got)
	}
	if got := ExitCode(fmt.Errorf("agent failed")); got != ExitExecutionFailure {
		t.Fatalf("generic error exit code = %d", got)
	}
	diagnosticErr := compiler.DiagnosticError{Diagnostics: []compiler.Diagnostic{{Severity: "error", Message: "bad task"}}}
	if got := ExitCode(fmt.Errorf("check failed: %w", diagnosticErr)); got != ExitValidationFailure {
		t.Fatalf("diagnostic error exit code = %d", got)
	}
	stateErr := StateInconsistentError{Err: fmt.Errorf("state/report mismatch")}
	if got := ExitCode(fmt.Errorf("audit failed: %w", stateErr)); got != ExitStateInconsistent {
		t.Fatalf("state error exit code = %d", got)
	}
}

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
	if err := os.WriteFile(file, []byte("/task\ndefault prompt\n"), 0o644); err != nil {
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

func TestExecSubcommandRunsPendingSnapshot(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/task\nsnapshot prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >> \"$CODEX_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if err := Run([]string{"exec", "-codex", codex, "-output", filepath.Join(dir, "out"), file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(log)) != "snapshot prompt" {
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
	if err := os.WriteFile("todo.md", []byte("/task\nmarkdown prompt\n"), 0o644); err != nil {
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
	if err := os.WriteFile(first, []byte("/task\nfirst prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("/task\nsecond prompt\n"), 0o644); err != nil {
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
	if err := os.WriteFile(first, []byte("/task\nalready done [done]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("/task\nalready done [done]\n"), 0o644); err != nil {
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
	if err := os.WriteFile(file, []byte("/for 2 /go\nreview {{n}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "flow: For(n in [0 1]) -> Go -> Execute") {
		t.Fatalf("unexpected plan output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "decision: dispatch-background - /go dispatches background work and continues; matching /wait controls join") {
		t.Fatalf("expected background decision summary, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "loops:") ||
		!strings.Contains(out.String(), "- task 1 block 1: For(n in [0 1]) (mode=static, count=2)") {
		t.Fatalf("expected loop summary, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "async:") ||
		!strings.Contains(out.String(), "- background task 1 block 1 via default: For(n in [0 1])") ||
		!strings.Contains(out.String(), "- unjoined task 1 block 1 via default: For(n in [0 1])") {
		t.Fatalf("expected async fan-out summary, got:\n%s", out.String())
	}
}

func TestPlanSubcommandPrintsDynamicLoopSummary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := "/for shard in range(1, 4) until(exist(\"done\"))\nreview {{shard}}\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"loops:",
		`For(shard in expr("range(1, 4)")) until expr("exist(\"done\")")`,
		`mode=dynamic`,
		`source=expr:range(1, 4)`,
		`until=expr:exist("done")`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("expected dynamic loop summary %q, got:\n%s", want, text.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"loops": [`, `"mode": "dynamic"`, `"source": "range(1, 4)"`, `"sourceKind": "expr"`, `"until": "exist(\"done\")"`, `"untilKind": "expr"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON loop summary %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanSubcommandPrintsAsyncJoins(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := "/pool reviewer 2\n\n/for area in [api docs] /go reviewer\nreview {{area}}\n\n/wait reviewer\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- background task 1 block 2 via reviewer: For(area in [api docs])",
		"- wait task 2 block 3 joins task 1 block 2 via reviewer: For(area in [api docs])",
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("expected plan to contain %q, got:\n%s", want, text.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"async": {`, `"background": [`, `"joins": [`, `"waitTask": 2`, `"pool": "reviewer"`, `"fanout": "For(area in [api docs])"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON plan to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanSubcommandPrintsVariableRefs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := strings.Join([]string{
		"/let root global",
		"",
		"/let local task",
		"/let lazy /bash printf hi",
		"/for 2",
		"Use {{root}} {{local}} {{lazy}} {{n}} {{missing}}.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`root(global-let, block 1, value="global")`,
		`local(task-let, block 2, value="task")`,
		`lazy(task-lazy-bash, block 2)`,
		`n(loop, block 2)`,
		`missing(unresolved)`,
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("expected plan variables to contain %q, got:\n%s", want, text.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"variables": [`, `"source": "global-let"`, `"source": "task-let"`, `"source": "task-lazy-bash"`, `"source": "loop"`, `"source": "unresolved"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON plan variables to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanSubcommandPrintsRuntimeSummary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := strings.Join([]string{
		"/def inspect",
		"/return ok",
		"",
		"/resume",
		"/args --model fast",
		"/cd --must-exist backend",
		"/bash echo prepare",
		"/let lazy /call inspect",
		"Review {{lazy}}.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"runtime: resume; args=--model fast; cd=backend (must-exist); bash=1; lazy=lazy:call(inspect)",
		"flow: Cd(backend, must-exist) -> Bash -> LazyCall(inspect -> lazy) -> Execute [resume, args=--model fast]",
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("expected plan runtime to contain %q, got:\n%s", want, text.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"runtime": {`, `"resume": true`, `"args": [`, `"workdirs": [`, `"mustExist": true`, `"bash": [`, `"lazyProviders": [`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON plan runtime to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanSubcommandPrintsContextSummary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.md")
	content := strings.Join([]string{
		"# Release",
		"",
		"Global release context.",
		"",
		"## Backend",
		"",
		"API migration notes.",
		"",
		"/task",
		"Review backend.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "context: ") || !strings.Contains(text.String(), "# Release") {
		t.Fatalf("expected plan context summary, got:\n%s", text.String())
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"context": {`, `"lines":`, `"chars":`, `"preview": "# Release"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON plan context to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanSubcommandAcceptsPositionalFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.md")
	if err := os.WriteFile(file, []byte("/task\nreview docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "atm plan dry-run: "+file) {
		t.Fatalf("unexpected plan output:\n%s", out.String())
	}
}

func TestPlanSubcommandDiscoversDefaultTodoMarkdown(t *testing.T) {
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
	if err := os.WriteFile("todo.md", []byte("/task\nreview docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", "-json"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"source": "todo.md"`) {
		t.Fatalf("expected todo.md plan output, got:\n%s", out.String())
	}
}

func TestPlanSubcommandAcceptsFlagsAfterPositionalFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	htmlFile := filepath.Join(dir, "plan.html")
	if err := os.WriteFile(file, []byte("/task\nreview docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"plan", file, "-html", htmlFile}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(htmlFile); err != nil {
		t.Fatalf("expected html plan file: %v", err)
	}
	if !strings.Contains(out.String(), "atm plan HTML:") {
		t.Fatalf("expected html path output, got:\n%s", out.String())
	}

	out.Reset()
	if err := Run([]string{"plan", file, "-json"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"source": "`+file+`"`) {
		t.Fatalf("expected JSON plan output, got:\n%s", out.String())
	}
}

func TestPlanPreviewExecutesLazyBashProviders(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	marker := filepath.Join(dir, "preview.marker")
	content := "/let name /bash printf previewed; printf touched > preview.marker\nUse {{name}}.\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var dry strings.Builder
	if err := Run([]string{"plan", "-file", file}, &dry, &dry); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("regular plan should not execute lazy bash provider, stat err=%v", err)
	}
	if strings.Contains(dry.String(), "preview providers:") {
		t.Fatalf("regular plan should not include preview provider output:\n%s", dry.String())
	}

	var preview strings.Builder
	if err := Run([]string{"plan", "--preview", "-file", file}, &preview, &preview); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("preview should execute lazy bash provider: %v", err)
	}
	for _, want := range []string{
		"preview mode: lazy providers may be executed",
		"preview providers:",
		`provider "name": executed value="previewed"`,
	} {
		if !strings.Contains(preview.String(), want) {
			t.Fatalf("expected preview output to contain %q, got:\n%s", want, preview.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "--preview", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"preview": [`, `"name": "name"`, `"executed": true`, `"value": "previewed"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON preview to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestPlanPreviewExecutesStaticLazyCallProviders(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	content := strings.Join([]string{
		"/def city",
		"",
		"/return Paris",
		"",
		"/let city /call city",
		"Weather for {{city}}.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var preview strings.Builder
	if err := Run([]string{"plan", "--preview", "-file", file}, &preview, &preview); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"preview providers:",
		`call provider "city": executed value="Paris"`,
	} {
		if !strings.Contains(preview.String(), want) {
			t.Fatalf("expected preview output to contain %q, got:\n%s", want, preview.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"plan", "--preview", "-json", "-file", file}, &jsonOut, &jsonOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind": "call"`, `"name": "city"`, `"executed": true`, `"value": "Paris"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("expected JSON preview to contain %q, got:\n%s", want, jsonOut.String())
		}
	}
}

func TestCheckSubcommandValidatesWithoutRunning(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	content := strings.Join([]string{
		"## Setup",
		"",
		"/let reviewer fast",
		"",
		"/def inspect",
		"",
		"/return ok",
		"",
		"## Main",
		"",
		"/for area in [api docs]",
		"Review {{area}} with {{reviewer}}.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"check", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"atm check ok:", "blocks: 2", "tasks: 1", "definitions: 1", "resources: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected check output to contain %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"check", "-json", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"files": [`, `"tasks": 1`, `"definitions": 1`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected check JSON to contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestCheckSubcommandRejectsInvalidDSL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/for until tests pass\nretry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"check", "-file", file}, &out, &out); err == nil {
		t.Fatal("expected invalid DSL to fail")
	}

	out.Reset()
	if err := Run([]string{"check", "-json", "-file", file}, &out, &out); err == nil {
		t.Fatal("expected invalid DSL JSON check to fail")
	}
	for _, want := range []string{`"diagnostics": [`, `"severity": "error"`, `"source": "` + file + `"`, `"block": 1`, `"line": 1`, `"column": 1`, "requires a parenthesized expression"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected JSON diagnostics to contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestCheckSubcommandReportsWarningsWithoutFailing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/go\nReview docs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"check", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warning:") || !strings.Contains(out.String(), "without a later /wait") {
		t.Fatalf("expected text warning, got:\n%s", out.String())
	}

	out.Reset()
	if err := Run([]string{"check", "-json", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"severity": "warning"`, "without a later /wait"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected JSON warning to contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestCheckAndPlanReportLazyProviderWarnings(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := strings.Join([]string{
		"/def maker",
		"/return /bash",
		"```",
		"printf made",
		"```",
		"",
		"/let changed /bash git diff --name-only",
		"/let made /call maker",
		"Use {{changed}} and {{made}}.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"check", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"warning:", "lazy provider with possible side effects", "lazy definition provider", "/return /bash"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected check warning %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"plan", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"LazyBash(changed)", "LazyCall(maker -> made)", "lazy provider with possible side effects"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected plan output %q, got:\n%s", want, out.String())
		}
	}
}

func TestCheckSubcommandReportsATMArtifactWarnings(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	content := strings.Join([]string{
		"/task",
		"Review docs.",
		"<!-- atm:report v=2 id=review-docs-abc123 source=sha256:abc rendered=sha256:def report=.atm/reports/review-docs-abc123.md status=done -->",
		"> [!ATM]",
		"> status: done",
		"> started: 2026-05-18 10:00",
		"> finished: 2026-05-18 10:01",
		"> duration: 1m",
		"> runs: 1x",
		"> id: review-docs-abc123",
		"> source: sha256:abc",
		"> rendered: sha256:def",
		"> report: .atm/reports/review-docs-abc123.md",
		"<!-- /atm:report -->",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".atm", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{"version":2,"document":"todo.txt","tasks":{"review-docs-abc123":{"status":"running","sourcePromptHash":"sha256:old-source","renderedPromptHash":"sha256:old-rendered","report":".atm/reports/review-docs-abc123.md"},"old-task":{"status":"done","report":".atm/reports/old-task.md"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".atm", "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".atm", "reports", "lonely.md"), []byte("# orphan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"check", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ATM report id "review-docs-abc123" references missing detail report .atm/reports/review-docs-abc123.md`,
		`ATM report id "review-docs-abc123" status mismatch: document=done state=running`,
		`ATM report id "review-docs-abc123" source hash mismatch: document=sha256:abc state=sha256:old-source`,
		`ATM report id "review-docs-abc123" rendered prompt hash mismatch: document=sha256:def state=sha256:old-rendered`,
		`.atm/state.json contains task "old-task" with no matching report in the todo document`,
		`orphan detail report .atm/reports/lonely.md has no matching report in the todo document or state`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected artifact warning %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"check", "-json", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"severity": "warning"`, `"message": "ATM report id \"review-docs-abc123\" status mismatch: document=done state=running"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected JSON artifact warning %q, got:\n%s", want, out.String())
		}
	}
}

func TestReportSubcommandSummarizesATMState(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	content := strings.Join([]string{
		"/task",
		"Done task.",
		"<!-- atm:report v=2 id=done-task-abc source=sha256:a rendered=sha256:ar report=.atm/reports/done-task-abc.md status=done -->",
		"> [!ATM]",
		"> status: done",
		"> id: done-task-abc",
		"> source: sha256:a",
		"> rendered: sha256:ar",
		"> report: .atm/reports/done-task-abc.md",
		"<!-- /atm:report -->",
		"",
		"/task",
		"Failed task.",
		"<!-- atm:report v=2 id=failed-task-def source=sha256:b rendered=sha256:br report=.atm/reports/failed-task-def.md status=failed -->",
		"> [!ATM]",
		"> status: failed",
		"> id: failed-task-def",
		"> source: sha256:b",
		"> rendered: sha256:br",
		"> report: .atm/reports/failed-task-def.md",
		"<!-- /atm:report -->",
		"",
		"/task",
		"Draft task.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".atm", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".atm", "reports", "lonely.md"), []byte("# lonely\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := `{"version":2,"document":"todo.txt","tasks":{"failed-task-def":{"status":"failed","sourcePromptHash":"sha256:b","renderedPromptHash":"sha256:br","report":".atm/reports/failed-task-def.md","logs":["out/task-001.log"]},"orphan-task":{"status":"done","sourcePromptHash":"sha256:orphan-source","renderedPromptHash":"sha256:orphan-rendered","report":".atm/reports/orphan-task.md","orphan":true,"logs":["out/task-002.log"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".atm", "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"report", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"atm report:", "done: 1", "failed: 1", "draft: 1", "failures:", "failed-task-def", "orphan reports:", "orphan-task", "lonely", "recent logs:", "out/task-002.log"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected report output to contain %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"report", "-json", "-file", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"counts": {`, `"failed": 1`, `"orphans": [`, `"id": "orphan-task"`, `"source": "sha256:orphan-source"`, `"rendered": "sha256:orphan-rendered"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected JSON report to contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestCleanSubcommandRemovesDocumentStateByDefault(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	content := strings.Join([]string{
		"/task",
		"Review docs.",
		"<!-- atm:report v=2 id=review-docs-abc123 source=sha256:abc report=.atm/reports/review-docs-abc123.md status=done -->",
		"> [!ATM]",
		"> status: done",
		"> id: review-docs-abc123",
		"> report: .atm/reports/review-docs-abc123.md",
		"<!-- /atm:report -->",
		"",
		"> user quote stays",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".atm", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".atm", "state.json"), []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"clean", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "atm:report") || strings.Contains(string(updated), "[!ATM]") {
		t.Fatalf("expected generated report block removed:\n%s", updated)
	}
	if !strings.Contains(string(updated), "> user quote stays") {
		t.Fatalf("expected user quote preserved:\n%s", updated)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atm", "state.json")); err != nil {
		t.Fatalf("default clean should keep state.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atm", "reports")); err != nil {
		t.Fatalf("default clean should keep reports dir: %v", err)
	}
	if !strings.Contains(out.String(), "document state blocks: 1") {
		t.Fatalf("unexpected clean output:\n%s", out.String())
	}
}

func TestCleanSubcommandRemovesSelectedArtifactsWithFileBeforeFlags(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("Review docs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, subdir := range []string{"reports", "logs"} {
		path := filepath.Join(dir, ".atm", subdir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "item.md"), []byte("artifact\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".atm", "state.json"), []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"clean", file, "--reports", "--state", "--logs"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dir, ".atm", "reports"),
		filepath.Join(dir, ".atm", "logs"),
		filepath.Join(dir, ".atm", "state.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat err=%v", path, err)
		}
	}
	for _, want := range []string{"reports removed: 1", "state files removed: 1", "log dirs removed: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected clean output to contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestRepairIDsSubcommandRewritesDuplicateReportIDs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
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
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var checkOut strings.Builder
	if err := Run([]string{"check", "-file", file}, &checkOut, &checkOut); err == nil {
		t.Fatal("expected duplicate id check to fail before repair")
	}

	var out strings.Builder
	if err := Run([]string{"repair-ids", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "repaired 1 duplicate ATM report id(s)") || !strings.Contains(out.String(), "block 2: dup -> two-") {
		t.Fatalf("unexpected repair output:\n%s", out.String())
	}
	updatedBytes, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(updatedBytes)
	if strings.Count(updated, "id=dup") != 1 || strings.Count(updated, "> id: dup") != 1 {
		t.Fatalf("expected only first duplicate id to remain:\n%s", updated)
	}
	if !strings.Contains(updated, "id=two-") || !strings.Contains(updated, "> id: two-") || !strings.Contains(updated, "report=.atm/reports/two-") {
		t.Fatalf("expected duplicate block to receive a new report identity:\n%s", updated)
	}

	checkOut.Reset()
	if err := Run([]string{"check", "-file", file}, &checkOut, &checkOut); err != nil {
		t.Fatalf("expected duplicate id check to pass after repair, got %v:\n%s", err, checkOut.String())
	}
}

func TestPlanSubcommandPrintsTaskToolingAndControls(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.md")
	content := strings.Join([]string{
		"/def inspect",
		"",
		"/return ok",
		"",
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
		"### //gate",
		"",
		"/if (exist(outputDir(\"gate.json\")))",
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
	if err := os.WriteFile(filepath.Join(filepath.Dir(file), "lib.todo.md"), []byte("/def lib\n/return ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var text strings.Builder
	if err := Run([]string{"plan", "-file", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"document: <untitled>",
		"- line 5: ## //setup",
		"  - line 22: ### //gate",
		"import block 1: lib.todo.md",
		"db block 2: decisions scope=global persist=run access=write",
		"line: ",
		"scope: ## //setup > ### //gate",
		`expr("exist(outputDir(\"gate.json\"))")`,
		"conditions:",
		"if expr(exist(outputDir(\"gate.json\"))): then execute when true; skipped when false; else skipped when true; execute when false",
		"resources: db=decisions(write,global/run)",
		"flow: Execute [resume, args=--yolo]",
		"db: use scratch access=append; access decisions read",
		"skill: use reviewer",
		"mcp: use helper; def use inspect",
		"resources: db=decisions(read,global/run), scratch(append,local/run); skill=reviewer; mcp=helper; def-mcp=inspect",
		"If(expr:exist(outputDir(\"gate.json\"))) {then: Execute; else: Execute}",
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
	for _, want := range []string{`"document": {`, `"sections": [`, `"title": "//setup"`, `"controls"`, `"conditions": [`, `"then": "execute when true; skipped when false"`, `"else": "skipped when true; execute when false"`, `"line":`, `"scope": [`, `"### //gate"`, `"decision": {`, `"action": "conditional-execute"`, `"skips": [`, `"conditionKind": "expr"`, `"flow": {`, `"children": [`, `"db": {`, `"resources": {`, `"dbs": [`, `"defMCPs": [`, `"defUse": [`} {
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
	for _, want := range []string{"ATM Plan", "For(area in [api docs])", "plan-data", "workspace", "splitter", "Execution Map", "Databases", "access board read", "Unjoined background work"} {
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
		"/def maker",
		"",
		"/return made",
		"",
		"/pool review 2",
		"",
		"## //setup",
		"",
		"## //main",
		"",
		"/task",
		"Review backend.",
		"",
		"### API",
		"",
		"API context.",
		"",
		"/task",
		"Fix API.",
		"",
		"/if (true)",
		"then branch",
		"",
		"/else",
		"else branch",
		"",
		"/for area in [api docs] /go review",
		"Review {{area}}.",
		"",
		"/wait review",
		"Coordinate reviewer pool.",
		"",
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
		"WaitAgent",
		"parentBlock",
		"childBlocks",
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
	if err := os.WriteFile(file, []byte("/task\nprompt [done]\n"), 0o644); err != nil {
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
	if string(updated) != "/task\nprompt\n[done]\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestAppendFormatsAndTargetsTodoFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/task\nexisting\n"), 0o644); err != nil {
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
	if string(updated) != "/task\nexisting\n\n/go\nnew\n[done]\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestAppendTargetsActiveTodoFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/task\nrunning\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.PrepareWorkspace(file, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Restore()

	var out strings.Builder
	if err := RunAppend(file, "/task\nnew work\n", &out); err != nil {
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
	if err := os.WriteFile(file, []byte("/task\ndone [done]\n\n/task\nrunning [running|20260508-14:32|1x]\n"), 0o644); err != nil {
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
	if string(updated) != "/task\ndone\n\n/task\nrunning\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestUntagSubcommandRemovesATMQuoteBlocks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	content := "/task\ndone\n> [!ATM]\n> status: done\n> started: 2026-05-18 10:00\n> finished: 2026-05-18 10:01\n> duration: 1m\n> runs: 1x\n"
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
	if string(updated) != "/task\ndone\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}
