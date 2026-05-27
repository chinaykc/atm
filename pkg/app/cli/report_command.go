package cli

import (
	"context"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/document"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chinaykc/atm/pkg/lang/compiler"

	urfavecli "github.com/urfave/cli/v3"
)

var reportStatuses = []string{"done", "running", "failed", "skipped", "draft"}

func reportCommand(stdout io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "report",
		Usage:     "summarize task reports and ATM audit state",
		ArgsUsage: "[files...]",
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{Name: "json", Usage: "print report summary as JSON"},
			&urfavecli.BoolFlag{Name: "last", Usage: "report the latest run for the current project"},
			&urfavecli.StringFlag{Name: "project", Usage: "select the latest run for this project root"},
			&urfavecli.StringFlag{Name: "source", Usage: "select the latest run for this source file"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			return runReportCommand(cmd, stdout)
		},
	}
}

func runReportCommand(cmd *urfavecli.Command, stdout io.Writer) error {
	targets, err := resolveReportTargets(cmd)
	if err != nil {
		return err
	}
	results := make([]reportResult, 0, len(targets))
	for _, target := range targets {
		result, err := buildReportTarget(target)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	if cmd.Bool("json") {
		return writeIndentedJSON(stdout, struct {
			Files []reportResult `json:"files"`
		}{Files: results})
	}
	printReportResults(stdout, results)
	return nil
}

type reportTarget struct {
	File     string
	Manifest *runManifest
}

func resolveReportTargets(cmd *urfavecli.Command) ([]reportTarget, error) {
	if cmd.Bool("last") || cmd.String("project") != "" || cmd.String("source") != "" || cmd.Args().Len() == 0 {
		if cmd.Args().Len() > 0 {
			return nil, fmt.Errorf("report filters cannot be combined with positional arguments")
		}
		project := cmd.String("project")
		if project == "" && cmd.String("source") == "" {
			wd, _ := os.Getwd()
			project = wd
		}
		manifest, _, err := selectLatestRunCopy(project, cmd.String("source"))
		if err != nil {
			return nil, err
		}
		return []reportTarget{{Manifest: &manifest}}, nil
	}
	var targets []reportTarget
	for _, arg := range cmd.Args().Slice() {
		if manifest, _, err := loadRunManifest(arg); err == nil {
			targets = append(targets, reportTarget{Manifest: &manifest})
			continue
		}
		file, err := filepath.Abs(arg)
		if err != nil {
			return nil, err
		}
		if manifest, _, err := manifestForRunFile(file); err == nil {
			targets = append(targets, reportTarget{Manifest: &manifest})
			continue
		}
		if manifest, _, err := selectLatestRunCopy("", file); err == nil {
			targets = append(targets, reportTarget{Manifest: &manifest})
			continue
		}
		targets = append(targets, reportTarget{File: arg})
	}
	return targets, nil
}

func manifestForRunFile(file string) (runManifest, string, error) {
	dir := filepath.Dir(file)
	for {
		manifestPath := filepath.Join(dir, "manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			return loadRunManifest(manifestPath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return runManifest{}, "", fmt.Errorf("no run manifest found for %s", file)
}

func buildReportTarget(target reportTarget) (reportResult, error) {
	if target.Manifest != nil {
		file := target.Manifest.WorkingFile
		if _, err := os.Stat(file); err != nil {
			file = target.Manifest.ResultFile
		}
		result, err := buildReportWithTaskDir(file, target.Manifest.TaskDir)
		if err != nil {
			return reportResult{}, err
		}
		result.File = target.Manifest.ResultFile
		result.RunID = target.Manifest.RunID
		result.Source = target.Manifest.SourcePath
		return result, nil
	}
	return buildReport(target.File)
}

func printReportResults(stdout io.Writer, results []reportResult) {
	for _, result := range results {
		fmt.Fprintf(stdout, "atm report: %s\n", result.File)
		if result.RunID != "" {
			fmt.Fprintf(stdout, "  run: %s\n", result.RunID)
		}
		if result.Source != "" {
			fmt.Fprintf(stdout, "  source: %s\n", result.Source)
		}
		for _, status := range reportStatuses {
			fmt.Fprintf(stdout, "  %s: %d\n", status, result.Counts[status])
		}
		if len(result.Failures) > 0 {
			fmt.Fprintln(stdout, "  failures:")
			for _, task := range result.Failures {
				fmt.Fprintf(stdout, "  - %s %s\n", task.ID, task.Report)
			}
		}
		if len(result.Orphans) > 0 {
			fmt.Fprintln(stdout, "  orphan reports:")
			for _, task := range result.Orphans {
				fmt.Fprintf(stdout, "  - %s %s\n", task.ID, task.Report)
			}
		}
		if len(result.RecentLogs) > 0 {
			fmt.Fprintln(stdout, "  recent logs:")
			for _, logPath := range result.RecentLogs {
				fmt.Fprintf(stdout, "  - %s\n", logPath)
			}
		}
	}
}

type reportResult struct {
	File       string         `json:"file"`
	RunID      string         `json:"runId,omitempty"`
	Source     string         `json:"source,omitempty"`
	Counts     map[string]int `json:"counts"`
	Failures   []reportTask   `json:"failures,omitempty"`
	Orphans    []reportTask   `json:"orphans,omitempty"`
	RecentLogs []string       `json:"recent_logs,omitempty"`
}

type reportTask struct {
	ID       string `json:"id"`
	Status   string `json:"status,omitempty"`
	Report   string `json:"report,omitempty"`
	Source   string `json:"source,omitempty"`
	Rendered string `json:"rendered,omitempty"`
}

func buildReport(file string) (reportResult, error) {
	return buildReportWithTaskDir(file, "")
}

func buildReportWithTaskDir(file, extraTaskDir string) (reportResult, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return reportResult{}, fmt.Errorf("read ATM file %q: %w", file, err)
	}
	result := reportResult{File: file, Counts: emptyReportCounts()}
	docReports := make(map[string]marker.ATMReportMeta)
	for _, block := range document.ParseBlocks(string(content)) {
		if meta, ok := marker.ATMReportMetadata(block.Body); ok {
			status := strings.ToLower(strings.TrimSpace(meta.Status))
			if status == "" {
				status = "unknown"
			}
			result.Counts[status]++
			docReports[meta.ID] = meta
			if status == "failed" {
				result.Failures = append(result.Failures, reportTask{ID: meta.ID, Status: status, Report: meta.Report, Source: meta.Source, Rendered: meta.Rendered})
			}
		}
	}
	if plan, diagnostics := compiler.CompileProgramDiagnostics(file, string(content)); !hasErrorDiagnostics(diagnostics) {
		result.Counts["draft"] = len(plan.Tasks)
	}
	statePath := filepath.Join(filepath.Dir(file), ".atm", "state.json")
	state, hasState, _ := readStateFile(statePath)
	if hasState {
		appendStateReport(&result, state)
	}
	appendOrphanReportFiles(file, extraTaskDir, &result, docReports, stateTaskIDs(state))
	result.RecentLogs = dedupeStrings(result.RecentLogs)
	if len(result.RecentLogs) > 5 {
		result.RecentLogs = result.RecentLogs[len(result.RecentLogs)-5:]
	}
	return result, nil
}

func emptyReportCounts() map[string]int {
	counts := make(map[string]int, len(reportStatuses))
	for _, status := range reportStatuses {
		counts[status] = 0
	}
	return counts
}

func appendStateReport(result *reportResult, state atmStateFile) {
	for id, task := range state.Tasks {
		if task.Orphan {
			result.Orphans = append(result.Orphans, reportTask{ID: id, Status: task.Status, Report: task.Report, Source: task.SourcePromptHash, Rendered: task.RenderedPromptHash})
		}
		if task.Status == "failed" {
			result.Failures = appendMissingReportTask(result.Failures, reportTask{ID: id, Status: task.Status, Report: task.Report, Source: task.SourcePromptHash, Rendered: task.RenderedPromptHash})
		}
		result.RecentLogs = append(result.RecentLogs, task.Logs...)
	}
}

func appendOrphanReportFiles(file, extraTaskDir string, result *reportResult, docReports map[string]marker.ATMReportMeta, stateIDs map[string]bool) {
	root := filepath.Dir(file)
	reportFiles, _ := filepath.Glob(filepath.Join(root, ".atm", "reports", "*.md"))
	for _, reportFile := range reportFiles {
		id := strings.TrimSuffix(filepath.Base(reportFile), filepath.Ext(reportFile))
		if _, ok := docReports[id]; ok {
			continue
		}
		if stateIDs[id] {
			continue
		}
		rel := reportFile
		if r, err := filepath.Rel(root, reportFile); err == nil {
			rel = filepath.ToSlash(r)
		}
		result.Orphans = appendMissingReportTask(result.Orphans, reportTask{ID: id, Report: rel})
	}
	appendTaskReportFiles(filepath.Join(root, "tasks"), result, docReports, stateIDs, root)
	if extraTaskDir != "" {
		appendTaskReportFiles(extraTaskDir, result, docReports, stateIDs, root)
	}
}

func appendTaskReportFiles(taskDir string, result *reportResult, docReports map[string]marker.ATMReportMeta, stateIDs map[string]bool, root string) {
	if strings.TrimSpace(taskDir) == "" {
		return
	}
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, ok := docReports[id]; ok {
			continue
		}
		if stateIDs[id] {
			continue
		}
		reportFile := filepath.Join(taskDir, id, "report.md")
		if _, err := os.Stat(reportFile); err != nil {
			continue
		}
		rel := reportFile
		if r, err := filepath.Rel(root, reportFile); err == nil {
			rel = filepath.ToSlash(r)
		}
		result.Orphans = appendMissingReportTask(result.Orphans, reportTask{ID: id, Report: rel})
	}
}

func appendMissingReportTask(tasks []reportTask, task reportTask) []reportTask {
	for _, existing := range tasks {
		if existing.ID == task.ID {
			return tasks
		}
	}
	return append(tasks, task)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
