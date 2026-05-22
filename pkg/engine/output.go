package engine

import (
	"atm/pkg/dsl"
	"fmt"
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

func (r *outputRegistry) create(taskIndex int) (*os.File, string, error) {
	file, err := os.CreateTemp(r.dir, fmt.Sprintf("task-%03d-*.log", taskIndex+1))
	if err != nil {
		return nil, "", fmt.Errorf("create task output file: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, file.Name())
	r.mu.Unlock()
	return file, file.Name(), nil
}

func (r *outputRegistry) writeEvents(taskIndex, runNumber int, tool, agent, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	name := fmt.Sprintf("task-%03d-run-%03d-%s", taskIndex+1, runNumber, sanitizeFilePart(tool))
	if agent != "" {
		name += "-" + sanitizeFilePart(agent)
	}
	name += ".jsonl"
	path := filepath.Join(r.dir, name)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		return "", fmt.Errorf("write agent events: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) writeStructuredOutput(taskIndex, runNumber int, requestedName, suffix string, data []byte) (string, error) {
	name := cleanStructuredOutputName(requestedName)
	if name == "" {
		name = defaultStructuredOutputName(taskIndex, runNumber, suffix)
	} else if suffix != "" {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		name = stem + "-" + sanitizeFilePart(suffix) + ext
	}
	path, err := r.writeUniqueFile(name, data)
	if err != nil {
		return "", fmt.Errorf("write structured output: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) writeTextOutput(taskIndex, runNumber int, requestedName, suffix string, data []byte) (string, error) {
	name := cleanTextOutputName(requestedName)
	if name == "" {
		name = defaultTextOutputName(taskIndex, runNumber, suffix)
	} else if suffix != "" {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		name = stem + "-" + sanitizeFilePart(suffix) + ext
	}
	path, err := r.writeUniqueFile(name, data)
	if err != nil {
		return "", fmt.Errorf("write text output: %w", err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return path, nil
}

func (r *outputRegistry) writeUniqueFile(name string, data []byte) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		path := filepath.Join(r.dir, candidate)
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

func (e *Engine) taskWriters(taskIndex int) (io.Writer, io.Writer, *os.File, string, error) {
	file, path, err := e.outputs.create(taskIndex)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return io.MultiWriter(e.stdout, file), io.MultiWriter(e.stderr, file), file, path, nil
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
	blocks := dsl.ParseBlocks(string(content))
	line := 1
	for i, block := range blocks {
		line += strings.Count(block.Prefix, "\n")
		start := line
		displayBody := block.Body
		if clean, _, err := dsl.StripRunning(displayBody); err == nil {
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
