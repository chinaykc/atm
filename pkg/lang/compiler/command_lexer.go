package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

func commandFields(line string) ([]string, error) {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	tokenStart := -1
	flush := func() {
		if tokenStart >= 0 {
			fields = append(fields, b.String())
			b.Reset()
			tokenStart = -1
		}
	}
	for i, r := range line {
		if escaped {
			if quote != 0 {
				b.WriteRune(r)
			} else {
				switch r {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteRune(r)
				}
			}
			escaped = false
			continue
		}
		if quote != 0 {
			if r == '\\' {
				b.WriteRune(r)
				escaped = true
				continue
			}
			if r == quote {
				b.WriteRune(r)
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		switch r {
		case ' ', '\t', '\r', '\n':
			flush()
		case '\'', '"':
			if tokenStart < 0 {
				tokenStart = i
			}
			b.WriteRune(r)
			quote = r
		case '\\':
			if tokenStart < 0 {
				tokenStart = i
			}
			escaped = true
		default:
			if tokenStart < 0 {
				tokenStart = i
			}
			b.WriteRune(r)
		}
	}
	if escaped {
		return nil, fmt.Errorf("command line has trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("command line has unterminated quote")
	}
	flush()
	return fields, nil
}

func mustCommandFields(line string) []string {
	fields, err := commandFields(line)
	if err != nil {
		return strings.Fields(line)
	}
	return fields
}

func unquoteCommandValue(value string) string {
	if len(value) < 2 {
		return value
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return value
	}
	return unquoted
}

func unquoteCommandValues(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = unquoteCommandValue(value)
	}
	return out
}
