package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinaykc/atm/pkg/lang/compiler"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "atm-cli-test-config-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	_ = os.Setenv("APPDATA", dir)
	_ = os.Setenv("HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func resultDocumentPathFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if path, ok := strings.CutPrefix(line, "result document: "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatalf("missing result document line in output:\n%s", output)
	return ""
}

func artifactsPathFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if path, ok := strings.CutPrefix(line, "artifacts: "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatalf("missing artifacts line in output:\n%s", output)
	return ""
}

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

func TestInternalAndUnavailableCommandsHiddenFromRootHelp(t *testing.T) {
	var out strings.Builder
	if err := Run([]string{"-h"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\n   mcp") {
		t.Fatalf("mcp command should be hidden from root help:\n%s", out.String())
	}
	if strings.Contains(out.String(), "\n   web") {
		t.Fatalf("web command should not be exposed in root help:\n%s", out.String())
	}
	if strings.Contains(out.String(), "\n   exec") {
		t.Fatalf("exec command should not be exposed in root help:\n%s", out.String())
	}
	if strings.Contains(out.String(), "\n   plan") {
		t.Fatalf("plan command should not be exposed in root help:\n%s", out.String())
	}
	if strings.Contains(out.String(), "\n   untag") {
		t.Fatalf("untag command should not be exposed in root help:\n%s", out.String())
	}
	if strings.Contains(out.String(), "\n   repair-ids") {
		t.Fatalf("repair-ids command should not be exposed in root help:\n%s", out.String())
	}
}

func TestVersionFlagPrintsBuildInfo(t *testing.T) {
	var out strings.Builder
	if err := Run([]string{"--version"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "atm version dev\n" {
		t.Fatalf("unexpected version output: %q", got)
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
	if err := Run([]string{"run", file, "-codex", codex, "-danger", "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "/args --yolo\nhello {{name}}\n" {
		t.Fatalf("source file should remain unchanged:\n%s", updated)
	}
	resultPath := resultDocumentPathFromOutput(t, out.String())
	result, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeDoneMarkersForTest(string(result)) != "/args --yolo\nhello {{name}}\n[done]\n" {
		t.Fatalf("unexpected result content:\n%s", result)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "args:exec --json --dangerously-bypass-approvals-and-sandbox --yolo -") || !strings.Contains(string(log), "hello {{name}}") {
		t.Fatalf("unexpected codex log:\n%s", log)
	}
}

func TestRunSubcommandRetriesRetryableCodexErrorByDefault(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	countFile := filepath.Join(dir, "count")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/task\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
count=0
if [ -f "$COUNT_FILE" ]; then
  count=$(cat "$COUNT_FILE")
fi
count=$((count + 1))
printf '%s' "$count" > "$COUNT_FILE"
cat >/dev/null
if [ "$count" -eq 1 ]; then
  printf '{"type":"thread.started","thread_id":"thread_1"}\n'
  printf '{"type":"turn.started"}\n'
  printf '{"type":"turn.failed","error":{"message":"exceeded retry limit, last status: 429 Too Many Requests"}}\n'
  exit 1
fi
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'
`
	if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COUNT_FILE", countFile)

	var out strings.Builder
	if err := Run([]string{"run", file, "-codex", codex, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	count, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "2" {
		t.Fatalf("codex attempts = %s, want 2", count)
	}
	if !strings.Contains(out.String(), "[atm] retry") {
		t.Fatalf("expected retry event in output:\n%s", out.String())
	}
}

func TestRunSubcommandRetriesRetryableClaudeAPIRetryByDefault(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	countFile := filepath.Join(dir, "count")
	claude := filepath.Join(dir, "claude")
	if err := os.WriteFile(file, []byte("/task\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
count=0
if [ -f "$COUNT_FILE" ]; then
  count=$(cat "$COUNT_FILE")
fi
count=$((count + 1))
printf '%s' "$count" > "$COUNT_FILE"
if [ "$count" -eq 1 ]; then
  printf '{"type":"system","subtype":"api_retry","error":"server_error","error_status":503,"attempt":1,"max_retries":5}\n'
  printf '{"type":"result","subtype":"error","is_error":true,"result":"server_error status 503"}\n'
  exit 1
fi
printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}\n'
`
	if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COUNT_FILE", countFile)

	var out strings.Builder
	err := Run([]string{"run", file, "-tool", "claude", "-claude", claude, "-output", filepath.Join(dir, "out")}, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	count, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "2" {
		t.Fatalf("claude attempts = %s, want 2", count)
	}
	if !strings.Contains(out.String(), "[atm] retry") || !strings.Contains(out.String(), "api_retry server_error status 503") {
		t.Fatalf("expected retry event in output:\n%s", out.String())
	}
}

func TestRunSubcommandRetriesCanBeDisabled(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	countFile := filepath.Join(dir, "count")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/task\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
count=0
if [ -f "$COUNT_FILE" ]; then
  count=$(cat "$COUNT_FILE")
fi
count=$((count + 1))
printf '%s' "$count" > "$COUNT_FILE"
cat >/dev/null
printf '{"type":"turn.failed","error":{"message":"exceeded retry limit, last status: 429 Too Many Requests"}}\n'
exit 1
`
	if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COUNT_FILE", countFile)

	var out strings.Builder
	err := Run([]string{"run", file, "-codex", codex, "-retries", "0", "-output", filepath.Join(dir, "out")}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "Too Many Requests") {
		t.Fatalf("expected retryable codex error, got %v\n%s", err, out.String())
	}
	count, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "1" {
		t.Fatalf("codex attempts = %s, want 1", count)
	}
}

func TestRunSubcommandAppendToSourceDuringRunReachesActiveWorkFile(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	logFile := filepath.Join(dir, "codex.log")
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/task\nslow first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
prompt=$(cat)
printf '%s
---
' "$prompt" >> "$CODEX_LOG"
case "$prompt" in
*"slow first"*)
  : > "$STARTED"
  while [ ! -f "$RELEASE" ]; do sleep 0.05; done
  ;;
esac
`
	if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)
	t.Setenv("STARTED", started)
	t.Setenv("RELEASE", release)

	var runOut strings.Builder
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run([]string{"run", file, "-codex", codex}, &runOut, &runOut)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first task did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}

	var appendOut strings.Builder
	if err := RunAppend(file, "/task\nsecond\n", &appendOut); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not finish")
	}

	restored, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "/task\nslow first\n" {
		t.Fatalf("source file should be restored unchanged, got:\n%s", restored)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "slow first") || !strings.Contains(string(log), "second") {
		t.Fatalf("expected current run to execute appended task, log:\n%s\nappend output:\n%s", log, appendOut.String())
	}
	resultPath := resultDocumentPathFromOutput(t, runOut.String())
	result, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "second") {
		t.Fatalf("expected result document to include appended task:\n%s", result)
	}
}

func TestRunSubcommandInjectsDocumentFlags(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/flag string name user name\n\n/task\nhello {{name}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >> \"$CODEX_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if err := Run([]string{"run", file, "-name", "Ada", "-codex", codex, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(log)) != "hello Ada" {
		t.Fatalf("unexpected codex log: %q", log)
	}
}

func TestRunSubcommandCdUsesStartWorkdir(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	withWorkingDirectory(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "marker.txt"), []byte("from-start-workdir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "todo.md")
	pwdFile := filepath.Join(dir, "pwd.log")
	markerLog := filepath.Join(dir, "marker.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/cd --must-exist app\nread marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\npwd > \"$PWD_LOG\"\ncat marker.txt > \"$MARKER_LOG\"\ncat >/dev/null\n"
	if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD_LOG", pwdFile)
	t.Setenv("MARKER_LOG", markerLog)

	var out strings.Builder
	if err := Run([]string{"run", file, "-codex", codex}, &out, &out); err != nil {
		t.Fatalf("run failed: %v\n%s", err, out.String())
	}
	gotPWD, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(gotPWD)) != filepath.Join(dir, "app") {
		t.Fatalf("expected /cd to use start workdir, got %q", gotPWD)
	}
	gotMarker, err := os.ReadFile(markerLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotMarker) != "from-start-workdir\n" {
		t.Fatalf("expected marker from start workdir app, got %q", gotMarker)
	}
}

func TestRunHidesImportedFilesAndExecutesWorkingCopies(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	withWorkingDirectory(t, dir)
	file := filepath.Join(dir, "todo.md")
	importFile := filepath.Join(dir, "lib.todo.md")
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	mainOriginal := "/import lib.todo.md\n\n/let msg /call message\n{{msg}}\n"
	importOriginal := "/def message\n/return from import\n"
	if err := os.WriteFile(file, []byte(mainOriginal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(importFile, []byte(importOriginal), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat \"$ORIG_IMPORT\" > \"$CODEX_LOG.import\"\ncat >> \"$CODEX_LOG\"\n"
	if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)
	t.Setenv("ORIG_IMPORT", importFile)

	var out strings.Builder
	if err := Run([]string{"run", file, "-codex", codex}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != mainOriginal {
		t.Fatalf("main source should be restored unchanged:\n%s", got)
	}
	if got, _ := os.ReadFile(importFile); string(got) != importOriginal {
		t.Fatalf("import source should be restored unchanged:\n%s", got)
	}
	seenImport, err := os.ReadFile(logFile + ".import")
	if err != nil {
		t.Fatal(err)
	}
	if string(seenImport) != placeholderContent {
		t.Fatalf("agent should see placeholder at original import path, got:\n%s", seenImport)
	}
	log, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(log)) != "from import" {
		t.Fatalf("expected prompt from working import copy, got %q", log)
	}
}

func TestResumeRestoreSourceRestoresMissingSourceFromRunCopy(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	importFile := filepath.Join(dir, "lib.todo.md")
	codex := filepath.Join(dir, "codex")
	original := "/import lib.todo.md\n\n/task\nrestore me\n"
	importOriginal := "/def note\n/return restored import\n"
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(importFile, []byte(importOriginal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"run", file, "-codex", codex}, &out, &out); err != nil {
		t.Fatal(err)
	}
	_ = artifactsPathFromOutput(t, out.String())
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(importFile); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run([]string{"resume", "--restore-source"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != original {
		t.Fatalf("unexpected restored source:\n%s", restored)
	}
	restoredImport, err := os.ReadFile(importFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredImport) != importOriginal {
		t.Fatalf("unexpected restored import source:\n%s", restoredImport)
	}
}

func TestResumeLastFlagIsAcceptedForManagedRunResume(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	var out strings.Builder
	err := Run([]string{"resume", "--last"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "no unfinished ATM run found") {
		t.Fatalf("expected --last to select unfinished runs, got %v\n%s", err, out.String())
	}
}

func TestDynamicCommandRunsCopyWithoutUpdatingSourceDocument(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	if err := os.MkdirAll(filepath.Join(dir, ".atm", "flag"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	file := filepath.Join(dir, ".atm", "flag", "greet.todo.md")
	original := "/flag string name user name\n\n/task\nhello {{name}}\n"
	logFile := filepath.Join(dir, "codex.log")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >> \"$CODEX_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_LOG", logFile)

	var out strings.Builder
	if commands, err := discoverDynamicCommands(); err != nil {
		t.Fatal(err)
	} else if len(commands) != 0 {
		t.Fatalf("expected no dynamic command before registration, got %#v", commands)
	}
	if err := Run([]string{"flag", "register", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"greet", "-name", "Ada", "-codex", codex}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != original {
		t.Fatalf("dynamic command updated source document:\n%s", updated)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atm", "commands", "greet")); err != nil {
		t.Fatalf("expected command artifacts: %v", err)
	}
}

func TestFlagScanRegistersProjectLocalDynamicCommands(t *testing.T) {
	dir := t.TempDir()
	withWorkingDirectory(t, dir)
	if err := os.MkdirAll(filepath.Join(".atm", "flag"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".atm", "flag", "review.todo.md"), []byte("/task\nreview\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"flag", "scan"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	commands, err := discoverDynamicCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Name != "review" || commands[0].File != ".atm/flag/review.todo.md" {
		t.Fatalf("unexpected dynamic commands: %#v", commands)
	}
}

func TestFlagRegisterGlobalWritesGlobalRegistry(t *testing.T) {
	dir := t.TempDir()
	withGlobalRegistryDirectory(t, dir)
	withWorkingDirectory(t, dir)
	file := filepath.Join(dir, "review.todo.md")
	if err := os.WriteFile(file, []byte("/task\nreview\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"flag", "register", file, "--name", "review", "-g"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atm", "flag", "index.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no local registry, stat err=%v", err)
	}
	registry, err := loadDynamicRegistry(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Commands) != 1 || registry.Commands[0].Name != "review" {
		t.Fatalf("unexpected global registry: %#v", registry.Commands)
	}
	assertSameFile(t, registry.Commands[0].File, file)
}

func TestMissingRegisteredDynamicCommandCanBeUnregistered(t *testing.T) {
	dir := t.TempDir()
	withGlobalRegistryDirectory(t, dir)
	withWorkingDirectory(t, dir)

	missing := filepath.Join(dir, "deleted.todo.md")
	if err := saveDynamicRegistry(dynamicIndex{Commands: []dynamicRegistration{{Name: "deleted", File: missing}}}, true); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"flag", "unregister", "deleted", "-g"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("unregister stale command: %v", err)
	}
	registry, err := loadDynamicRegistry(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Commands) != 0 {
		t.Fatalf("expected stale registration removed, got %#v", registry.Commands)
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
	if err := Run([]string{file, "-codex", codex, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
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

func TestExecSubcommandIsRemoved(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/task\nalready done [done]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := Run([]string{"exec", file}, &out, &out)
	if err == nil {
		t.Fatal("expected removed exec command to fail")
	}
}

func TestDefaultRunRequiresExplicitTodoFile(t *testing.T) {
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
	if err := os.WriteFile("todo.md", []byte("/task\nmarkdown prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	err = Run([]string{"run"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "no ATM file specified") {
		t.Fatalf("expected explicit-file error, got %v", err)
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
	if err := Run([]string{first, second, "-codex", codex, "-output", outDir}, &out, &out); err != nil {
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

func TestRunQueuesPositionalFiles(t *testing.T) {
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
	if err := Run([]string{"run", first, second, "-output", filepath.Join(dir, "out")}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "atm run file 1/2") || !strings.Contains(out.String(), "atm run file 2/2") {
		t.Fatalf("expected queued file progress, got:\n%s", out.String())
	}
}

func TestRunRejectsRemovedFileFlag(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/task\nalready done [done]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := Run([]string{"run", "-file", file}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("expected removed -file flag to fail, got %v", err)
	}
}

func TestPlanSubcommandPrintsDryRun(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/for 2 /go\nreview {{n}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"check", "--plan", file}, &out, &out); err != nil {
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
	if err := Run([]string{"check", "--plan", file}, &text, &text); err != nil {
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
	if err := Run([]string{"check", "--plan", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", "--plan", file}, &text, &text); err != nil {
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
	if err := Run([]string{"check", "--plan", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", "--plan", file}, &text, &text); err != nil {
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
	if err := Run([]string{"check", "--plan", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
		"/resume alpha",
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
	if err := Run([]string{"check", "--plan", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"runtime: resume=alpha; args=--model fast; cd=backend (must-exist); bash=1; lazy=lazy:call(inspect)",
		"flow: Cd(backend, must-exist) -> Bash -> LazyCall(inspect -> lazy) -> Execute [resume=alpha, args=--model fast]",
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("expected plan runtime to contain %q, got:\n%s", want, text.String())
		}
	}

	var jsonOut strings.Builder
	if err := Run([]string{"check", "--plan", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", "--plan", file}, &text, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "context: ") || !strings.Contains(text.String(), "# Release") {
		t.Fatalf("expected plan context summary, got:\n%s", text.String())
	}

	var jsonOut strings.Builder
	if err := Run([]string{"check", "--plan", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", "--plan", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "atm check plan dry-run: "+file) {
		t.Fatalf("unexpected plan output:\n%s", out.String())
	}
}

func TestPlanSubcommandRequiresExplicitFile(t *testing.T) {
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
	err = Run([]string{"check", "--plan", "-json"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "no ATM file specified") {
		t.Fatalf("expected explicit-file error, got %v", err)
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
	if err := Run([]string{"check", "--plan", file, "-html", htmlFile}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(htmlFile); err != nil {
		t.Fatalf("expected html plan file: %v", err)
	}
	if !strings.Contains(out.String(), "atm check plan HTML:") {
		t.Fatalf("expected html path output, got:\n%s", out.String())
	}

	out.Reset()
	if err := Run([]string{"check", "--plan", file, "-json"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(out.String()), &plan); err != nil {
		t.Fatalf("parse JSON plan output: %v\n%s", err, out.String())
	}
	if plan.Source != file {
		t.Fatalf("expected JSON plan source %q, got %q\n%s", file, plan.Source, out.String())
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
	if err := Run([]string{"check", "--plan", file}, &dry, &dry); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("regular plan should not execute lazy bash provider, stat err=%v", err)
	}
	if strings.Contains(dry.String(), "preview providers:") {
		t.Fatalf("regular plan should not include preview provider output:\n%s", dry.String())
	}

	var preview strings.Builder
	if err := Run([]string{"check", "--plan", "--preview", file}, &preview, &preview); err != nil {
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
	if err := Run([]string{"check", "--plan", "--preview", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", "--plan", "--preview", file}, &preview, &preview); err != nil {
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
	if err := Run([]string{"check", "--plan", "--preview", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"atm check ok:", "blocks: 2", "tasks: 1", "definitions: 1", "resources: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected check output to contain %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"check", "-json", file}, &out, &out); err != nil {
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
	if err := Run([]string{"check", file}, &out, &out); err == nil {
		t.Fatal("expected invalid DSL to fail")
	}

	out.Reset()
	if err := Run([]string{"check", "-json", file}, &out, &out); err == nil {
		t.Fatal("expected invalid DSL JSON check to fail")
	}
	var diagnostics struct {
		Files []struct {
			File        string `json:"file"`
			Diagnostics []struct {
				Severity string `json:"severity"`
				Source   string `json:"source"`
				Block    int    `json:"block"`
				Line     int    `json:"line"`
				Column   int    `json:"column"`
				Message  string `json:"message"`
			} `json:"diagnostics"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out.String()), &diagnostics); err != nil {
		t.Fatalf("parse JSON diagnostics: %v\n%s", err, out.String())
	}
	if len(diagnostics.Files) != 1 || diagnostics.Files[0].File != file || len(diagnostics.Files[0].Diagnostics) != 1 {
		t.Fatalf("unexpected JSON diagnostics:\n%s", out.String())
	}
	got := diagnostics.Files[0].Diagnostics[0]
	if got.Severity != "error" || got.Source != file || got.Block != 1 || got.Line != 1 || got.Column != 1 || !strings.Contains(got.Message, "requires a parenthesized expression") {
		t.Fatalf("unexpected JSON diagnostic: %#v\n%s", got, out.String())
	}
	for _, want := range []string{`"diagnostics": [`, `"severity": "error"`, `"block": 1`, `"line": 1`, `"column": 1`, "requires a parenthesized expression"} {
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
	if err := Run([]string{"check", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warning:") || !strings.Contains(out.String(), "without a later /wait") {
		t.Fatalf("expected text warning, got:\n%s", out.String())
	}

	out.Reset()
	if err := Run([]string{"check", "-json", file}, &out, &out); err != nil {
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
	if err := Run([]string{"check", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"warning:", "lazy provider with possible side effects", "lazy definition provider", "/return /bash"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected check warning %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"check", "--plan", file}, &out, &out); err != nil {
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
	if err := Run([]string{"check", file}, &out, &out); err != nil {
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
	if err := Run([]string{"check", "-json", file}, &out, &out); err != nil {
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
	if err := Run([]string{"report", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"atm report:", "done: 1", "failed: 1", "draft: 1", "failures:", "failed-task-def", "orphan reports:", "orphan-task", "lonely", "recent logs:", "out/task-002.log"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected report output to contain %q, got:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run([]string{"report", "-json", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"counts": {`, `"failed": 1`, `"orphans": [`, `"id": "orphan-task"`, `"source": "sha256:orphan-source"`, `"rendered": "sha256:orphan-rendered"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected JSON report to contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestReportDefaultsToLatestRunForCurrentProject(t *testing.T) {
	requirePOSIXShell(t)

	dir := t.TempDir()
	withWorkingDirectory(t, dir)
	t.Setenv("ATM_HOME", filepath.Join(dir, "home"))
	file := filepath.Join(dir, "todo.txt")
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(file, []byte("/task\nSay hello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'done\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"run", file, "-codex", codex}, &out, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run([]string{"report"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"atm report:", "run:", "source: " + file, "done: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected report output to contain %q, got:\n%s", want, out.String())
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
	if err := Run([]string{"check", file}, &checkOut, &checkOut); err == nil {
		t.Fatal("expected duplicate id check to fail before repair")
	}

	var out strings.Builder
	if err := Run([]string{"clean", "--repair-ids", file}, &out, &out); err != nil {
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
	if err := Run([]string{"check", file}, &checkOut, &checkOut); err != nil {
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
		"/resume alpha",
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
	if err := Run([]string{"check", "--plan", file}, &text, &text); err != nil {
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
		"flow: Execute [resume=alpha, args=--yolo]",
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
	if err := Run([]string{"check", "--plan", "-json", file}, &jsonOut, &jsonOut); err != nil {
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
	if err := Run([]string{"check", "--plan", file, "-html", htmlFile}, &out, &out); err != nil {
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
	if !strings.Contains(out.String(), "atm check plan HTML:") {
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
	if err := Run([]string{"check", "--plan", file, "-html", htmlFile}, &out, &out); err != nil {
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
		"Wait",
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
	if err := Run([]string{"format", file}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "/task\n\nprompt\n[done]\n" {
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
	if string(updated) != "/task\nexisting\n\n/go\n\nnew\n[done]\n" {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestAppendSubcommandAcceptsPositionalTodoFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/task\nexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Run([]string{"append", file, "/task\nnew work\n"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "/task\n\nnew work\n") {
		t.Fatalf("unexpected content: %q", updated)
	}
}

func TestUntagSubcommandIsRemoved(t *testing.T) {
	file := filepath.Join(t.TempDir(), "todo.txt")
	if err := os.WriteFile(file, []byte("/task\ndone [done]\n\n/task\nrunning [running|20260508-14:32|1x]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := Run([]string{"untag", file}, &out, &out)
	if err == nil {
		t.Fatal("expected removed untag command to fail")
	}
}
