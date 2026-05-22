package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const OutputToolName = "atm_report_output"

func RunOutputServerCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var resultFile string
	var schemaFile string
	var schemaFormat string
	flags := flag.NewFlagSet("atm mcp output", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&resultFile, "result-file", "", "path where the structured output is written")
	flags.StringVar(&schemaFile, "schema-file", "", "path to the JSON Schema text")
	flags.StringVar(&schemaFormat, "schema-format", "json", "schema format: json or yaml")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm mcp output runs a temporary stdio MCP server for structured output.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage of atm mcp output:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if resultFile == "" {
		return fmt.Errorf("-result-file is required")
	}
	if schemaFile == "" {
		return fmt.Errorf("-schema-file is required")
	}
	schema, err := os.ReadFile(schemaFile)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}
	return ServeOutput(stdin, stdout, resultFile, string(schema), schemaFormat)
}

func ServeOutput(stdin io.Reader, stdout io.Writer, resultFile, schemaText, schemaFormat string) error {
	schema, err := outputInputSchema(schemaText, schemaFormat)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdin)
	writer := json.NewEncoder(stdout)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		resp := handleOutputRequest(req, resultFile, schema)
		if err := writer.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleOutputRequest(req request, resultFile string, schema any) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "atm-output",
				"version": "1",
			},
		}
	case "tools/list":
		resp.Result = map[string]any{
			"tools": []any{
				map[string]any{
					"name":        OutputToolName,
					"description": "Submit the final structured output for the current ATM task. Call this exactly once when the task is complete.",
					"inputSchema": schema,
				},
			},
		}
	case "tools/call":
		result, err := handleOutputToolCall(req.Params, resultFile)
		if err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return resp
}

func handleOutputToolCall(raw json.RawMessage, resultFile string) (any, error) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Name != OutputToolName {
		return nil, fmt.Errorf("unknown tool %q", params.Name)
	}
	if !json.Valid(params.Arguments) {
		return nil, fmt.Errorf("structured output arguments must be valid JSON")
	}
	if err := WriteOutputResult(resultFile, params.Arguments); err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "ATM structured output recorded.",
			},
		},
	}, nil
}

func outputInputSchema(schemaText, schemaFormat string) (any, error) {
	var schema any
	switch schemaFormat {
	case "yaml", "yml":
		var err error
		schema, err = parseSimpleYAML(schemaText)
		if err != nil {
			return nil, fmt.Errorf("parse YAML output schema: %w", err)
		}
	default:
		if err := json.Unmarshal([]byte(schemaText), &schema); err != nil {
			return nil, fmt.Errorf("parse JSON output schema: %w", err)
		}
	}
	normalized := normalizeYAMLValue(schema)
	if normalized == nil {
		return nil, fmt.Errorf("output schema is empty")
	}
	return normalized, nil
}

type yamlLine struct {
	indent int
	text   string
}

func parseSimpleYAML(input string) (any, error) {
	var lines []yamlLine
	for _, raw := range strings.Split(input, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := 0
		for indent < len(raw) && raw[indent] == ' ' {
			indent++
		}
		text := strings.TrimSpace(raw)
		if strings.HasPrefix(text, "#") {
			continue
		}
		lines = append(lines, yamlLine{indent: indent, text: text})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}
	value, next, err := parseYAMLBlock(lines, 0, lines[0].indent)
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("unexpected YAML line %q", lines[next].text)
	}
	return value, nil
}

func parseYAMLBlock(lines []yamlLine, start, indent int) (any, int, error) {
	if start >= len(lines) {
		return map[string]any{}, start, nil
	}
	if lines[start].indent < indent {
		return map[string]any{}, start, nil
	}
	if strings.HasPrefix(lines[start].text, "- ") {
		return parseYAMLList(lines, start, indent)
	}
	return parseYAMLMap(lines, start, indent)
}

func parseYAMLMap(lines []yamlLine, start, indent int) (map[string]any, int, error) {
	out := map[string]any{}
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, i, fmt.Errorf("unexpected indentation before %q", line.text)
		}
		if strings.HasPrefix(line.text, "- ") {
			break
		}
		key, rest, ok := strings.Cut(line.text, ":")
		if !ok {
			return nil, i, fmt.Errorf("expected key: value line, got %q", line.text)
		}
		key = strings.TrimSpace(key)
		rest = strings.TrimSpace(rest)
		if key == "" {
			return nil, i, fmt.Errorf("empty YAML key")
		}
		if rest != "" {
			out[key] = parseYAMLScalar(rest)
			i++
			continue
		}
		if i+1 >= len(lines) || lines[i+1].indent <= indent {
			out[key] = map[string]any{}
			i++
			continue
		}
		value, next, err := parseYAMLBlock(lines, i+1, lines[i+1].indent)
		if err != nil {
			return nil, next, err
		}
		out[key] = value
		i = next
	}
	return out, i, nil
}

func parseYAMLList(lines []yamlLine, start, indent int) ([]any, int, error) {
	var out []any
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, i, fmt.Errorf("unexpected indentation before %q", line.text)
		}
		if !strings.HasPrefix(line.text, "- ") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(line.text, "- "))
		if item != "" {
			out = append(out, parseYAMLScalar(item))
			i++
			continue
		}
		if i+1 >= len(lines) || lines[i+1].indent <= indent {
			out = append(out, nil)
			i++
			continue
		}
		value, next, err := parseYAMLBlock(lines, i+1, lines[i+1].indent)
		if err != nil {
			return nil, next, err
		}
		out = append(out, value)
		i = next
	}
	return out, i, nil
}

func parseYAMLScalar(value string) any {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	switch strings.ToLower(value) {
	case "true":
		return true
	case "false":
		return false
	case "null", "~":
		return nil
	}
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}

func normalizeYAMLValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeYAMLValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeYAMLValue(item)
		}
		return out
	default:
		return v
	}
}

func WriteOutputResult(path string, result json.RawMessage) error {
	if path == "" {
		return errors.New("result file is required")
	}
	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func ReadOutputResult(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !json.Valid(data) {
		return nil, true, fmt.Errorf("output result is not valid JSON")
	}
	return data, true, nil
}
