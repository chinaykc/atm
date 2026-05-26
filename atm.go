package atm

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/chinaykc/atm/pkg/app/cli"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/syntax"
	"github.com/chinaykc/atm/pkg/runtime/engine"
	"github.com/chinaykc/atm/pkg/view/plan"
	"github.com/chinaykc/atm/pkg/workspace/taskdoc"

	urfavecli "github.com/urfave/cli/v3"
)

const (
	ExitOK                = cli.ExitOK
	ExitExecutionFailure  = cli.ExitExecutionFailure
	ExitValidationFailure = cli.ExitValidationFailure
	ExitStateInconsistent = cli.ExitStateInconsistent
)

type (
	Plan            = ir.Plan
	SyntaxDocument  = syntax.Document
	PlanOptions     = plan.Options
	CompileOptions  = compiler.CompileOptions
	RunOptions      = engine.Options
	CleanOptions    = cli.CleanOptions
	CleanResult     = cli.CleanResult
	ToolConfig      = agent.Config
	ToolRunner      = agent.Runner
	ToolResult      = agent.ExecuteResult
	UntagOptions    = taskdoc.UntagOptions
	FormatResult    = taskdoc.FormatResult
	UntagResult     = taskdoc.UntagResult
	RepairIDsResult = taskdoc.RepairIDsResult
	RepairIDChange  = taskdoc.RepairIDChange
	AppendResult    = taskdoc.AppendResult
)

type ExecutionOptions struct {
	FilePath     string
	ToolName     string
	CodexPath    string
	ClaudePath   string
	Stdout       io.Writer
	Stderr       io.Writer
	MessageLimit int
	OutputDir    string
	GlobalJobs   int
	Snapshot     bool
}

func RunCLI(args []string, stdout, stderr io.Writer) error {
	return cli.Run(args, stdout, stderr)
}

func NewCLICommand(stdout, stderr io.Writer) *urfavecli.Command {
	return cli.NewCommand(stdout, stderr)
}

func ExitCode(err error) int {
	return cli.ExitCode(err)
}

func Compile(sourcePath, content string) (Plan, error) {
	return compiler.CompileProgram(sourcePath, content)
}

func CompileWithOptions(sourcePath, content string, opts CompileOptions) (Plan, error) {
	return compiler.CompileProgramWithOptions(sourcePath, content, opts)
}

func CompileFile(filePath string) (Plan, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return Plan{}, fmt.Errorf("read todo file %q: %w", filePath, err)
	}
	return Compile(filePath, string(content))
}

func ParseSyntax(sourcePath, content string) SyntaxDocument {
	return compiler.ParseSyntax(sourcePath, content)
}

func PlanFile(filePath string, stdout io.Writer, opts PlanOptions) error {
	return plan.RunFile(filePath, stdout, opts)
}

func Run(ctx context.Context, opts RunOptions) error {
	return engine.Run(ctx, opts)
}

func Execute(ctx context.Context, opts ExecutionOptions) error {
	toolName := opts.ToolName
	if toolName == "" {
		toolName = "codex"
	}
	runner, err := agent.NewRunner(toolName, agent.Config{CodexPath: opts.CodexPath, ClaudePath: opts.ClaudePath})
	if err != nil {
		return err
	}
	return engine.Run(ctx, engine.Options{
		FilePath:     opts.FilePath,
		Runner:       runner,
		ToolName:     toolName,
		CodexPath:    opts.CodexPath,
		ClaudePath:   opts.ClaudePath,
		Stdout:       opts.Stdout,
		Stderr:       opts.Stderr,
		MessageLimit: opts.MessageLimit,
		OutputDir:    opts.OutputDir,
		GlobalJobs:   opts.GlobalJobs,
		Snapshot:     opts.Snapshot,
	})
}

func NewToolRunner(name string, config ToolConfig) (ToolRunner, error) {
	return agent.NewRunner(name, config)
}

func FormatContent(content string) (string, int) {
	return taskdoc.FormatContent(content)
}

func FormatFile(filePath string) (FormatResult, error) {
	return taskdoc.FormatFile(filePath)
}

func UntagContent(content string, opts UntagOptions) (string, UntagResult) {
	return taskdoc.UntagContent(content, opts)
}

func UntagFile(filePath string, opts UntagOptions) (UntagResult, error) {
	return taskdoc.UntagFile(filePath, opts)
}

func RepairIDsContent(content string) (string, RepairIDsResult) {
	return taskdoc.RepairIDsContent(content)
}

func RepairIDsFile(filePath string) (RepairIDsResult, error) {
	return taskdoc.RepairIDsFile(filePath)
}

func FormatAppendPrompt(prompt string) (string, int, error) {
	return taskdoc.FormatAppendPrompt(prompt)
}

func AppendFile(filePath, prompt string) (AppendResult, error) {
	return taskdoc.AppendFile(filePath, prompt)
}

func CleanFile(filePath string, opts CleanOptions) (CleanResult, error) {
	return cli.RunClean(filePath, opts)
}
