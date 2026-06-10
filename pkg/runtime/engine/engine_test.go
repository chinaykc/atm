package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/runtime/store"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeRunner struct {
	mu     sync.Mutex
	events []string
	checks int
}

func (r *fakeRunner) Name() string {
	return "fake"
}

func (r *fakeRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	if opts.Resume {
		r.events = append(r.events, "resume:"+opts.ResumeSessionID)
	}
	if opts.Fork {
		r.events = append(r.events, "fork:"+opts.ResumeSessionID)
	}
	r.events = append(r.events, "start:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	if strings.Contains(prompt, "slow") || strings.Contains(prompt, "parallel") {
		time.Sleep(50 * time.Millisecond)
	}
	r.mu.Lock()
	r.events = append(r.events, "end:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	return agent.ExecuteResult{
		Messages:  []compiler.OutputMessage{{Tool: "fake", Role: "assistant", Text: "done " + strings.TrimSpace(prompt)}},
		RawEvents: `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}` + "\n",
		SessionID: "session-" + strings.ReplaceAll(strings.TrimSpace(prompt), " ", "-"),
	}, nil
}

func (r *fakeRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if opts.Resume {
		r.events = append(r.events, "check-resume:"+opts.ResumeSessionID)
	}
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

type failingRunner struct{}

func (r *failingRunner) Name() string {
	return "fail"
}

func (r *failingRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	return agent.ExecuteResult{}, fmt.Errorf("simulated failure")
}

func (r *failingRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return false, fmt.Errorf("simulated check failure")
}

type promptFailRunner struct {
	mu     sync.Mutex
	events []string
}

func (r *promptFailRunner) Name() string {
	return "prompt-fail"
}

func (r *promptFailRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	r.events = append(r.events, "start:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	if strings.Contains(prompt, "fail branch") {
		return agent.ExecuteResult{}, fmt.Errorf("simulated branch failure")
	}
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "prompt-fail", Role: "assistant", Text: "done"}}}, nil
}

func (r *promptFailRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

func (r *promptFailRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

type retryableRunner struct {
	mu             sync.Mutex
	executeCalls   int
	checkCalls     int
	failExecUntil  int
	failCheckUntil int
}

func (r *retryableRunner) Name() string {
	return "retryable"
}

func (r *retryableRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	r.executeCalls++
	call := r.executeCalls
	failUntil := r.failExecUntil
	r.mu.Unlock()
	if call <= failUntil {
		return agent.ExecuteResult{}, agent.NewRetryableError("exceeded retry limit, last status: 429 Too Many Requests")
	}
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "retryable", Role: "assistant", Text: "done"}}}, nil
}

func (r *retryableRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.mu.Lock()
	r.checkCalls++
	call := r.checkCalls
	failUntil := r.failCheckUntil
	r.mu.Unlock()
	if call <= failUntil {
		return false, agent.NewRetryableError("exceeded retry limit, last status: 429 Too Many Requests")
	}
	return true, nil
}

func (r *retryableRunner) counts() (execute, check int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.executeCalls, r.checkCalls
}

type deletingRunner struct{}

func (r *deletingRunner) Name() string {
	return "delete"
}

func (r *deletingRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	if err := os.WriteFile(todoPath, nil, 0o644); err != nil {
		return agent.ExecuteResult{}, err
	}
	return agent.ExecuteResult{
		Messages:  []compiler.OutputMessage{{Tool: "delete", Role: "assistant", Text: "completed after deletion"}},
		RawEvents: `{"type":"item.completed","item":{"type":"agent_message","text":"completed after deletion"}}` + "\n",
	}, nil
}

func (r *deletingRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

type blockingRunner struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	once     sync.Once
}

func (r *blockingRunner) Name() string {
	return "blocking"
}

func (r *blockingRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.once.Do(func() { close(r.started) })
	defer close(r.finished)
	select {
	case <-r.release:
	case <-ctx.Done():
		return agent.ExecuteResult{}, ctx.Err()
	}
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "blocking", Role: "assistant", Text: "done"}}}, nil
}

func (r *blockingRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
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
	options []compiler.RunOptions
}

func (r *optionsRunner) Name() string {
	return "options"
}

func (r *optionsRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	r.options = append(r.options, opts)
	r.mu.Unlock()
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "options", Role: "assistant", Text: "ok"}}}, nil
}

func (r *optionsRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

func (r *optionsRunner) snapshot() []compiler.RunOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]compiler.RunOptions, len(r.options))
	copy(out, r.options)
	return out
}

func (r *workdirRunner) Name() string {
	return "workdir"
}

func (r *workdirRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	r.workdirs = append(r.workdirs, opts.Workdir)
	r.prompts = append(r.prompts, strings.TrimSpace(prompt))
	run := len(r.prompts)
	writeAt := r.writeAt
	r.mu.Unlock()
	if writeAt > 0 && run >= writeAt {
		if err := os.WriteFile(filepath.Join(opts.Workdir, "gate.json"), []byte(`{"passed":true}`), 0o644); err != nil {
			return agent.ExecuteResult{}, err
		}
	}
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "workdir", Role: "assistant", Text: "ok"}}}, nil
}

func (r *workdirRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks++
	r.workdirs = append(r.workdirs, opts.Workdir)
	return true, nil
}

func (r *workdirRunner) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	workdirs := slices.Clone(r.workdirs)
	prompts := slices.Clone(r.prompts)
	return workdirs, prompts
}

type structuredRunner struct {
	prompt string
	output *compiler.OutputSpec
}

func (r *structuredRunner) Name() string {
	return "structured"
}

func (r *structuredRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.prompt = prompt
	r.output = opts.Output
	return agent.ExecuteResult{
		Messages:         []compiler.OutputMessage{{Tool: "structured", Role: "assistant", Text: "reported through MCP"}},
		RawEvents:        `{"type":"item.completed","item":{"type":"agent_message","text":"reported"}}` + "\n",
		StructuredOutput: []byte("{\"reason\":\"ok\"}\n"),
	}, nil
}

