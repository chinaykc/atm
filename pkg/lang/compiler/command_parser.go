package compiler

import (
	"fmt"
	"slices"
	"strings"
)

type commandLineDefaults struct {
	Options      RunOptions
	TaskName     string
	goRun        bool
	wait         bool
	flow         []astOp
	hasLet       bool
	letName      string
	letValue     string
	bashCommands []BashCommand
	prefixVar    string
	output       *OutputSpec
	db           DBTaskConfig
	skill        SkillTaskConfig
	mcp          MCPTaskConfig
	webhook      WebhookTaskConfig
	contextRefs  []string
}

func parseCommandLine(line string, vars map[string]any, root string) ([]forAST, commandLineDefaults, error) {
	commands, err := parseCommandSequence(line, vars, root)
	if err != nil {
		return nil, commandLineDefaults{}, err
	}
	return lowerCommandSequence(commands)
}

func parseCommandSequence(line string, vars map[string]any, root string) ([]command, error) {
	fields, err := commandFields(line)
	if err != nil {
		return nil, err
	}
	var commands []command
	for i := 0; i < len(fields); {
		token := fields[i]
		if !strings.HasPrefix(token, "/") {
			return nil, fmt.Errorf("unexpected command argument %q", token)
		}
		switch token {
		case "/task":
			taskName := ""
			next := i + 1
			if next < len(fields) && !isCommandToken(fields[next]) {
				if !isVariableName(fields[next]) {
					return nil, fmt.Errorf("invalid task name %q", fields[next])
				}
				taskName = fields[next]
				next++
			}
			commands = append(commands, command{Kind: commandTask, TaskName: taskName})
			i = next
		case "/resume":
			if i+1 >= len(fields) || isCommandToken(fields[i+1]) {
				return nil, fmt.Errorf("/resume requires a task name")
			}
			target := fields[i+1]
			if !isVariableName(target) {
				return nil, fmt.Errorf("invalid resume task name %q", target)
			}
			commands = append(commands, command{Kind: commandResume, ResumeTarget: target})
			i += 2
		case "/fork":
			if i+1 >= len(fields) || isCommandToken(fields[i+1]) {
				return nil, fmt.Errorf("/fork requires a task name")
			}
			target := fields[i+1]
			if !isVariableName(target) {
				return nil, fmt.Errorf("invalid fork task name %q", target)
			}
			commands = append(commands, command{Kind: commandFork, ForkTarget: target})
			i += 2
		case "/go":
			pool, next, err := parseOptionalPoolName(fields, i+1)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandGo, Pool: pool})
			i = next
		case "/wait":
			pool, next, err := parseOptionalPoolName(fields, i+1)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandWait, Pool: pool})
			i = next
		case "/args":
			args, next := collectCommandArgs(fields, i+1)
			if len(args) == 0 {
				return nil, fmt.Errorf("/args requires at least one argument")
			}
			commands = append(commands, command{Kind: commandArgs, Options: RunOptions{Args: args}})
			i = next
		case "/cd":
			cd, next, err := parseCdCommand(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandCd, Cd: cd})
			i = next
		case "/let":
			if i+1 >= len(fields) || isCommandToken(fields[i+1]) {
				return nil, fmt.Errorf("/let requires a name and value")
			}
			name := fields[i+1]
			if !isVariableName(name) {
				return nil, fmt.Errorf("invalid variable name %q", name)
			}
			valueStart := i + 2
			if valueStart >= len(fields) {
				return nil, fmt.Errorf("/let %s requires a value", name)
			}
			if fields[valueStart] == "/call" {
				call, next, err := parseCallFields(fields, valueStart)
				if err != nil {
					return nil, err
				}
				call.Assign = name
				commands = append(commands, command{Kind: commandCall, Call: call})
				i = next
			} else if fields[valueStart] == "/bash" {
				args, next := collectCommandArgs(fields, valueStart+1)
				script := strings.Join(args, " ")
				if script == "" {
					return nil, fmt.Errorf("/let %s /bash requires a script", name)
				}
				commands = append(commands, command{Kind: commandBash, Bash: BashCommand{Name: name, Script: script}})
				i = next
			} else {
				args, next := collectCommandArgs(fields, valueStart)
				if len(args) == 0 {
					return nil, fmt.Errorf("/let %s requires a value", name)
				}
				value := strings.Join(args, " ")
				commands = append(commands, command{Kind: commandLet, LetName: name, LetValue: value})
				i = next
			}
		case "/bash":
			args, next := collectCommandArgs(fields, i+1)
			script := strings.Join(args, " ")
			if script == "" {
				return nil, fmt.Errorf("/bash requires a script")
			}
			commands = append(commands, command{Kind: commandBash, Bash: BashCommand{Script: script}})
			i = next
		case "/if":
			condition, next, err := parseIfCommandFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandIf, Condition: condition})
			i = next
		case "/else":
			commands = append(commands, command{Kind: commandElse})
			i++
		case "/output":
			output, next, err := parseOutputFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandOutput, Output: output})
			i = next
		case "/pool":
			return nil, fmt.Errorf("/pool must be written as a standalone global block")
		case "/def":
			return nil, fmt.Errorf("%s must be written as a standalone definition block", token)
		case "/import":
			return nil, fmt.Errorf("/import must be written as a standalone global block")
		case "/context":
			args, next := collectCommandArgs(fields, i+1)
			if len(args) == 0 {
				return nil, fmt.Errorf("/context requires a Markdown heading reference")
			}
			ref := strings.TrimLeft(strings.TrimSpace(strings.Join(args, " ")), "# \t")
			if ref == "" {
				return nil, fmt.Errorf("/context requires a Markdown heading reference")
			}
			commands = append(commands, command{Kind: commandContext, ContextRefs: []string{ref}})
			i = next
		case "/doc":
			return nil, fmt.Errorf("/doc requires inline text or a fenced block in a Markdown section before task commands")
		case "/db":
			config, next, err := parseDBTaskFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandDB, DB: config})
			i = next
		case "/skill":
			config, next, err := parseSkillTaskFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandSkill, Skill: config})
			i = next
		case "/mcp":
			config, next, err := parseMCPTaskFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandMCP, MCP: config})
			i = next
		case "/return":
			return nil, fmt.Errorf("/return must be written as a standalone line")
		case "/call":
			call, next, err := parseCallFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandCall, Call: call})
			i = next
		case "/webhook":
			if i+1 < len(fields) && fields[i+1] == "use" {
				config, next, err := parseWebhookTaskFields(fields, i)
				if err != nil {
					return nil, err
				}
				commands = append(commands, command{Kind: commandWebhookUse, WebhookUse: config})
				i = next
				continue
			}
			call, next, err := parseWebhookCallFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandWebhook, Webhook: call})
			i = next
		case "/for":
			step, next, err := parseForCommand(fields, i+1, root)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandFor, For: step})
			i = next
		default:
			if isIfCommandToken(token) {
				condition, next, err := parseIfCommandFields(fields, i)
				if err != nil {
					return nil, err
				}
				commands = append(commands, command{Kind: commandIf, Condition: condition})
				i = next
				continue
			}
			name := strings.TrimPrefix(token, "/")
			if !isVariableName(name) {
				return nil, fmt.Errorf("unsupported command %q", token)
			}
			if _, ok := vars[name]; !ok {
				return nil, fmt.Errorf("unsupported command %q", token)
			}
			commands = append(commands, command{Kind: commandPrefixVar, PrefixVar: name})
			i++
		}
	}
	return commands, nil
}

