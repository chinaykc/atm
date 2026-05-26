package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema any
}

func NewSDKServer(name string) *mcpsdk.Server {
	return mcpsdk.NewServer(&mcpsdk.Implementation{Name: name, Version: "1"}, nil)
}

func AddTool(server *mcpsdk.Server, tool ToolDefinition, handler mcpsdk.ToolHandler) {
	server.AddTool(&mcpsdk.Tool{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: tool.InputSchema,
	}, handler)
}

func runSDKServer(ctx context.Context, server *mcpsdk.Server, stdin io.Reader, stdout io.Writer) error {
	err := runSDKServerRaw(ctx, server, stdin, stdout)
	if err != nil && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "EOF") {
		return err
	}
	return nil
}

func ServeSDKServer(ctx context.Context, server *mcpsdk.Server, stdin io.Reader, stdout io.Writer) error {
	return runSDKServer(ctx, server, stdin, stdout)
}

func runSDKServerRaw(ctx context.Context, server *mcpsdk.Server, stdin io.Reader, stdout io.Writer) error {
	return server.Run(ctx, &mcpsdk.IOTransport{
		Reader: io.NopCloser(stdin),
		Writer: nopWriteCloser{Writer: stdout},
	})
}

type nopWriteCloser struct {
	io.Writer
}

func (w nopWriteCloser) Close() error {
	return nil
}

func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
	}
}

func JSONTextResult(value any) (*mcpsdk.CallToolResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return textResult(string(data)), nil
}

func objectSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
}
