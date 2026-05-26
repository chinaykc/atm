package compiler

import (
	"strings"

	"github.com/chinaykc/atm/pkg/lang/syntax"
)

func ParseSyntax(sourcePath, content string) syntax.Document {
	blocks := ParseBlocks(content)
	spans := blockSourceSpans(content, blocks)
	doc := syntax.Document{
		SourcePath: sourcePath,
		Blocks:     make([]syntax.Block, 0, len(blocks)),
	}
	for i, block := range blocks {
		item := syntax.Block{
			Kind:        classifySyntaxBlock(block.Body),
			Prefix:      block.Prefix,
			Body:        block.Body,
			Sep:         block.Sep,
			Context:     block.Context,
			Scope:       append([]string(nil), block.Scope...),
			StartLine:   block.StartLine,
			HasParent:   block.HasParent,
			ParentIndex: block.ParentIndex,
			Commands:    syntaxCommands(block.Body, block.StartLine),
		}
		if i < len(spans) {
			item.Span = syntax.Span{
				Block: spans[i].Block,
				Start: syntax.Position{
					Line:   spans[i].Line,
					Column: spans[i].Column,
				},
			}
		}
		doc.Blocks = append(doc.Blocks, item)
	}
	return doc
}

func classifySyntaxBlock(body string) syntax.BlockKind {
	line := firstNonBlankLine(body)
	name := syntaxCommandName(line)
	switch name {
	case "task", "resume", "args", "cd", "bash", "output", "for", "go", "wait", "call":
		return syntax.BlockTask
	case "let":
		return syntax.BlockLet
	case "pool":
		return syntax.BlockPool
	case "db":
		return syntax.BlockDB
	case "skill":
		return syntax.BlockSkill
	case "mcp":
		return syntax.BlockMCP
	case "import":
		return syntax.BlockImport
	case "def", "return":
		return syntax.BlockDefinition
	case "if", "else":
		return syntax.BlockControl
	default:
		return syntax.BlockMarkdown
	}
}

func syntaxCommands(body string, startLine int) []syntax.Command {
	lines := SplitLines(body)
	commands := make([]syntax.Command, 0)
	inFence := false
	for i, line := range lines {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(text, "/") {
			continue
		}
		raw := strings.TrimRight(line, "\r\n")
		name := syntaxCommandName(text)
		if name == "" {
			continue
		}
		commands = append(commands, syntax.Command{
			Kind: syntaxCommandKind(name),
			Raw:  raw,
			Name: name,
			Args: syntaxCommandArgs(text),
			Line: startLine + i,
		})
	}
	return commands
}

func firstNonBlankLine(body string) string {
	for _, line := range SplitLines(body) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func syntaxCommandName(line string) string {
	if !strings.HasPrefix(line, "/") {
		return ""
	}
	field := firstField(line)
	if field == "" {
		return ""
	}
	return strings.TrimPrefix(field, "/")
}

func syntaxCommandArgs(line string) []string {
	fields := strings.Fields(line)
	if len(fields) <= 1 {
		return nil
	}
	return append([]string(nil), fields[1:]...)
}

func syntaxCommandKind(name string) syntax.CommandKind {
	switch name {
	case "task":
		return syntax.CommandTask
	case "resume":
		return syntax.CommandResume
	case "args":
		return syntax.CommandArgs
	case "cd":
		return syntax.CommandCd
	case "let":
		return syntax.CommandLet
	case "bash":
		return syntax.CommandBash
	case "if":
		return syntax.CommandIf
	case "else":
		return syntax.CommandElse
	case "call":
		return syntax.CommandCall
	case "for":
		return syntax.CommandFor
	case "go":
		return syntax.CommandGo
	case "wait":
		return syntax.CommandWait
	case "output":
		return syntax.CommandOutput
	case "db":
		return syntax.CommandDB
	case "skill":
		return syntax.CommandSkill
	case "mcp":
		return syntax.CommandMCP
	case "def":
		return syntax.CommandDef
	case "return":
		return syntax.CommandReturn
	case "import":
		return syntax.CommandImport
	case "pool":
		return syntax.CommandPool
	default:
		return syntax.CommandOther
	}
}
