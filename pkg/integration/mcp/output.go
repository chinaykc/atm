package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
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
	server, err := newOutputSDKServer(resultFile, schemaText, schemaFormat)
	if err != nil {
		return err
	}
	return runSDKServer(context.Background(), server, stdin, stdout)
}

func RegisterNetworkOutput(resultFile, schemaText, schemaFormat string) (NetworkEndpoint, error) {
	manager, err := DefaultNetworkManager()
	if err != nil {
		return NetworkEndpoint{}, err
	}
	server, err := newOutputSDKServer(resultFile, schemaText, schemaFormat)
	if err != nil {
		return NetworkEndpoint{}, err
	}
	return manager.Register(server)
}

func newOutputSDKServer(resultFile, schemaText, schemaFormat string) (*mcpsdk.Server, error) {
	tool, err := outputToolDefinition(schemaText, schemaFormat)
	if err != nil {
		return nil, err
	}
	server := NewSDKServer("atm-output")
	AddTool(server, tool, func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return recordOutputResult(resultFile, req.Params.Arguments)
	})
	return server, nil
}

func outputToolDefinition(schemaText, schemaFormat string) (ToolDefinition, error) {
	schema, err := outputInputSchema(schemaText, schemaFormat)
	if err != nil {
		return ToolDefinition{}, err
	}
	return ToolDefinition{
		Name:        OutputToolName,
		Description: "Submit the final structured output for the current ATM task. Call this exactly once when the task is complete.",
		InputSchema: schema,
	}, nil
}

func recordOutputResult(resultFile string, arguments json.RawMessage) (*mcpsdk.CallToolResult, error) {
	if !json.Valid(arguments) {
		return nil, fmt.Errorf("structured output arguments must be valid JSON")
	}
	if err := WriteOutputResult(resultFile, arguments); err != nil {
		return nil, err
	}
	return textResult("ATM structured output recorded."), nil
}

func outputInputSchema(schemaText, schemaFormat string) (any, error) {
	var schema any
	switch schemaFormat {
	case "yaml", "yml":
		var raw any
		if err := yaml.Unmarshal([]byte(schemaText), &raw); err != nil {
			return nil, fmt.Errorf("parse YAML output schema: %w", err)
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("convert YAML output schema to JSON: %w", err)
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("normalize YAML output schema: %w", err)
		}
	default:
		if err := json.Unmarshal([]byte(schemaText), &schema); err != nil {
			return nil, fmt.Errorf("parse JSON output schema: %w", err)
		}
	}
	if schema == nil {
		return nil, fmt.Errorf("output schema is empty")
	}
	return schema, nil
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