func (r *structuredRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

type planStructuredRunner struct {
	mu     sync.Mutex
	events []string
}

func (r *planStructuredRunner) Name() string {
	return "plan-structured"
}

func (r *planStructuredRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	if opts.Output != nil {
		return agent.ExecuteResult{
			Messages:         []compiler.OutputMessage{{Tool: "plan-structured", Role: "assistant", Text: "planned"}},
			StructuredOutput: []byte(`{"plans":["review api and write ./result/api.md","review docs and write ./result/docs.md"]}`),
		}, nil
	}
	r.mu.Lock()
	r.events = append(r.events, "start:"+strings.TrimSpace(prompt))
	r.events = append(r.events, "end:"+strings.TrimSpace(prompt))
	r.mu.Unlock()
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "plan-structured", Role: "assistant", Text: "done " + strings.TrimSpace(prompt)}}}, nil
}

func (r *planStructuredRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

func (r *planStructuredRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

type todoWorkflowRunner struct {
	mu      sync.Mutex
	prompts []string
}

func (r *todoWorkflowRunner) Name() string {
	return "todo-workflow"
}

func (r *todoWorkflowRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, strings.TrimSpace(prompt))
	r.mu.Unlock()
	result := agent.ExecuteResult{
		Messages:  []compiler.OutputMessage{{Tool: "todo-workflow", Role: "assistant", Text: "ok"}},
		RawEvents: `{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}` + "\n",
	}
	if opts.Output != nil {
		result.StructuredOutput = []byte(`{"passed":true,"reason":"ok","open_p0_p1":[],"missing_evidence":[],"next_actions":[]}`)
	}
	return result, nil
}

func (r *todoWorkflowRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return true, nil
}

func (r *todoWorkflowRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.prompts))
	copy(out, r.prompts)
	return out
}

type ifCheckRunner struct {
	fakeRunner
	result    bool
	condition string
	prompt    string
}

func (r *ifCheckRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	r.prompt = strings.TrimSpace(prompt)
	r.condition = condition
	return r.result, nil
}

type exprFileRunner struct {
	mu       sync.Mutex
	prompts  []string
	root     string
	passAt   int
	fileName string
}

func (r *exprFileRunner) Name() string {
	return "expr-file"
}

func (r *exprFileRunner) Execute(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, strings.TrimSpace(prompt))
	run := len(r.prompts)
	r.mu.Unlock()
	if run >= r.passAt {
		if err := os.WriteFile(filepath.Join(r.root, r.fileName), []byte(`{"passed":true}`), 0o644); err != nil {
			return agent.ExecuteResult{}, err
		}
	}
	return agent.ExecuteResult{Messages: []compiler.OutputMessage{{Tool: "expr-file", Role: "assistant", Text: "ok"}}}, nil
}

func (r *exprFileRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return false, nil
}

func (r *exprFileRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.prompts))
	copy(out, r.prompts)
	return out
}

func TestForBeforeGoStartsOneBackgroundBranchPerLoopItem(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for item in [api docs] /go\nparallel {{item}}\n\n/wait\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 2}); err != nil {
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

func TestGoWithoutWaitDoesNotJoinBeforeExit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/go\nslow background\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		close(runner.release)
		t.Fatal("expected run to exit without waiting for background task")
	}
	select {
	case <-runner.started:
	case <-time.After(200 * time.Millisecond):
		close(runner.release)
		t.Fatal("expected background task to start")
	}

	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(updated)
	if !strings.Contains(content, "> status: running") || strings.Contains(content, "> status: done") {
		t.Fatalf("expected abandoned background task to remain running, got:\n%s", content)
	}
	close(runner.release)
	select {
	case <-runner.finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected background task to finish after release")
	}
}

func TestRunReportsCurrentTaskLineRange(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("\n/task\nfirst line\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: &stderr, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "[atm] run task 1 lines 2-4 step 1 via fake") {
		t.Fatalf("expected task line range in stderr, got:\n%s", stderr.String())
	}
}

