package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/mcp"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"io"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

type agentStreamResult struct {
	messages []ir.OutputMessage
	raw      string
}

type agentDisplayEvent struct {
	tool string
	kind string
	role string
	name string
	text string
}

type agentEventParser struct {
	tool              string
	messages          []ir.OutputMessage
	claudeFallback    string
	displayedToolCall map[string]bool
}

func runAgentCommand(cmd *exec.Cmd, tool string, stdout, stderr io.Writer) (agentStreamResult, error) {
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return agentStreamResult{}, err
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return agentStreamResult{}, err
	}

	parser := newAgentEventParser(tool)
	var raw strings.Builder
	rendered := 0
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		raw.WriteString(line)
		raw.WriteByte('\n')
		events, recognized := parser.consume(line)
		if !recognized {
			fmt.Fprintln(stdout, line)
			rendered++
			continue
		}
		for _, event := range events {
			writeAgentDisplayEvent(stdout, event)
			rendered++
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	for _, event := range parser.finish() {
		writeAgentDisplayEvent(stdout, event)
		rendered++
	}
	if rendered == 0 {
		writeRawIfPresent(stdout, raw.String())
	}
	result := agentStreamResult{messages: parser.messages, raw: raw.String()}
	if waitErr != nil {
		return result, waitErr
	}
	if scanErr != nil {
		return result, scanErr
	}
	return result, nil
}

func newAgentEventParser(tool string) *agentEventParser {
	return &agentEventParser{tool: tool, displayedToolCall: make(map[string]bool)}
}

func (p *agentEventParser) consume(line string) ([]agentDisplayEvent, bool) {
	var event map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &event); err != nil {
		return nil, false
	}
	switch p.tool {
	case "codex":
		return p.consumeCodex(event), true
	case "claude":
		return p.consumeClaude(event), true
	default:
		return nil, true
	}
}

func (p *agentEventParser) finish() []agentDisplayEvent {
	if p.tool != "claude" || len(p.messages) > 0 || strings.TrimSpace(p.claudeFallback) == "" {
		return nil
	}
	message := ir.OutputMessage{Tool: "claude", Role: "assistant", Text: p.claudeFallback}
	p.messages = append(p.messages, message)
	return []agentDisplayEvent{{tool: "claude", kind: "message", role: "assistant", text: p.claudeFallback}}
}

func (p *agentEventParser) consumeCodex(event map[string]any) []agentDisplayEvent {
	eventType := stringField(event, "type")
	switch eventType {
	case "thread.started":
		if id := stringField(event, "thread_id"); id != "" {
			return []agentDisplayEvent{{tool: "codex", kind: "system", name: "thread " + id}}
		}
		return []agentDisplayEvent{{tool: "codex", kind: "system", name: "thread started"}}
	case "turn.started":
		return []agentDisplayEvent{{tool: "codex", kind: "system", name: "turn started"}}
	case "turn.completed":
		name := "turn completed"
		if summary := usageSummary(mapField(event, "usage")); summary != "" {
			name += " (" + summary + ")"
		}
		return []agentDisplayEvent{{tool: "codex", kind: "system", name: name}}
	case "error":
		if message := firstString(event, "message", "error"); message != "" {
			return []agentDisplayEvent{{tool: "codex", kind: "system", name: "error: " + message}}
		}
		return []agentDisplayEvent{{tool: "codex", kind: "system", name: "error"}}
	}
	item := mapField(event, "item")
	itemType := stringField(item, "type")
	if eventType == "item.completed" && itemType == "agent_message" {
		text := strings.TrimSpace(stringField(item, "text"))
		if text == "" {
			return nil
		}
		message := ir.OutputMessage{Tool: "codex", Role: "assistant", Text: text}
		p.messages = append(p.messages, message)
		return []agentDisplayEvent{{tool: "codex", kind: "message", role: "assistant", text: text}}
	}
	if eventType != "item.started" && eventType != "item.completed" {
		return nil
	}
	if display, ok := codexATMMCPEvent(item); ok {
		return p.codexToolEvent(eventType, item, display)
	}
	name := codexToolName(item)
	if name == "" {
		return nil
	}
	return p.codexToolEvent(eventType, item, agentDisplayEvent{tool: "codex", kind: "tool", name: name})
}

