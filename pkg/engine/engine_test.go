package engine

import (
	"atm/pkg/dsl"
	"atm/pkg/store"
	"atm/pkg/tools"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu     sync.Mutex
	events []string
	checks int
}

func (r *fakeRunner) Name() string {
	return "fake"
}

func (r *fakeRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (tools.ExecuteResult, error) {
	r.mu.Lock()
	r.events = append(r.events, "start:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	if strings.Contains(prompt, "slow") || strings.Contains(prompt, "parallel") {
		time.Sleep(50 * time.Millisecond)
	}
	r.mu.Lock()
	r.events = append(r.events, "end:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	return tools.ExecuteResult{
		Messages:  []dsl.OutputMessage{{Tool: "fake", Role: "assistant", Text: "done " + strings.TrimSpace(prompt)}},
		RawEvents: `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}` + "\n",
	}, nil
}

func (r *fakeRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks++
	return r.checks >= 2, nil
}

func (r *fakeRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

type workdirRunner struct {
	mu       sync.Mutex
	workdirs []string
	prompts  []string
	checks   int
	writeAt  int
}

type optionsRunner struct {
	mu      sync.Mutex
	options []dsl.RunOptions
}

func (r *optionsRunner) Name() string {
	return "options"
}

func (r *optionsRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (tools.ExecuteResult, error) {
	r.mu.Lock()
	r.options = append(r.options, opts)
	r.mu.Unlock()
	return tools.ExecuteResult{Messages: []dsl.OutputMessage{{Tool: "options", Role: "assistant", Text: "ok"}}}, nil
}

func (r *optionsRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

func (r *optionsRunner) snapshot() []dsl.RunOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]dsl.RunOptions, len(r.options))
	copy(out, r.options)
	return out
}

func (r *workdirRunner) Name() string {
	return "workdir"
}

func (r *workdirRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (tools.ExecuteResult, error) {
	r.mu.Lock()
	r.workdirs = append(r.workdirs, opts.Workdir)
	r.prompts = append(r.prompts, strings.TrimSpace(prompt))
	run := len(r.prompts)
	writeAt := r.writeAt
	r.mu.Unlock()
	if writeAt > 0 && run >= writeAt {
		if err := os.WriteFile(filepath.Join(opts.Workdir, "gate.json"), []byte(`{"passed":true}`), 0o644); err != nil {
			return tools.ExecuteResult{}, err
		}
	}
	return tools.ExecuteResult{Messages: []dsl.OutputMessage{{Tool: "workdir", Role: "assistant", Text: "ok"}}}, nil
}

func (r *workdirRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks++
	r.workdirs = append(r.workdirs, opts.Workdir)
	return true, nil
}

func (r *workdirRunner) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	workdirs := append([]string{}, r.workdirs...)
	prompts := append([]string{}, r.prompts...)
	return workdirs, prompts
}

type structuredRunner struct {
	prompt string
	output *dsl.OutputSpec
}

func (r *structuredRunner) Name() string {
	return "structured"
}

func (r *structuredRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (tools.ExecuteResult, error) {
	r.prompt = prompt
	r.output = opts.Output
	return tools.ExecuteResult{
		Messages:         []dsl.OutputMessage{{Tool: "structured", Role: "assistant", Text: "reported through MCP"}},
		RawEvents:        `{"type":"item.completed","item":{"type":"agent_message","text":"reported"}}` + "\n",
		StructuredOutput: []byte("{\"reason\":\"ok\"}\n"),
	}, nil
}

func (r *structuredRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

type planStructuredRunner struct {
	mu     sync.Mutex
	events []string
}

func (r *planStructuredRunner) Name() string {
	return "plan-structured"
}

func (r *planStructuredRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (tools.ExecuteResult, error) {
	if opts.Output != nil {
		return tools.ExecuteResult{
			Messages:         []dsl.OutputMessage{{Tool: "plan-structured", Role: "assistant", Text: "planned"}},
			StructuredOutput: []byte(`{"plans":["review api and write ./result/api.md","review docs and write ./result/docs.md"]}`),
		}, nil
	}
	r.mu.Lock()
	r.events = append(r.events, "start:"+strings.TrimSpace(prompt))
	r.events = append(r.events, "end:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	return tools.ExecuteResult{Messages: []dsl.OutputMessage{{Tool: "plan-structured", Role: "assistant", Text: "done " + strings.TrimSpace(prompt)}}}, nil
}

func (r *planStructuredRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

func (r *planStructuredRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

type ifCheckRunner struct {
	fakeRunner
	result    bool
	condition string
	prompt    string
}

func (r *ifCheckRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.prompt = strings.TrimSpace(prompt)
	r.condition = condition
	return r.result, nil
}

type celFileRunner struct {
	mu       sync.Mutex
	prompts  []string
	root     string
	passAt   int
	fileName string
}

func (r *celFileRunner) Name() string {
	return "cel-file"
}

func (r *celFileRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (tools.ExecuteResult, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, strings.TrimSpace(prompt))
	run := len(r.prompts)
	r.mu.Unlock()
	if run >= r.passAt {
		if err := os.WriteFile(filepath.Join(r.root, r.fileName), []byte(`{"passed":true}`), 0o644); err != nil {
			return tools.ExecuteResult{}, err
		}
	}
	return tools.ExecuteResult{Messages: []dsl.OutputMessage{{Tool: "cel-file", Role: "assistant", Text: "ok"}}}, nil
}

func (r *celFileRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return false, nil
}

func (r *celFileRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.prompts))
	copy(out, r.prompts)
	return out
}

func TestForBeforeGoStartsOneBackgroundBranchPerLoopItem(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for item in [api docs] /go\nparallel {{item}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	firstEnd := strings.Index(events, "end:")
	if firstEnd < 0 {
		t.Fatalf("expected end events, got %s", events)
	}
	if strings.Count(events[:firstEnd], "start:") != 2 {
		t.Fatalf("expected both branches to start before first end, got:\n%s", events)
	}
}

func TestRunReportsCurrentTaskLineRange(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("\nfirst line\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: &stderr, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "[atm] run task 1 lines 2-3 step 1 via fake") {
		t.Fatalf("expected task line range in stderr, got:\n%s", stderr.String())
	}
}

func TestCdCreatesWorkdirAndRunsAgentThere(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/cd generated/service\nimplement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	workdirs, _ := runner.snapshot()
	want := filepath.Join(dir, "generated", "service")
	if len(workdirs) != 1 || workdirs[0] != want {
		t.Fatalf("expected runner workdir %q, got %#v", want, workdirs)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("expected /cd to create directory, info=%v err=%v", info, err)
	}
}

func TestCdMustExistFailsWithoutStartingRunner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/cd --must-exist missing\nimplement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{}
	err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "directory does not exist") {
		t.Fatalf("expected missing directory error, got %v", err)
	}
	workdirs, prompts := runner.snapshot()
	if len(workdirs) != 0 || len(prompts) != 0 {
		t.Fatalf("runner should not start after failed /cd, workdirs=%#v prompts=%#v", workdirs, prompts)
	}
}

func TestCdRejectsProjectRootEscape(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/cd ../outside\nimplement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &workdirRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "escapes project root") {
		t.Fatalf("expected project-root escape error, got %v", err)
	}
}

func TestCdAppliesToBashAndPrompt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := "/cd app\n/let pwd /bash pwd\nworkspace {{pwd}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	_, prompts := runner.snapshot()
	want := filepath.Join(dir, "app")
	if len(prompts) != 1 || !strings.Contains(prompts[0], "workspace "+want) {
		t.Fatalf("expected /let /bash pwd from %q, prompts=%#v", want, prompts)
	}
}

func TestCdAppliesToCELUntil(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := "/cd app\n/for 3 until(exists(\"gate.json\") && json(\"gate.json\").passed)\nretry {{N}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{writeAt: 2}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	_, prompts := runner.snapshot()
	if got := strings.Join(prompts, "\n"); got != "retry 1\nretry 2" {
		t.Fatalf("expected CEL until to read gate.json from /cd workdir, got:\n%s", got)
	}
}

func TestTaskToolConfigPassesSkillsMCPAndDefsToRunner(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "reviewer")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Reviewer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"/skill new reviewer from skills/reviewer",
		"",
		"/mcp new helper",
		"```json",
		`{"command":"helper","args":["--serve"],"env":{"A":"B"}}`,
		"```",
		"",
		"/def check area",
		"Check {{area}}.",
		"",
		"/return {{agent.last_message}}",
		"",
		"/cd work",
		"/skill use reviewer",
		"/mcp use helper",
		"/mcp def use check",
		"Run main task.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &optionsRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), ToolName: "codex", CodexPath: "codex-test"}); err != nil {
		t.Fatal(err)
	}
	opts := runner.snapshot()
	if len(opts) != 1 {
		t.Fatalf("expected one run, got %d", len(opts))
	}
	got := opts[0]
	if got.Workdir != filepath.Join(dir, "work") {
		t.Fatalf("unexpected workdir %q", got.Workdir)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "reviewer" || got.Skills[0].Path != skillDir {
		t.Fatalf("unexpected skills: %#v", got.Skills)
	}
	if len(got.MCPs) != 1 || got.MCPs[0].Name != "helper" || !strings.Contains(got.MCPs[0].Config, `"helper"`) {
		t.Fatalf("unexpected mcps: %#v", got.MCPs)
	}
	if got.DefMCP == nil || len(got.DefMCP.Definitions) != 1 || got.DefMCP.Definitions[0] != "check" {
		t.Fatalf("unexpected def mcp: %#v", got.DefMCP)
	}
	if got.DefMCP.Workdir != got.Workdir || got.DefMCP.Tool != "codex" || got.DefMCP.CodexPath != "codex-test" {
		t.Fatalf("def mcp did not inherit task config: %#v", got.DefMCP)
	}
}

func TestDefsMCPServerCallsDefinition(t *testing.T) {
	dir := t.TempDir()
	fakeCodex := filepath.Join(dir, "codex")
	script := `#!/bin/sh
cat >/tmp/atm-def-mcp-prompt
printf '{"type":"item.completed","item":{"type":"agent_message","text":"done Check api."}}\n'
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"## /def check area",
		"",
		"Check {{area}}.",
		"",
		"/return {{agent.last_message}}",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	config := dsl.DefMCPRuntime{
		TodoPath:    file,
		Definitions: []string{"check"},
		Defs:        []dsl.DefinitionRef{{Name: "check", Params: []string{"area"}}},
		Tool:        "codex",
		CodexPath:   fakeCodex,
		OutputDir:   filepath.Join(dir, "out"),
		Messages:    1,
		Depth:       1,
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"atm_def_check","arguments":{"area":"api"}}}`,
		"",
	}, "\n")
	var out strings.Builder
	if err := ServeDefsMCP(context.Background(), strings.NewReader(input), &out, io.Discard, config); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, `"name":"atm_def_check"`) {
		t.Fatalf("missing def tool in tools/list:\n%s", text)
	}
	if !strings.Contains(text, `\"returned\":true`) || !strings.Contains(text, `done Check api.`) {
		t.Fatalf("missing definition return in tool result:\n%s", text)
	}
}