func TestCdCreatesWorkdirAndRunsAgentThere(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
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

func TestCdUsesConfiguredWorkdirRoot(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run", "work")
	projectDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(runDir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/cd --must-exist app\nimplement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{}
	if err := Run(context.Background(), Options{FilePath: file, WorkdirRoot: projectDir, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	workdirs, _ := runner.snapshot()
	want := filepath.Join(projectDir, "app")
	if len(workdirs) != 1 || workdirs[0] != want {
		t.Fatalf("expected runner workdir %q, got %#v", want, workdirs)
	}
}

func TestCdMustExistFailsWithoutStartingRunner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
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
	file := filepath.Join(dir, "taskdoc.txt")
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
	if err := os.Mkdir(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "marker.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/cd app\n/let marker /bash cat marker.txt\nworkspace {{marker}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	_, prompts := runner.snapshot()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "workspace ok") {
		t.Fatalf("expected /let /bash to read from /cd workdir, prompts=%#v", prompts)
	}
}

func TestCdAppliesToExprUntil(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/cd app\n/for 3 until(exist(\"gate.json\") && json(open(\"gate.json\")).passed)\nretry {{n}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &workdirRunner{writeAt: 2}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	_, prompts := runner.snapshot()
	if got := strings.Join(prompts, "\n"); got != "retry 0\nretry 1" {
		t.Fatalf("expected expression until to read gate.json from /cd workdir, got:\n%s", got)
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
	file := filepath.Join(dir, "taskdoc.md")
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

func TestDefinitionTaskWebhookUsePassesMCPToRunner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/webhook new alarm provider:dingtalk url:env:DINGTALK_WEBHOOK secret:env:DINGTALK_SECRET",
		"",
		"/def scan",
		"/webhook use alarm",
		"Call the webhook with ok.",
		"/return ok",
		"",
		"/call scan",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &optionsRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	opts := runner.snapshot()
	if len(opts) != 1 {
		t.Fatalf("expected one definition task run, got %d", len(opts))
	}
	var webhook compiler.MCPRuntime
	for _, item := range opts[0].MCPs {
		if item.Name == "atm_webhook" {
			webhook = item
			break
		}
	}
	if webhook.Name == "" {
		t.Fatalf("missing atm_webhook MCP in definition task options: %#v", opts[0].MCPs)
	}
	if !slices.Contains(webhook.ApprovedTools, "atm_webhook_alarm") {
		t.Fatalf("missing approved webhook tool: %#v", webhook.ApprovedTools)
	}
	if !strings.Contains(webhook.Config, `"mcp"`) || !strings.Contains(webhook.Config, `"webhook"`) {
		t.Fatalf("unexpected webhook MCP config: %s", webhook.Config)
	}
}

func TestDefsMCPServerCallsDefinition(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/def check area",
		"",
		"Check {{area}}.",
		"",
		"/return {{agent.last_message}}",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	config := compiler.DefMCPRuntime{
		TodoPath:    file,
		Definitions: []string{"check"},
		Defs:        []compiler.DefinitionRef{{Name: "check", Params: []string{"area"}}},
		Tool:        "codex",
		OutputDir:   filepath.Join(dir, "out"),
		Messages:    1,
		Depth:       1,
	}
	eng, err := New(Options{
		FilePath:     config.TodoPath,
		Runner:       &fakeRunner{},
		ToolName:     config.Tool,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		MessageLimit: config.Messages,
		OutputDir:    config.OutputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.loadDefinitions(config.TodoPath); err != nil {
		t.Fatal(err)
	}
	if err := eng.loadGlobalDeclarations(); err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := defsMCPServer{engine: eng, config: config, stderr: io.Discard}.mcpServer().Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "atm-test", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	list, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "atm_def_check" {
		t.Fatalf("missing def tool in tools/list: %#v", list.Tools)
	}
	result, err := clientSession.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "atm_def_check",
		Arguments: map[string]any{"area": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) == 0 {
		t.Fatal("missing definition return")
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected text result, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, `"returned":true`) || !strings.Contains(text.Text, `done Check api.`) {
		t.Fatalf("missing definition return in tool result:\n%s", text.Text)
	}
	_, err = clientSession.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "atm_def_check",
		Arguments: map[string]any{"area": "api", "extra": "ignored"},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown argument "extra"`) {
		t.Fatalf("expected unknown argument error, got %v", err)
	}
}

func TestDefsMCPConfigRejectsUnknownFields(t *testing.T) {
	_, err := parseDefsMCPConfig([]byte(`{"todo_path":"todo.md","definitions":[],"tool":"codex","depth":1,"definitionz":[]}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "definitionz"`) {
		t.Fatalf("expected unknown field error, got %v", err)
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
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/pool reviewer 2\n\n/for area in (json(open(outputDir(\"release-plan.json\"))).areas) /go reviewer\nparallel {{area.name}} for {{area.owner}}\n\n/wait reviewer\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir, GlobalJobs: 2}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "parallel api for payments") || !strings.Contains(events, "parallel docs for support") {
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

func TestDynamicForSourceCanUseRangeHelper(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for shard in(range(1, 4))\nReview shard {{shard}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	for _, shard := range []string{"1", "2", "3"} {
		if !strings.Contains(events, "Review shard "+shard+".") {
			t.Fatalf("expected range shard %s prompt, got:\n%s", shard, events)
		}
	}
}

func TestDynamicForSourceEmptySequenceWarnsAndSkips(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for shard in(range(0))\nReview shard {{shard}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: &stderr, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.snapshot(), "\n"); got != "" {
		t.Fatalf("expected no runner executions, got:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "warning") || !strings.Contains(stderr.String(), "empty sequence") {
		t.Fatalf("expected empty sequence warning, got:\n%s", stderr.String())
	}
}

func TestDynamicForSourceCanUseFilesHelper(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api", "server.go"), []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for file in(walkFiles())\nReview {{file}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "Review README.md.") || !strings.Contains(events, "Review api/server.go.") {
		t.Fatalf("expected walkFiles() prompts, got:\n%s", events)
	}
}

func TestDynamicForSourceCanReadCallReturnArray(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/def plan",
		"",
		"/return {\"areas\":[{\"name\":\"api\",\"owner\":\"payments\"},{\"name\":\"docs\",\"owner\":\"support\"}]}",
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
		"/wait reviewer",
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
	if !strings.Contains(events, "parallel api for payments") || !strings.Contains(events, "parallel docs for support") {
		t.Fatalf("expected call-return dynamic branches, got:\n%s", events)
	}
}

func TestDynamicForSourceCanCallStructuredPlanner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/def plan_shards",
		"",
		"Split the work.",
		"",
		"/return",
		"```schema",
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
	if !strings.Contains(events, "review api and write ./result/api.md") || !strings.Contains(events, "review docs and write ./result/docs.md") {
		t.Fatalf("expected structured planner branches, got:\n%s", events)
	}
}

func TestGlobalJobsLimitsBackgroundBranches(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for item in [api docs] /go\nparallel {{item}}\n\n/wait\n"), 0o644); err != nil {
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
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/pool tester 1\n\n/for item in [api docs] /go tester\nparallel {{item}}\n\n/wait tester\n"
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
	file := filepath.Join(dir, "taskdoc.txt")
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
	afterFast := strings.Index(events, "after fast")
	slowEnd := strings.Index(events, "end:slow branch")
	if afterFast < 0 || slowEnd < 0 {
		t.Fatalf("expected after-fast and slow-end events, got:\n%s", events)
	}
	if afterFast > slowEnd {
		t.Fatalf("expected /wait fastpool not to wait slowpool, got:\n%s", events)
	}
	for _, want := range []string{
		"Waited for pool: fastpool.",
		"Completed wait objects:",
		"block 4, pool fastpool, status done",
		"task-004-",
	} {
		if !strings.Contains(events, want) {
			t.Fatalf("expected wait prompt to contain %q, got:\n%s", want, events)
		}
	}
	if !strings.Contains(events, "visible report:") {
		t.Fatalf("expected wait prompt to receive wait result context, got:\n%s", events)
	}
}

func TestWaitWithPromptRunsAfterFailedBackgroundBranch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := strings.Join([]string{
		"/go",
		"fail branch",
		"",
		"/wait",
		"Summarize failures.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &promptFailRunner{}
	err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 2})
	if err == nil || !strings.Contains(err.Error(), "simulated branch failure") {
		t.Fatalf("expected wait failure after summary prompt, got %v", err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	for _, want := range []string{
		"start:fail branch",
		"ATM wait result context.",
		"status failed",
		"error: task 1 run failed: simulated branch failure",
		"Prompt:\nSummarize failures.",
	} {
		if !strings.Contains(events, want) {
			t.Fatalf("expected wait summary prompt to contain %q, got:\n%s", want, events)
		}
	}
}

func TestPoolScopeAtRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/pool scoped 1",
		"",
		"## B",
		"",
		"/go scoped",
		"Should not run.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), `undeclared pool "scoped"`) {
		t.Fatalf("expected runtime scoped pool error, got %v", err)
	}
}

func TestMCPScopeAtRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/mcp new helper",
		"```json",
		`{"command":"helper"}`,
		"```",
		"",
		"## B",
		"",
		"/mcp use helper",
		"Should not run.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), `unknown mcp "helper"`) {
		t.Fatalf("expected runtime scoped mcp error, got %v", err)
	}
}

func TestDBScopeAtRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/db new board scope:global persist:run access:append",
		"Scoped board.",
		"",
		"## B",
		"",
		"/db access board read",
		"Should not run.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), `unavailable db "board"`) {
		t.Fatalf("expected runtime scoped db error, got %v", err)
	}
}

func TestGoBeforeForRunsLoopInsideOneBackgroundBranch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/go /for 2\nslow {{n}}\n\n/wait\n\n/task\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:slow 0\nend:slow 0\nstart:slow 1\nend:slow 1\nstart:after") {
		t.Fatalf("expected /wait to join background loop before after task, got:\n%s", got)
	}
}

func TestForUntilChecksAfterEachExecution(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
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

func TestForUntilExprChecksLocallyAfterEachExecution(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for 5 until(exist(\"gate.json\") && json(open(\"gate.json\")).passed)\nretry {{n}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &exprFileRunner{root: dir, passAt: 3, fileName: "gate.json"}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "retry 0\nretry 1\nretry 2") || strings.Contains(got, "retry 3") {
		t.Fatalf("expected expression loop to stop after local condition passed, got:\n%s", got)
	}
}

func TestUnboundedForUntilExprRunsUntilSatisfied(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for until(json(open(\"gate.json\")).passed)\nretry {{n}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"passed":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &exprFileRunner{root: dir, passAt: 2, fileName: "gate.json"}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if got != "retry 0\nretry 1" {
		t.Fatalf("expected unbounded expression loop to stop at passAt, got:\n%s", got)
	}
}

func TestUnboundedForUntilExprRejectsForBeforeGo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for until(exist(\"never.json\")) /go\nretry {{n}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "cannot launch background branches") {
		t.Fatalf("expected unbounded /for /go error, got %v", err)
	}
}

func TestIfTrueRunsThenAndSkipsElse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "/if (json(open(\"gate.json\")).passed)\nthen branch\n\n/else\nelse branch\n\n/task\nafter\n"
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
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"passed":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "/if (json(open(\"gate.json\")).passed)\nthen branch\n\n/else\nelse branch\n"
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
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/if (false)\nthen branch\n\n/task\nafter\n"
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

func TestIfFalseWithEmptyElseIsNoop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/if (false)\nthen branch\n\n/else\n\n/task\nafter\n"
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
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(updated)
	if !strings.Contains(content, "\n/else\n") || !strings.Contains(content, "> status: done") {
		t.Fatalf("expected empty else to be marked done:\n%s", content)
	}
}

func TestTopLevelReturnIsRejected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/return done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "/return is only allowed inside /def") {
		t.Fatalf("expected top-level /return rejection, got %v", err)
	}
}

func TestChildHeadingTasksRunBeforeParentAndFeedParentPrompt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := `# Review

/task
Review backend.

### Scope1

API and migrations.

/task
Fix API.

### Scope2

Docs.

/task
Fix docs.
`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := runner.snapshot()
	if len(events) < 6 {
		t.Fatalf("expected child and parent runs, got:\n%s", strings.Join(events, "\n"))
	}
	got := strings.Join(events, "\n")
	firstChild := strings.Index(got, "start:# Review\n\nReview backend.\n\n### Scope1")
	secondChild := strings.Index(got, "start:# Review\n\nReview backend.\n\n### Scope2")
	parent := strings.LastIndex(got, "start:# Review\n\n## Completed child task reports")
	if firstChild < 0 || secondChild < 0 || parent < 0 {
		t.Fatalf("expected two child prompts followed by parent prompt with child reports, got:\n%s", got)
	}
	if !(firstChild < parent && secondChild < parent) {
		t.Fatalf("expected child tasks to run before parent, got:\n%s", got)
	}
	parentPrompt := got[parent:]
	for _, want := range []string{"> [!ATM]", "done # Review", "Fix API.", "Fix docs."} {
		if !strings.Contains(parentPrompt, want) {
			t.Fatalf("expected parent prompt to include child report text %q, got:\n%s", want, parentPrompt)
		}
	}
}

func TestChildHeadingTaskInheritsParentHeaderLetAtRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := `# Review

/task
/let area backend
Review {{area}}.

### Scope

/task
Fix {{area}} tests.
`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "Fix backend tests.") {
		t.Fatalf("expected child prompt to inherit parent header /let, got:\n%s", got)
	}
}

