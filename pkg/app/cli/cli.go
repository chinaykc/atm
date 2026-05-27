package cli

import (
	"context"
	"io"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

const (
	appName  = "atm"
	appUsage = "run todo-driven agent workflows"
)

var supportedSlashCommands = []string{
	"/task",
	"/resume",
	"/fork",
	"/args",
	"/cd",
	"/let",
	"/bash",
	"/output",
	"/db",
	"/skill",
	"/flag",
	"/webhook",
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
	env := newCommandEnv(stdout, stderr)
	command, normalized, err := newCommandForArgs(env, args)
	if err != nil {
		return err
	}
	return command.Run(context.Background(), append([]string{appName}, normalized...))
}

func newCommandForArgs(env commandEnv, args []string) (*urfavecli.Command, []string, error) {
	dynamic, err := discoverDynamicCommands()
	if err != nil {
		return nil, nil, err
	}
	command := baseCommand(env, dynamic)
	normalized := defaultRunArgs(command, args)
	if err := addDocumentFlagsForArgs(command, normalized); err != nil {
		return nil, nil, err
	}
	return command, normalized, nil
}

func baseCommand(env commandEnv, dynamic []dynamicCommand) *urfavecli.Command {
	return &urfavecli.Command{
		Name:                  appName,
		Usage:                 appUsage,
		Version:               versionString(),
		Description:           "Supported slash commands: " + strings.Join(supportedSlashCommands, ", "),
		EnableShellCompletion: true,
		Suggest:               true,
		Writer:                env.Stdout,
		ErrWriter:             env.Stderr,
		Commands:              rootCommands(env, dynamic),
	}
}

func rootCommands(env commandEnv, dynamic []dynamicCommand) []*urfavecli.Command {
	commands := []*urfavecli.Command{
		executeCommand("run", "run pending prompt blocks (also the default command)", env),
		resumeCommand(env),
		flagCommand(env),
		mcpCommand(env),
		appendCommand(env),
		checkCommand(env.Stdout),
		reportCommand(env.Stdout),
		cleanCommand(env.Stdout),
		formatCommand(env.Stdout),
		serveCommand(env),
	}
	for _, item := range dynamic {
		commands = append(commands, dynamicCLICommand(item, env))
	}
	return commands
}
