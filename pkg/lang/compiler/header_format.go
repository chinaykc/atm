package compiler

import (
	"strconv"
	"strings"
)

// FormatTaskHeaderBody normalizes composed header commands without touching
// prompt or payload content. It assumes body already belongs to a task block.
func FormatTaskHeaderBody(body string) string {
	lines := SplitLines(body)
	var out strings.Builder
	header := true
	fence := outputFenceInfo{}
	fencePending := false
	heredocDelim := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !header {
			out.WriteString(line)
			continue
		}
		if fence.marker != "" {
			out.WriteString(line)
			if isFenceClose(line, fence) {
				fence = outputFenceInfo{}
			}
			continue
		}
		if fencePending {
			out.WriteString(line)
			if parsed, ok := parseAnyFenceStart(line); ok {
				fence = parsed
			}
			fencePending = false
			continue
		}
		if heredocDelim != "" {
			out.WriteString(line)
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if trimmed == "" {
			out.WriteString(line)
			continue
		}
		if !strings.HasPrefix(trimmed, "/") {
			header = false
			out.WriteString(line)
			continue
		}
		out.WriteString(formatHeaderLine(trimmed, line))
		if startsFencedPayloadCommand(trimmed) {
			fencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
	}
	return out.String()
}

func FormatTaskMarkdownSpacing(body string) string {
	lines := SplitLines(body)
	units, promptStart := taskHeaderUnitsForMarkdownSpacing(lines)
	if len(units) == 0 || promptStart < 0 {
		return body
	}
	var out strings.Builder
	for i, unit := range units {
		for _, line := range unit {
			out.WriteString(line)
		}
		if i+1 < len(units) || promptStart < len(lines) {
			out.WriteString(blankLineAfterUnit(unit))
		}
	}
	for _, line := range lines[promptStart:] {
		out.WriteString(line)
	}
	return out.String()
}

func taskHeaderUnitsForMarkdownSpacing(lines []string) ([][]string, int) {
	var units [][]string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isGeneratedStateLine(trimmed) {
			return nil, -1
		}
		if !strings.HasPrefix(trimmed, "/") {
			return units, i
		}
		unit := []string{line}
		if isMultilineLetCommandLine(trimmed) {
			_, next := collectLetMultilineValue(lines, i+1)
			unit = append(unit, lines[i+1:next]...)
			units = append(units, trimTrailingBlankLines(unit))
			i = next - 1
			continue
		}
		if startsFencedPayloadCommand(trimmed) {
			next := appendFollowingFence(lines, i+1, &unit)
			units = append(units, unit)
			i = next - 1
			continue
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			next := appendHeredocPayload(lines, i+1, delim, &unit)
			units = append(units, unit)
			i = next - 1
			continue
		}
		units = append(units, unit)
	}
	return units, len(lines)
}

func appendFollowingFence(lines []string, start int, unit *[]string) int {
	if start >= len(lines) {
		return start
	}
	fence, ok := parseAnyFenceStart(lines[start])
	if !ok {
		return start
	}
	*unit = append(*unit, lines[start])
	for i := start + 1; i < len(lines); i++ {
		*unit = append(*unit, lines[i])
		if isFenceClose(lines[i], fence) {
			return i + 1
		}
	}
	return len(lines)
}

func appendHeredocPayload(lines []string, start int, delim string, unit *[]string) int {
	for i := start; i < len(lines); i++ {
		*unit = append(*unit, lines[i])
		if strings.TrimSpace(lines[i]) == delim {
			return i + 1
		}
	}
	return len(lines)
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && IsBlankLine(lines[end-1]) {
		end--
	}
	return lines[:end]
}

func isGeneratedStateLine(trimmed string) bool {
	return trimmed == "[done]" ||
		strings.HasPrefix(trimmed, "[done|") ||
		strings.HasPrefix(trimmed, "[running|") ||
		strings.HasPrefix(trimmed, "<!-- atm:report ") ||
		strings.HasPrefix(trimmed, "> [!ATM]")
}

func blankLineAfterUnit(lines []string) string {
	if len(lines) == 0 {
		return "\n"
	}
	last := lines[len(lines)-1]
	switch {
	case strings.HasSuffix(last, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(last, "\n"):
		return "\n"
	default:
		return "\n\n"
	}
}

func formatHeaderLine(trimmed, original string) string {
	fields, err := commandFields(trimmed)
	if err != nil || len(fields) < 2 || preservesNestedProvider(fields) {
		return original
	}
	var chunks [][]string
	start := 0
	for i := 1; i < len(fields); i++ {
		if isCommandToken(fields[i]) || isIfCommandToken(fields[i]) {
			chunks = append(chunks, fields[start:i])
			start = i
		}
	}
	if len(chunks) == 0 {
		return original
	}
	chunks = append(chunks, fields[start:])
	var out strings.Builder
	for _, chunk := range chunks {
		for i, field := range chunk {
			if i > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(formatCommandField(field))
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func preservesNestedProvider(fields []string) bool {
	return len(fields) >= 3 && fields[0] == "/let" && (fields[2] == "/bash" || fields[2] == "/call")
}

func formatCommandField(field string) string {
	if field == "" || strings.ContainsAny(field, " \t\r\n") && !isQuotedCommandField(field) {
		return strconv.Quote(field)
	}
	return field
}

func isQuotedCommandField(field string) bool {
	if len(field) < 2 {
		return false
	}
	first := field[0]
	last := field[len(field)-1]
	return (first == '"' && last == '"') || (first == '\'' && last == '\'')
}
