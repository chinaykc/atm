package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type codexSessionMeta struct {
	ID           string
	ForkedFromID string
	Cwd          string
	Timestamp    time.Time
}

type codexRolloutRecord struct {
	raw     []byte
	parsed  map[string]any
	lineNum int
}

func (r codexRunner) executeFork(ctx context.Context, todoPath, prompt string, opts ir.RunOptions, stdout, stderr io.Writer) (ExecuteResult, error) {
	skillCleanup, err := prepareSkills("codex", opts)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer skillCleanup()
	resultFile, schemaFile, cleanup, err := prepareOutputMCP(opts.Output)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer cleanup()
	dbConfigFile, dbCleanup, err := prepareDBMCP(opts.DBs)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer dbCleanup()
	defConfigFile, defCleanup, err := prepareDefMCP(opts.DefMCP)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer defCleanup()
	if err := validateRuntimeMCPs(opts.MCPs); err != nil {
		return ExecuteResult{}, err
	}
	if opts.Output != nil && opts.Output.IsStructured() {
		prompt = appendOutputToolInstruction(prompt)
	}
	if len(opts.DBs) > 0 {
		prompt = appendDBToolInstruction(prompt)
	}
	if err := validateResumeOptions(opts); err != nil {
		return ExecuteResult{}, err
	}
	if opts.DefMCP != nil && len(opts.DefMCP.Definitions) > 0 && opts.DefMCP.Depth > 0 {
		prompt = appendDefToolInstruction(prompt, opts.DefMCP.Definitions)
	}

	forkID, _, err := materializeCodexForkSession(opts.ResumeSessionID, effectiveWorkdir(opts.Workdir))
	if err != nil {
		return ExecuteResult{}, err
	}
	resumeOpts := opts
	resumeOpts.Fork = false
	resumeOpts.Resume = true
	resumeOpts.ResumeSessionID = forkID

	cmd := exec.CommandContext(ctx, r.path, codexArgs(resumeOpts, resultFile, schemaFile, dbConfigFile, defConfigFile, false)...)
	cmd.Env = toolEnv(todoPath)
	cmd.Dir = opts.Workdir
	cmd.Stdin = strings.NewReader(prompt)
	stream, err := runAgentCommand(cmd, r.Name(), stdout, stderr)
	if stream.sessionID == "" {
		stream.sessionID = forkID
	}
	if err != nil {
		return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw, SessionID: stream.sessionID}, err
	}
	structuredOutput, err := readOutputMCPResult(resultFile)
	if err != nil {
		return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw, SessionID: stream.sessionID}, err
	}
	return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw, StructuredOutput: structuredOutput, SessionID: stream.sessionID}, nil
}

// materializeCodexForkSession mirrors Codex fork persistence: fresh metadata followed by parent history.
func materializeCodexForkSession(parentID, workdir string) (sessionID, path string, err error) {
	home := codexHomeDir()
	parentPath, parentMeta, ok, err := findCodexSessionByID(home, parentID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", fmt.Errorf("no codex rollout found for session %s", parentID)
	}
	parentData, err := readCodexForkSnapshot(parentPath)
	if err != nil {
		return "", "", fmt.Errorf("read codex rollout %q: %w", parentPath, err)
	}
	metaPayload, err := forkedCodexSessionPayload(parentPath, parentID, workdir)
	if err != nil {
		return "", "", err
	}
	sessionID, err = newUUIDv7String()
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	utcTimestamp := now.UTC().Format("2006-01-02T15:04:05.000Z")
	metaPayload["id"] = sessionID
	metaPayload["forked_from_id"] = parentID
	metaPayload["timestamp"] = utcTimestamp
	metaPayload["cwd"] = workdir
	if strings.TrimSpace(workdir) == "" {
		metaPayload["cwd"] = parentMeta.Cwd
	}

	outPath := codexRolloutPath(home, now, sessionID)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", "", fmt.Errorf("create codex session directory: %w", err)
	}
	file, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("create codex fork rollout: %w", err)
	}
	closeFile := true
	defer func() {
		if closeFile {
			_ = file.Close()
			if err != nil {
				_ = os.Remove(outPath)
			}
		}
	}()

	line, err := json.Marshal(map[string]any{
		"timestamp": utcTimestamp,
		"type":      "session_meta",
		"payload":   metaPayload,
	})
	if err != nil {
		return "", "", err
	}
	if _, err = file.Write(append(line, '\n')); err != nil {
		return "", "", err
	}
	if _, err = file.Write(parentData); err != nil {
		return "", "", err
	}
	if len(parentData) > 0 && parentData[len(parentData)-1] != '\n' {
		if _, err = file.Write([]byte("\n")); err != nil {
			return "", "", err
		}
	}
	if err = file.Close(); err != nil {
		return "", "", err
	}
	closeFile = false
	return sessionID, outPath, nil
}

