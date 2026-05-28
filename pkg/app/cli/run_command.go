package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/runtime/engine"
	"github.com/chinaykc/atm/pkg/runtime/store"

	urfavecli "github.com/urfave/cli/v3"
)

func executeCommand(name, usage string, env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "[files...]",
		Flags:     executeFlags(),
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			return runExecuteCommand(ctx, cmd, env)
		},
	}
}

func executeFlags() []urfavecli.Flag {
	return []urfavecli.Flag{
		&urfavecli.StringFlag{Name: "tool", Value: "codex", Usage: "tool adapter to run: codex, claude, or claude-code"},
		&urfavecli.StringFlag{Name: "codex", Value: "codex", Usage: "codex executable path used by --tool codex"},
		&urfavecli.StringFlag{Name: "claude", Value: "claude", Usage: "claude executable path used by --tool claude or --tool claude-code"},
		&urfavecli.IntFlag{Name: "messages", Value: 1, Usage: "number of recent structured assistant messages to keep in each result block"},
		&urfavecli.IntFlag{Name: "retries", Value: 3, Usage: "maximum retries for retryable agent errors; use 0 to disable"},
		&urfavecli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "directory for output artifacts; source backups and result.todo.md stay under ATM_HOME/runs"},
		&urfavecli.IntFlag{Name: "jobs", Usage: "maximum number of concurrently running background branches across all pools; defaults to NumCPU"},
	}
}

func runExecuteCommand(ctx context.Context, cmd *urfavecli.Command, env commandEnv) error {
	opts, err := executeOptions(cmd, env)
	if err != nil {
		return err
	}
	runFiles, err := resolveRunFiles(cmd.Args().Slice())
	if err != nil {
		return err
	}
	runner, err := agent.NewRunner(opts.ToolName, agent.Config{CodexPath: opts.CodexPath, ClaudePath: opts.ClaudePath})
	if err != nil {
		return err
	}
	opts.Runner = runner
	docFlags, err := documentFlagsForFiles(runFiles)
	if err != nil {
		return err
	}
	if len(docFlags) > 0 {
		if len(runFiles) > 1 {
			for _, flag := range docFlags {
				if cmd.IsSet(flag.Name) {
					return fmt.Errorf("document flag -%s can only be used with a single ATM file", flag.Name)
				}
			}
		} else {
			vars, err := valuesForDocumentFlags(cmd, docFlags)
			if err != nil {
				return err
			}
			opts.Vars = vars
		}
	}
	for i, file := range runFiles {
		if err := runOneFile(ctx, cmd, env, opts, file, i, len(runFiles)); err != nil {
			return err
		}
	}
	return nil
}

func runOneFile(ctx context.Context, cmd *urfavecli.Command, env commandEnv, opts engine.Options, file string, index, total int) error {
	if total > 1 {
		fmt.Fprintf(env.Stderr, "atm %s file %d/%d: %s\n", cmd.Name, index+1, total, file)
	}
	outputOverride := outputDirForRunFile(cmd.String("output"), file, index, total)
	workspace, err := newManagedRunWorkspace(cmd.Name, file, outputOverride)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stderr, "atm %s started\n", cmd.Name)
	fmt.Fprintf(env.Stderr, "source hidden during execution: %s\n", workspace.manifest.SourcePath)
	if len(workspace.manifest.Imports) > 0 {
		fmt.Fprintf(env.Stderr, "imports hidden during execution: %d file(s)\n", len(workspace.manifest.Imports))
	}
	fmt.Fprintf(env.Stderr, "source backup: %s\n", workspace.manifest.SourceCopy)
	fmt.Fprintf(env.Stderr, "working file: %s\n", workspace.manifest.WorkingFile)
	opts.FilePath = workspace.manifest.WorkingFile
	opts.OutputDir = workspace.manifest.OutputDir
	opts.TaskDir = workspace.manifest.TaskDir
	runErr := engine.Run(ctx, opts)
	status := "succeeded"
	if runErr != nil {
		status = "failed"
		if ctx.Err() != nil {
			status = "interrupted"
		}
	}
	finishErr := workspace.finish(status, runErr)
	if runErr != nil {
		var readErr store.ReadError
		if errors.As(runErr, &readErr) {
			_ = urfavecli.ShowCommandHelp(ctx, cmd, cmd.Name)
		}
		fmt.Fprintf(env.Stderr, "atm %s %s\n", cmd.Name, status)
		fmt.Fprintf(env.Stderr, "source restored unchanged: %s\n", workspace.manifest.SourcePath)
		fmt.Fprintf(env.Stderr, "partial result: %s\n", workspace.manifest.ResultFile)
		fmt.Fprintf(env.Stderr, "resume with:\n  %s\n", workspace.manifest.ResumeCommand)
		if finishErr != nil {
			return fmt.Errorf("%w; additionally failed to finish run workspace: %v", runErr, finishErr)
		}
		return runErr
	}
	if finishErr != nil {
		return finishErr
	}
	fmt.Fprintf(env.Stderr, "atm %s finished: %s\n", cmd.Name, status)
	fmt.Fprintf(env.Stderr, "source restored unchanged: %s\n", workspace.manifest.SourcePath)
	fmt.Fprintf(env.Stderr, "result document: %s\n", workspace.manifest.ResultFile)
	fmt.Fprintf(env.Stderr, "artifacts: %s\n", workspace.runDir)
	return nil
}

func executeOptions(cmd *urfavecli.Command, env commandEnv) (engine.Options, error) {
	messages := cmd.Int("messages")
	if messages < 1 {
		return engine.Options{}, fmt.Errorf("--messages must be at least 1")
	}
	jobs := cmd.Int("jobs")
	if jobs < 0 {
		return engine.Options{}, fmt.Errorf("--jobs must be at least 0")
	}
	retries := cmd.Int("retries")
	if retries < 0 {
		return engine.Options{}, fmt.Errorf("--retries must be at least 0")
	}
	return engine.Options{
		ToolName:     cmd.String("tool"),
		CodexPath:    cmd.String("codex"),
		ClaudePath:   cmd.String("claude"),
		Stdout:       env.Stdout,
		Stderr:       env.Stderr,
		MessageLimit: messages,
		AgentRetries: retries,
		GlobalJobs:   jobs,
	}, nil
}
