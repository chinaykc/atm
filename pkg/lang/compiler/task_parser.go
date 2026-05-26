package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"slices"
	"strconv"
	"strings"
)

func parseTask(index int, body string, globals map[string]any, opts CompileOptions) (taskAST, error) {
	t := taskAST{vars: CloneVars(globals)}
	body, running, err := marker.StripRunning(body)
	if err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	t.running = running

	lines := SplitLines(body)
	lines, t.returnSpec, err = extractReturnSpec(lines)
	if err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	scan, err := scanTaskCommandPrefix(index, lines, &t, opts.Root)
	if err != nil {
		return taskAST{}, err
	}

	if err := rejectPromptOnlyCommands(index, lines[scan.promptStart:]); err != nil {
		return taskAST{}, err
	}
	prefixes := slices.Clone(scan.prefixes)
	if strings.TrimSpace(opts.Context) != "" {
		prefixes = append([]string{opts.Context}, prefixes...)
	}
	t.prompt = prependPromptPrefixes(prefixes, strings.Join(lines[scan.promptStart:], ""))
	if err := validateDBTaskConfig(t.db); err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	if strings.TrimSpace(t.prompt) == "" {
		if (t.wait || t.returnSpec != nil) && !t.goRun && len(t.steps) == 0 && len(t.bashCommands) == 0 && !scan.defaults.Resume && len(scan.defaults.Args) == 0 {
			return t, nil
		}
		if len(t.bashCommands) == 0 && t.returnSpec == nil && len(t.flow) == 0 {
			return taskAST{}, fmt.Errorf("task %d: prompt is empty", index+1)
		}
	}

	finalizeTaskFlowDefaults(&t, scan.defaults)
	return t, nil
}

func rejectPromptOnlyCommands(index int, lines []string) error {
	fence := outputFenceInfo{}
	heredocDelim := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence.marker != "" {
			if isFenceClose(line, fence) {
				fence = outputFenceInfo{}
			}
			continue
		}
		if heredocDelim != "" {
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if nextFence, ok := parseAnyFenceStart(line); ok {
			fence = nextFence
			continue
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "/output" {
			return fmt.Errorf("task %d: /output is only allowed in task header", index+1)
		}
		if isCommandToken(fields[0]) || isIfCommandToken(fields[0]) {
			return fmt.Errorf("task %d: command %s is not allowed in prompt body", index+1, fields[0])
		}
	}
	return nil
}

func finalizeTaskFlowDefaults(t *taskAST, defaults RunOptions) {
	if len(t.steps) == 0 {
		t.steps = []forAST{{MaxRuns: 1}}
	}
	for i := range t.steps {
		t.steps[i].Options = MergeRunOptions(defaults, t.steps[i].Options)
	}
	for i := range t.flow {
		if t.flow[i].kind == astOpFor {
			t.flow[i].step.Options = MergeRunOptions(defaults, t.flow[i].step.Options)
		}
	}
}

func isMultilineLetCommandLine(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	return len(fields) == 2 && fields[0] == "/let"
}

func validateDBTaskConfig(config DBTaskConfig) error {
	seen := map[string]DBAccess{}
	record := func(name string, access DBAccess) error {
		if access == "" {
			return nil
		}
		if previous, ok := seen[name]; ok && previous != access {
			return fmt.Errorf("db %q has conflicting access overrides %s and %s", name, previous, access)
		}
		seen[name] = access
		return nil
	}
	for _, use := range config.Use {
		for _, name := range use.Names {
			if err := record(name, use.Access); err != nil {
				return err
			}
		}
	}
	for _, rule := range config.Access {
		for _, name := range rule.Names {
			if err := record(name, rule.Access); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseCommandLineAt(lines []string, index int, vars map[string]any, root string) ([]forAST, commandLineDefaults, int, error) {
	line := strings.TrimSpace(lines[index])
	if defaults, next, ok, err := parseBashHeredocCommand(lines, index, line); ok || err != nil {
		return nil, defaults, next, err
	}
	if rewritten, next, ok, err := rewriteFencedNaturalCommandArg(lines, index, line); ok || err != nil {
		if err != nil {
			return nil, commandLineDefaults{}, next, err
		}
		steps, defaults, err := parseCommandLine(rewritten, vars, root)
		return steps, defaults, next, err
	}
	steps, defaults, err := parseCommandLine(line, vars, root)
	return steps, defaults, index + 1, err
}

func rewriteFencedNaturalCommandArg(lines []string, index int, line string) (string, int, bool, error) {
	if index+1 >= len(lines) {
		return "", index + 1, false, nil
	}
	fence, ok := parseAnyFenceStart(lines[index+1])
	if !ok {
		return "", index + 1, false, nil
	}
	fields, err := commandFields(line)
	if err != nil {
		return "", index + 1, true, err
	}
	if len(fields) == 0 {
		return "", index + 1, false, nil
	}
	if len(fields) == 1 && fields[0] == "/if" {
		condition, next, err := collectTextFenceBlock(lines, index+2, fence)
		if err != nil {
			return "", next, true, err
		}
		if strings.TrimSpace(condition) == "" {
			return "", next, true, fmt.Errorf("/if requires a condition")
		}
		return "/if " + strconv.Quote(condition), next, true, nil
	}
	if fields[len(fields)-1] == "until" {
		condition, next, err := collectTextFenceBlock(lines, index+2, fence)
		if err != nil {
			return "", next, true, err
		}
		if strings.TrimSpace(condition) == "" {
			return "", next, true, fmt.Errorf("/for until requires a condition")
		}
		return line + " " + strconv.Quote(condition), next, true, nil
	}
	return "", index + 1, false, nil
}
