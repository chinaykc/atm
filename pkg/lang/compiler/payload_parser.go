package compiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

func extractReturnSpec(lines []string) ([]string, *ReturnSpec, error) {
	var out []string
	var spec *ReturnSpec
	heredocDelim := ""
	fence := outputFenceInfo{}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if fence.marker != "" {
			out = append(out, line)
			if isFenceClose(line, fence) {
				fence = outputFenceInfo{}
			}
			continue
		}
		if heredocDelim != "" {
			out = append(out, line)
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if parsed, next, ok, err := parseReturnAt(lines, i, trimmed); ok || err != nil {
			if err != nil {
				return nil, nil, err
			}
			if spec != nil {
				return nil, nil, fmt.Errorf("/return can only appear once")
			}
			spec = parsed
			i = next - 1
			continue
		}
		out = append(out, line)
		if nextFence, ok := parseAnyFenceStart(line); ok {
			fence = nextFence
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
	}
	return out, spec, nil
}

func parseReturnAt(lines []string, index int, line string) (*ReturnSpec, int, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/return" {
		return nil, index + 1, false, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "/return"))
	if rest == "" {
		if index+1 < len(lines) {
			if fence, ok := parseAnyFenceStart(lines[index+1]); ok {
				if isReturnSchemaFence(fence) {
					schema, next, err := collectSchemaFenceBlock(lines, index+2, fence, "/return")
					if err != nil {
						return nil, next, true, err
					}
					return &ReturnSpec{
						Kind: ReturnStructured,
						Output: &OutputSpec{
							Schema:       schema.body,
							SchemaFormat: schema.format,
							Structured:   true,
						},
					}, next, true, nil
				}
				text, next, err := collectTextFenceBlock(lines, index+2, fence)
				if err != nil {
					return nil, next, true, err
				}
				return &ReturnSpec{Kind: ReturnTemplate, Text: text}, next, true, nil
			}
			if isTildeFenceStart(lines[index+1]) {
				return nil, index + 1, true, fmt.Errorf("/return fenced block must use backticks")
			}
		}
		return nil, index + 1, true, fmt.Errorf("/return requires an inline value or a fenced block")
	}
	if rest == "/bash" || strings.HasPrefix(rest, "/bash ") {
		script := strings.TrimSpace(strings.TrimPrefix(rest, "/bash"))
		if script == "" {
			if index+1 < len(lines) {
				if fence, ok := parseAnyFenceStart(lines[index+1]); ok {
					script, next, err := collectTextFenceBlock(lines, index+2, fence)
					if err != nil {
						return nil, next, true, err
					}
					if strings.TrimSpace(script) == "" {
						return nil, next, true, fmt.Errorf("/return /bash requires a script")
					}
					return &ReturnSpec{Kind: ReturnBash, Script: script}, next, true, nil
				}
				if isTildeFenceStart(lines[index+1]) {
					return nil, index + 1, true, fmt.Errorf("/return /bash fenced script must use backticks")
				}
			}
			return nil, index + 1, true, fmt.Errorf("/return /bash requires a script")
		}
		if index+1 < len(lines) {
			if _, ok := parseAnyFenceStart(lines[index+1]); ok {
				return nil, index + 1, true, fmt.Errorf("/return /bash inline script cannot be followed by a fenced script")
			}
			if isTildeFenceStart(lines[index+1]) {
				return nil, index + 1, true, fmt.Errorf("/return /bash inline script cannot be followed by a fenced script")
			}
		}
		return &ReturnSpec{Kind: ReturnBash, Script: script}, index + 1, true, nil
	}
	if index+1 < len(lines) {
		if _, ok := parseAnyFenceStart(lines[index+1]); ok {
			return nil, index + 1, true, fmt.Errorf("/return inline value cannot be followed by a fenced value")
		}
		if isTildeFenceStart(lines[index+1]) {
			return nil, index + 1, true, fmt.Errorf("/return inline value cannot be followed by a fenced value")
		}
	}
	return &ReturnSpec{Kind: ReturnTemplate, Text: rest}, index + 1, true, nil
}

func parseOutputAt(lines []string, index int, line string) (*OutputSpec, int, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/output" {
		return nil, index + 1, false, nil
	}
	if len(fields) > 2 {
		return nil, index + 1, true, fmt.Errorf("/output accepts at most one file name")
	}
	if index+1 >= len(lines) {
		fileName := ""
		if len(fields) == 2 {
			fileName = fields[1]
		}
		return &OutputSpec{FileName: fileName}, index + 1, true, nil
	}
	fence, ok := parseFenceStart(lines[index+1])
	if !ok {
		if isTildeFenceStart(lines[index+1]) {
			return nil, index + 1, true, fmt.Errorf("/output schema fence must use backticks")
		}
		fileName := ""
		if len(fields) == 2 {
			fileName = fields[1]
		}
		return &OutputSpec{FileName: fileName}, index + 1, true, nil
	}
	schema, next, err := collectSchemaFenceBlock(lines, index+2, fence, "/output")
	if err != nil {
		return nil, next, true, err
	}
	fileName := ""
	if len(fields) == 2 {
		fileName = fields[1]
	}
	return &OutputSpec{FileName: fileName, Schema: schema.body, SchemaFormat: schema.format, Structured: true}, next, true, nil
}

func startsFencedPayloadCommand(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	if fields[0] == "/output" {
		return true
	}
	if fields[0] == "/return" {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "/return"))
		return rest == "" || rest == "/bash"
	}
	if len(fields) == 1 && fields[0] == "/if" {
		return true
	}
	return fields[len(fields)-1] == "until"
}

