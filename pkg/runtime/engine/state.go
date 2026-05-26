package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type atmStateFile struct {
	Version  int                     `json:"version"`
	Document string                  `json:"document"`
	Tasks    map[string]atmTaskState `json:"tasks"`
}

type atmTaskState struct {
	Status             string   `json:"status"`
	SourcePromptHash   string   `json:"sourcePromptHash,omitempty"`
	RenderedPromptHash string   `json:"renderedPromptHash,omitempty"`
	PlanHash           string   `json:"planHash,omitempty"`
	StartedAt          string   `json:"startedAt,omitempty"`
	UpdatedAt          string   `json:"updatedAt,omitempty"`
	Path               []string `json:"path,omitempty"`
	Runs               int      `json:"runs"`
	Report             string   `json:"report,omitempty"`
	Logs               []string `json:"logs,omitempty"`
	Orphan             bool     `json:"orphan,omitempty"`
}

type taskStateUpdate struct {
	ID                 string
	Status             string
	SourcePromptHash   string
	RenderedPromptHash string
	PlanHash           string
	StartedAt          time.Time
	UpdatedAt          time.Time
	Path               []string
	Runs               int
	Report             string
	Logs               []string
	Orphan             bool
}

func (e *Engine) updateTaskState(blockIndex int, update taskStateUpdate) error {
	_ = blockIndex
	id := strings.TrimSpace(update.ID)
	if id == "" {
		return nil
	}
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	path := e.statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	state := atmStateFile{Version: 2, Document: e.stateDocument(), Tasks: make(map[string]atmTaskState)}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("read state file: %w", err)
		}
		if state.Tasks == nil {
			state.Tasks = make(map[string]atmTaskState)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read state file: %w", err)
	}
	state.Version = 2
	state.Document = e.stateDocument()

	task := state.Tasks[id]
	if update.Status != "" {
		task.Status = update.Status
	}
	if update.SourcePromptHash != "" {
		task.SourcePromptHash = update.SourcePromptHash
	}
	if update.RenderedPromptHash != "" {
		task.RenderedPromptHash = update.RenderedPromptHash
	}
	if update.PlanHash != "" {
		task.PlanHash = update.PlanHash
	}
	if !update.StartedAt.IsZero() {
		task.StartedAt = update.StartedAt.Format(time.RFC3339)
	}
	if !update.UpdatedAt.IsZero() {
		task.UpdatedAt = update.UpdatedAt.Format(time.RFC3339)
	}
	if len(update.Path) > 0 {
		task.Path = slices.Clone(update.Path)
	}
	if update.Runs >= 0 {
		task.Runs = update.Runs
	}
	if update.Report != "" {
		task.Report = update.Report
	}
	if update.Orphan {
		task.Orphan = true
	}
	task.Logs = mergeStateLogs(task.Logs, e.relativeStatePaths(update.Logs))
	state.Tasks[id] = task

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

func (e *Engine) statePath() string {
	return filepath.Join(e.root, ".atm", "state.json")
}

func (e *Engine) stateDocument() string {
	document := e.document
	if document == "" {
		document = e.filePath
	}
	return e.relativeStatePath(document)
}

func (e *Engine) relativeStatePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		out = append(out, e.relativeStatePath(path))
	}
	return out
}

func (e *Engine) relativeStatePath(path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(e.root, path)
	if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func mergeStateLogs(existing, next []string) []string {
	if len(existing) == 0 {
		return slices.Clone(next)
	}
	seen := make(map[string]bool, len(existing)+len(next))
	out := make([]string, 0, len(existing)+len(next))
	for _, value := range existing {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, value := range next {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func hashStateText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hashTaskPlan(task compiler.Task) string {
	shape := struct {
		Flow   compiler.FlowNode        `json:"flow"`
		Output *compiler.OutputSpec     `json:"output,omitempty"`
		Return *compiler.ReturnSpec     `json:"return,omitempty"`
		DB     compiler.DBTaskConfig    `json:"db,omitempty"`
		Skill  compiler.SkillTaskConfig `json:"skill,omitempty"`
		MCP    compiler.MCPTaskConfig   `json:"mcp,omitempty"`
	}{
		Flow:   task.Flow,
		Output: task.Output,
		Return: task.Return,
		DB:     task.DB,
		Skill:  task.Skill,
		MCP:    task.MCP,
	}
	data, err := json.Marshal(shape)
	if err != nil {
		return ""
	}
	return hashStateText(string(data))
}

func (x *taskExecution) stateLogPaths() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.stateLogPathsLocked()
}

func (x *taskExecution) currentRuns() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.runs
}

func (x *taskExecution) stateLogPathsLocked() []string {
	var logs []string
	if x.logPath != "" {
		logs = append(logs, x.logPath)
	}
	logs = append(logs, x.eventPaths...)
	return logs
}
