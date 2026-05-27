package cli

import (
	"context"
	"github.com/chinaykc/atm/pkg/integration/mcp"
	"github.com/chinaykc/atm/pkg/runtime/engine"

	urfavecli "github.com/urfave/cli/v3"
)

func mcpCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:   "mcp",
		Usage:  "run a temporary stdio MCP server",
		Hidden: true,
		Commands: []*urfavecli.Command{
			mcpServerCommand("check", "run the todo check MCP server", func(args []string) error {
				return mcp.RunCheckServerCLI(args, env.Stdin, env.Stdout, env.Stderr)
			}),
			mcpServerCommand("output", "run the output MCP server", func(args []string) error {
				return mcp.RunOutputServerCLI(args, env.Stdin, env.Stdout, env.Stderr)
			}),
			mcpServerCommand("db", "run the database MCP server", func(args []string) error {
				return mcp.RunDBServerCLI(args, env.Stdin, env.Stdout, env.Stderr)
			}),
			mcpServerCommand("defs", "run the definitions MCP server", func(args []string) error {
				return engine.RunDefsMCPServerCLI(args, env.Stdin, env.Stdout, env.Stderr)
			}),
			mcpServerCommand("webhook", "run the webhook MCP server", func(args []string) error {
				return engine.RunWebhookMCPServerCLI(args, env.Stdin, env.Stdout, env.Stderr)
			}),
		},
	}
}

func mcpServerCommand(name, usage string, run func([]string) error) *urfavecli.Command {
	return &urfavecli.Command{
		Name:            name,
		Usage:           usage,
		SkipFlagParsing: true,
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return run(cmd.Args().Slice())
		},
	}
}
