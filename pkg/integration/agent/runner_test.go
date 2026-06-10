package agent

import (
	"bytes"
	"context"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("ATM_MCP_TRANSPORT", "stdio")
	os.Exit(m.Run())
}

func TestCodexCheckPrefersMCPResultFile(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeCodex := filepath.Join(dir, "codex")
	argsLog := filepath.Join(dir, "args.log")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_LOG"
if [ -n "$ATM_CHECK_RESULT_FILE" ] || [ -n "$ATM_CHECK_MCP_COMMAND" ] || [ -n "$ATM_CHECK_MCP_TOOL" ]; then
  exit 4
fi
prompt=$(cat)
case "$prompt" in
*"atm_report_check"*) ;;
*) exit 3 ;;
esac
result_file=""
for arg in "$@"; do
  case "$arg" in
    mcp_servers.atm_check.args=*)
      result_file=$(printf '%s\n' "$arg" | sed -n 's/.*"-result-file", "\([^"]*\)".*/\1/p')
      ;;
  esac
done
if [ -z "$result_file" ]; then
  exit 5
fi
printf '{"passed":true,"summary":"structured"}\n' > "$result_file"
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGS_LOG", argsLog)

	runner := codexRunner{path: fakeCodex}
	passed, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "fix tests", "tests pass", ir.RunOptions{}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !passed {
		t.Fatal("expected structured MCP result to pass")
	}
	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "mcp_servers.atm_check.command=") ||
		!strings.Contains(string(args), "mcp_servers.atm_check.args=") ||
		!strings.Contains(string(args), "-result-file") {
		t.Fatalf("expected temporary MCP config args, got:\n%s", args)
	}
}

func TestCodexCheckRequiresMCPResult(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeCodex := filepath.Join(dir, "codex")
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'text result ignored\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := codexRunner{path: fakeCodex}
	passed, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "fix tests", "tests pass", ir.RunOptions{}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected missing MCP result error")
	}
	if passed {
		t.Fatal("expected check to fail without MCP result")
	}
}