func TestDynamicForSourceStartsOneBackgroundBranchPerItem(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "release-plan.json"), []byte(`{"areas":[{"name":"api","owner":"payments"},{"name":"docs","owner":"support"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "todo.txt")
	body := "/pool reviewer 2\n\n/for area in (jsonOutput(\"release-plan.json\").areas) /go reviewer\nparallel {{area.name}} for {{area.owner}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir, GlobalJobs: 2}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "start:parallel api for payments") || !strings.Contains(events, "start:parallel docs for support") {
		t.Fatalf("expected dynamic branch prompts, got:\n%s", events)
	}
	firstEnd := strings.Index(events, "end:")
	if firstEnd < 0 {
		t.Fatalf("expected end events, got %s", events)
	}
	if strings.Count(events[:firstEnd], "start:") != 2 {
		t.Fatalf("expected both dynamic branches to start before first end, got:\n%s", events)
	}
}

func TestDynamicForSourceCanReadCallReturnArray(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"## /def plan",
		"",
		"/return",
		"{\"areas\":[{\"name\":\"api\",\"owner\":\"payments\"},{\"name\":\"docs\",\"owner\":\"support\"}]}",
		"",
		"## //review",
		"",
		"/pool reviewer 2",
		"",
		"/let plan /call plan",
		"/for area in(plan.areas)",
		"/go reviewer",
		"parallel {{area.name}} for {{area.owner}}",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 2}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "start:parallel api for payments") || !strings.Contains(events, "start:parallel docs for support") {
		t.Fatalf("expected call-return dynamic branches, got:\n%s", events)
	}
}

func TestDynamicForSourceCanCallStructuredPlanner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"## /def plan_shards",
		"",
		"Split the work.",
		"",
		"/return",
		"```",
		"plans:[]string:计划",
		"```",
		"",
		"## //review",
		"",
		"/pool reviewer 2",
		"",
		"/for plan in(/call plan_shards)",
		"/go reviewer",
		"{{plan}}",
		"",
		"/wait reviewer",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &planStructuredRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 2}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "start:review api and write ./result/api.md") || !strings.Contains(events, "start:review docs and write ./result/docs.md") {
		t.Fatalf("expected structured planner branches, got:\n%s", events)
	}
}