type parsedOutputSchema struct {
	format string
	body   string
}

type outputFenceInfo struct {
	marker string
	lang   string
}

func parseFenceStart(line string) (outputFenceInfo, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '`' {
		return outputFenceInfo{}, false
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == '`' {
		count++
	}
	if count < 3 {
		return outputFenceInfo{}, false
	}
	lang := strings.TrimSpace(trimmed[count:])
	switch strings.ToLower(lang) {
	case "", "json", "yaml", "yml", "schema":
		return outputFenceInfo{marker: strings.Repeat("`", count), lang: strings.ToLower(lang)}, true
	default:
		return outputFenceInfo{}, false
	}
}

func parseAnyFenceStart(line string) (outputFenceInfo, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '`' {
		return outputFenceInfo{}, false
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == '`' {
		count++
	}
	if count < 3 {
		return outputFenceInfo{}, false
	}
	return outputFenceInfo{marker: strings.Repeat("`", count), lang: strings.ToLower(strings.TrimSpace(trimmed[count:]))}, true
}

func isTildeFenceStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '~' {
		return false
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == '~' {
		count++
	}
	return count >= 3
}

func collectSchemaFenceBlock(lines []string, start int, fence outputFenceInfo, command string) (parsedOutputSchema, int, error) {
	var body strings.Builder
	for i := start; i < len(lines); i++ {
		if isFenceClose(lines[i], fence) {
			schema, err := parseOutputSchema(fence.lang, body.String())
			return schema, i + 1, err
		}
		body.WriteString(lines[i])
	}
	return parsedOutputSchema{}, len(lines), fmt.Errorf("%s fenced block is missing closing ```", command)
}

func collectTextFenceBlock(lines []string, start int, fence outputFenceInfo) (string, int, error) {
	body, next, err := collectRawFenceBlock(lines, start, fence)
	if err != nil {
		return "", next, err
	}
	return trimOneTrailingNewline(body), next, nil
}

func isFenceClose(line string, fence outputFenceInfo) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, fence.marker) {
		return false
	}
	for i := len(fence.marker); i < len(trimmed); i++ {
		if trimmed[i] != '`' {
			return false
		}
	}
	return true
}

func parseOutputSchema(lang, body string) (parsedOutputSchema, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return parsedOutputSchema{}, fmt.Errorf("/output schema block is empty")
	}
	switch lang {
	case "json":
		if !json.Valid([]byte(trimmed)) {
			return parsedOutputSchema{}, fmt.Errorf("/output json schema is not valid JSON")
		}
		return parsedOutputSchema{format: "json", body: trimmed}, nil
	case "yaml", "yml":
		return parsedOutputSchema{format: "yaml", body: trimmed}, nil
	case "schema":
		if json.Valid([]byte(trimmed)) {
			return parsedOutputSchema{format: "json", body: trimmed}, nil
		}
		schema, err := parseSimpleOutputSchema(trimmed)
		if err != nil {
			return parsedOutputSchema{}, err
		}
		return parsedOutputSchema{format: "json", body: schema}, nil
	default:
		if json.Valid([]byte(trimmed)) {
			return parsedOutputSchema{format: "json", body: trimmed}, nil
		}
		schema, err := parseSimpleOutputSchema(trimmed)
		if err != nil {
			return parsedOutputSchema{}, err
		}
		return parsedOutputSchema{format: "json", body: schema}, nil
	}
}

func isReturnSchemaFence(fence outputFenceInfo) bool {
	switch fence.lang {
	case "json", "yaml", "yml", "schema":
		return true
	default:
		return false
	}
}

func trimOneTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return strings.TrimSuffix(s, "\r\n")
	}
	return strings.TrimSuffix(s, "\n")
}

func parseSimpleOutputSchema(body string) (string, error) {
	properties := make(map[string]any)
	var required []string
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		name := strings.TrimSpace(parts[0])
		if !isVariableName(name) {
			return "", fmt.Errorf("/output field name %q is invalid", name)
		}
		typ := "string"
		description := ""
		switch len(parts) {
		case 1:
			description = ""
		case 2:
			description = strings.TrimSpace(parts[1])
		default:
			if candidate := strings.TrimSpace(parts[1]); candidate != "" {
				if strings.HasPrefix(candidate, "[]") {
					itemType := strings.TrimPrefix(candidate, "[]")
					if !isJSONSchemaScalarType(itemType) {
						return "", fmt.Errorf("/output field %q has unsupported array item type %q", name, itemType)
					}
					properties[name] = map[string]any{"type": "array", "items": map[string]string{"type": itemType}}
					description = strings.TrimSpace(parts[2])
					if description != "" {
						properties[name].(map[string]any)["description"] = description
					}
					required = append(required, name)
					continue
				}
				if !isJSONSchemaScalarType(candidate) {
					return "", fmt.Errorf("/output field %q has unsupported type %q", name, candidate)
				}
				typ = candidate
			}
			description = strings.TrimSpace(parts[2])
		}
		property := map[string]any{"type": typ}
		if description != "" {
			property["description"] = description
		}
		properties[name] = property
		required = append(required, name)
	}
	if len(required) == 0 {
		return "", fmt.Errorf("/output simple schema has no fields")
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func isJSONSchemaScalarType(value string) bool {
	switch value {
	case "string", "number", "integer", "boolean", "object", "array", "null":
		return true
	default:
		return false
	}
}
