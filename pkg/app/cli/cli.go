package cli

import (
	"context"
	"github.com/chinaykc/atm/pkg/view/plan"
	"io"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

const (
	appName  = "atm"
	appUsage = "run todo-driven agent workflows"
)

var supportedSlashCommands = []string{
	"/resume",
	"/args",
	"/cd",
	"/let",
	"/bash",
	"/output",
	"/db",
	"/skill",
	"/mcp",
	"/def",
	"/call",
	"/return",
	"/import",
	"/pool",
	"/if",
	"/else",
	"/for",
	"/go",
	"/wait",
}

func Run(args []string, stdout, stderr io.Writer) error {
	command := NewCommand(stdout, stderr)
	return command.Run(context.Background(), append([]string{appName}, defaultRunArgs(command, args)...))
}

func NewCommand(stdout, stderr io.Writer) *urfavecli.Command {
	return newCommand(newCommandEnv(stdout, stderr))
}

func newCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:                  appName,
		Usage:                 appUsage,
		Description:           "Supported slash commands: " + strings.Join(supportedSlashCommands, ", "),
		HideVersion:           true,
		EnableShellCompletion: true,
		Suggest:               true,
		Writer:                env.Stdout,
		ErrWriter:             env.Stderr,
		Commands:              rootCommands(env),
	}
}

func rootCommands(env commandEnv) []*urfavecli.Command {
	return []*urfavecli.Command{
		executeCommand("run", "run pending prompt blocks (also the default command)", false, env),
		executeCommand("exec", "run pending prompt blocks from the startup snapshot", true, env),
		mcpCommand(env),
		appendCommand(env),
		planCommand(env),
		checkCommand(env.Stdout),
		reportCommand(env.Stdout),
		cleanCommand(env.Stdout),
		repairIDsCommand(env.Stdout),
		formatCommand(env.Stdout),
		untagCommand(env.Stdout),
		webCommand(env),
	}
}

func planCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:            "plan",
		Usage:           "print a dry-run execution plan",
		SkipFlagParsing: true,
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return plan.RunCLI(cmd.Args().Slice(), env.Stdout, env.Stderr)
		},
	}
}
