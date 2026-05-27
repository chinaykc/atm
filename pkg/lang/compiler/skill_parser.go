package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strings"
)

func ParseGlobalSkillBlock(body string) (SkillDecl, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return SkillDecl{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok {
		return SkillDecl{}, false, nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/skill" {
		return SkillDecl{}, false, nil
	}
	if len(fields) < 2 || fields[1] != "new" {
		return SkillDecl{}, false, nil
	}
	if len(fields) != 5 || fields[3] != "from" {
		return SkillDecl{}, true, fmt.Errorf("/skill new form is /skill new name from path")
	}
	if strings.TrimSpace(rest) != "" {
		return SkillDecl{}, true, fmt.Errorf("/skill new does not accept a body")
	}
	if !isVariableName(fields[2]) {
		return SkillDecl{}, true, fmt.Errorf("invalid skill name %q", fields[2])
	}
	return SkillDecl{Name: fields[2], Path: fields[4]}, true, nil
}

func parseSkillTaskLine(line string) (SkillTaskConfig, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/skill" {
		return SkillTaskConfig{}, false, nil
	}
	out, next, err := parseSkillTaskFields(fields, 0)
	if err != nil {
		return SkillTaskConfig{}, true, err
	}
	if next != len(fields) {
		return SkillTaskConfig{}, true, fmt.Errorf("unexpected command argument %q", fields[next])
	}
	return out, true, nil
}

func parseSkillTaskFields(fields []string, start int) (SkillTaskConfig, int, error) {
	if start >= len(fields) || fields[start] != "/skill" {
		return SkillTaskConfig{}, start, fmt.Errorf("expected /skill")
	}
	if start+1 >= len(fields) || isCommandToken(fields[start+1]) {
		return SkillTaskConfig{}, start, fmt.Errorf("/skill requires subcommand")
	}
	var out SkillTaskConfig
	switch fields[start+1] {
	case "use":
		args, next := collectCommandArgs(fields, start+2)
		if len(args) == 0 {
			return SkillTaskConfig{}, start, fmt.Errorf("/skill use requires at least one name or path")
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
	case "new":
		return SkillTaskConfig{}, start, fmt.Errorf("/skill new must be written as a standalone global block")
	default:
		return SkillTaskConfig{}, start, fmt.Errorf("unsupported /skill subcommand %q", fields[start+1])
	}
}
