package mcp

import (
	"context"
	"encoding/json"
	"github.com/chinaykc/atm/pkg/lang/ir"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type DefToolHandler func(context.Context, string, json.RawMessage) (any, error)

func DefToolDefinitions(refs []ir.DefinitionRef) []ToolDefinition {
	out := make([]ToolDefinition, 0, len(refs))
	for _, ref := range refs {
		properties := map[string]any{}
		required := make([]string, 0, len(ref.Params))
		for _, param := range ref.Params {
			properties[param] = map[string]any{"type": "string", "description": "Argument for definition parameter " + param}
			required = append(required, param)
		}
		out = append(out, ToolDefinition{
			Name:        ir.DefMCPToolName(ref.Name),
			Description: "Run ATM definition " + ref.Name + " and return its result.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           properties,
				"required":             required,
				"additionalProperties": false,
			},
		})
	}
	return out
}

func DefNameForTool(refs []ir.DefinitionRef, tool string) (string, bool) {
	for _, ref := range refs {
		if ir.DefMCPToolName(ref.Name) == tool {
			return ref.Name, true
		}
	}
	return "", false
}

func NewDefsSDKServer(refs []ir.DefinitionRef, handler DefToolHandler) *mcpsdk.Server {
	server := NewSDKServer("atm-defs")
	for _, tool := range DefToolDefinitions(refs) {
		AddTool(server, tool, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			value, err := handler(ctx, req.Params.Name, req.Params.Arguments)
			if err != nil {
				return nil, err
			}
			return JSONTextResult(value)
		})
	}
	return server
}