func TestChildHeadingTaskInheritsParentLazyLetAtRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := `# Review

/task
/let area /bash printf backend
Review {{area}}.

### Scope

/task
Fix {{area}} tests.
`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "Fix backend tests.") {
		t.Fatalf("expected child prompt to resolve inherited parent lazy /let, got:\n%s", got)
	}
}

func TestChildHeadingTaskInheritsParentLazyCallWithScopedDefinitionAtRuntime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"# Review",
		"",
		"## Backend",
		"",
		"/def area_name",
		"/return",
		"```",
		"backend",
		"```",
		"",
		"/task",
		"/let area /call area_name",
		"Review {{area}}.",
		"",
		"### Scope",
		"",
		"/task",
		"Fix {{area}} tests.",
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
	if !strings.Contains(got, "Fix backend tests.") {
		t.Fatalf("expected child prompt to resolve inherited parent lazy /call in parent scope, got:\n%s", got)
	}
}

func TestIfNaturalLanguageUsesCheckRunner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
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

func TestForIfRunsOnlyMatchingIterations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/for 4 /if(n % 2 == 0)\nshard {{n}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:shard 0") || !strings.Contains(got, "start:shard 2") {
		t.Fatalf("expected even shards to run, got:\n%s", got)
	}
	if strings.Contains(got, "shard 1") || strings.Contains(got, "shard 3") {
		t.Fatalf("did not expect odd shards to run, got:\n%s", got)
	}
}