func lowerCommandSequence(commands []command) ([]forAST, commandLineDefaults, error) {
	var steps []forAST
	var defaults commandLineDefaults
	ifCount := 0
	elseCount := 0
	for _, command := range commands {
		switch command.Kind {
		case commandTask:
			if command.TaskName != "" {
				if defaults.TaskName != "" && defaults.TaskName != command.TaskName {
					return nil, commandLineDefaults{}, fmt.Errorf("conflicting task names %q and %q", defaults.TaskName, command.TaskName)
				}
				defaults.TaskName = command.TaskName
			}
		case commandResume:
			defaults.Options.Resume = true
			defaults.Options.ResumeTarget = command.ResumeTarget
		case commandFork:
			defaults.Options.Fork = true
			defaults.Options.ForkTarget = command.ForkTarget
		case commandArgs:
			defaults.Options.Args = append(defaults.Options.Args, command.Options.Args...)
		case commandCd:
			defaults.flow = append(defaults.flow, astOp{kind: astOpCd, CdCommand: command.Cd})
		case commandLet:
			defaults.hasLet = true
			defaults.letName = command.LetName
			defaults.letValue = command.LetValue
		case commandBash:
			defaults.bashCommands = append(defaults.bashCommands, command.Bash)
			defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: command.Bash})
		case commandCall:
			defaults.flow = append(defaults.flow, astOp{kind: astOpCall, Call: command.Call})
		case commandWebhook:
			defaults.flow = append(defaults.flow, astOp{kind: astOpWebhook, Webhook: command.Webhook})
		case commandWebhookUse:
			defaults.webhook.Use = append(defaults.webhook.Use, command.WebhookUse.Use...)
		case commandOutput:
			if defaults.output != nil && command.Output != nil {
				return nil, commandLineDefaults{}, fmt.Errorf("/output can only appear once")
			}
			defaults.output = command.Output
		case commandDB:
			defaults.db.IgnoreAll = defaults.db.IgnoreAll || command.DB.IgnoreAll
			defaults.db.Ignore = append(defaults.db.Ignore, command.DB.Ignore...)
			defaults.db.Use = append(defaults.db.Use, command.DB.Use...)
			defaults.db.Access = append(defaults.db.Access, command.DB.Access...)
		case commandSkill:
			defaults.skill.IgnoreAll = defaults.skill.IgnoreAll || command.Skill.IgnoreAll
			defaults.skill.Use = append(defaults.skill.Use, command.Skill.Use...)
			defaults.skill.Ignore = append(defaults.skill.Ignore, command.Skill.Ignore...)
		case commandMCP:
			defaults.mcp.IgnoreAll = defaults.mcp.IgnoreAll || command.MCP.IgnoreAll
			defaults.mcp.Use = append(defaults.mcp.Use, command.MCP.Use...)
			defaults.mcp.Ignore = append(defaults.mcp.Ignore, command.MCP.Ignore...)
			defaults.mcp.DefUse = append(defaults.mcp.DefUse, command.MCP.DefUse...)
		case commandContext:
			defaults.contextRefs = append(defaults.contextRefs, command.ContextRefs...)
		case commandFor:
			steps = append(steps, command.For)
			defaults.flow = append(defaults.flow, astOp{kind: astOpFor, step: command.For})
		case commandGo:
			defaults.goRun = true
			defaults.flow = append(defaults.flow, astOp{kind: astOpGo, Pool: command.Pool})
		case commandWait:
			defaults.wait = true
			defaults.flow = append(defaults.flow, astOp{kind: astOpWait, Pool: command.Pool})
		case commandPrefixVar:
			defaults.prefixVar = command.PrefixVar
		case commandIf:
			ifCount++
			if ifCount > 1 {
				return nil, commandLineDefaults{}, fmt.Errorf("/if does not support nesting; wrap complex branches in /def")
			}
			defaults.flow = append(defaults.flow, astOp{kind: astOpIf, Condition: command.Condition})
		case commandElse:
			if ifCount == 0 {
				continue
			}
			elseCount++
			if elseCount > 1 {
				return nil, commandLineDefaults{}, fmt.Errorf("/else can only appear once in a task header")
			}
			defaults.flow = append(defaults.flow, astOp{kind: astOpElse})
		}
	}
	return steps, defaults, nil
}

