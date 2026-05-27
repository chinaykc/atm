package cli

import (
	"errors"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/document"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"os"
	"path/filepath"
	"strings"
)

func checkATMArtifacts(file, content string) []compiler.Diagnostic {
	root := filepath.Dir(file)
	docReports := make(map[string]marker.ATMReportMeta)
	for _, block := range document.ParseBlocks(content) {
		meta, ok := marker.ATMReportMetadata(block.Body)
		if !ok {
			continue
		}
		docReports[meta.ID] = meta
	}
	var diagnostics []compiler.Diagnostic
	warn := func(message string) {
		diagnostics = append(diagnostics, compiler.Diagnostic{Severity: "warning", Source: file, Message: message})
	}

	checkDocumentReports(root, docReports, warn)
	stateIDs := checkStateArtifacts(root, docReports, warn)
	checkOrphanReports(root, docReports, stateIDs, warn)
	return diagnostics
}

func checkDocumentReports(root string, docReports map[string]marker.ATMReportMeta, warn func(string)) {
	for id, meta := range docReports {
		if strings.TrimSpace(meta.Report) == "" {
			warn(fmt.Sprintf("ATM report id %q has no detail report path", id))
			continue
		}
		if _, err := os.Stat(resolveArtifactPath(root, meta.Report)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				warn(fmt.Sprintf("ATM report id %q references missing detail report %s", id, meta.Report))
			} else {
				warn(fmt.Sprintf("ATM report id %q detail report %s cannot be checked: %v", id, meta.Report, err))
			}
		}
	}
}

func checkStateArtifacts(root string, docReports map[string]marker.ATMReportMeta, warn func(string)) map[string]bool {
	statePath := filepath.Join(root, ".atm", "state.json")
	state, hasState, err := readStateFile(statePath)
	if err != nil {
		if hasState {
			warn(fmt.Sprintf(".atm/state.json is invalid: %v", err))
		} else {
			warn(fmt.Sprintf(".atm/state.json cannot be checked: %v", err))
		}
		return nil
	}
	if !hasState {
		return nil
	}
	stateIDs := stateTaskIDs(state)
	for id, task := range state.Tasks {
		checkStateTask(root, id, task, docReports, warn)
	}
	return stateIDs
}

func checkStateTask(root, id string, task atmTaskState, docReports map[string]marker.ATMReportMeta, warn func(string)) {
	meta, ok := docReports[id]
	if !ok {
		warn(fmt.Sprintf(".atm/state.json contains task %q with no matching report in the todo document", id))
	} else {
		if task.Status != "" && meta.Status != "" && task.Status != meta.Status {
			warn(fmt.Sprintf("ATM report id %q status mismatch: document=%s state=%s", id, meta.Status, task.Status))
		}
		if task.Report != "" && meta.Report != "" && task.Report != meta.Report {
			warn(fmt.Sprintf("ATM report id %q report path mismatch: document=%s state=%s", id, meta.Report, task.Report))
		}
		if task.SourcePromptHash != "" && meta.Source != "" && task.SourcePromptHash != meta.Source {
			warn(fmt.Sprintf("ATM report id %q source hash mismatch: document=%s state=%s", id, meta.Source, task.SourcePromptHash))
		}
		if task.RenderedPromptHash != "" && meta.Rendered != "" && task.RenderedPromptHash != meta.Rendered {
			warn(fmt.Sprintf("ATM report id %q rendered prompt hash mismatch: document=%s state=%s", id, meta.Rendered, task.RenderedPromptHash))
		}
	}
	if task.Report == "" {
		return
	}
	if _, err := os.Stat(resolveArtifactPath(root, task.Report)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			warn(fmt.Sprintf(".atm/state.json task %q references missing detail report %s", id, task.Report))
		} else {
			warn(fmt.Sprintf(".atm/state.json task %q detail report %s cannot be checked: %v", id, task.Report, err))
		}
	}
}

func resolveArtifactPath(root, path string) string {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func checkOrphanReports(root string, docReports map[string]marker.ATMReportMeta, stateIDs map[string]bool, warn func(string)) {
	reportFiles, err := filepath.Glob(filepath.Join(root, ".atm", "reports", "*.md"))
	if err != nil {
		warn(fmt.Sprintf(".atm/reports cannot be checked: %v", err))
		return
	}
	for _, reportFile := range reportFiles {
		id := strings.TrimSuffix(filepath.Base(reportFile), filepath.Ext(reportFile))
		if _, ok := docReports[id]; ok {
			continue
		}
		if stateIDs[id] {
			continue
		}
		rel, relErr := filepath.Rel(root, reportFile)
		if relErr != nil {
			rel = reportFile
		}
		warn(fmt.Sprintf("orphan detail report %s has no matching report in the todo document or state", filepath.ToSlash(rel)))
	}
	checkOrphanTaskReports(root, docReports, stateIDs, warn)
}

func checkOrphanTaskReports(root string, docReports map[string]marker.ATMReportMeta, stateIDs map[string]bool, warn func(string)) {
	entries, err := os.ReadDir(filepath.Join(root, "tasks"))
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
		reportFile := filepath.Join(root, "tasks", id, "report.md")
		if _, err := os.Stat(reportFile); err != nil {
			continue
		}
		rel, relErr := filepath.Rel(root, reportFile)
		if relErr != nil {
			rel = reportFile
		}
		warn(fmt.Sprintf("orphan detail report %s has no matching report in the todo document or state", filepath.ToSlash(rel)))
	}
}