func TestGlobalJobsLimitsBackgroundBranches(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for item in [api docs] /go\nparallel {{item}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 1}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	firstEnd := strings.Index(events, "end:")
	if firstEnd < 0 {
		t.Fatalf("expected end events, got %s", events)
	}
	if strings.Count(events[:firstEnd], "start:") != 1 {
		t.Fatalf("expected global job limit to serialize branches, got:\n%s", events)
	}
}

func TestNamedPoolLimitsBackgroundBranches(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := "/pool tester 1\n\n/for item in [api docs] /go tester\nparallel {{item}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 4}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	firstEnd := strings.Index(events, "end:")
	if firstEnd < 0 {
		t.Fatalf("expected end events, got %s", events)
	}
	if strings.Count(events[:firstEnd], "start:") != 1 {
		t.Fatalf("expected named pool to serialize branches, got:\n%s", events)
	}
}

func TestNamedWaitOnlyJoinsSelectedPool(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := strings.Join([]string{
		"/pool slowpool 1",
		"/pool fastpool 1",
		"",
		"/go slowpool",
		"slow branch",
		"",
		"/go fastpool",
		"fast branch",
		"",
		"/wait fastpool",
		"after fast",
		"",
		"/wait slowpool",
		"after slow",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 2}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	afterFast := strings.Index(events, "start:after fast")
	slowEnd := strings.Index(events, "end:slow branch")
	if afterFast < 0 || slowEnd < 0 {
		t.Fatalf("expected after-fast and slow-end events, got:\n%s", events)
	}
	if afterFast > slowEnd {
		t.Fatalf("expected /wait fastpool not to wait slowpool, got:\n%s", events)
	}
}

func TestGoBeforeForRunsLoopInsideOneBackgroundBranch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/go /for 2\nslow {{N}}\n\n/wait\n\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:slow 1\nend:slow 1\nstart:slow 2\nend:slow 2\nstart:after") {
		t.Fatalf("expected /wait to join background loop before after task, got:\n%s", got)
	}
}