func parseOptionalPoolName(fields []string, start int) (string, int, error) {
	if start >= len(fields) || isCommandToken(fields[start]) {
		return "", start, nil
	}
	name := fields[start]
	if !isVariableName(name) {
		return "", start, fmt.Errorf("invalid pool name %q", name)
	}
	return name, start + 1, nil
}

func parseCdCommand(fields []string, start int) (CdCommand, int, error) {
	if start >= len(fields) || fields[start] != "/cd" {
		return CdCommand{}, start, fmt.Errorf("expected /cd")
	}
	next := start + 1
	mustExist := false
	if next < len(fields) && fields[next] == "--must-exist" {
		mustExist = true
		next++
	}
	args, end := collectCommandArgs(fields, next)
	if len(args) == 0 {
		return CdCommand{}, end, fmt.Errorf("/cd requires a path")
	}
	if len(args) > 1 {
		return CdCommand{}, end, fmt.Errorf("/cd accepts exactly one path")
	}
	if strings.HasPrefix(args[0], "-") {
		return CdCommand{}, end, fmt.Errorf("unsupported /cd flag %q", args[0])
	}
	return CdCommand{Path: args[0], MustExist: mustExist}, end, nil
}

func parseOutputFields(fields []string, start int) (*OutputSpec, int, error) {
	if start >= len(fields) || fields[start] != "/output" {
		return nil, start, fmt.Errorf("expected /output")
	}
	args, next := collectCommandArgs(fields, start+1)
	if len(args) > 1 {
		return nil, next, fmt.Errorf("/output accepts at most one file name")
	}
	fileName := ""
	if len(args) == 1 {
		fileName = args[0]
	}
	return &OutputSpec{FileName: fileName}, next, nil
}

func parseCallFields(fields []string, start int) (Call, int, error) {
	if start >= len(fields) || fields[start] != "/call" {
		return Call{}, start, fmt.Errorf("expected /call")
	}
	if start+1 >= len(fields) {
		return Call{}, start, fmt.Errorf("/call requires a definition name")
	}
	name := fields[start+1]
	if !isDefinitionName(name) {
		return Call{}, start, fmt.Errorf("invalid definition name %q", name)
	}
	args, next := collectCommandArgs(fields, start+2)
	return Call{Name: name, Args: slices.Clone(args)}, next, nil
}

func ParseCallExpression(text string) (Call, error) {
	fields, err := commandFields(strings.TrimSpace(text))
	if err != nil {
		return Call{}, err
	}
	call, next, err := parseCallFields(fields, 0)
	if err != nil {
		return Call{}, err
	}
	if next != len(fields) {
		return Call{}, fmt.Errorf("unexpected command argument %q", fields[next])
	}
	return call, nil
}

func collectCommandArgs(fields []string, start int) ([]string, int) {
	end := start
	for end < len(fields) && !isCommandToken(fields[end]) {
		end++
	}
	return unquoteCommandValues(fields[start:end]), end
}

func hasNamedBashCommand(commands []BashCommand, name string) bool {
	for _, command := range commands {
		if command.Name == name {
			return true
		}
	}
	return false
}