func (p *agentEventParser) codexToolEvent(eventType string, item map[string]any, display agentDisplayEvent) []agentDisplayEvent {
	key := firstString(item, "id", "call_id") + ":" + display.tool + ":" + display.name
	if key == ":"+display.tool+":"+display.name {
		key = eventType + ":" + display.tool + ":" + display.name
	}
	if eventType == "item.completed" && p.displayedToolCall[key] {
		return nil
	}
	p.displayedToolCall[key] = true
	return []agentDisplayEvent{display}
}

func (p *agentEventParser) consumeClaude(event map[string]any) []agentDisplayEvent {
	eventType := stringField(event, "type")
	if eventType == "system" {
		subtype := stringField(event, "subtype")
		if subtype == "" {
			subtype = "system"
		}
		name := subtype
		if model := stringField(event, "model"); model != "" {
			name += " " + model
		}
		if sessionID := stringField(event, "session_id"); sessionID != "" {
			name += " session " + sessionID
		}
		return []agentDisplayEvent{{tool: "claude", kind: "system", name: name}}
	}
	if eventType == "result" {
		p.claudeFallback = stringField(event, "result")
		name := stringField(event, "subtype")
		if name == "" {
			name = "result"
		}
		if boolField(event, "is_error") {
			name = "error"
		}
		if duration := intField(event, "duration_ms"); duration > 0 {
			name += fmt.Sprintf(" in %.1fs", float64(duration)/1000)
		}
		if summary := usageSummary(mapField(event, "usage")); summary != "" {
			name += " (" + summary + ")"
		}
		return []agentDisplayEvent{{tool: "claude", kind: "system", name: name}}
	}
	if eventType == "error" {
		if message := firstString(event, "message", "error"); message != "" {
			return []agentDisplayEvent{{tool: "claude", kind: "system", name: "error: " + message}}
		}
		return []agentDisplayEvent{{tool: "claude", kind: "system", name: "error"}}
	}
	if eventType != "assistant" {
		return nil
	}
	message := mapField(event, "message")
	role := stringField(message, "role")
	if role == "" {
		role = "assistant"
	}
	content, ok := message["content"].([]any)
	if !ok {
		return nil
	}
	var events []agentDisplayEvent
	var textParts []string
	for _, value := range content {
		part, ok := value.(map[string]any)
		if !ok {
			continue
		}
		switch stringField(part, "type") {
		case "thinking":
			events = append(events, agentDisplayEvent{tool: "claude", kind: "system", name: "thinking"})
		case "text":
			text := strings.TrimSpace(stringField(part, "text"))
			if text != "" {
				textParts = append(textParts, text)
				events = append(events, agentDisplayEvent{tool: "claude", kind: "message", role: role, text: text})
			}
		case "tool_use":
			name := stringField(part, "name")
			if name != "" {
				if display, ok := claudeATMMCPEvent(name); ok {
					events = append(events, display)
				} else {
					events = append(events, agentDisplayEvent{tool: "claude", kind: "tool", name: name})
				}
			}
		}
	}
	if len(textParts) > 0 {
		p.messages = append(p.messages, ir.OutputMessage{Tool: "claude", Role: role, Text: strings.Join(textParts, "\n")})
	}
	return events
}