func readCodexForkSnapshot(path string) ([]byte, error) {
	records, err := readCompleteCodexRolloutRecords(path)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("codex rollout is empty")
	}
	var out bytes.Buffer
	for _, record := range records {
		out.Write(record.raw)
		out.WriteByte('\n')
	}
	if midTurn, turnID := codexRolloutEndsMidTurn(records); midTurn {
		for _, record := range codexInterruptedTurnRecords(turnID, time.Now()) {
			out.Write(record)
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

func readCompleteCodexRolloutRecords(path string) ([]codexRolloutRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var records []codexRolloutRecord
	lineNum := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			hadNewline := line[len(line)-1] == '\n'
			line = bytes.TrimRight(line, "\r\n")
			if len(bytes.TrimSpace(line)) > 0 {
				var parsed map[string]any
				if jsonErr := json.Unmarshal(line, &parsed); jsonErr != nil {
					if err == io.EOF && !hadNewline {
						break
					}
					return nil, fmt.Errorf("parse rollout line %d: %w", lineNum, jsonErr)
				}
				records = append(records, codexRolloutRecord{raw: append([]byte(nil), line...), parsed: parsed, lineNum: lineNum})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

func codexRolloutEndsMidTurn(records []codexRolloutRecord) (bool, string) {
	explicitActive := false
	activeTurnID := ""
	lastUserIndex := -1
	terminalAfterLastUser := false
	for i, record := range records {
		recordType := stringField(record.parsed, "type")
		payload := mapField(record.parsed, "payload")
		if recordType == "event_msg" {
			switch stringField(payload, "type") {
			case "task_started", "turn_started":
				explicitActive = true
				activeTurnID = stringField(payload, "turn_id")
			case "task_complete", "turn_complete", "task_aborted", "turn_aborted":
				explicitActive = false
				if lastUserIndex >= 0 && i > lastUserIndex {
					terminalAfterLastUser = true
				}
			case "user_message":
				lastUserIndex = i
				terminalAfterLastUser = false
			}
			continue
		}
		if recordType == "response_item" && stringField(payload, "type") == "message" && stringField(payload, "role") == "user" {
			lastUserIndex = i
			terminalAfterLastUser = false
		}
	}
	if explicitActive {
		return true, activeTurnID
	}
	if lastUserIndex >= 0 && !terminalAfterLastUser {
		return true, ""
	}
	return false, ""
}

func codexInterruptedTurnRecords(turnID string, now time.Time) [][]byte {
	timestamp := now.UTC().Format("2006-01-02T15:04:05.000Z")
	marker := map[string]any{
		"timestamp": timestamp,
		"type":      "response_item",
		"payload": map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": "<turn_aborted>\nThe user interrupted the previous turn on purpose. Any running unified exec processes may still be running in the background. If any tools/commands were aborted, they may have partially executed.\n</turn_aborted>",
			}},
		},
	}
	abortPayload := map[string]any{
		"type":   "turn_aborted",
		"reason": "interrupted",
	}
	if turnID == "" {
		abortPayload["turn_id"] = nil
	} else {
		abortPayload["turn_id"] = turnID
	}
	abort := map[string]any{
		"timestamp": timestamp,
		"type":      "event_msg",
		"payload":   abortPayload,
	}
	out := make([][]byte, 0, 2)
	for _, record := range []map[string]any{marker, abort} {
		data, _ := json.Marshal(record)
		out = append(out, data)
	}
	return out
}

func codexHomeDir() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil || userHome == "" {
		return ".codex"
	}
	return filepath.Join(userHome, ".codex")
}

