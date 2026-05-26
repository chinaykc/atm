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

var defaultRunFiles = []string{"todo.txt", "todo.md", "toto.md"}

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
	case "help", "-h", "--help":
		return true
	}
	return command.Command(arg) != nil
}

func todoFilesFlag() *urfavecli.StringSliceFlag {
	return &urfavecli.StringSliceFlag{Name: "file", Usage: "todo file path; repeat for multiple files"}
}

func commandFiles(cmd *urfavecli.Command) []string {
	return cmd.StringSlice("file")
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

func resolveRunFiles(flagFiles []string, positional []string) ([]string, error) {
	var files []string
	files = append(files, flagFiles...)
	files = append(files, positional...)
	if len(files) > 0 {
		return files, nil
	}
	for _, candidate := range defaultRunFiles {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return []string{candidate}, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat default todo file %q: %w", candidate, err)
		}
	}
	return nil, fmt.Errorf("no todo file specified and no default todo file found; looked for %s", strings.Join(defaultRunFiles, ", "))
}

func outputDirForRunFile(base, file string, index, total int) string {
	if base == "" || total <= 1 {
		return base
	}
	return filepath.Join(base, fmt.Sprintf("%03d-%s", index+1, sanitizePathPart(filepath.Base(file))))
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
