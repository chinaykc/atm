package engine

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	atmmcp "github.com/chinaykc/atm/pkg/integration/mcp"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type webhookMCPConfig struct {
	Webhooks []compiler.WebhookDecl `json:"webhooks"`
}

func RunWebhookMCPServerCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var configFile string
	flags := flag.NewFlagSet("atm mcp webhook", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&configFile, "config-file", "", "path to webhook MCP config JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if configFile == "" {
		return fmt.Errorf("-config-file is required")
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("read webhook config: %w", err)
	}
	config, err := parseWebhookMCPConfig(data)
	if err != nil {
		return fmt.Errorf("parse webhook config: %w", err)
	}
	return serveWebhookMCP(context.Background(), stdin, stdout, config)
}

func parseWebhookMCPConfig(data []byte) (webhookMCPConfig, error) {
	var config webhookMCPConfig
	if err := atmmcp.DecodeStrictJSON(data, &config); err != nil {
		return webhookMCPConfig{}, err
	}
	return config, nil
}

func (e *Engine) taskWebhookMCPs(task compiler.Task) ([]compiler.MCPRuntime, error) {
	if len(task.Webhook.Use) == 0 {
		return nil, nil
	}
	var decls []compiler.WebhookDecl
	var approvedTools []string
	seen := map[string]struct{}{}
	for _, name := range task.Webhook.Use {
		if _, ok := seen[name]; ok {
			continue
		}
		decl, ok := e.webhooks[name]
		if !ok {
			return nil, fmt.Errorf("unknown webhook %q", name)
		}
		seen[name] = struct{}{}
		decls = append(decls, decl)
		approvedTools = append(approvedTools, "atm_webhook_"+sanitizeWebhookToolName(decl.Name))
	}
	config := webhookMCPConfig{Webhooks: decls}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	configDir := filepath.Join(e.outputs.dirPath(), "mcp")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	configFile, err := os.CreateTemp(configDir, "webhook-*.json")
	if err != nil {
		return nil, err
	}
	configPath := configFile.Name()
	if _, err := configFile.Write(data); err != nil {
		configFile.Close()
		os.Remove(configPath)
		return nil, err
	}
	if err := configFile.Close(); err != nil {
		os.Remove(configPath)
		return nil, err
	}
	e.outputs.track(configPath)
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "atm"
	}
	cfg := map[string]any{"command": exe, "args": []string{"mcp", "webhook", "-config-file", configPath}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return []compiler.MCPRuntime{{Name: "atm_webhook", Config: string(raw), ApprovedTools: approvedTools}}, nil
}

func serveWebhookMCP(ctx context.Context, stdin io.Reader, stdout io.Writer, config webhookMCPConfig) error {
	server := atmmcp.NewSDKServer("atm-webhook")
	decls := slices.Clone(config.Webhooks)
	for _, decl := range decls {
		decl := decl
		toolName := "atm_webhook_" + sanitizeWebhookToolName(decl.Name)
		description := "Send a message through ATM webhook " + decl.Name + ". Use only when the workflow needs an external notification."
		if decl.Provider == "dingtalk" && len(decl.Keywords) > 0 {
			description += " DingTalk keyword security is enabled; the message or payload must contain at least one of: " + strings.Join(decl.Keywords, ", ") + "."
		}
		atmmcp.AddTool(server, atmmcp.ToolDefinition{
			Name:        toolName,
			Description: description,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string", "description": "Text message to send when no payload is provided."},
					"payload": map[string]any{"type": "object", "description": "Optional complete provider payload object."},
				},
				"additionalProperties": false,
			},
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			args, err := decodeWebhookToolArgs(req.Params.Arguments)
			if err != nil {
				return nil, err
			}
			body := []byte(args.Payload)
			if len(strings.TrimSpace(string(body))) == 0 {
				body, err = defaultWebhookPayload(decl, args.Message)
				if err != nil {
					return nil, err
				}
			}
			if err := deliverWebhook(ctx, decl, body); err != nil {
				return nil, err
			}
			return atmmcp.JSONTextResult(map[string]any{"sent": true, "webhook": decl.Name})
		})
	}
	return atmmcp.ServeSDKServer(ctx, server, stdin, stdout)
}

type webhookToolArgs struct {
	Message string          `json:"message"`
	Payload json.RawMessage `json:"payload"`
}

func decodeWebhookToolArgs(arguments json.RawMessage) (webhookToolArgs, error) {
	if len(arguments) == 0 {
		return webhookToolArgs{}, nil
	}
	var args webhookToolArgs
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return webhookToolArgs{}, err
	}
	payload := strings.TrimSpace(string(args.Payload))
	if payload != "" && payload != "null" && !strings.HasPrefix(payload, "{") {
		return webhookToolArgs{}, fmt.Errorf("payload must be a JSON object")
	}
	return args, nil
}

func sanitizeWebhookToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "notify"
	}
	return out
}