func TestForIfElseRunsDifferentPromptBodies(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/for 3 /if(n == 1)\nthen {{n}}\n\n/else\nelse {{n}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	for _, want := range []string{"start:else 0", "start:then 1", "start:else 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in events:\n%s", want, got)
		}
	}
	if strings.Contains(got, "then 0") || strings.Contains(got, "else 1") || strings.Contains(got, "then 2") {
		t.Fatalf("unexpected branch prompt in events:\n%s", got)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(updated)
	if !strings.Contains(content, "\n/else\nelse {{n}}\n") || !strings.Contains(content, "> status: done") {
		t.Fatalf("expected done first block and preserved else block, got:\n%s", content)
	}
}

func TestForIfGoRunsOnlyMatchingBackgroundIterations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/for 4 /if(n % 2 == 0) /go\nparallel {{n}}\n\n/wait\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "start:parallel 0") || !strings.Contains(got, "start:parallel 2") {
		t.Fatalf("expected even background shards to run, got:\n%s", got)
	}
	if strings.Contains(got, "parallel 1") || strings.Contains(got, "parallel 3") {
		t.Fatalf("did not expect odd background shards to run, got:\n%s", got)
	}
}

func TestNestedIfIsRejected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := strings.Join([]string{
		"/if (true) /if (false)",
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
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "/if does not support nesting") {
		t.Fatalf("expected nested /if rejection, got %v", err)
	}
}

func TestHeaderOnlyNestedIfIsRejected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/if (true) /if (false)\n\ninner then\n\n/else\nouter else\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "/if does not support nesting") {
		t.Fatalf("expected nested /if rejection, got %v", err)
	}
}

func TestForGoResultBlockKeepsOneMessagePerBranchByDefault(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for item in [api docs] /go\nparallel {{item}}\n\n/wait\n"), 0o644); err != nil {
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
	if strings.Count(content, "> [!ATM]") != 2 {
		t.Fatalf("expected result block plus explicit /wait block, got:\n%s", content)
	}
	if !strings.Contains(content, "> - assistant (fake) [item=api]:") ||
		!strings.Contains(content, "> - assistant (fake) [item=docs]:") {
		t.Fatalf("expected one message per /for /go branch:\n%s", content)
	}
}

