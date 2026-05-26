package cli

import (
	"context"
	"fmt"
	"io"

	urfavecli "github.com/urfave/cli/v3"
)

type WebOptions struct {
	Addr       string
	Projects   []string
	Tool       string
	CodexPath  string
	ClaudePath string
	Jobs       int
	Messages   int
	Stdout     io.Writer
	Stderr     io.Writer
}

func webCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "web",
		Usage: "start the local Web IDE backend",
		Flags: webFlags(),
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runWebCommand(cmd, env)
		},
	}
}

func runWebCommand(cmd *urfavecli.Command, env commandEnv) error {
	if _, err := webOptionsFromCommand(cmd, env); err != nil {
		return err
	}
	return fmt.Errorf("web command is not available in this build")
}

func webFlags() []urfavecli.Flag {
	return []urfavecli.Flag{
		&urfavecli.StringFlag{Name: "addr", Value: "127.0.0.1:0", Usage: "listen address"},
		&urfavecli.StringSliceFlag{Name: "project", Usage: "project root to register; repeat for multiple projects"},
		&urfavecli.StringFlag{Name: "tool", Value: "codex", Usage: "default tool adapter: codex, claude, or claude-code"},
		&urfavecli.StringFlag{Name: "codex", Value: "codex", Usage: "codex executable path"},
		&urfavecli.StringFlag{Name: "claude", Value: "claude", Usage: "claude executable path"},
		&urfavecli.IntFlag{Name: "jobs", Usage: "maximum background jobs for future web runs; 0 uses runtime default"},
		&urfavecli.IntFlag{Name: "messages", Value: 1, Usage: "recent structured assistant messages to keep in each result block"},
	}
}

func webOptionsFromCommand(cmd *urfavecli.Command, env commandEnv) (WebOptions, error) {
	opts := WebOptions{
		Addr:       cmd.String("addr"),
		Projects:   append([]string(nil), cmd.StringSlice("project")...),
		Tool:       cmd.String("tool"),
		CodexPath:  cmd.String("codex"),
		ClaudePath: cmd.String("claude"),
		Jobs:       cmd.Int("jobs"),
		Messages:   cmd.Int("messages"),
		Stdout:     env.Stdout,
		Stderr:     env.Stderr,
	}
	if err := rejectArgs(cmd); err != nil {
		return opts, err
	}
	if opts.Messages < 1 {
		return opts, fmt.Errorf("--messages must be at least 1")
	}
	if opts.Jobs < 0 {
		return opts, fmt.Errorf("--jobs must be at least 0")
	}
	return opts, nil
}
