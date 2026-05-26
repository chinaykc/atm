package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strings"
)

func ParseGlobalLetBlock(body string) ([]LetBinding, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return nil, false, err
	}
	lines := SplitLines(body)
	var bindings []LetBinding
	seen := false
	for i := 0; i < len(lines); i++ {
		rawLine := lines[i]
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/let ") {
			return nil, false, nil
		}
		if defaults, next, ok, err := parseBashHeredocCommand(lines, i, line); ok || err != nil {
			if err != nil {
				return nil, true, err
			}
			if !defaults.hasLet {
				return nil, false, nil
			}
			bindings = append(bindings, LetBinding{Name: defaults.letName, BashScript: defaults.bashCommands[0].Script})
			seen = true
			i = next - 1
			continue
		}
		fields := strings.Fields(line)
		multiline := len(fields) == 2
		if len(fields) < 2 {
			return nil, true, fmt.Errorf("/let requires a name")
		}
		name := fields[1]
		if !isVariableName(name) {
			return nil, true, fmt.Errorf("invalid variable name %q", name)
		}
		if multiline {
			valueLines, next := collectLetMultilineValue(lines, i+1)
			if strings.TrimSpace(strings.Join(valueLines, "")) == "" {
				return nil, true, fmt.Errorf("/let %s requires a value", name)
			}
			bindings = append(bindings, LetBinding{Name: name, Value: joinLetMultilineValue(valueLines)})
			seen = true
			i = next - 1
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "/let "+name))
		if value == "/call" || strings.HasPrefix(value, "/call ") {
			return nil, false, nil
		}
		if value == "/bash" || strings.HasPrefix(value, "/bash ") {
			script := strings.TrimSpace(strings.TrimPrefix(value, "/bash"))
			if script == "" {
				return nil, true, fmt.Errorf("/let %s /bash requires a script", name)
			}
			bindings = append(bindings, LetBinding{Name: name, BashScript: script})
		} else {
			bindings = append(bindings, LetBinding{Name: name, Value: value})
		}
		seen = true
	}
	return bindings, seen, nil
}

func collectLetMultilineValue(lines []string, start int) ([]string, int) {
	end := start
	for end < len(lines) {
		if strings.HasPrefix(strings.TrimSpace(lines[end]), "/") {
			break
		}
		end++
	}
	return lines[start:end], end
}

func joinLetMultilineValue(lines []string) string {
	return strings.TrimRight(strings.Join(lines, ""), "\r\n")
}