func TestForUntilChecksAfterEachExecution(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for 3 until complete\nretry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.Join(runner.snapshot(), "\n"), "start:retry"); got != 2 {
		t.Fatalf("expected two executions before condition passed, got %d events=%v", got, runner.snapshot())
	}
}

func TestForUntilCELChecksLocallyAfterEachExecution(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for 5 until(exists(\"gate.json\") && json(\"gate.json\").passed)\nretry {{N}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &celFileRunner{root: dir, passAt: 3, fileName: "gate.json"}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "retry 1\nretry 2\nretry 3") || strings.Contains(got, "retry 4") {
		t.Fatalf("expected CEL loop to stop after local condition passed, got:\n%s", got)
	}
}

func TestUnboundedForUntilCELRunsUntilSatisfied(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for until(json(\"gate.json\").passed)\nretry {{N}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"passed":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &celFileRunner{root: dir, passAt: 2, fileName: "gate.json"}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if got != "retry 1\nretry 2" {
		t.Fatalf("expected unbounded CEL loop to stop at passAt, got:\n%s", got)
	}
}

func TestUnboundedForUntilCELRejectsForBeforeGo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for until(exists(\"never.json\")) /go\nretry {{N}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "cannot launch background branches") {
		t.Fatalf("expected unbounded /for /go error, got %v", err)
	}
}

func TestIfTrueRunsThenAndSkipsElse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "/if (json(\"gate.json\").passed)\nthen branch\n\n/else\nelse branch\n\nafter\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if strings.Contains(got, "else branch") || !strings.Contains(got, "start:then branch") || !strings.Contains(got, "start:after") {
		t.Fatalf("unexpected events:\n%s", got)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(updated)
	if !strings.Contains(content, "> status: skipped\n> time:") || !strings.Contains(content, "> reason: if condition evaluated true") {
		t.Fatalf("expected skipped else block:\n%s", content)
	}
}

func TestIfFalseSkipsThenAndRunsElse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"passed":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "/if (json(\"gate.json\").passed)\nthen branch\n\n/else\nelse branch\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if strings.Contains(got, "then branch") || !strings.Contains(got, "start:else branch") {
		t.Fatalf("unexpected events:\n%s", got)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(updated)
	if !strings.Contains(content, "> reason: if condition evaluated false") {
		t.Fatalf("expected skipped if block:\n%s", content)
	}
}