func writeAgentDisplayEvent(stdout io.Writer, event agentDisplayEvent) {
	toolLabel := color.New(color.FgHiCyan, color.Bold).SprintFunc()
	kindLabel := color.New(color.FgHiMagenta, color.Bold).SprintFunc()
	roleLabel := color.New(color.FgHiGreen, color.Bold).SprintFunc()
	switch event.kind {
	case "tool":
		fmt.Fprintf(stdout, "%s %s %s\n", toolLabel("["+event.tool+"]"), kindLabel("tool"), event.name)
	case "system":
		fmt.Fprintf(stdout, "%s %s\n", kindLabel("["+event.tool+"]"), event.name)
	case "message":
		fmt.Fprintf(stdout, "%s %s\n", toolLabel("["+event.tool+"]"), roleLabel(event.role))
		fmt.Fprintln(stdout, indentBlock(strings.TrimRight(event.text, "\r\n")))
	}
}

func indentBlock(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = "  " + strings.TrimRight(lines[i], "\r")
	}
	return strings.Join(lines, "\n")
}

func codexToolName(item map[string]any) string {
	name := firstString(item, "name", "tool_name")
	itemType := stringField(item, "type")
	if name != "" {
		return name
	}
	if itemType == "command_execution" {
		command := stringField(item, "command")
		if command != "" {
			return itemType + ": " + command
		}
		return itemType
	}
	if strings.Contains(itemType, "tool") || strings.Contains(itemType, "function") || strings.Contains(itemType, "shell") {
		return itemType
	}
	return ""
}

func codexATMMCPEvent(item map[string]any) (agentDisplayEvent, bool) {
	if stringField(item, "type") != "mcp_tool_call" {
		return agentDisplayEvent{}, false
	}
	return atmMCPDisplayEvent(stringField(item, "server"), stringField(item, "tool"))
}

func claudeATMMCPEvent(name string) (agentDisplayEvent, bool) {
	server := ""
	tool := name
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
		if len(parts) == 2 {
			server = parts[0]
			tool = parts[1]
		}
	}
	return atmMCPDisplayEvent(server, tool)
}

func atmMCPDisplayEvent(server, tool string) (agentDisplayEvent, bool) {
	label := atmMCPLabel(server, tool)
	if label == "" || tool == "" {
		return agentDisplayEvent{}, false
	}
	return agentDisplayEvent{tool: label, kind: "system", name: tool}, true
}

func atmMCPLabel(server, tool string) string {
	switch server {
	case outputMCPName:
		return "output"
	case mcpServerName:
		return "check"
	case dbMCPName:
		return "db"
	}
	switch {
	case tool == mcp.OutputToolName:
		return "output"
	case tool == mcp.CheckToolName:
		return "check"
	case strings.HasPrefix(tool, "atm_db_"):
		return "db"
	}
	return ""
}

func mapField(value map[string]any, key string) map[string]any {
	child, ok := value[key].(map[string]any)
	if !ok {
		return nil
	}
	return child
}

func stringField(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	text, ok := value[key].(string)
	if !ok {
		return ""
	}
	return text
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringField(value, key); text != "" {
			return text
		}
	}
	return ""
}

func boolField(value map[string]any, key string) bool {
	if value == nil {
		return false
	}
	flag, ok := value[key].(bool)
	return ok && flag
}

func intField(value map[string]any, key string) int {
	if value == nil {
		return 0
	}
	switch n := value[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func usageSummary(usage map[string]any) string {
	if usage == nil {
		return ""
	}
	input := intField(usage, "input_tokens")
	if input == 0 {
		input = intField(usage, "inputTokens")
	}
	output := intField(usage, "output_tokens")
	if output == 0 {
		output = intField(usage, "outputTokens")
	}
	reasoning := intField(usage, "reasoning_output_tokens")
	if input == 0 && output == 0 && reasoning == 0 {
		return ""
	}
	var parts []string
	if input > 0 {
		parts = append(parts, fmt.Sprintf("in=%d", input))
	}
	if output > 0 {
		parts = append(parts, fmt.Sprintf("out=%d", output))
	}
	if reasoning > 0 {
		parts = append(parts, fmt.Sprintf("reasoning=%d", reasoning))
	}
	return strings.Join(parts, " ")
}

func writeRawIfPresent(stdout io.Writer, raw string) {
	if raw == "" {
		return
	}
	fmt.Fprint(stdout, raw)
}
