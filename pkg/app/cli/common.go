package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	urfavecli "github.com/urfave/cli/v3"
)

type commandEnv struct {
	Stdin  stdinFile
	Stdout io.Writer
	Stderr io.Writer
}

type stdinFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
}

func newCommandEnv(stdout, stderr io.Writer) commandEnv {
	return commandEnv{Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}
}

func defaultRunArgs(command *urfavecli.Command, args []string) []string {
	if len(args) == 0 {
		return []string{"run"}
	}
	if isExplicitCLICommand(command, args[0]) {
		return args
	}
	return append([]string{"run"}, args...)
}

func isExplicitCLICommand(command *urfavecli.Command, arg string) bool {
	switch arg {
	case "help", "-h", "--help", "-v", "--version":
		return true
	}
	return command.Command(arg) != nil
}

func writeIndentedJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func rejectArgs(cmd *urfavecli.Command) error {
	if cmd.Args().Len() > 0 {
		return fmt.Errorf("unexpected argument %q", cmd.Args().Get(0))
	}
	return nil
}

func resolveRunFiles(positional []string) ([]string, error) {
	if len(positional) == 0 {
		return nil, fmt.Errorf("no ATM file specified")
	}
	return positional, nil
}

func outputDirForRunFile(base, file string, index, total int) string {
	if base == "" || total <= 1 {
		return base
	}
	return filepath.Join(base, fmt.Sprintf("%03d-%s", index+1, sanitizePathPart(filepath.Base(file))))
}

func registryScopeFlag() *urfavecli.BoolFlag {
	return &urfavecli.BoolFlag{Name: "global", Aliases: []string{"g"}, Usage: "use the global registry instead of the project-local registry"}
}

func registryScopeName(global bool) string {
	if global {
		return "global"
	}
	return "local"
}

func atmRegistryPath(global bool, parts ...string) (string, error) {
	if !global {
		all := append([]string{".", ".atm"}, parts...)
		return filepath.Join(all...), nil
	}
	root, err := os.UserConfigDir()
	if err == nil && root != "" {
		all := append([]string{root, "atm"}, parts...)
		return filepath.Join(all...), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve global registry directory: %w", err)
	}
	all := append([]string{home, ".atm"}, parts...)
	return filepath.Join(all...), nil
}

func registryFilePath(file string, global bool) (string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	if global {
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cwd, abs)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel), nil
	}
	return abs, nil
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "todo"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "todo"
	}
	return out
}
