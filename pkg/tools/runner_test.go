package tools

import (
	"atm/pkg/dsl"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	passed, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "fix tests", "tests pass", dsl.RunOptions{}, io.Discard, io.Discard)
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
	passed, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "fix tests", "tests pass", dsl.RunOptions{}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected missing MCP result error")
	}
	if passed {
		t.Fatal("expected check to fail without MCP result")
	}
}

func TestCodexCheckArgsAddTemporaryMCPServerConfig(t *testing.T) {
	args := strings.Join(codexCheckArgs(dsl.RunOptions{Args: []string{"--json"}}, "/tmp/result.json"), "\n")
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

	args := strings.Join(codexCheckArgs(dsl.RunOptions{}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, `mcp_servers.atm_check.env.ATM_MCP_CHECK_LOG="/tmp/atm-mcp.log"`) {
		t.Fatalf("missing debug log env config:\n%s", args)
	}
}

func TestCodexExecuteArgsIncludeExternalAndDefsMCP(t *testing.T) {
	opts := dsl.RunOptions{
		MCPs:   []dsl.MCPRuntime{{Name: "helper", Config: `{"command":"helper","args":["--serve"],"env":{"A":"B"}}`}},
		DefMCP: &dsl.DefMCPRuntime{Definitions: []string{"echo"}},
	}
	args := strings.Join(codexArgs(opts, "", "", "", "/tmp/defs.json", false), "\n")
	if !strings.Contains(args, `mcp_servers.helper.command="helper"`) ||
		!strings.Contains(args, `mcp_servers.helper.args=["--serve"]`) ||
		!strings.Contains(args, `mcp_servers.helper.env.A="B"`) {
		t.Fatalf("missing external mcp config:\n%s", args)
	}
	if !strings.Contains(args, "mcp_servers.atm_defs.command=") ||
		!strings.Contains(args, `mcp_servers.atm_defs.args=["mcp", "defs", "-config-file", "/tmp/defs.json"]`) {
		t.Fatalf("missing defs mcp config:\n%s", args)
	}
	if !strings.Contains(args, `mcp_servers.atm_defs.tools.atm_def_echo.approval_mode="approve"`) {
		t.Fatalf("missing defs mcp approval config:\n%s", args)
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
	result, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "report", dsl.RunOptions{Output: &dsl.OutputSpec{Schema: `{"type":"object"}`, SchemaFormat: "json"}}, io.Discard, io.Discard)
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
	if _, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "report", dsl.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != workdir {
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
	_, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "run", dsl.RunOptions{
		Workdir: workdir,
		Skills:  []dsl.SkillRuntime{{Name: "reviewer", Path: skillDir}},
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
	if _, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "report", "ok", dsl.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != workdir {
		t.Fatalf("expected codex check workdir %q, got %q", workdir, strings.TrimSpace(string(got)))
	}
}

func TestCodexOutputArgsAddTemporaryMCPServerConfig(t *testing.T) {
	args := strings.Join(codexOutputMCPArgs(&dsl.OutputSpec{SchemaFormat: "json"}, "/tmp/out.json", "/tmp/schema.json"), "\n")
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
	passed, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "fix tests", "tests pass", dsl.RunOptions{}, io.Discard, io.Discard)
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
	args := strings.Join(claudeCheckArgs("check prompt", dsl.RunOptions{Args: []string{"--output-format", "json"}}, "/tmp/result.json"), "\n")
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

func TestClaudeCheckArgsPassDebugLogEnvToMCPServer(t *testing.T) {
	t.Setenv("ATM_MCP_CHECK_LOG", "/tmp/atm-mcp.log")

	args := strings.Join(claudeCheckArgs("check prompt", dsl.RunOptions{}, "/tmp/result.json"), "\n")
	if !strings.Contains(args, `"env":{"ATM_MCP_CHECK_LOG":"/tmp/atm-mcp.log"}`) {
		t.Fatalf("missing debug log env config:\n%s", args)
	}
}

func TestClaudeExecuteAllowsTemporaryMCPTools(t *testing.T) {
	args := strings.Join(claudeArgs("prompt", dsl.RunOptions{
		Output: &dsl.OutputSpec{Schema: `{"type":"object"}`, SchemaFormat: "json", Structured: true},
		DefMCP: &dsl.DefMCPRuntime{
			Definitions: []string{"echo"},
		},
	}, "/tmp/out.json", "/tmp/schema.json", "", "/tmp/defs.json", false), "\n")
	if !strings.Contains(args, "--allowedTools") ||
		!strings.Contains(args, "mcp__atm_output__atm_report_output") ||
		!strings.Contains(args, "mcp__atm_defs__atm_def_echo") {
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
	if _, err := runner.Execute(context.Background(), filepath.Join(dir, "todo.txt"), "report", dsl.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != workdir {
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
	if _, err := runner.Check(context.Background(), filepath.Join(dir, "todo.txt"), "report", "ok", dsl.RunOptions{Workdir: workdir}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(pwdLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != workdir {
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

func TestParseCodexMessagesFromJSONL(t *testing.T) {
	output := `{"type":"thread.started","thread_id":"t"}
{"type":"item.completed","item":{"type":"agent_message","text":"first"}}
{"type":"item.completed","item":{"type":"agent_message","text":"second"}}
`
	messages := parseCodexMessages(output)
	if len(messages) != 2 || messages[1].Text != "second" || messages[1].Tool != "codex" {
		t.Fatalf("unexpected messages: %#v", messages)
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

func TestParseClaudeMessagesFromStreamJSON(t *testing.T) {
	output := `{"type":"system","subtype":"init"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}
{"type":"result","result":"hello"}
`
	messages := parseClaudeMessages(output)
	if len(messages) != 1 || messages[0].Text != "hello" || messages[0].Tool != "claude" {
		t.Fatalf("unexpected messages: %#v", messages)
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
