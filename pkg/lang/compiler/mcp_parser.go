package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strings"
)

func ParseGlobalMCPBlock(body string) (MCPDecl, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return MCPDecl{}, false, err
	}
	lines := SplitLines(body)
	first := -1
	for i, raw := range lines {
		if strings.TrimSpace(raw) != "" {
			first = i
			break
		}
	}
	if first < 0 {
		return MCPDecl{}, false, nil
	}
	line := strings.TrimSpace(lines[first])
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/mcp" {
		return MCPDecl{}, false, nil
	}
	if len(fields) < 2 || fields[1] != "new" {
		return MCPDecl{}, false, nil
	}
	if len(fields) != 3 {
		return MCPDecl{}, true, fmt.Errorf("/mcp new form is /mcp new name followed by a fenced JSON block")
	}
	if !isVariableName(fields[2]) {
		return MCPDecl{}, true, fmt.Errorf("invalid mcp name %q", fields[2])
	}
	next := first + 1
	for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
		next++
	}
	if next >= len(lines) {
		return MCPDecl{}, true, fmt.Errorf("/mcp new requires a fenced JSON block")
	}
	fence, ok := parseFenceStart(lines[next])
	if !ok || fence.lang != "json" {
		return MCPDecl{}, true, fmt.Errorf("/mcp new requires a fenced json block")
	}
	bodyText, end, err := collectRawFenceBlock(lines, next+1, fence)
	if err != nil {
		return MCPDecl{}, true, err
	}
	for i := end; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return MCPDecl{}, true, fmt.Errorf("/mcp new does not accept content after its fenced JSON block")
		}
	}
	return MCPDecl{Name: fields[2], Config: bodyText}, true, nil
}

func collectRawFenceBlock(lines []string, start int, fence outputFenceInfo) (string, int, error) {
	var body strings.Builder
	for i := start; i < len(lines); i++ {
		if isFenceClose(lines[i], fence) {
			return body.String(), i + 1, nil
		}
		body.WriteString(lines[i])
	}
	return "", len(lines), fmt.Errorf("fenced block is missing closing ```")
}

func parseMCPTaskLine(line string) (MCPTaskConfig, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/mcp" {
		return MCPTaskConfig{}, false, nil
	}
	out, next, err := parseMCPTaskFields(fields, 0)
	if err != nil {
		return MCPTaskConfig{}, true, err
	}
	if next != len(fields) {
		return MCPTaskConfig{}, true, fmt.Errorf("unexpected command argument %q", fields[next])
	}
	return out, true, nil
}

func parseMCPTaskFields(fields []string, start int) (MCPTaskConfig, int, error) {
	if start >= len(fields) || fields[start] != "/mcp" {
		return MCPTaskConfig{}, start, fmt.Errorf("expected /mcp")
	}
	if start+1 >= len(fields) || isCommandToken(fields[start+1]) {
		return MCPTaskConfig{}, start, fmt.Errorf("/mcp requires subcommand")
	}
	var out MCPTaskConfig
	switch fields[start+1] {
	case "use":
		args, next := collectCommandArgs(fields, start+2)
		if len(args) == 0 {
			return MCPTaskConfig{}, start, fmt.Errorf("/mcp use requires at least one name")
		}
		out.Use = append(out.Use, args...)
		return out, next, nil
	case "ignore":
		args, next := collectCommandArgs(fields, start+2)
		if len(args) == 0 {
			out.IgnoreAll = true
			return out, next, nil
		}
		out.Ignore = append(out.Ignore, args...)
		return out, next, nil
	case "def":
		if start+2 >= len(fields) || fields[start+2] != "use" {
			return MCPTaskConfig{}, start, fmt.Errorf("/mcp def form is /mcp def use name...")
		}
		args, next := collectCommandArgs(fields, start+3)
		if len(args) == 0 {
			return MCPTaskConfig{}, start, fmt.Errorf("/mcp def form is /mcp def use name...")
		}
		for _, name := range args {
			if !isDefinitionName(name) {
				return MCPTaskConfig{}, start, fmt.Errorf("invalid definition name %q", name)
			}
		}
		out.DefUse = append(out.DefUse, args...)
		return out, next, nil
	case "new":
		return MCPTaskConfig{}, start, fmt.Errorf("/mcp new must be written as a standalone global block")
	default:
		return MCPTaskConfig{}, start, fmt.Errorf("unsupported /mcp subcommand %q", fields[start+1])
	}
}
