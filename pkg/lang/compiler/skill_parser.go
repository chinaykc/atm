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
	if len(fields) < 2 {
		return SkillTaskConfig{}, true, fmt.Errorf("/skill requires subcommand")
	}
	var out SkillTaskConfig
	switch fields[1] {
	case "use":
		if len(fields) < 3 {
			return SkillTaskConfig{}, true, fmt.Errorf("/skill use requires at least one name or path")
		}
		out.Use = append(out.Use, fields[2:]...)
	case "ignore":
		if len(fields) == 2 {
			out.IgnoreAll = true
			return out, true, nil
		}
		out.Ignore = append(out.Ignore, fields[2:]...)
	case "new":
		return SkillTaskConfig{}, true, fmt.Errorf("/skill new must be written as a standalone global block")
	default:
		return SkillTaskConfig{}, true, fmt.Errorf("unsupported /skill subcommand %q", fields[1])
	}
	return out, true, nil
}
