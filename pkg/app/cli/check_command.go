package cli

import (
	"context"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/document"
	"github.com/chinaykc/atm/pkg/view/plan"
	"io"
	"os"
	"slices"

	urfavecli "github.com/urfave/cli/v3"
)

func checkCommand(stdout io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "check",
		Usage:     "compile and validate ATM files without running them",
		ArgsUsage: "[files...]",
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{Name: "json", Usage: "print validation summary as JSON"},
			&urfavecli.BoolFlag{Name: "plan", Usage: "print the dry-run execution plan"},
			&urfavecli.BoolFlag{Name: "preview", Usage: "execute previewable lazy providers and include preview values with --plan"},
			&urfavecli.StringFlag{Name: "html", Usage: "write a plan HTML flowchart to this file"},
			&urfavecli.BoolFlag{Name: "open", Usage: "open a temporary plan HTML flowchart"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runCheckCommand(cmd, stdout)
		},
	}
}

func runCheckCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	if cmd.Bool("plan") || cmd.Bool("preview") || cmd.String("html") != "" || cmd.Bool("open") {
		return runCheckPlanCommand(cmd, stdout)
	}
	checkFiles, err := resolveRunFiles(cmd.Args().Slice())
	if err != nil {
		return err
	}
	results := make([]checkResult, 0, len(checkFiles))
	var failed bool
	for _, file := range checkFiles {
		if cmd.Bool("json") {
			result := checkFileDiagnostics(file)
			if hasErrorDiagnostics(result.Diagnostics) {
				failed = true
			}
			results = append(results, result)
			continue
		}
		result, err := checkFile(file)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	if cmd.Bool("json") {
		if err := writeIndentedJSON(stdout, struct {
			Files []checkResult `json:"files"`
		}{Files: results}); err != nil {
			return err
		}
		if failed {
			return fmt.Errorf("atm check failed")
		}
		return nil
	}
	printCheckResults(stdout, results)
	return nil
}

func runCheckPlanCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	checkFiles, err := resolveRunFiles(cmd.Args().Slice())
	if err != nil {
		return err
	}
	if len(checkFiles) != 1 {
		return fmt.Errorf("check --plan accepts exactly one ATM file")
	}
	if cmd.Bool("json") && (cmd.String("html") != "" || cmd.Bool("open")) {
		return fmt.Errorf("--json cannot be combined with --html or --open")
	}
	if cmd.Bool("preview") && (cmd.String("html") != "" || cmd.Bool("open")) {
		return fmt.Errorf("--preview cannot be combined with --html or --open")
	}
	return plan.RunFile(checkFiles[0], stdout, plan.Options{
		JSON:    cmd.Bool("json"),
		HTML:    cmd.String("html"),
		Open:    cmd.Bool("open"),
		Preview: cmd.Bool("preview"),
	})
}

func printCheckResults(stdout io.Writer, results []checkResult) {
	for _, result := range results {
		fmt.Fprintf(stdout, "atm check ok: %s\n", result.File)
		fmt.Fprintf(stdout, "  blocks: %d\n", result.Blocks)
		fmt.Fprintf(stdout, "  tasks: %d\n", result.Tasks)
		fmt.Fprintf(stdout, "  definitions: %d\n", result.Definitions)
		fmt.Fprintf(stdout, "  imports: %d\n", result.Imports)
		fmt.Fprintf(stdout, "  resources: %d\n", result.Resources)
		fmt.Fprintf(stdout, "  controls: %d\n", result.Controls)
		for _, diagnostic := range result.Diagnostics {
			fmt.Fprintf(stdout, "  %s: %s\n", diagnostic.Severity, diagnostic.Message)
		}
	}
}

type checkResult struct {
	File        string                `json:"file"`
	Blocks      int                   `json:"blocks,omitempty"`
	Tasks       int                   `json:"tasks,omitempty"`
	Definitions int                   `json:"definitions,omitempty"`
	Imports     int                   `json:"imports,omitempty"`
	Resources   int                   `json:"resources,omitempty"`
	Controls    int                   `json:"controls,omitempty"`
	Diagnostics []compiler.Diagnostic `json:"diagnostics,omitempty"`
}

func checkFile(file string) (checkResult, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return checkResult{}, fmt.Errorf("read ATM file %q: %w", file, err)
	}
	source := string(content)
	plan, err := compiler.CompileProgram(file, source)
	if err != nil {
		return checkResult{}, fmt.Errorf("check %q: %w", file, err)
	}
	diagnostics := slices.Clone(plan.Diagnostics)
	diagnostics = append(diagnostics, checkATMArtifacts(file, source)...)
	return newCheckResult(file, source, plan, diagnostics), nil
}

func checkFileDiagnostics(file string) checkResult {
	content, err := os.ReadFile(file)
	if err != nil {
		return checkResult{File: file, Diagnostics: []compiler.Diagnostic{{Severity: "error", Source: file, Message: fmt.Sprintf("read ATM file %q: %v", file, err)}}}
	}
	source := string(content)
	plan, diagnostics := compiler.CompileProgramDiagnostics(file, source)
	if len(diagnostics) > 0 {
		if hasErrorDiagnostics(diagnostics) {
			return checkResult{File: file, Diagnostics: diagnostics}
		}
	}
	diagnostics = append(diagnostics, checkATMArtifacts(file, source)...)
	return newCheckResult(file, source, plan, diagnostics)
}

func newCheckResult(file, source string, plan compiler.Plan, diagnostics []compiler.Diagnostic) checkResult {
	return checkResult{
		File:        file,
		Blocks:      len(document.ParseBlocks(source)),
		Tasks:       len(plan.Tasks),
		Definitions: len(plan.Definitions),
		Imports:     len(plan.Imports),
		Resources:   len(plan.Globals) + len(plan.Pools) + len(plan.DBs) + len(plan.Skills) + len(plan.MCPs),
		Controls:    len(plan.Controls),
		Diagnostics: diagnostics,
	}
}

func hasErrorDiagnostics(diagnostics []compiler.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "" || diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}
