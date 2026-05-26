package compiler

import (
	"fmt"
	"strings"
)

type taskCommandScan struct {
	promptStart int
	defaults    RunOptions
	prefixes    []string
}

func scanTaskCommandPrefix(index int, lines []string, t *taskAST, root string) (taskCommandScan, error) {
	scan := taskCommandScan{}
	for ; scan.promptStart < len(lines); scan.promptStart++ {
		line := strings.TrimSpace(lines[scan.promptStart])
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/") {
			break
		}
		if parsed, next, ok, err := parseOutputAt(lines, scan.promptStart, line); ok || err != nil {
			if err != nil {
				return taskCommandScan{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if t.output != nil {
				return taskCommandScan{}, fmt.Errorf("task %d: /output can only appear once", index+1)
			}
			t.output = parsed
			scan.promptStart = next - 1
			continue
		}
		if dbConfig, ok, err := parseDBTaskLine(line); ok || err != nil {
			if err != nil {
				return taskCommandScan{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if err := applyTaskDBConfig(index, t, dbConfig); err != nil {
				return taskCommandScan{}, err
			}
			continue
		}
		if skillConfig, ok, err := parseSkillTaskLine(line); ok || err != nil {
			if err != nil {
				return taskCommandScan{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if err := applyTaskSkillConfig(index, t, skillConfig); err != nil {
				return taskCommandScan{}, err
			}
			continue
		}
		if mcpConfig, ok, err := parseMCPTaskLine(line); ok || err != nil {
			if err != nil {
				return taskCommandScan{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if err := applyTaskMCPConfig(index, t, mcpConfig); err != nil {
				return taskCommandScan{}, err
			}
			continue
		}
		if isMultilineLetCommandLine(line) {
			fields := strings.Fields(line)
			name := fields[1]
			if !isVariableName(name) {
				return taskCommandScan{}, fmt.Errorf("task %d: invalid variable name %q", index+1, name)
			}
			valueLines, next := collectLetMultilineValue(lines, scan.promptStart+1)
			if strings.TrimSpace(strings.Join(valueLines, "")) == "" {
				return taskCommandScan{}, fmt.Errorf("task %d: /let %s requires a value", index+1, name)
			}
			t.vars[name] = joinLetMultilineValue(valueLines)
			scan.promptStart = next - 1
			continue
		}

		lineSteps, lineDefaults, nextPromptStart, err := parseCommandLineAt(lines, scan.promptStart, t.vars, root)
		if err != nil {
			return taskCommandScan{}, fmt.Errorf("task %d: %w", index+1, err)
		}
		scan.promptStart = nextPromptStart - 1
		if err := applyTaskCommandLine(t, &scan, lineSteps, lineDefaults); err != nil {
			return taskCommandScan{}, err
		}
	}
	return scan, nil
}

func applyTaskDBConfig(index int, t *taskAST, config DBTaskConfig) error {
	if config.IgnoreAll && (len(t.db.Use) > 0 || len(t.db.Access) > 0) {
		return fmt.Errorf("task %d: /db ignore cannot be combined with /db use or /db access", index+1)
	}
	if t.db.IgnoreAll && (len(config.Use) > 0 || len(config.Access) > 0) {
		return fmt.Errorf("task %d: /db ignore cannot be combined with /db use or /db access", index+1)
	}
	t.db.IgnoreAll = t.db.IgnoreAll || config.IgnoreAll
	t.db.Ignore = append(t.db.Ignore, config.Ignore...)
	t.db.Use = append(t.db.Use, config.Use...)
	t.db.Access = append(t.db.Access, config.Access...)
	return nil
}

func applyTaskSkillConfig(index int, t *taskAST, config SkillTaskConfig) error {
	if config.IgnoreAll && len(t.skill.Use) > 0 {
		return fmt.Errorf("task %d: /skill ignore cannot be combined with /skill use", index+1)
	}
	if t.skill.IgnoreAll && len(config.Use) > 0 {
		return fmt.Errorf("task %d: /skill ignore cannot be combined with /skill use", index+1)
	}
	t.skill.IgnoreAll = t.skill.IgnoreAll || config.IgnoreAll
	t.skill.Use = append(t.skill.Use, config.Use...)
	t.skill.Ignore = append(t.skill.Ignore, config.Ignore...)
	return nil
}

func applyTaskMCPConfig(index int, t *taskAST, config MCPTaskConfig) error {
	if config.IgnoreAll && (len(t.mcp.Use) > 0 || len(t.mcp.DefUse) > 0) {
		return fmt.Errorf("task %d: /mcp ignore cannot be combined with /mcp use or /mcp def use", index+1)
	}
	if t.mcp.IgnoreAll && (len(config.Use) > 0 || len(config.DefUse) > 0) {
		return fmt.Errorf("task %d: /mcp ignore cannot be combined with /mcp use or /mcp def use", index+1)
	}
	t.mcp.IgnoreAll = t.mcp.IgnoreAll || config.IgnoreAll
	t.mcp.Use = append(t.mcp.Use, config.Use...)
	t.mcp.Ignore = append(t.mcp.Ignore, config.Ignore...)
	t.mcp.DefUse = append(t.mcp.DefUse, config.DefUse...)
	return nil
}

func applyTaskCommandLine(t *taskAST, scan *taskCommandScan, lineSteps []forAST, defaults commandLineDefaults) error {
	t.goRun = t.goRun || defaults.goRun
	t.wait = t.wait || defaults.wait
	for _, op := range defaults.flow {
		if op.kind == astOpFor {
			op.step.Options = MergeRunOptions(defaults.Options, op.step.Options)
		}
		t.flow = append(t.flow, op)
	}
	if defaults.hasLet {
		t.vars[defaults.letName] = defaults.letValue
	}
	if len(defaults.bashCommands) > 0 {
		t.bashCommands = append(t.bashCommands, defaults.bashCommands...)
	}
	if defaults.prefixVar != "" {
		value, ok := t.vars[defaults.prefixVar]
		if !ok {
			return fmt.Errorf("unknown variable command %q", "/"+defaults.prefixVar)
		}
		if StringValue(value) == "" && hasNamedBashCommand(t.bashCommands, defaults.prefixVar) {
			value = "{{" + defaults.prefixVar + "}}"
		}
		scan.prefixes = append(scan.prefixes, StringValue(value))
	}
	if len(lineSteps) == 0 {
		scan.defaults = MergeRunOptions(scan.defaults, defaults.Options)
		return nil
	}
	for _, step := range lineSteps {
		step.Options = MergeRunOptions(defaults.Options, step.Options)
		t.steps = append(t.steps, step)
	}
	return nil
}
