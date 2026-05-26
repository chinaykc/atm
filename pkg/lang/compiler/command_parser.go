package compiler

import (
	"fmt"
	"slices"
	"strings"
)

type commandLineDefaults struct {
	Options      RunOptions
	goRun        bool
	wait         bool
	flow         []astOp
	hasLet       bool
	letName      string
	letValue     string
	bashCommands []BashCommand
	prefixVar    string
	output       *OutputSpec
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
			if len(fields) != 1 {
				return nil, fmt.Errorf("/task must be the only command on its line")
			}
			commands = append(commands, command{Kind: commandTask})
			i++
		case "/resume":
			commands = append(commands, command{Kind: commandResume})
			i++
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
			if i != 0 {
				return nil, fmt.Errorf("/let must be the only command on its line")
			}
			if len(fields) < 3 {
				return nil, fmt.Errorf("/let requires a name and value")
			}
			name := fields[1]
			if !isVariableName(name) {
				return nil, fmt.Errorf("invalid variable name %q", name)
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, "/let "+name))
			if value == "/call" || strings.HasPrefix(value, "/call ") {
				call, err := ParseCallExpression(value)
				if err != nil {
					return nil, err
				}
				call.Assign = name
				commands = append(commands, command{Kind: commandCall, Call: call})
			} else if value == "/bash" || strings.HasPrefix(value, "/bash ") {
				script := strings.TrimSpace(strings.TrimPrefix(value, "/bash"))
				if script == "" {
					return nil, fmt.Errorf("/let %s /bash requires a script", name)
				}
				commands = append(commands, command{Kind: commandBash, Bash: BashCommand{Name: name, Script: script}})
			} else {
				commands = append(commands, command{Kind: commandLet, LetName: name, LetValue: value})
			}
			return commands, nil
		case "/bash":
			if i != 0 {
				return nil, fmt.Errorf("/bash must be the only command on its line")
			}
			script := strings.TrimSpace(strings.TrimPrefix(line, "/bash"))
			if script == "" {
				return nil, fmt.Errorf("/bash requires a script")
			}
			commands = append(commands, command{Kind: commandBash, Bash: BashCommand{Script: script}})
			return commands, nil
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
			return nil, fmt.Errorf("/output must be the only command on its line and followed by a fenced schema block")
		case "/pool":
			return nil, fmt.Errorf("/pool must be written as a standalone global block")
		case "/def":
			return nil, fmt.Errorf("%s must be written as a standalone definition block", token)
		case "/import":
			return nil, fmt.Errorf("/import must be written as a standalone global block")
		case "/context":
			return nil, fmt.Errorf("/context requires a known Markdown heading reference")
		case "/doc":
			return nil, fmt.Errorf("/doc requires inline text or a fenced block in a Markdown section before task commands")
		case "/db":
			return nil, fmt.Errorf("/db must be written as a standalone line")
		case "/skill":
			return nil, fmt.Errorf("/skill must be written as a standalone line")
		case "/mcp":
			return nil, fmt.Errorf("/mcp must be written as a standalone line")
		case "/return":
			return nil, fmt.Errorf("/return must be written as a standalone line")
		case "/call":
			call, err := parseCallFields(fields, i)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command{Kind: commandCall, Call: call})
			i = len(fields)
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
			if len(fields) != 1 {
				return nil, fmt.Errorf("variable command %q must be the only command on its line", token)
			}
			commands = append(commands, command{Kind: commandPrefixVar, PrefixVar: name})
			return commands, nil
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
			// Explicit task start; no runtime option.
		case commandResume:
			defaults.Options.Resume = true
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

func parseCallFields(fields []string, start int) (Call, error) {
	if start >= len(fields) || fields[start] != "/call" {
		return Call{}, fmt.Errorf("expected /call")
	}
	if start+1 >= len(fields) {
		return Call{}, fmt.Errorf("/call requires a definition name")
	}
	name := fields[start+1]
	if !isDefinitionName(name) {
		return Call{}, fmt.Errorf("invalid definition name %q", name)
	}
	return Call{Name: name, Args: unquoteCommandValues(slices.Clone(fields[start+2:]))}, nil
}

func ParseCallExpression(text string) (Call, error) {
	fields, err := commandFields(strings.TrimSpace(text))
	if err != nil {
		return Call{}, err
	}
	return parseCallFields(fields, 0)
}

func collectCommandArgs(fields []string, start int) ([]string, int) {
	end := start
	for end < len(fields) && !isCommandToken(fields[end]) {
		end++
	}
	return unquoteCommandValues(fields[start:end]), end
}

func isCommandToken(token string) bool {
	switch token {
	case "/task", "/resume", "/go", "/wait", "/args", "/cd", "/let", "/bash", "/for", "/if", "/else", "/output", "/pool", "/call", "/return", "/def", "/import", "/context", "/doc", "/db", "/skill", "/mcp":
		return true
	default:
		return false
	}
}

func hasNamedBashCommand(commands []BashCommand, name string) bool {
	for _, command := range commands {
		if command.Name == name {
			return true
		}
	}
	return false
}