func TestCodexCheckArgsAddTemporaryMCPServerConfig(t *testing.T) {
	args := strings.Join(codexCheckArgs(ir.RunOptions{Args: []string{"--json"}}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, "mcp_servers.atm_check.command=") {
		t.Fatalf("missing command config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.atm_check.args=["mcp", "check", "-result-file", "/tmp/result.json"]`) {
		t.Fatalf("missing args config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.atm_check.tools.atm_report_check.approval_mode="approve"`) {
		t.Fatalf("missing approval config:\n%s", args)
	}
	if !strings.Contains(args, "--json") {
		t.Fatalf("missing user args:\n%s", args)
	}
	if strings.Contains(args, "ATM_MCP_CHECK_LOG") {
		t.Fatalf("unexpected debug env config:\n%s", args)
	}
}

func TestCodexCheckArgsPassDebugLogEnvToMCPServer(t *testing.T) {
	t.Setenv("ATM_MCP_CHECK_LOG", "/tmp/atm-mcp.log")

	args := strings.Join(codexCheckArgs(ir.RunOptions{}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, `mcp_servers.atm_check.env.ATM_MCP_CHECK_LOG="/tmp/atm-mcp.log"`) {
		t.Fatalf("missing debug log env config:\n%s", args)
	}
}

func TestCodexCheckArgsCanUseHTTPMCPServer(t *testing.T) {
	t.Setenv("ATM_MCP_TRANSPORT", "http")

	args := strings.Join(codexCheckArgs(ir.RunOptions{}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, `mcp_servers.atm_check.url="http://127.0.0.1:`) {
		t.Fatalf("missing HTTP MCP url config:\n%s", args)
	}
	if strings.Contains(args, "mcp_servers.atm_check.command=") || strings.Contains(args, "mcp_servers.atm_check.args=") {
		t.Fatalf("unexpected stdio MCP config:\n%s", args)
	}
}

func TestCodexExecuteArgsIncludeExternalAndDefsMCP(t *testing.T) {
	opts := ir.RunOptions{
		MCPs:   []ir.MCPRuntime{{Name: "helper", Config: `{"command":"helper","args":["--serve"],"env":{"A":"B"}}`, ApprovedTools: []string{"notify"}}},
		DefMCP: &ir.DefMCPRuntime{Definitions: []string{"echo"}},
	}
	args := strings.Join(codexArgs(opts, "", "", "", "/tmp/defs.json", false), "\n")
	if !strings.Contains(args, `mcp_servers.helper.command="helper"`) ||
		!strings.Contains(args, `mcp_servers.helper.args=["--serve"]`) ||
		!strings.Contains(args, `mcp_servers.helper.env.A="B"`) {
		t.Fatalf("missing external mcp config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.helper.tools.notify.approval_mode="approve"`) {
		t.Fatalf("missing approved external mcp tool config:\n%s", args)
	}
	if !strings.Contains(args, "mcp_servers.atm_defs.command=") ||
		!strings.Contains(args, `mcp_servers.atm_defs.args=["mcp", "defs", "-config-file", "/tmp/defs.json"]`) {
		t.Fatalf("missing defs mcp config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.atm_defs.tools.atm_def_echo.approval_mode="approve"`) {
		t.Fatalf("missing defs mcp approval config:\n%s", args)
	}
}

func TestCodexResumeArgsUseSpecificSession(t *testing.T) {
	args := strings.Join(codexArgs(ir.RunOptions{Resume: true, ResumeSessionID: "thread_1"}, "", "", "", "", false), "\n")
	if !strings.Contains(args, "resume\nthread_1") || strings.Contains(args, "--last") {
		t.Fatalf("unexpected codex resume args:\n%s", args)
	}
}

func TestCodexDangerArgsBypassApprovalsAndSandbox(t *testing.T) {
	execArgs := strings.Join(codexArgs(ir.RunOptions{Danger: true}, "", "", "", "", false), "\n")
	if !strings.Contains(execArgs, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("missing codex danger arg:\n%s", execArgs)
	}
	checkArgs := strings.Join(codexCheckArgs(ir.RunOptions{Danger: true}, "/tmp/result.json"), "\n")
	if !strings.Contains(checkArgs, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("missing codex check danger arg:\n%s", checkArgs)
	}
}

func TestCodexForkExecuteMaterializesSessionAndRunsExecResume(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(dir, "codex-home")
	parentID := "019e6994-b038-7011-be84-de68bff950f3"
	parentDir := filepath.Join(codexHome, "sessions", "2026", "05", "27")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(parentDir, "rollout-2026-05-27T21-16-52-"+parentID+".jsonl")
	parent := `{"timestamp":"2026-05-27T13:16:52.000Z","type":"session_meta","payload":{"id":"` + parentID + `","timestamp":"2026-05-27T13:16:52.000Z","cwd":"/parent/work","originator":"codex_cli_rs","cli_version":"0.134.0","source":"cli","model_provider":"codex","base_instructions":{"text":"base"}}}
{"timestamp":"2026-05-27T13:16:53.000Z","type":"event_msg","payload":{"type":"user_message","message":"parent prompt","images":[],"local_images":[],"text_elements":[]}}
{"timestamp":"2026-05-27T13:16:54.000Z","type":"event_msg","payload":{"type":"agent_message","message":"parent done"}}
`
	if err := os.WriteFile(parentPath, []byte(parent), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(dir, "codex")
	argsLog := filepath.Join(dir, "args.log")
	promptLog := filepath.Join(dir, "prompt.log")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_LOG"
cat > "$PROMPT_LOG"
case "$*" in
  *"exec --json resume"*) ;;
  *) exit 6 ;;
esac
session=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "resume" ]; then
    session="$arg"
    break
  fi
  prev="$arg"
done
if [ -z "$session" ]; then
  exit 7
fi
printf '{"type":"thread.started","thread_id":"%s"}\n' "$session"
printf '{"type":"item.completed","item":{"type":"agent_message","text":"fork done"}}\n'
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("ARGS_LOG", argsLog)
	t.Setenv("PROMPT_LOG", promptLog)

	runner := codexRunner{path: fakeCodex}
	var stdout bytes.Buffer
	result, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "prompt", ir.RunOptions{Fork: true, ResumeSessionID: parentID, Workdir: workdir}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID == "" || result.SessionID == parentID {
		t.Fatalf("expected new forked session id, got %q", result.SessionID)
	}
	if len(result.Messages) != 1 || result.Messages[0].Text != "fork done" {
		t.Fatalf("unexpected messages: %#v", result.Messages)
	}
	if !strings.Contains(stdout.String(), result.SessionID) || !strings.Contains(stdout.String(), "fork done") {
		t.Fatalf("unexpected rendered output:\n%s", stdout.String())
	}
	prompt, err := os.ReadFile(promptLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(prompt)) != "prompt" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
	forkPath, meta, ok, err := findCodexSessionByID(codexHome, result.SessionID)
	if err != nil || !ok {
		t.Fatalf("expected materialized fork path, ok=%v err=%v", ok, err)
	}
	if meta.ForkedFromID != parentID {
		t.Fatalf("expected forked_from_id %q, got %#v", parentID, meta)
	}
	if !samePath(meta.Cwd, workdir) {
		t.Fatalf("expected fork cwd %q, got %q", workdir, meta.Cwd)
	}
	forkData, err := os.ReadFile(forkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(forkData), `"message":"parent done"`) {
		t.Fatalf("expected parent rollout history to be copied:\n%s", forkData)
	}
}

func TestCodexForkSnapshotDropsPartialTailAndMarksInterruptedTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := strings.Join([]string{
		`{"timestamp":"2026-05-27T13:16:52.000Z","type":"session_meta","payload":{"id":"019e6994-b038-7011-be84-de68bff950f3","timestamp":"2026-05-27T13:16:52.000Z","cwd":"/work"}}`,
		`{"timestamp":"2026-05-27T13:16:53.000Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1"}}`,
		`{"timestamp":"2026-05-27T13:16:54.000Z","type":"event_msg","payload":{"type":"user_message","message":"parent prompt"}}`,
		`{"timestamp":"2026-05-27T13:16:55.000Z","type":"event_msg","payload":{"type":"agent_message","message":"partial`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readCodexForkSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(snapshot)
	if strings.Contains(text, "agent_message") {
		t.Fatalf("expected invalid trailing partial line to be dropped:\n%s", text)
	}
	if !strings.Contains(text, `"type":"turn_aborted"`) ||
		!strings.Contains(text, `"turn_id":"turn-1"`) ||
		!strings.Contains(text, `turn_aborted`) {
		t.Fatalf("expected interrupted boundary in snapshot:\n%s", text)
	}
}

func TestCodexForkSnapshotKeepsCompletedTurnUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := strings.Join([]string{
		`{"timestamp":"2026-05-27T13:16:52.000Z","type":"session_meta","payload":{"id":"019e6994-b038-7011-be84-de68bff950f3","timestamp":"2026-05-27T13:16:52.000Z","cwd":"/work"}}`,
		`{"timestamp":"2026-05-27T13:16:53.000Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1"}}`,
		`{"timestamp":"2026-05-27T13:16:54.000Z","type":"event_msg","payload":{"type":"user_message","message":"parent prompt"}}`,
		`{"timestamp":"2026-05-27T13:16:55.000Z","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1","last_agent_message":"done"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readCodexForkSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(snapshot), `"type":"turn_aborted"`) {
		t.Fatalf("did not expect interrupted boundary for completed turn:\n%s", snapshot)
	}
}

func TestCodexExecuteWithOutputUsesMCPResult(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeCodex := filepath.Join(dir, "codex")
	promptLog := filepath.Join(dir, "prompt.log")
	script := `#!/bin/sh
cat > "$PROMPT_LOG"
result_file=""
for arg in "$@"; do
  case "$arg" in
    mcp_servers.atm_output.args=*)
      result_file=$(printf '%s\n' "$arg" | sed -n 's/.*"-result-file", "\([^"]*\)".*/\1/p')
      ;;
  esac
done
if [ -z "$result_file" ]; then
  exit 5
fi
printf '{"reason":"ok"}\n' > "$result_file"
printf '{"type":"item.completed","item":{"type":"agent_message","text":"submitted"}}\n'
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROMPT_LOG", promptLog)

	runner := codexRunner{path: fakeCodex}
	result, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "report", ir.RunOptions{Output: &ir.OutputSpec{Schema: `{"type":"object"}`, SchemaFormat: "json"}}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.StructuredOutput), `"reason":"ok"`) {
		t.Fatalf("unexpected structured output: %s", result.StructuredOutput)
	}
	prompt, err := os.ReadFile(promptLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "atm_report_output") {
		t.Fatalf("expected MCP output instruction in prompt:\n%s", prompt)
	}
}

func TestCodexExecuteUsesWorkdir(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(dir, "codex")
	pwdLog := filepath.Join(dir, "pwd.log")
	script := `#!/bin/sh
pwd > "$PWD_LOG"
cat >/dev/null
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD_LOG", pwdLog)

	runner := codexRunner{path: fakeCodex}
	if _, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "report", ir.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(strings.TrimSpace(string(got)), workdir) {
		t.Fatalf("expected codex workdir %q, got %q", workdir, strings.TrimSpace(string(got)))
	}
}

func TestCodexExecuteMaterializesSkillsInWorkdir(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	skillDir := filepath.Join(dir, "reviewer")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Reviewer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(dir, "codex")
	script := `#!/bin/sh
test -f .agents/skills/reviewer/SKILL.md || exit 7
cat >/dev/null
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := codexRunner{path: fakeCodex}
	_, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "run", ir.RunOptions{
		Workdir: workdir,
		Skills:  []ir.SkillRuntime{{Name: "reviewer", Path: skillDir}},
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".agents", "skills", "reviewer")); !os.IsNotExist(err) {
		t.Fatalf("expected generated skill copy to be cleaned up, stat err=%v", err)
	}
}

func TestCodexCheckUsesWorkdir(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(dir, "codex")
	pwdLog := filepath.Join(dir, "pwd.log")
	script := `#!/bin/sh
pwd > "$PWD_LOG"
cat >/dev/null
result_file=""
for arg in "$@"; do
  case "$arg" in
    mcp_servers.atm_check.args=*)
      result_file=$(printf '%s\n' "$arg" | sed -n 's/.*"-result-file", "\([^"]*\)".*/\1/p')
      ;;
  esac
done
printf '{"passed":true,"summary":"ok"}\n' > "$result_file"
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD_LOG", pwdLog)

	runner := codexRunner{path: fakeCodex}
	if _, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "report", "ok", ir.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(strings.TrimSpace(string(got)), workdir) {
		t.Fatalf("expected codex check workdir %q, got %q", workdir, strings.TrimSpace(string(got)))
	}
}

func TestCodexOutputArgsAddTemporaryMCPServerConfig(t *testing.T) {
	args := strings.Join(codexOutputMCPArgs(&ir.OutputSpec{SchemaFormat: "json"}, "/tmp/out.json", "/tmp/schema.json"), "\n")
	if !strings.Contains(args, "mcp_servers.atm_output.command=") {
		t.Fatalf("missing command config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.atm_output.args=["mcp", "output", "-result-file", "/tmp/out.json", "-schema-file", "/tmp/schema.json", "-schema-format", "json"]`) {
		t.Fatalf("missing args config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.atm_output.tools.atm_report_output.approval_mode="approve"`) {
		t.Fatalf("missing approval config:\n%s", args)
	}
}

func TestClaudeCheckPrefersMCPResultFile(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	argsLog := filepath.Join(dir, "args.log")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_LOG"
config=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--mcp-config" ]; then
    config="$arg"
    break
  fi
  prev="$arg"
done
case "$*" in
*"atm_report_check"*) ;;
*) exit 3 ;;
esac
result_file=$(printf '%s\n' "$config" | sed -n 's/.*"-result-file","\([^"]*\)".*/\1/p')
if [ -z "$result_file" ]; then
  exit 5
fi
printf '{"passed":true,"summary":"structured"}\n' > "$result_file"
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGS_LOG", argsLog)

	runner := claudeRunner{path: fakeClaude}
	passed, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "fix tests", "tests pass", ir.RunOptions{}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !passed {
		t.Fatal("expected structured MCP result to pass")
	}
	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--mcp-config") ||
		!strings.Contains(string(args), `"mcpServers":{"atm_check"`) ||
		!strings.Contains(string(args), "-result-file") {
		t.Fatalf("expected temporary MCP config args, got:\n%s", args)
	}
}

func TestClaudeCheckArgsAddTemporaryMCPServerConfig(t *testing.T) {
	args := strings.Join(claudeCheckArgs("check prompt", ir.RunOptions{Args: []string{"--output-format", "json"}}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, "--mcp-config") {
		t.Fatalf("missing mcp config flag:\n%s", args)
	}
	if !strings.Contains(args, `"mcpServers":{"atm_check"`) {
		t.Fatalf("missing atm_check server:\n%s", args)
	}
	if !strings.Contains(args, `"-result-file","/tmp/result.json"`) {
		t.Fatalf("missing result file arg:\n%s", args)
	}
	if !strings.Contains(args, "--allowedTools") || !strings.Contains(args, "mcp__atm_check__atm_report_check") {
		t.Fatalf("missing allowed check tool:\n%s", args)
	}
	if !strings.Contains(args, "--output-format") || !strings.Contains(args, "json") {
		t.Fatalf("missing user args:\n%s", args)
	}
	if strings.Contains(args, "ATM_MCP_CHECK_LOG") {
		t.Fatalf("unexpected debug env config:\n%s", args)
	}
}

func TestClaudeResumeArgsUseSpecificSession(t *testing.T) {
	args := strings.Join(claudeArgs("prompt", ir.RunOptions{Resume: true, ResumeSessionID: "session_1"}, "", "", "", "", false), "\n")
	if !strings.Contains(args, "--resume\nsession_1") || strings.Contains(args, "\n-c\n") {
		t.Fatalf("unexpected claude resume args:\n%s", args)
	}
}

func TestClaudeForkArgsUseSpecificSession(t *testing.T) {
	args := strings.Join(claudeArgs("prompt", ir.RunOptions{Fork: true, ResumeSessionID: "session_1"}, "", "", "", "", false), "\n")
	if !strings.Contains(args, "--resume\nsession_1\n--fork-session") {
		t.Fatalf("unexpected claude fork args:\n%s", args)
	}
}

func TestClaudeDangerArgsSkipPermissions(t *testing.T) {
	execArgs := strings.Join(claudeArgs("prompt", ir.RunOptions{Danger: true}, "", "", "", "", false), "\n")
	if !strings.Contains(execArgs, "--dangerously-skip-permissions") {
		t.Fatalf("missing claude danger arg:\n%s", execArgs)
	}
	checkArgs := strings.Join(claudeCheckArgs("check prompt", ir.RunOptions{Danger: true}, "/tmp/result.json"), "\n")
	if !strings.Contains(checkArgs, "--dangerously-skip-permissions") {
		t.Fatalf("missing claude check danger arg:\n%s", checkArgs)
	}
}

func TestClaudeCheckArgsPassDebugLogEnvToMCPServer(t *testing.T) {
	t.Setenv("ATM_MCP_CHECK_LOG", "/tmp/atm-mcp.log")

	args := strings.Join(claudeCheckArgs("check prompt", ir.RunOptions{}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, `"env":{"ATM_MCP_CHECK_LOG":"/tmp/atm-mcp.log"}`) {
		t.Fatalf("missing debug log env config:\n%s", args)
	}
}

func TestClaudeCheckArgsCanUseHTTPMCPServer(t *testing.T) {
	t.Setenv("ATM_MCP_TRANSPORT", "http")

	args := strings.Join(claudeCheckArgs("check prompt", ir.RunOptions{}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, `"type":"http"`) || !strings.Contains(args, `"url":"http://127.0.0.1:`) {
		t.Fatalf("missing HTTP MCP config:\n%s", args)
	}
	if strings.Contains(args, `"command"`) || strings.Contains(args, `"args"`) {
		t.Fatalf("unexpected stdio MCP config:\n%s", args)
	}
}

func TestClaudeExecuteAllowsTemporaryMCPTools(t *testing.T) {
	args := strings.Join(claudeArgs("prompt", ir.RunOptions{
		Output: &ir.OutputSpec{Schema: `{"type":"object"}`, SchemaFormat: "json", Structured: true},
		DefMCP: &ir.DefMCPRuntime{
			Definitions: []string{"echo"},
		},
		MCPs: []ir.MCPRuntime{{Name: "atm_webhook", Config: `{"command":"atm"}`, ApprovedTools: []string{"atm_webhook_alarm"}}},
	}, "/tmp/out.json", "/tmp/schema.json", "", "/tmp/defs.json", false), "\n")
	if !strings.Contains(args, "--allowedTools") ||
		!strings.Contains(args, "mcp__atm_output__atm_report_output") ||
		!strings.Contains(args, "mcp__atm_defs__atm_def_echo") ||
		!strings.Contains(args, "mcp__atm_webhook__atm_webhook_alarm") {
		t.Fatalf("missing allowed temporary MCP tools:\n%s", args)
	}
}

func TestClaudeExecuteUsesWorkdir(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(dir, "claude")
	pwdLog := filepath.Join(dir, "pwd.log")
	script := `#!/bin/sh
pwd > "$PWD_LOG"
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}\n'
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD_LOG", pwdLog)

	runner := claudeRunner{path: fakeClaude}
	if _, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "report", ir.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(strings.TrimSpace(string(got)), workdir) {
		t.Fatalf("expected claude workdir %q, got %q", workdir, strings.TrimSpace(string(got)))
	}
}

func TestClaudeCheckUsesWorkdir(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(dir, "claude")
	pwdLog := filepath.Join(dir, "pwd.log")
	script := `#!/bin/sh
pwd > "$PWD_LOG"
config=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--mcp-config" ]; then
    config="$arg"
    break
  fi
  prev="$arg"
done
result_file=$(printf '%s\n' "$config" | sed -n 's/.*"-result-file","\([^"]*\)".*/\1/p')
printf '{"passed":true,"summary":"ok"}\n' > "$result_file"
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}\n'
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD_LOG", pwdLog)

	runner := claudeRunner{path: fakeClaude}
	if _, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "report", "ok", ir.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(strings.TrimSpace(string(got)), workdir) {
		t.Fatalf("expected claude check workdir %q, got %q", workdir, strings.TrimSpace(string(got)))
	}
}

func TestMCPCheckPromptDoesNotIncludeTaskCheckFallback(t *testing.T) {
	prompt := buildCheckPrompt("fix tests", "tests pass", true)
	if !strings.Contains(prompt, "atm_report_check") {
		t.Fatalf("missing MCP tool instruction:\n%s", prompt)
	}
	if strings.Contains(prompt, "TASK_CHECK") || strings.Contains(prompt, "fallback") {
		t.Fatalf("MCP prompt must not include text fallback:\n%s", prompt)
	}
}

func TestCodexParserRecognizesCommandExecution(t *testing.T) {
	parser := newAgentEventParser("codex")
	events, recognized := parser.consume(`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/bash -lc pwd","status":"in_progress"}}`)
	if !recognized || len(events) != 1 {
		t.Fatalf("expected command execution event, recognized=%v events=%#v", recognized, events)
	}
	if events[0].kind != "tool" || events[0].name != "command_execution: /bin/bash -lc pwd" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}

func TestCodexParserRendersLifecycleEvents(t *testing.T) {
	parser := newAgentEventParser("codex")
	cases := []string{
		`{"type":"thread.started","thread_id":"thread_1"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2,"reasoning_output_tokens":1}}`,
	}
	var names []string
	for _, line := range cases {
		events, recognized := parser.consume(line)
		if !recognized || len(events) != 1 {
			t.Fatalf("expected lifecycle event for %s, recognized=%v events=%#v", line, recognized, events)
		}
		if events[0].kind != "system" {
			t.Fatalf("expected system event, got %#v", events[0])
		}
		names = append(names, events[0].name)
	}
	got := strings.Join(names, "\n")
	for _, want := range []string{"thread thread_1", "turn started", "turn completed (in=10 out=2 reasoning=1)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in lifecycle output:\n%s", want, got)
		}
	}
	if parser.sessionID != "thread_1" {
		t.Fatalf("expected codex session id, got %q", parser.sessionID)
	}
}

func TestCodexParserRendersATMMCPCallAsSystemEvent(t *testing.T) {
	parser := newAgentEventParser("codex")
	events, recognized := parser.consume(`{"type":"item.started","item":{"id":"item_0","type":"mcp_tool_call","server":"atm_output","tool":"atm_report_output","arguments":{"reason":"ok"},"status":"in_progress"}}`)
	if !recognized || len(events) != 1 {
		t.Fatalf("expected ATM output MCP event, recognized=%v events=%#v", recognized, events)
	}
	if events[0].kind != "system" || events[0].tool != "output" || events[0].name != "atm_report_output" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}

func TestClaudeParserRendersSystemThinkingAndResultEvents(t *testing.T) {
	parser := newAgentEventParser("claude")
	lines := []string{
		`{"type":"system","subtype":"init","model":"test-model","session_id":"session_1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"done"}]}}`,
		`{"type":"result","subtype":"success","duration_ms":2500,"usage":{"input_tokens":10,"output_tokens":2},"result":"done"}`,
	}
	var rendered []agentDisplayEvent
	for _, line := range lines {
		events, recognized := parser.consume(line)
		if !recognized {
			t.Fatalf("expected recognized event for %s", line)
		}
		rendered = append(rendered, events...)
	}
	var names []string
	var messages []string
	for _, event := range rendered {
		names = append(names, event.name)
		messages = append(messages, event.text)
	}
	gotNames := strings.Join(names, "\n")
	for _, want := range []string{"init test-model session session_1", "thinking", "success in 2.5s (in=10 out=2)"} {
		if !strings.Contains(gotNames, want) {
			t.Fatalf("expected %q in rendered events:\n%#v", want, rendered)
		}
	}
	if strings.Contains(strings.Join(messages, "\n"), "hidden") {
		t.Fatalf("thinking content must not be rendered: %#v", rendered)
	}
	if len(parser.messages) != 1 || parser.messages[0].Text != "done" {
		t.Fatalf("unexpected parsed messages: %#v", parser.messages)
	}
	if parser.sessionID != "session_1" {
		t.Fatalf("expected claude session id, got %q", parser.sessionID)
	}
}

func TestClaudeParserMarksAPIRetryAsCriticalError(t *testing.T) {
	parser := newAgentEventParser("claude")

	events, recognized := parser.consume(`{"type":"system","subtype":"api_retry","error":"rate_limit","error_status":429,"attempt":1,"max_retries":5,"session_id":"session_1"}`)
	if !recognized || len(events) != 1 {
		t.Fatalf("expected one api retry event, recognized=%v events=%#v", recognized, events)
	}
	if !strings.Contains(events[0].name, "api_retry rate_limit status 429 attempt 1/5") {
		t.Fatalf("unexpected rendered event: %#v", events[0])
	}
	if parser.criticalError == "" || !isRetryableAgentMessage(parser.criticalError) {
		t.Fatalf("expected retryable critical error, got %q", parser.criticalError)
	}
	_, recognized = parser.consume(`{"type":"result","subtype":"error","is_error":true}`)
	if !recognized {
		t.Fatal("expected recognized result event")
	}
	if !strings.Contains(parser.criticalError, "rate_limit") {
		t.Fatalf("generic result error should not replace api retry cause, got %q", parser.criticalError)
	}
	if parser.sessionID != "session_1" {
		t.Fatalf("expected claude session id, got %q", parser.sessionID)
	}
}

func TestRetryableAgentMessageAllowsClaudeServerLimitingText(t *testing.T) {
	message := "API Error: Server is temporarily limiting requests (not your usage limit) · Rate limited"
	if !isRetryableAgentMessage(message) {
		t.Fatalf("expected server limiting message to be retryable")
	}
}

func TestRunAgentCommandRendersCodexToolCallsAndMessages(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeCodex := filepath.Join(dir, "codex")
	script := `#!/bin/sh
printf '{"type":"item.started","item":{"id":"call_1","type":"tool_call","name":"shell"}}\n'
printf '{"type":"item.completed","item":{"id":"call_1","type":"tool_call","name":"shell"}}\n'
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	result, err := runAgentCommand(exec.Command(fakeCodex), "codex", &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.messages) != 1 || result.messages[0].Text != "done" {
		t.Fatalf("unexpected messages: %#v", result.messages)
	}
	rendered := stdout.String()
	if strings.Count(rendered, "shell") != 1 || !strings.Contains(rendered, "[codex]") || !strings.Contains(rendered, "assistant") || !strings.Contains(rendered, "  done") {
		t.Fatalf("unexpected rendered output:\n%s", rendered)
	}
	if !strings.Contains(result.raw, `"type":"tool_call"`) {
		t.Fatalf("expected raw JSONL to be captured, got:\n%s", result.raw)
	}
}

func TestRunAgentCommandMarksCodex429AsRetryable(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeCodex := filepath.Join(dir, "codex")
	script := `#!/bin/sh
printf '{"type":"thread.started","thread_id":"thread_1"}\n'
printf '{"type":"turn.started"}\n'
printf '{"type":"error","message":"exceeded retry limit, last status: 429 Too Many Requests"}\n'
printf '{"type":"turn.failed","error":{"message":"exceeded retry limit, last status: 429 Too Many Requests"}}\n'
exit 1
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runAgentCommand(exec.Command(fakeCodex), "codex", io.Discard, io.Discard)
	if !IsRetryableError(err) {
		t.Fatalf("expected retryable error, got %T %v", err, err)
	}
}

func TestRunAgentCommandMarksClaudeAPIRetryAsRetryable(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '{"type":"system","subtype":"api_retry","error":"rate_limit","error_status":429,"attempt":1,"max_retries":5}\n'
printf '{"type":"result","subtype":"error","is_error":true,"result":"Rate limit exceeded"}\n'
exit 1
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	_, err := runAgentCommand(exec.Command(fakeClaude), "claude", &stdout, io.Discard)
	if !IsRetryableError(err) {
		t.Fatalf("expected retryable error, got %T %v\n%s", err, err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "api_retry rate_limit status 429") {
		t.Fatalf("expected api retry event in output:\n%s", stdout.String())
	}
}

func TestRunAgentCommandRendersClaudeToolCallsAndMessages(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read"},{"type":"text","text":"looked"}]}}\n'
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	result, err := runAgentCommand(exec.Command(fakeClaude), "claude", &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.messages) != 1 || result.messages[0].Text != "looked" {
		t.Fatalf("unexpected messages: %#v", result.messages)
	}
	rendered := stdout.String()
	if !strings.Contains(rendered, "[claude]") || !strings.Contains(rendered, "tool") || !strings.Contains(rendered, "Read") || !strings.Contains(rendered, "assistant") || !strings.Contains(rendered, "  looked") {
		t.Fatalf("unexpected rendered output:\n%s", rendered)
	}
}

func TestClaudeParserRendersATMDBMCPCallAsSystemEvent(t *testing.T) {
	parser := newAgentEventParser("claude")
	events, recognized := parser.consume(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"mcp__atm_db__atm_db_append"}]}}`)
	if !recognized || len(events) != 1 {
		t.Fatalf("expected ATM db MCP event, recognized=%v events=%#v", recognized, events)
	}
	if events[0].kind != "system" || events[0].tool != "db" || events[0].name != "atm_db_append" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}

func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("test requires sh")
	}
}

func samePath(got, want string) bool {
	got = cleanRealPath(got)
	want = cleanRealPath(want)
	return got == want
}

func cleanRealPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}