func effectiveWorkdir(workdir string) string {
	if strings.TrimSpace(workdir) != "" {
		if abs, err := filepath.Abs(workdir); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(workdir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(cwd)
}

func findCodexSessionByID(home, id string) (string, codexSessionMeta, bool, error) {
	root := filepath.Join(home, "sessions")
	var foundPath string
	var foundMeta codexSessionMeta
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if foundPath != "" || entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if !strings.Contains(filepath.Base(path), id) {
			return nil
		}
		meta, ok, err := readCodexSessionMeta(path)
		if err != nil || !ok {
			return err
		}
		if meta.ID != id {
			return nil
		}
		foundPath = path
		foundMeta = meta
		return nil
	})
	if err != nil {
		return "", codexSessionMeta{}, false, err
	}
	return foundPath, foundMeta, foundPath != "", nil
}

func readCodexSessionMeta(path string) (codexSessionMeta, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexSessionMeta{}, false, nil
		}
		return codexSessionMeta{}, false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		meta, ok := parseCodexSessionMetaLine(scanner.Text())
		if ok {
			return meta, true, nil
		}
		if strings.TrimSpace(scanner.Text()) != "" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return codexSessionMeta{}, false, err
	}
	return codexSessionMeta{}, false, nil
}

func forkedCodexSessionPayload(parentPath, parentID, workdir string) (map[string]any, error) {
	file, err := os.Open(parentPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &record); err != nil {
			continue
		}
		if stringField(record, "type") != "session_meta" {
			continue
		}
		payload := mapField(record, "payload")
		if stringField(payload, "id") != parentID {
			continue
		}
		copyPayload := make(map[string]any, len(payload)+2)
		for key, value := range payload {
			copyPayload[key] = value
		}
		if strings.TrimSpace(workdir) != "" {
			copyPayload["cwd"] = workdir
		}
		return copyPayload, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("codex rollout %q does not start with session metadata for %s", parentPath, parentID)
}

func parseCodexSessionMetaLine(line string) (codexSessionMeta, bool) {
	var record struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string `json:"id"`
			ForkedFromID string `json:"forked_from_id"`
			Cwd          string `json:"cwd"`
			Timestamp    string `json:"timestamp"`
		} `json:"payload"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &record); err != nil || record.Type != "session_meta" {
		return codexSessionMeta{}, false
	}
	timestamp := parseCodexTimestamp(firstNonEmpty(record.Payload.Timestamp, record.Timestamp))
	return codexSessionMeta{
		ID:           record.Payload.ID,
		ForkedFromID: record.Payload.ForkedFromID,
		Cwd:          record.Payload.Cwd,
		Timestamp:    timestamp,
	}, true
}

func codexRolloutPath(home string, timestamp time.Time, sessionID string) string {
	local := timestamp.Local()
	dir := filepath.Join(
		home,
		"sessions",
		local.Format("2006"),
		local.Format("01"),
		local.Format("02"),
	)
	return filepath.Join(dir, fmt.Sprintf("rollout-%s-%s.jsonl", local.Format("2006-01-02T15-04-05"), sessionID))
}

func newUUIDv7String() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80

	var hexed [32]byte
	hex.Encode(hexed[:], b[:])
	return string(hexed[0:8]) + "-" +
		string(hexed[8:12]) + "-" +
		string(hexed[12:16]) + "-" +
		string(hexed[16:20]) + "-" +
		string(hexed[20:32]), nil
}

func parseCodexTimestamp(text string) time.Time {
	if strings.TrimSpace(text) == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15-04-05"} {
		if ts, err := time.Parse(layout, text); err == nil {
			return ts
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