func TestOutputDirectoryReceivesEventsAndResultDocument(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	outDir := filepath.Join(dir, "artifacts")
	if err := os.WriteFile(file, []byte("/task\none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "result.md")); err != nil {
		t.Fatalf("expected result.md: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "tasks", "*", "task-001-run-001-fake.jsonl"))
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
	reports, err := filepath.Glob(filepath.Join(outDir, "tasks", "one-*", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected detail report, got %v", reports)
	}
	report, err := os.ReadFile(reports[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "# ATM Report: one-") || !strings.Contains(string(report), "- Status: done") || !strings.Contains(string(report), "- Rendered prompt: sha256:") || !strings.Contains(string(report), "- Plan: sha256:") {
		t.Fatalf("unexpected detail report:\n%s", report)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatalf("expected state.json: %v", err)
	}
	var state struct {
		Version  int `json:"version"`
		Document string
		Tasks    map[string]struct {
			Status             string   `json:"status"`
			SourcePromptHash   string   `json:"sourcePromptHash"`
			RenderedPromptHash string   `json:"renderedPromptHash"`
			PlanHash           string   `json:"planHash"`
			StartedAt          string   `json:"startedAt"`
			UpdatedAt          string   `json:"updatedAt"`
			Runs               int      `json:"runs"`
			Report             string   `json:"report"`
			Logs               []string `json:"logs"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != 2 || state.Document != "taskdoc.txt" || len(state.Tasks) != 1 {
		t.Fatalf("unexpected state header:\n%s", stateData)
	}
	for id, task := range state.Tasks {
		if !strings.HasPrefix(id, "one-") || task.Status != "done" || task.Runs != 1 || task.Report != "artifacts/tasks/"+id+"/report.md" {
			t.Fatalf("unexpected task state for %q: %#v\n%s", id, task, stateData)
		}
		if !strings.HasPrefix(task.SourcePromptHash, "sha256:") || !strings.HasPrefix(task.RenderedPromptHash, "sha256:") || !strings.HasPrefix(task.PlanHash, "sha256:") || task.StartedAt == "" || task.UpdatedAt == "" {
			t.Fatalf("missing task state fields for %q: %#v\n%s", id, task, stateData)
		}
		if len(task.Logs) < 2 || !strings.HasPrefix(task.Logs[0], "artifacts/tasks/"+id+"/logs/task-001-") || !strings.Contains(strings.Join(task.Logs, "\n"), "artifacts/tasks/"+id+"/task-001-run-001-fake.jsonl") {
			t.Fatalf("unexpected task logs for %q: %#v\n%s", id, task.Logs, stateData)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(task.Logs[0]))); err != nil {
			t.Fatalf("expected task log under task artifact directory: %v", err)
		}
	}
}

func TestSourcePromptHashIncludesMarkdownContext(t *testing.T) {
	first := runAndReadOnlySourcePromptHash(t, "# Work\n\nReview backend.\n\n/task\nDo it.\n")
	second := runAndReadOnlySourcePromptHash(t, "# Work\n\nReview frontend.\n\n/task\nDo it.\n")
	if first == second {
		t.Fatalf("expected visible Markdown context to affect sourcePromptHash, got %s", first)
	}
}

func TestSourcePromptHashIncludesExplicitContext(t *testing.T) {
	first := runAndReadOnlySourcePromptHash(t, "# Shared\n\nUse PostgreSQL.\n\n# Work\n\n/task\n/context #Shared\nDo it.\n")
	second := runAndReadOnlySourcePromptHash(t, "# Shared\n\nUse MySQL.\n\n# Work\n\n/task\n/context #Shared\nDo it.\n")
	if first == second {
		t.Fatalf("expected /context content to affect sourcePromptHash, got %s", first)
	}
}

func TestSourcePromptHashExcludesDocContext(t *testing.T) {
	first := runAndReadOnlySourcePromptHash(t, "# Work\n\n/doc\n```\nSecret alpha.\n```\n\n/task\nDo it.\n")
	second := runAndReadOnlySourcePromptHash(t, "# Work\n\n/doc\n```\nSecret beta.\n```\n\n/task\nDo it.\n")
	if first != second {
		t.Fatalf("expected /doc content to be excluded from sourcePromptHash, got %s and %s", first, second)
	}
}

func TestReportIdentityIncludesMarkdownContext(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	content := "# API\n\nUse PostgreSQL.\n\n/task\nDo it.\n\n# Docs\n\nUse SQLite.\n\n/task\nDo it.\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Tasks map[string]struct{} `json:"tasks"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("expected two distinct report identities:\n%s", stateData)
	}
}

func runAndReadOnlySourcePromptHash(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Tasks map[string]struct {
			SourcePromptHash string `json:"sourcePromptHash"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("expected one task state:\n%s", stateData)
	}
	for _, task := range state.Tasks {
		if !strings.HasPrefix(task.SourcePromptHash, "sha256:") {
			t.Fatalf("missing sourcePromptHash:\n%s", stateData)
		}
		return task.SourcePromptHash
	}
	t.Fatalf("missing task state:\n%s", stateData)
	return ""
}

func TestRunFailureWritesFailedReportAndState(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	outDir := filepath.Join(dir, "artifacts")
	if err := os.WriteFile(file, []byte("/task\nfail this task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &failingRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir})
	if err == nil || !strings.Contains(err.Error(), "simulated failure") {
		t.Fatalf("expected runner failure, got %v", err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "> status: failed") || !strings.Contains(string(content), "> error: task 1 run failed: simulated failure") {
		t.Fatalf("expected failed report block:\n%s", content)
	}
	reports, err := filepath.Glob(filepath.Join(outDir, "tasks", "fail-this-task-*", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected failed detail report, got %v", reports)
	}
	report, err := os.ReadFile(reports[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "- Status: failed") || !strings.Contains(string(report), "- Error: task 1 run failed: simulated failure") || !strings.Contains(string(report), "- Rendered prompt: sha256:") || !strings.Contains(string(report), "- Plan: sha256:") {
		t.Fatalf("unexpected failed detail report:\n%s", report)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatalf("expected state.json: %v", err)
	}
	var state struct {
		Tasks map[string]struct {
			Status             string `json:"status"`
			RenderedPromptHash string `json:"renderedPromptHash"`
			PlanHash           string `json:"planHash"`
			Runs               int    `json:"runs"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("unexpected failed state:\n%s", stateData)
	}
	for _, task := range state.Tasks {
		if task.Status != "failed" || task.Runs != 0 || !strings.HasPrefix(task.RenderedPromptHash, "sha256:") || !strings.HasPrefix(task.PlanHash, "sha256:") {
			t.Fatalf("unexpected failed task state: %#v\n%s", task, stateData)
		}
	}
}

func TestRunRetriesRetryableExecuteErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/task\nretry me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &retryableRunner{failExecUntil: 2}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: &stderr, OutputDir: filepath.Join(dir, "out"), AgentRetries: 3}); err != nil {
		t.Fatal(err)
	}
	execute, _ := runner.counts()
	if execute != 3 {
		t.Fatalf("execute calls = %d, want 3", execute)
	}
	if got := stderr.String(); strings.Count(got, "[atm] retry") != 2 {
		t.Fatalf("expected two retry events, got:\n%s", got)
	}
}

func TestRunRetriesRetryableNaturalConditionCheckErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/for 3 until tests pass\nretry me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &retryableRunner{failCheckUntil: 1}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: &stderr, OutputDir: filepath.Join(dir, "out"), AgentRetries: 3}); err != nil {
		t.Fatal(err)
	}
	execute, check := runner.counts()
	if execute != 1 || check != 2 {
		t.Fatalf("execute/check calls = %d/%d, want 1/2", execute, check)
	}
	if got := stderr.String(); !strings.Contains(got, "agent check failed with retryable error; retry 1/3") {
		t.Fatalf("missing check retry event:\n%s", got)
	}
}

func TestRunDoesNotRetryPastConfiguredRetryLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/task\nretry me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &retryableRunner{failExecUntil: 2}
	err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), AgentRetries: 1})
	if err == nil || !strings.Contains(err.Error(), "Too Many Requests") {
		t.Fatalf("expected retryable failure after limit, got %v", err)
	}
	execute, _ := runner.counts()
	if execute != 2 {
		t.Fatalf("execute calls = %d, want 2", execute)
	}
}