func TestIfFalseWithoutElseOnlySkipsBlock(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := "/if (false)\nthen branch\n\nafter\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if strings.Contains(got, "then branch") || !strings.Contains(got, "start:after") {
		t.Fatalf("unexpected events:\n%s", got)
	}
}

func TestIfNaturalLanguageUsesCheckRunner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := "/if release gate is open\nthen branch\n\n/else\nelse branch\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &ifCheckRunner{result: true}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if runner.condition != "release gate is open" || runner.prompt != "then branch" {
		t.Fatalf("unexpected check prompt=%q condition=%q", runner.prompt, runner.condition)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:then branch") || strings.Contains(got, "else branch") {
		t.Fatalf("unexpected events:\n%s", got)
	}
}

func TestNestedIfElseUsesNearestPairedElse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := strings.Join([]string{
		"/if (true)",
		"",
		"/if (false)",
		"",
		"inner then",
		"",
		"/else",
		"inner else",
		"",
		"/else",
		"outer else",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if got != "start:inner else\nend:inner else" {
		t.Fatalf("unexpected nested branch execution:\n%s", got)
	}
}

func TestHeaderOnlyNestedIfRequiresMatchingElse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	body := "/if (true)\n\n/if (false)\n\ninner then\n\n/else\nouter else\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "header-only /if requires a matching /else") {
		t.Fatalf("expected matching else error, got %v", err)
	}
}

