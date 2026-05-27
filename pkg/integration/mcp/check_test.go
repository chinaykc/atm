package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServeCheckReportsToolAndWritesResult(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "result.json")
	session, cleanup := connectTestMCP(t, newCheckSDKServer(resultFile))
	defer cleanup()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != CheckToolName {
		t.Fatalf("expected check tool, got %#v", tools.Tools)
	}
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      CheckToolName,
		Arguments: map[string]any{"passed": true, "summary": "done"},
	}); err != nil {
		t.Fatal(err)
	}
	result, ok, err := ReadCheckResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !result.Passed || result.Summary != "done" {
		t.Fatalf("unexpected result: ok=%v result=%#v", ok, result)
	}
}

func TestCheckRejectsUnknownOrMissingPassed(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{
			name: "unknown passed typo",
			args: `{"passsed":true,"summary":"done"}`,
			want: `unknown field "passsed"`,
		},
		{
			name: "missing passed",
			args: `{"summary":"done"}`,
			want: "passed is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeCheckResult(json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
	result, err := decodeCheckResult(json.RawMessage(`{"passed":false,"summary":"not yet"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed || result.Summary != "not yet" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunCheckServerCLIRequiresResultFile(t *testing.T) {
	var stderr strings.Builder
	err := RunCheckServerCLI(nil, strings.NewReader(""), &strings.Builder{}, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "-result-file is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteCheckResultProducesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := WriteCheckResult(path, CheckResult{Passed: false, Summary: "not yet"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CheckResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Passed || decoded.Summary != "not yet" {
		t.Fatalf("unexpected decoded result: %#v", decoded)
	}
}

func TestCheckStdioE2E(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "result.json")
	cmd := exec.Command(os.Args[0], "-test.run=TestMCPStdioHelperProcess", "--", "check", resultFile)
	cmd.Env = append(os.Environ(), "ATM_MCP_STDIO_HELPER=1")
	cmd.Stderr = os.Stderr

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "atm-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      CheckToolName,
		Arguments: map[string]any{"passed": true, "summary": "stdio e2e"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	result, ok, err := ReadCheckResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !result.Passed || result.Summary != "stdio e2e" {
		t.Fatalf("unexpected result: ok=%v result=%#v", ok, result)
	}
}

func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("ATM_MCP_STDIO_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 3 || args[1] != "check" {
		fmt.Fprintln(os.Stderr, "usage: -- check RESULT_FILE")
		os.Exit(2)
	}
	if err := RunCheckServerCLI([]string{"-result-file", args[2]}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestServeOutputReportsSchemaAndWritesResult(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "output.json")
	schema := `{"type":"object","required":["reason"],"properties":{"reason":{"type":"string"}}}`
	server, err := newOutputSDKServer(resultFile, schema, "json")
	if err != nil {
		t.Fatal(err)
	}
	session, cleanup := connectTestMCP(t, server)
	defer cleanup()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != OutputToolName {
		t.Fatalf("expected output tool, got %#v", tools.Tools)
	}
	schemaData, err := json.Marshal(tools.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schemaData), `"reason"`) {
		t.Fatalf("expected output schema to include reason, got %s", schemaData)
	}
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      OutputToolName,
		Arguments: map[string]any{"reason": "done"},
	}); err != nil {
		t.Fatal(err)
	}
	data, ok, err := ReadOutputResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.Contains(string(data), `"reason": "done"`) {
		t.Fatalf("unexpected output result ok=%v data=%s", ok, data)
	}
}

func TestServeOutputRejectsSchemaInvalidResult(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "output.json")
	schema := `{"type":"object","required":["reason"],"properties":{"reason":{"type":"string"}}}`
	server, err := newOutputSDKServer(resultFile, schema, "json")
	if err != nil {
		t.Fatal(err)
	}
	session, cleanup := connectTestMCP(t, server)
	defer cleanup()
	_, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      OutputToolName,
		Arguments: map[string]any{"detail": "missing required reason"},
	})
	if err == nil || !strings.Contains(err.Error(), "structured output does not match schema") {
		t.Fatalf("expected schema validation error, got %v", err)
	}
	if _, ok, err := ReadOutputResult(resultFile); err != nil || ok {
		t.Fatalf("invalid output should not be recorded: ok=%v err=%v", ok, err)
	}
	_, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      OutputToolName,
		Arguments: map[string]any{"reason": 123},
	})
	if err == nil || !strings.Contains(err.Error(), "structured output does not match schema") {
		t.Fatalf("expected schema validation error, got %v", err)
	}
}

func TestServeOutputAcceptsYAMLSchema(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "output.json")
	schema := "type: object\nrequired:\n  - weather\nproperties:\n  weather:\n    type: string\n"
	server, err := newOutputSDKServer(resultFile, schema, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	session, cleanup := connectTestMCP(t, server)
	defer cleanup()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	schemaData, err := json.Marshal(tools.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schemaData), `"weather"`) {
		t.Fatalf("expected YAML schema converted into tool schema:\n%s", schemaData)
	}
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      OutputToolName,
		Arguments: map[string]any{"weather": "sunny"},
	}); err != nil {
		t.Fatal(err)
	}
	data, ok, err := ReadOutputResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.Contains(string(data), `"weather": "sunny"`) {
		t.Fatalf("unexpected output result ok=%v data=%s", ok, data)
	}
}

func connectTestMCP(t *testing.T, server *mcpsdk.Server) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "atm-test", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}
