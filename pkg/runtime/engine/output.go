package engine

import (
	"bytes"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/document"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

type outputRegistry struct {
	dir   string
	mu    sync.Mutex
	files []string
}

func newOutputRegistry(dir string) (*outputRegistry, error) {
	if dir == "" {
		var err error
		dir, err = defaultOutputDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	return &outputRegistry{dir: dir}, nil
}

func defaultOutputDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	base := filepath.Join(wd, ".atm")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("create output base directory: %w", err)
	}
	stamp := time.Now().Format("20060102150405")
	for i := 0; ; i++ {
		name := stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", stamp, i)
		}
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("create output directory: %w", err)
		}
		return dir, nil
	}
}

func (r *outputRegistry) track(path string) {
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
}

func (r *outputRegistry) writeEvents(taskDir string, taskIndex, runNumber int, tool, agent, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	name := fmt.Sprintf("task-%03d-run-%03d-%s", taskIndex+1, runNumber, sanitizeFilePart(tool))
	if agent != "" {
		name += "-" + sanitizeFilePart(agent)
	}
	name += ".jsonl"
	if strings.TrimSpace(taskDir) == "" {
		taskDir = r.dir
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", fmt.Errorf("create task event directory: %w", err)
	}
	path := filepath.Join(taskDir, name)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		return "", fmt.Errorf("write agent events: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) writeStructuredOutput(taskDir string, taskIndex, runNumber int, requestedName, suffix string, data []byte) (string, error) {
	name := cleanStructuredOutputName(requestedName)
	if name == "" {
		name = defaultStructuredOutputName(taskIndex, runNumber, suffix)
	} else if suffix != "" {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		name = stem + "-" + sanitizeFilePart(suffix) + ext
	}
	path, err := r.writeUniqueFileInDir(taskDir, name, data)
	if err != nil {
		return "", fmt.Errorf("write structured output: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) writeTextOutput(taskDir string, taskIndex, runNumber int, requestedName, suffix string, data []byte) (string, error) {
	name := cleanTextOutputName(requestedName)
	if name == "" {
		name = defaultTextOutputName(taskIndex, runNumber, suffix)
	} else if suffix != "" {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		name = stem + "-" + sanitizeFilePart(suffix) + ext
	}
	path, err := r.writeUniqueFileInDir(taskDir, name, data)
	if err != nil {
		return "", fmt.Errorf("write text output: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) writeUniqueFileInDir(dir, name string, data []byte) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = r.dir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		path := filepath.Join(dir, candidate)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			os.Remove(path)
			return "", err
		}
		if err := file.Close(); err != nil {
			os.Remove(path)
			return "", err
		}
		return path, nil
	}
}

func (r *outputRegistry) writeResultDocument(sourcePath string) (string, error) {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("read result document source: %w", err)
	}
	path := filepath.Join(r.dir, "result.md")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("write result document: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	files := make([]string, len(r.files))
	copy(files, r.files)
	return files
}

func (r *outputRegistry) dirPath() string {
	return r.dir
}

func (e *Engine) taskWriters(taskIndex int, taskDir string) (io.Writer, io.Writer, *os.File, string, error) {
	if strings.TrimSpace(taskDir) == "" {
		taskDir = e.taskArtifactDir(taskIndex, "")
	}
	logDir := filepath.Join(taskDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, nil, nil, "", fmt.Errorf("create task log directory: %w", err)
	}
	file, err := os.CreateTemp(logDir, fmt.Sprintf("task-%03d-*.log", taskIndex+1))
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("create task log file: %w", err)
	}
	path := file.Name()
	e.outputs.track(path)
	return io.MultiWriter(e.stdout, file), io.MultiWriter(e.stderr, file), file, path, nil
}

type detailReportInfo struct {
	ID                 string
	Source             string
	RenderedPromptHash string
	PlanHash           string
	Report             string
	Start              time.Time
	End                time.Time
	Runs               int
	Error              string
	Messages           []compiler.OutputMessage
	Orphan             bool
}

func (e *Engine) writeDetailReport(taskIndex int, status string, info detailReportInfo) error {
	if strings.TrimSpace(info.ID) == "" || strings.TrimSpace(info.Report) == "" {
		return nil
	}
	reportPath := filepath.Join(e.taskArtifactDir(taskIndex, info.ID), "report.md")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "# ATM Report: %s\n\n", info.ID)
	fmt.Fprintf(&b, "- Status: %s\n", status)
	fmt.Fprintf(&b, "- Task block: %d\n", taskIndex+1)
	fmt.Fprintf(&b, "- Source: %s\n", info.Source)
	if strings.TrimSpace(info.RenderedPromptHash) != "" {
		fmt.Fprintf(&b, "- Rendered prompt: %s\n", info.RenderedPromptHash)
	}
	if strings.TrimSpace(info.PlanHash) != "" {
		fmt.Fprintf(&b, "- Plan: %s\n", info.PlanHash)
	}
	fmt.Fprintf(&b, "- Started: %s\n", info.Start.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Finished: %s\n", info.End.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Duration: %s\n", info.End.Sub(info.Start).Round(time.Millisecond))
	fmt.Fprintf(&b, "- Runs: %d\n", info.Runs)
	fmt.Fprintf(&b, "- Output directory: %s\n", e.outputs.dirPath())
	if info.Orphan {
		b.WriteString("- Orphan: true\n")
	}
	if strings.TrimSpace(info.Error) != "" {
		fmt.Fprintf(&b, "- Error: %s\n", strings.Join(strings.Fields(info.Error), " "))
	}
	if len(info.Messages) > 0 {
		b.WriteString("\n## Recent Messages\n\n")
		for _, message := range info.Messages {
			label := strings.TrimSpace(message.Role)
			if label == "" {
				label = "assistant"
			}
			if tool := strings.TrimSpace(message.Tool); tool != "" {
				label += " (" + tool + ")"
			}
			if agent := strings.TrimSpace(message.Agent); agent != "" {
				label += " [" + agent + "]"
			}
			fmt.Fprintf(&b, "### %s\n\n", label)
			text := strings.TrimSpace(message.Text)
			if text == "" {
				text = "(empty)"
			}
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	}
	if err := os.WriteFile(reportPath, b.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write detail report: %w", err)
	}
	return nil
}

func (e *Engine) taskArtifactDir(taskIndex int, taskID string) string {
	name := strings.TrimSpace(taskID)
	if name == "" {
		name = fmt.Sprintf("task-%03d", taskIndex+1)
	}
	return filepath.Join(e.taskDir, sanitizeFilePart(name))
}

func (e *Engine) taskReportPath(taskIndex int, taskID string) string {
	return e.relativeStatePath(filepath.Join(e.taskArtifactDir(taskIndex, taskID), "report.md"))
}

func (e *Engine) taskLineRangeLabel(taskIndex int) string {
	start, end, ok := e.taskLineRange(taskIndex)
	if !ok {
		return ""
	}
	if start == end {
		return fmt.Sprintf(" line %d", start)
	}
	return fmt.Sprintf(" lines %d-%d", start, end)
}

func (e *Engine) taskLineRange(taskIndex int) (int, int, bool) {
	content, err := os.ReadFile(e.filePath)
	if err != nil {
		return 0, 0, false
	}
	blocks := document.ParseBlocks(string(content))
	line := 1
	for i, block := range blocks {
		line += strings.Count(block.Prefix, "\n")
		start := line
		displayBody := block.Body
		if clean, _, err := marker.StripRunning(displayBody); err == nil {
			displayBody = clean
		}
		end := start + logicalLineCount(displayBody) - 1
		if i == taskIndex {
			return start, end, true
		}
		line += strings.Count(block.Body, "\n") + strings.Count(block.Sep, "\n")
	}
	return 0, 0, false
}

func logicalLineCount(text string) int {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return 1
	}
	return strings.Count(text, "\n") + 1
}

func sanitizeFilePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "agent"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r)
		if ok {
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
		return "agent"
	}
	return out
}
