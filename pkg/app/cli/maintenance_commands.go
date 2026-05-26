package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

func cleanCommand(stdout io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:        "clean",
		Usage:       "remove generated ATM state and audit artifacts",
		Description: "With no cleanup option, atm clean removes generated report blocks from the document only.",
		ArgsUsage:   "[files...]",
		Flags: []urfavecli.Flag{
			todoFilesFlag(),
			&urfavecli.BoolFlag{Name: "document", Usage: "remove generated report blocks from the document"},
			&urfavecli.BoolFlag{Name: "reports", Usage: "remove detail reports"},
			&urfavecli.BoolFlag{Name: "state", Usage: "remove state files"},
			&urfavecli.BoolFlag{Name: "logs", Usage: "remove logs"},
			&urfavecli.BoolFlag{Name: "all", Usage: "remove document, reports, state, and logs"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runCleanCommand(cmd, stdout)
		},
	}
}

func runCleanCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	opts := cleanOptionsFromCommand(cmd)
	cleanFiles, err := resolveRunFiles(commandFiles(cmd), cmd.Args().Slice())
	if err != nil {
		return err
	}
	for _, file := range cleanFiles {
		result, err := RunClean(file, opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "atm clean: %s\n", file)
		fmt.Fprintf(stdout, "  document state blocks: %d\n", result.DocumentBlocks)
		fmt.Fprintf(stdout, "  reports removed: %d\n", result.ReportDirs)
		fmt.Fprintf(stdout, "  state files removed: %d\n", result.StateFiles)
		fmt.Fprintf(stdout, "  log dirs removed: %d\n", result.LogDirs)
	}
	return nil
}

func cleanOptionsFromCommand(cmd *urfavecli.Command) CleanOptions {
	opts := CleanOptions{
		Document: cmd.Bool("document"),
		Reports:  cmd.Bool("reports"),
		State:    cmd.Bool("state"),
		Logs:     cmd.Bool("logs"),
	}
	if cmd.Bool("all") {
		return CleanOptions{Document: true, Reports: true, State: true, Logs: true}
	}
	if !opts.Document && !opts.Reports && !opts.State && !opts.Logs {
		opts.Document = true
	}
	return opts
}

func repairIDsCommand(stdout io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "repair-ids",
		Usage:     "repair duplicate ATM report identities",
		ArgsUsage: "[files...]",
		Flags: []urfavecli.Flag{
			todoFilesFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runRepairIDsCommand(cmd, stdout)
		},
	}
}

func runRepairIDsCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	repairFiles, err := resolveRunFiles(commandFiles(cmd), cmd.Args().Slice())
	if err != nil {
		return err
	}
	for _, file := range repairFiles {
		if err := RunRepairIDs(file, stdout); err != nil {
			return err
		}
	}
	return nil
}

func appendCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "append",
		Usage:     "append a formatted prompt block to the active todo file",
		ArgsUsage: "[prompt...]",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "file", Value: "todo.txt", Usage: "todo file path"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runAppendCommand(cmd, env)
		},
	}
}

func runAppendCommand(cmd *urfavecli.Command, env commandEnv) error {
	prompt, err := readAppendPrompt(cmd, env)
	if err != nil {
		return err
	}
	return RunAppend(cmd.String("file"), prompt, env.Stdout)
}

func readAppendPrompt(cmd *urfavecli.Command, env commandEnv) (string, error) {
	if cmd.Args().Len() > 0 {
		return strings.Join(cmd.Args().Slice(), " "), nil
	}
	stat, err := env.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("stat stdin: %w", err)
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return editPromptBlock(env)
	}
	data, err := io.ReadAll(env.Stdin)
	if err != nil {
		return "", fmt.Errorf("read prompt block from stdin: %w", err)
	}
	return string(data), nil
}

func formatCommand(stdout io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "format",
		Usage: "format generated tags in the todo file",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "file", Value: "todo.txt", Usage: "todo file path"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runFormatCommand(cmd, stdout)
		},
	}
}

func runFormatCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	if err := rejectArgs(cmd); err != nil {
		return err
	}
	return RunFormat(cmd.String("file"), stdout)
}

func untagCommand(stdout io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "untag",
		Usage: "remove done and/or running state from the todo file",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "file", Value: "todo.txt", Usage: "todo file path"},
			&urfavecli.BoolFlag{Name: "done", Value: true, Usage: "remove done state"},
			&urfavecli.BoolFlag{Name: "running", Value: true, Usage: "remove running state"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runUntagCommand(cmd, stdout)
		},
	}
}

func runUntagCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	if err := rejectArgs(cmd); err != nil {
		return err
	}
	opts := UntagOptions{Done: cmd.Bool("done"), Running: cmd.Bool("running")}
	return RunUntag(cmd.String("file"), stdout, opts)
}