func TestForGoResultBlockKeepsOneMessagePerBranchByDefault(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("/for item in [api docs] /go\nparallel {{item}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(updated)
	if strings.Count(content, "> [!ATM]") != 1 {
		t.Fatalf("expected one result block, got:\n%s", content)
	}
	if !strings.Contains(content, "> - assistant (fake) [item=api]:") ||
		!strings.Contains(content, "> - assistant (fake) [item=docs]:") {
		t.Fatalf("expected one message per /for /go branch:\n%s", content)
	}
}

func TestOutputDirectoryReceivesEventsAndResultDocument(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	outDir := filepath.Join(dir, "artifacts")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "result.md")); err != nil {
		t.Fatalf("expected result.md: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "task-001-run-001-fake.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected native event stream, got %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"agent_message"`) {
		t.Fatalf("unexpected event stream: %s", data)
	}
}

func TestOutputCommandConstrainsPromptAndWritesStructuredArtifact(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	outDir := filepath.Join(dir, "artifacts")
	body := "/output summary\n```\nreason:string:why this passed\n```\nExplain the result.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &structuredRunner{}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: &stderr, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	if runner.output == nil || !strings.Contains(runner.output.Schema, `"reason"`) {
		t.Fatalf("expected structured output spec passed to runner: %#v", runner.output)
	}
	if strings.Contains(runner.prompt, `"reason"`) {
		t.Fatalf("did not expect schema injected into prompt:\n%s", runner.prompt)
	}
	outputPath := filepath.Join(outDir, "summary.json")
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("expected structured output file: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(string(data), `"reason":"ok"`) {
		t.Fatalf("unexpected structured output:\n%s", data)
	}
	if !strings.Contains(stderr.String(), "[atm] output task 1 run 1 "+outputPath) {
		t.Fatalf("expected structured output path in log:\n%s", stderr.String())
	}
}

func TestOutputCommandWithoutSchemaWritesLatestMessage(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	outDir := filepath.Join(dir, "artifacts")
	body := "/output summary\nExplain the result.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "done Explain the result.") {
		t.Fatalf("unexpected text output: %s", data)
	}
}

func TestOutputCommandInGoBranchAddsSuffixAndRendersAgentVars(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	outDir := filepath.Join(dir, "artifacts")
	body := "/go\n/output summary-{{agent_index}}\n```\nreason:string:why this passed\n```\nExplain the result.\n\n/wait\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &structuredRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(outDir, "summary-1-agent-1.json")
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected branch-suffixed structured output file: %v", err)
	}
}

func TestInlineCallEmbedsReturnBeforeOuterPromptRuns(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := "## /def whereami\n\nReturn only the city.\n\n/return {{agent.last_message}}\n\n## /weather\n\nWeather for\n/call whereami\ntoday.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:Weather for\ndone Return only the city.\ntoday.") {
		t.Fatalf("expected inline call return embedded in outer prompt, got:\n%s", got)
	}
}

func TestLetCallBindsReturnValue(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := "## /def whereami\n\nReturn city.\n\n/return Paris\n\n## /weather\n\n/let city /call whereami\nWeather for {{city}}.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:Weather for Paris.") {
		t.Fatalf("expected /let /call value in prompt, got:\n%s", got)
	}
}

func TestForCarriesAssignedVariablesToNextRun(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"## /def add text n",
		"",
		"/return {{text}}{{n}}",
		"",
		"## /collect",
		"",
		"/let transcript start",
		"/for 3",
		"/let transcript /call add {{transcript}} {{N}}",
		"{{transcript}}",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:start1\nend:start1\nstart:start12\nend:start12\nstart:start123") {
		t.Fatalf("expected /for to carry assigned variables between runs, got:\n%s", got)
	}
}

func TestCallFallsBackToStructuredOutputReturn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := "## /def check\n\n/output result\n```\nreason:string:why\n```\nCheck it.\n\n## /use\n\n/let result /call check\nReason: {{result.reason}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &structuredRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.prompt, "Reason: ok") {
		t.Fatalf("expected structured output return field in outer prompt, got:\n%s", runner.prompt)
	}
}

func TestCallReturnsStructuredReturn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"## /def gate",
		"",
		"/return",
		"```json",
		"{\"type\":\"object\"}",
		"```",
		"Assess gate.",
		"",
		"## /use",
		"",
		"/let gate /call gate",
		"Reason: {{gate.reason}}",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &structuredRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.prompt, "Reason: ok") {
		t.Fatalf("expected structured return field in outer prompt, got:\n%s", runner.prompt)
	}
}

func TestListDefinitionSupportsLocalPoolGoWaitAndMultilineReturn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.md")
	body := strings.Join([]string{
		"## //def reviews area",
		"",
		"/pool reviewer 2",
		"",
		"/go reviewer",
		"parallel {{area}} implementation",
		"",
		"/go reviewer",
		"parallel {{area}} docs",
		"",
		"/wait reviewer",
		"",
		"/return",
		"Review {{area}} done.",
		"Last: {{agent.last_message}}",
		"",
		"## /summary",
		"",
		"/let review /call reviews checkout",
		"Summary: {{review}}",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 4}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:Summary: Review checkout done.") || !strings.Contains(got, "Last: done parallel checkout") {
		t.Fatalf("expected multiline return from list definition, got:\n%s", got)
	}
}

func TestImportDefinitionWithNamespace(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.todo.md")
	if err := os.WriteFile(lib, []byte("## /def city\n\n/return Paris\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "todo.txt")
	body := "/import loc from lib.todo.md\n\n/let city /call loc.city\nWeather for {{city}}.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:Weather for Paris.") {
		t.Fatalf("expected imported definition call, got:\n%s", got)
	}
}

func TestAppendDuringRunningTaskIsPickedUpByCurrentRun(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "todo.txt")
	if err := os.WriteFile(file, []byte("slow first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	}()

	waitForEvent(t, runner, "start:slow first")
	active, err := store.ResolveActiveTodoPath(file)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(active, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nsecond\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, runner, "end:second")
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not exit after appended task completed")
	}
}

func waitForEvent(t *testing.T, runner *fakeRunner, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(strings.Join(runner.snapshot(), "\n"), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q, events=%v", want, runner.snapshot())
}
