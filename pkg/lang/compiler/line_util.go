package compiler

import "strings"

func SplitLines(content string) []string {
	if content == "" {
		return nil
	}

	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		lines = append(lines, content[start:i+1])
		start = i + 1
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

func IsBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

func IsCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#") || isMarkdownReferenceCommentLine(trimmed)
}

func IsIgnoredLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return IsCommentLine(line) || isMarkdownRuleLine(trimmed) || strings.HasPrefix(trimmed, "/flag ") || strings.HasPrefix(trimmed, "/webhook new ")
}

func isMarkdownRuleLine(line string) bool {
	if len(line) < 3 {
		return false
	}
	var want rune
	for i, r := range line {
		if i == 0 {
			switch r {
			case '-', '=':
				want = r
			default:
				return false
			}
			continue
		}
		if r != want {
			return false
		}
	}
	return true
}

func isPreservedPromptHeading(line string) bool {
	_, ok := markdownAnyHeading(line)
	return ok
}

func isHTMLCommentStartLine(line string) bool {
	if !strings.HasPrefix(line, "<!--") {
		return false
	}
	end := strings.Index(line, "-->")
	return end < 0 || end == len(line)-3
}

func isHTMLCommentEndLine(line string) bool {
	return strings.HasSuffix(line, "-->")
}

func isMarkdownReferenceCommentLine(line string) bool {
	if strings.HasPrefix(line, "[//]: # (") && strings.HasSuffix(line, ")") {
		return true
	}
	if strings.HasPrefix(line, "[//]: # \"") && strings.HasSuffix(line, "\"") {
		return true
	}
	if strings.HasPrefix(line, "[comment]: <> (") && strings.HasSuffix(line, ")") {
		return true
	}
	return false
}

func firstNonBlankLineWithRest(body string) (string, string, bool) {
	lines := SplitLines(body)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed, strings.Join(lines[i+1:], ""), true
		}
	}
	return "", "", false
}

func firstField(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
