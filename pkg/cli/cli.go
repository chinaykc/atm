package cli

import (
	"atm/pkg/dsl"
	"atm/pkg/engine"
	"atm/pkg/mcp"
	"atm/pkg/store"
	"atm/pkg/tools"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var defaultRunFiles = []string{"todo.txt", "todo.md", "toto.md"}

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return runRunCLI(args[1:], stdout, stderr)
		case "mcp":
			return runMCPCLI(args[1:], stdout, stderr)
		case "append":
			return runAppendCLI(args[1:], stdout, stderr)
		case "plan":
			return dsl.RunPlanCLI(args[1:], stdout, stderr)
		case "format":
			return runFormatCLI(args[1:], stdout, stderr)
		case "untag":
			return runUntagCLI(args[1:], stdout, stderr)
		}
	}

	return runRunCLI(args, stdout, stderr)
}

func runRunCLI(args []string, stdout, stderr io.Writer) error {
	var files fileListFlag
	var codex string
	var claude string
	var tool string
	var messages int
	var output string
	var jobs int
	flags := flag.NewFlagSet("atm run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Var(&files, "file", "todo file path; repeat for multiple files")
	flags.StringVar(&tool, "tool", "codex", "tool adapter to run: codex, claude, or claude-code")
	flags.StringVar(&codex, "codex", "codex", "codex executable path used by -tool codex")
	flags.StringVar(&claude, "claude", "claude", "claude executable path used by -tool claude or -tool claude-code")
	flags.IntVar(&messages, "messages", 1, "number of recent structured assistant messages to keep in each result block")
	flags.StringVar(&output, "output", "", "directory for execution artifacts; defaults to .atm/YYYYMMDDHHMMSS[-N]")
	flags.StringVar(&output, "o", "", "shorthand for -output")
	flags.IntVar(&jobs, "jobs", 0, "maximum number of concurrently running background branches across all pools; defaults to NumCPU")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm runs pending prompts from a todo file through a tool adapter.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Subcommands:")
		fmt.Fprintln(stderr, "  run     run pending prompt blocks (also the default command)")
		fmt.Fprintln(stderr, "  mcp     run a temporary stdio MCP server")
		fmt.Fprintln(stderr, "  append  append a formatted prompt block to the active todo file")
		fmt.Fprintln(stderr, "  plan    print a dry-run execution plan")
		fmt.Fprintln(stderr, "  format  format generated tags in the todo file")
		fmt.Fprintln(stderr, "  untag   remove done and/or running state from the todo file")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Supported task commands: /resume, /args, /cd, /let, /bash, /output, /db, /skill, /mcp, /def, /call, /return, /import, /pool, /if, /else, /for, /go, /wait")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage of atm run [files...]:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if messages < 1 {
		return fmt.Errorf("-messages must be at least 1")
	}
	if jobs < 0 {
		return fmt.Errorf("-jobs must be at least 1 when set")
	}
	runFiles, err := resolveRunFiles(files, flags.Args())
	if err != nil {
		return err
	}

	runner, err := tools.NewRunner(tool, tools.Config{CodexPath: codex, ClaudePath: claude})
	if err != nil {
		return err
	}
	for i, file := range runFiles {
		if len(runFiles) > 1 {
			fmt.Fprintf(stderr, "atm run file %d/%d: %s\n", i+1, len(runFiles), file)
		}
		outputDir := outputDirForRunFile(output, file, i, len(runFiles))
		if err := engine.Run(context.Background(), engine.Options{FilePath: file, Runner: runner, ToolName: tool, CodexPath: codex, ClaudePath: claude, Stdout: stdout, Stderr: stderr, MessageLimit: messages, OutputDir: outputDir, GlobalJobs: jobs}); err != nil {
			var readErr store.ReadError
			if errors.As(err, &readErr) {
				flags.Usage()
			}
			return err
		}
	}
	return nil
}

type fileListFlag []string

func (f *fileListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *fileListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("-file cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func resolveRunFiles(flagFiles fileListFlag, positional []string) ([]string, error) {
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
		if err != nil && !errors.Is(err, os.ErrNotExist) {
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

func runMCPCLI(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing mcp subcommand")
	}
	switch args[0] {
	case "check":
		return mcp.RunCheckServerCLI(args[1:], os.Stdin, stdout, stderr)
	case "output":
		return mcp.RunOutputServerCLI(args[1:], os.Stdin, stdout, stderr)
	case "db":
		return mcp.RunDBServerCLI(args[1:], os.Stdin, stdout, stderr)
	case "defs":
		return engine.RunDefsMCPServerCLI(args[1:], os.Stdin, stdout, stderr)
	default:
		return fmt.Errorf("unknown mcp subcommand %q", args[0])
	}
}

func runAppendCLI(args []string, stdout, stderr io.Writer) error {
	var file string
	flags := flag.NewFlagSet("atm append", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&file, "file", "todo.txt", "todo file path")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm append appends a formatted prompt block to a todo file.")
		fmt.Fprintln(stderr, "Usage of atm append:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	var prompt string
	if flags.NArg() > 0 {
		prompt = strings.Join(flags.Args(), " ")
	} else {
		stat, err := os.Stdin.Stat()
		if err != nil {
			return fmt.Errorf("stat stdin: %w", err)
		}
		if stat.Mode()&os.ModeCharDevice != 0 {
			edited, err := editPromptBlock(stderr)
			if err != nil {
				return err
			}
			prompt = edited
		} else {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read prompt block from stdin: %w", err)
			}
			prompt = string(data)
		}
	}
	return RunAppend(file, prompt, stdout)
}

func runFormatCLI(args []string, stdout, stderr io.Writer) error {
	var file string
	flags := flag.NewFlagSet("atm format", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&file, "file", "todo.txt", "todo file path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	return RunFormat(file, stdout)
}

func runUntagCLI(args []string, stdout, stderr io.Writer) error {
	var file string
	removeDone := true
	removeRunning := true
	flags := flag.NewFlagSet("atm untag", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&file, "file", "todo.txt", "todo file path")
	flags.BoolVar(&removeDone, "done", true, "remove done state")
	flags.BoolVar(&removeRunning, "running", true, "remove running state")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	return RunUntag(file, stdout, UntagOptions{Done: removeDone, Running: removeRunning})
}
