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
