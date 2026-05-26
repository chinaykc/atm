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

func executeCommand(name, usage string, snapshot bool, env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      name,
		Usage:     usage,
		ArgsUsage: "[files...]",
		Flags:     executeFlags(),
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			return runExecuteCommand(ctx, cmd, env, snapshot)
		},
	}
}

func executeFlags() []urfavecli.Flag {
	return []urfavecli.Flag{
		todoFilesFlag(),
		&urfavecli.StringFlag{Name: "tool", Value: "codex", Usage: "tool adapter to run: codex, claude, or claude-code"},
		&urfavecli.StringFlag{Name: "codex", Value: "codex", Usage: "codex executable path used by --tool codex"},
		&urfavecli.StringFlag{Name: "claude", Value: "claude", Usage: "claude executable path used by --tool claude or --tool claude-code"},
		&urfavecli.IntFlag{Name: "messages", Value: 1, Usage: "number of recent structured assistant messages to keep in each result block"},
		&urfavecli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "directory for execution artifacts; defaults to .atm/YYYYMMDDHHMMSS[-N]"},
		&urfavecli.IntFlag{Name: "jobs", Usage: "maximum number of concurrently running background branches across all pools; defaults to NumCPU"},
	}
}

func runExecuteCommand(ctx context.Context, cmd *urfavecli.Command, env commandEnv, snapshot bool) error {
	opts, err := executeOptions(cmd, env, snapshot)
	if err != nil {
		return err
	}
	runFiles, err := resolveRunFiles(commandFiles(cmd), cmd.Args().Slice())
	if err != nil {
		return err
	}
	runner, err := agent.NewRunner(opts.ToolName, agent.Config{CodexPath: opts.CodexPath, ClaudePath: opts.ClaudePath})
	if err != nil {
		return err
	}
	opts.Runner = runner
	for i, file := range runFiles {
		if len(runFiles) > 1 {
			fmt.Fprintf(env.Stderr, "atm %s file %d/%d: %s\n", cmd.Name, i+1, len(runFiles), file)
		}
		opts.FilePath = file
		opts.OutputDir = outputDirForRunFile(cmd.String("output"), file, i, len(runFiles))
		if err := engine.Run(ctx, opts); err != nil {
			var readErr store.ReadError
			if errors.As(err, &readErr) {
				_ = urfavecli.ShowCommandHelp(ctx, cmd, cmd.Name)
			}
			return err
		}
	}
	return nil
}

func executeOptions(cmd *urfavecli.Command, env commandEnv, snapshot bool) (engine.Options, error) {
	messages := cmd.Int("messages")
	if messages < 1 {
		return engine.Options{}, fmt.Errorf("--messages must be at least 1")
	}
	jobs := cmd.Int("jobs")
	if jobs < 0 {
		return engine.Options{}, fmt.Errorf("--jobs must be at least 0")
	}
	return engine.Options{
		ToolName:     cmd.String("tool"),
		CodexPath:    cmd.String("codex"),
		ClaudePath:   cmd.String("claude"),
		Stdout:       env.Stdout,
		Stderr:       env.Stderr,
		MessageLimit: messages,
		GlobalJobs:   jobs,
		Snapshot:     snapshot,
	}, nil
}
