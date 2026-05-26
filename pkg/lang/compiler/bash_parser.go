package compiler

import (
	"fmt"
	"strings"
)

func parseBashHeredocCommand(lines []string, index int, line string) (commandLineDefaults, int, bool, error) {
	var defaults commandLineDefaults
	fields, err := commandFields(line)
	if err != nil {
		return defaults, index + 1, true, err
	}
	if len(fields) == 0 {
		return defaults, index + 1, false, nil
	}

	if fields[0] == "/bash" {
		delim, ok := parseHeredocDelimiter(fields[1:])
		if !ok {
			return defaults, index + 1, false, nil
		}
		script, next, err := collectHeredocScript(lines, index+1, delim)
		if err != nil {
			return defaults, next, true, err
		}
		defaults.bashCommands = append(defaults.bashCommands, BashCommand{Script: script})
		defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: BashCommand{Script: script}})
		return defaults, next, true, nil
	}

	if len(fields) >= 4 && fields[0] == "/let" && fields[2] == "/bash" {
		name := fields[1]
		if !isVariableName(name) {
			return defaults, index + 1, true, fmt.Errorf("invalid variable name %q", name)
		}
		delim, ok := parseHeredocDelimiter(fields[3:])
		if !ok {
			return defaults, index + 1, false, nil
		}
		script, next, err := collectHeredocScript(lines, index+1, delim)
		if err != nil {
			return defaults, next, true, err
		}
		defaults.hasLet = true
		defaults.letName = name
		defaults.letValue = ""
		command := BashCommand{Name: name, Script: script}
		defaults.bashCommands = append(defaults.bashCommands, command)
		defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: command})
		return defaults, next, true, nil
	}

	return defaults, index + 1, false, nil
}

func lineHeredocDelimiter(line string) (string, bool) {
	fields := mustCommandFields(line)
	if len(fields) == 0 {
		return "", false
	}
	if fields[0] == "/bash" {
		return parseHeredocDelimiter(fields[1:])
	}
	if len(fields) >= 4 && fields[0] == "/let" && fields[2] == "/bash" {
		return parseHeredocDelimiter(fields[3:])
	}
	return "", false
}

func parseHeredocDelimiter(fields []string) (string, bool) {
	if len(fields) == 1 && strings.HasPrefix(fields[0], "<<") {
		return normalizeHeredocDelimiter(strings.TrimPrefix(fields[0], "<<")), true
	}
	if len(fields) == 2 && fields[0] == "<<" {
		return normalizeHeredocDelimiter(fields[1]), true
	}
	return "", false
}

func normalizeHeredocDelimiter(delim string) string {
	delim = strings.TrimSpace(delim)
	if len(delim) >= 2 {
		first := delim[0]
		last := delim[len(delim)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return delim[1 : len(delim)-1]
		}
	}
	return delim
}

func collectHeredocScript(lines []string, start int, delim string) (string, int, error) {
	if delim == "" {
		return "", start, fmt.Errorf("/bash heredoc requires a delimiter")
	}
	var script strings.Builder
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == delim {
			return script.String(), i + 1, nil
		}
		script.WriteString(line)
	}
	return "", len(lines), fmt.Errorf("/bash heredoc missing delimiter %q", delim)
}