func TestRunWritesOrphanReportWhenTaskBlockDisappears(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/task\norphan me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if err := Run(context.Background(), Options{FilePath: file, Runner: &deletingRunner{}, Stdout: io.Discard, Stderr: &stderr, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "[!ATM]") {
		t.Fatalf("did not expect main document report after task deletion:\n%s", content)
	}
	reports, err := filepath.Glob(filepath.Join(dir, "out", "tasks", "orphan-me-*", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected orphan detail report, got %v", reports)
	}
	report, err := os.ReadFile(reports[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "- Status: done") || !strings.Contains(string(report), "- Orphan: true") {
		t.Fatalf("unexpected orphan detail report:\n%s", report)
	}
	if !strings.Contains(stderr.String(), "[atm] orphan task 1 completed") {
		t.Fatalf("expected orphan log, got:\n%s", stderr.String())
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Tasks map[string]struct {
			Status string `json:"status"`
			Orphan bool   `json:"orphan"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("unexpected orphan state:\n%s", stateData)
	}
	for _, task := range state.Tasks {
		if task.Status != "done" || !task.Orphan {
			t.Fatalf("unexpected orphan task state: %#v\n%s", task, stateData)
		}
	}
}

func TestOutputCommandConstrainsPromptAndWritesStructuredArtifact(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
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
	matches, err := filepath.Glob(filepath.Join(outDir, "tasks", "*", "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected structured output file, got %v\nstderr:\n%s", matches, stderr.String())
	}
	outputPath := matches[0]
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
	file := filepath.Join(dir, "taskdoc.txt")
	outDir := filepath.Join(dir, "artifacts")
	body := "/output summary\nExplain the result.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "tasks", "*", "summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected text output file, got %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "done Explain the result.") {
		t.Fatalf("unexpected text output: %s", data)
	}
}

func TestOutputCommandInGoBranchAddsSuffixAndRendersAgentVars(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	outDir := filepath.Join(dir, "artifacts")
	body := "/go\n/output summary-{{agent_index}}\n```\nreason:string:why this passed\n```\nExplain the result.\n\n/wait\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &structuredRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: outDir}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "tasks", "*", "summary-1-agent-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected branch-suffixed structured output file, got %v", matches)
	}
}

func TestPromptCallLineStartsSiblingTaskInsideExplicitTask(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := "/def whereami\nReturn only the city.\n\n/return {{agent.last_message}}\n\n## Weather\n\n/task\nWeather for\n/call whereami\ntoday.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	for _, want := range []string{"Weather for", "today."} {
		if !strings.Contains(events, want) {
			t.Fatalf("expected sibling task event %q, got:\n%s", want, events)
		}
	}
}

func TestLetCallBindsReturnValue(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := "/def whereami\nReturn city.\n\n/return Paris\n\n## Weather\n\n/let city /call whereami\nWeather for {{city}}.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "Weather for Paris.") {
		t.Fatalf("expected /let /call value in prompt, got:\n%s", got)
	}
}

func TestUnusedLetBashIsLazy(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := "/let unused /bash exit 7\nNo use.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(got, "No use.") {
		t.Fatalf("expected task prompt to run, got:\n%s", got)
	}
}

func TestLetBashExecutesOnUseAndCaches(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/cd .",
		`/let value /bash n=$(cat count 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > count; printf "v%s" "$n"`,
		"{{value}} {{value}}",
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
	if !strings.Contains(got, "v1 v1") {
		t.Fatalf("expected cached bash value in prompt, got:\n%s", got)
	}
	count, err := os.ReadFile(filepath.Join(dir, "count"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("expected bash provider to run once, got count %q", string(count))
	}
}

func TestStandaloneLazyBashDoesNotUseConsumingTaskWorkdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "root.marker"), []byte("root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/let here /bash cat root.marker",
		"",
		"/cd sub",
		"Task {{here}}.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	want := "Task root."
	if got := strings.Join(runner.snapshot(), "\n"); !strings.Contains(got, want) {
		t.Fatalf("expected standalone lazy bash to use declaration workdir, got:\n%s", got)
	}
}

func TestUnusedLetCallIsLazy(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := "/def expensive\nSHOULD NOT RUN\n\n/return never\n\n/let unused /call expensive\nNo use.\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if strings.Contains(got, "SHOULD NOT RUN") {
		t.Fatalf("expected unused /let /call not to execute definition, got:\n%s", got)
	}
}

func TestLetCallExecutesOnUseAndCaches(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := "/def marker\nMARKER\n\n/return {{agent.message}}\n\n/let value /call marker\n{{value}}\n{{value}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.snapshot(), "\n")
	if strings.Count(got, "start:MARKER") != 1 {
		t.Fatalf("expected /let /call provider to execute once, got:\n%s", got)
	}
	if !strings.Contains(got, "done MARKER\ndone MARKER") {
		t.Fatalf("expected cached call value in prompt, got:\n%s", got)
	}
}

func TestForCarriesAssignedVariablesToNextRun(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/def add text n",
		"",
		"/return {{text}}{{n}}",
		"",
		"## /collect",
		"",
		"/let transcript start",
		"/for 3",
		"/let transcript /call add {{transcript}} {{n}}",
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
	if !strings.Contains(got, "start0") || !strings.Contains(got, "start01") || !strings.Contains(got, "start012") {
		t.Fatalf("expected /for to carry assigned variables between runs, got:\n%s", got)
	}
}

func TestCallDoesNotFallbackToStructuredOutputReturn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := "/def check\n/output result\n```\nreason:string:why\n```\nCheck it.\n\n## Use\n\n/let result /call check\nReason: {{result.reason}}\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &structuredRunner{}
	err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "requires /return") {
		t.Fatalf("expected missing /return error, got %v", err)
	}
}

func TestCallReturnsStructuredReturn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/def gate",
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

func TestTodoWorkflowPatternInjectsScopeAndWritesAcceptanceJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"# workflow",
		"",
		"## Run context",
		"",
		"/let qa_dir .atm/test-todo-workflow",
		"",
		"/let company_context",
		"Company context line one.",
		"Company context line two.",
		"",
		"/def acceptance_gate phase slug",
		"",
		"/company_context",
		"Assess {{phase}} at {{slug}}.",
		"",
		"/return",
		"```json",
		`{"type":"object","required":["passed","reason","open_p0_p1","missing_evidence","next_actions"],"properties":{"passed":{"type":"boolean"},"reason":{"type":"string"},"open_p0_p1":{"type":"array","items":{"type":"string"}},"missing_evidence":{"type":"array","items":{"type":"string"}},"next_actions":{"type":"array","items":{"type":"string"}}}}`,
		"```",
		"",
		"/def phase_iteration phase slug",
		"",
		"/pool phase_review 2 10",
		"",
		"/go phase_review",
		"/company_context",
		"Review {{phase}}.",
		"{{phase_scope}}",
		"",
		"/wait phase_review",
		"",
		"/company_context",
		"Develop {{phase}} in round {{n}}.",
		"{{phase_scope}}",
		"",
		"/let gate /call acceptance_gate {{phase}} {{slug}}",
		"",
		"/bash <<'SH'",
		`mkdir -p "{{qa_dir}}/{{slug}}"`,
		`cat > "{{qa_dir}}/{{slug}}/acceptance.json" <<'JSON'`,
		"{{gate}}",
		"JSON",
		"SH",
		"",
		"/company_context",
		"Record gate {{gate.passed}} because {{gate.reason}}.",
		"{{phase_scope}}",
		"",
		"/return",
		"```",
		"{{phase}} done {{gate.passed}}",
		"```",
		"",
		"### /phase test",
		`/for 3 until(exist("{{qa_dir}}/phase-a/acceptance.json") && json(open("{{qa_dir}}/phase-a/acceptance.json")).passed)`,
		"/let phase_scope",
		"Scoped phase details line one.",
		"Scoped phase details line two.",
		"/call phase_iteration phase-a phase-a",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &todoWorkflowRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out"), GlobalJobs: 4}); err != nil {
		t.Fatal(err)
	}
	acceptancePath := filepath.Join(dir, ".atm", "test-todo-workflow", "phase-a", "acceptance.json")
	data, err := os.ReadFile(acceptancePath)
	if err != nil {
		t.Fatalf("expected acceptance JSON at todo root: %v", err)
	}
	if !strings.Contains(string(data), `"passed":true`) || !strings.Contains(string(data), `"reason":"ok"`) {
		t.Fatalf("unexpected acceptance JSON:\n%s", data)
	}
	prompts := strings.Join(runner.snapshot(), "\n---\n")
	if !strings.Contains(prompts, "Company context line one.") {
		t.Fatalf("expected company context to be injected, got:\n%s", prompts)
	}
	if !strings.Contains(prompts, "Scoped phase details line one.") {
		t.Fatalf("expected phase_scope to be inherited by phase_iteration, got:\n%s", prompts)
	}
}

func TestListDefinitionSupportsLocalPoolGoWaitAndMultilineReturn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"/def reviews area",
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
		"```",
		"Review {{area}} done.",
		"Last: {{agent.last_message}}",
		"```",
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
	if !strings.Contains(got, "Summary: Review checkout done.") || !strings.Contains(got, "Last: done parallel checkout") {
		t.Fatalf("expected multiline return from list definition, got:\n%s", got)
	}
}

func TestImportDefinitionWithNamespace(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.taskdoc.md")
	if err := os.WriteFile(lib, []byte("/def city\n/return Paris\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "taskdoc.txt")
	body := "/import loc from lib.taskdoc.md\n\n/let city /call loc.city\nWeather for {{city}}.\n"
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

func TestImportDefinitionScopeAtRuntime(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.taskdoc.md")
	if err := os.WriteFile(lib, []byte("/def city\n/return Paris\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "taskdoc.md")
	body := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/import lib.taskdoc.md",
		"",
		"## B",
		"",
		"/task",
		"/call city",
		"Should not run.",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), `unknown definition "city"`) {
		t.Fatalf("expected runtime import scope error, got %v", err)
	}
}

func TestAppendDuringRunningTaskIsPickedUpByCurrentRun(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/task\nslow first\n"), 0o644); err != nil {
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
	if _, err := f.WriteString("\n/task\nsecond\n"); err != nil {
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

func TestResumeUsesRecordedNamedTaskSession(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := strings.Join([]string{
		"/task alpha",
		"first task",
		"",
		"/resume alpha",
		"follow up",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "resume:session-first-task") {
		t.Fatalf("expected resume to use recorded session id, got:\n%s", events)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stateData), `"alpha"`) || !strings.Contains(string(stateData), `"id": "session-first-task"`) {
		t.Fatalf("expected named session in state:\n%s", stateData)
	}
}

func TestForkUsesRecordedNamedTaskSessionAndRecordsNewTask(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := strings.Join([]string{
		"/task alpha",
		"first task",
		"",
		"/task branch /fork alpha",
		"branch task",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "fork:session-first-task") {
		t.Fatalf("expected fork to use recorded session id, got:\n%s", events)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stateData), `"branch"`) || !strings.Contains(string(stateData), `"id": "session-branch-task"`) {
		t.Fatalf("expected forked task session in state:\n%s", stateData)
	}
}

func TestForkAllowsAnonymousCurrentTaskWithoutRecordingNewSession(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := strings.Join([]string{
		"/task alpha",
		"first task",
		"",
		"/fork alpha",
		"anonymous branch",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "fork:session-first-task") {
		t.Fatalf("expected anonymous fork to use recorded session id, got:\n%s", events)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state := string(stateData)
	if !strings.Contains(state, `"alpha"`) || !strings.Contains(state, `"id": "session-first-task"`) {
		t.Fatalf("expected source named session in state:\n%s", stateData)
	}
	if strings.Contains(state, `"session-anonymous-branch"`) {
		t.Fatalf("anonymous fork should not record a reusable named session:\n%s", stateData)
	}
}

func TestForkAndTaskHeaderOrderVariantsRecordNewSession(t *testing.T) {
	cases := []string{
		"/fork alpha\n/task branch\nbranch task\n",
		"/task branch\n/fork alpha\nbranch task\n",
		"/fork alpha /task branch\nbranch task\n",
		"/task branch /fork alpha\nbranch task\n",
	}
	for _, branch := range cases {
		name := strings.ReplaceAll(strings.TrimSpace(strings.Split(branch, "\n")[0]), " ", "_")
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "taskdoc.txt")
			body := strings.Join([]string{
				"/task alpha",
				"first task",
				"",
				branch,
			}, "\n")
			if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{}
			if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
				t.Fatal(err)
			}
			events := strings.Join(runner.snapshot(), "\n")
			if !strings.Contains(events, "fork:session-first-task") {
				t.Fatalf("expected fork to use recorded session id, got:\n%s", events)
			}
			stateData, err := os.ReadFile(filepath.Join(dir, ".atm", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(stateData), `"branch"`) || !strings.Contains(string(stateData), `"id": "session-branch-task"`) {
				t.Fatalf("expected branch session in state:\n%s", stateData)
			}
		})
	}
}

func TestResumeLoopCheckUsesRecordedNamedTaskSession(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	body := strings.Join([]string{
		"/task alpha",
		"first task",
		"",
		"/resume alpha /for 2 until done",
		"follow up",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if err := Run(context.Background(), Options{FilePath: file, Runner: runner, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")}); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(runner.snapshot(), "\n")
	if !strings.Contains(events, "check-resume:session-first-task") {
		t.Fatalf("expected loop check to use recorded session id, got:\n%s", events)
	}
}

func TestResumeWithoutRecordedNamedTaskFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "taskdoc.txt")
	if err := os.WriteFile(file, []byte("/resume alpha\nfollow up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Options{FilePath: file, Runner: &fakeRunner{}, Stdout: io.Discard, Stderr: io.Discard, OutputDir: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "no recorded agent session for /resume alpha") {
		t.Fatalf("expected missing named session error, got %v", err)
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
