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
			&urfavecli.BoolFlag{Name: "document", Usage: "remove generated report blocks from the document"},
			&urfavecli.BoolFlag{Name: "reports", Usage: "remove detail reports"},
			&urfavecli.BoolFlag{Name: "state", Usage: "remove state files"},
			&urfavecli.BoolFlag{Name: "logs", Usage: "remove logs"},
			&urfavecli.BoolFlag{Name: "all", Usage: "remove document, reports, state, and logs"},
			&urfavecli.BoolFlag{Name: "repair-ids", Usage: "repair duplicate ATM report identities in result documents"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runCleanCommand(cmd, stdout)
		},
	}
}

func runCleanCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	if cmd.Bool("repair-ids") {
		return runRepairIDsCommand(cmd, stdout)
	}
	opts := cleanOptionsFromCommand(cmd)
	cleanFiles, err := resolveRunFiles(cmd.Args().Slice())
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

func runRepairIDsCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	repairFiles, err := resolveRunFiles(cmd.Args().Slice())
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
		Usage:     "append a formatted prompt block to an ATM file",
		ArgsUsage: "<file> [prompt...]",
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runAppendCommand(cmd, env)
		},
	}
}

func runAppendCommand(cmd *urfavecli.Command, env commandEnv) error {
	if cmd.Args().Len() == 0 {
		return fmt.Errorf("no ATM file specified")
	}
	prompt, err := readAppendPrompt(cmd.Args().Slice()[1:], env)
	if err != nil {
		return err
	}
	return RunAppend(cmd.Args().Get(0), prompt, env.Stdout)
}

func readAppendPrompt(args []string, env commandEnv) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
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
		Name:      "format",
		Usage:     "format generated tags in the ATM file",
		ArgsUsage: "<file>",
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runFormatCommand(cmd, stdout)
		},
	}
}

func runFormatCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	file, err := singleTodoFileArg(cmd)
	if err != nil {
		return err
	}
	return RunFormat(file, stdout)
}

func singleTodoFileArg(cmd *urfavecli.Command) (string, error) {
	if cmd.Args().Len() == 0 {
		return "", fmt.Errorf("no ATM file specified")
	}
	if cmd.Args().Len() > 1 {
		return "", fmt.Errorf("unexpected argument %q", cmd.Args().Get(1))
	}
	return cmd.Args().Get(0), nil
}
