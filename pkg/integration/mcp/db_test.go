package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinaykc/atm/pkg/lang/ir"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServeDBSupportsReadWriteAndGlobScan(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	session, cleanup := connectTestMCP(t, newDBSDKServer([]ir.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: ir.DBAccessAdmin,
		Usage:  "test notes",
	}}, false))
	defer cleanup()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTool(tools.Tools, DBAppendToolName) {
		t.Fatalf("expected append tool, got %#v", tools.Tools)
	}
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      DBAppendToolName,
		Arguments: map[string]any{"db": "notes", "key": "release/api", "values": []string{"a"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      DBAppendToolName,
		Arguments: map[string]any{"db": "notes", "key": "release/web", "values": []string{"b"}},
	}); err != nil {
		t.Fatal(err)
	}
	scan, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      DBScanToolName,
		Arguments: map[string]any{"db": "notes", "pattern": "release/*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := contentText(t, scan)
	if !strings.Contains(got, `release/api`) || !strings.Contains(got, `release/web`) {
		t.Fatalf("unexpected scan output:\n%s", got)
	}
	get, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      DBGetToolName,
		Arguments: map[string]any{"db": "notes", "key": "release/api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contentText(t, get), `"values":["a"]`) {
		t.Fatalf("unexpected get output:\n%s", contentText(t, get))
	}
}

func TestServeDBRejectsWritesWithoutAccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	session, cleanup := connectTestMCP(t, newDBSDKServer([]ir.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: ir.DBAccessRead,
	}}, false))
	defer cleanup()
	_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      DBAppendToolName,
		Arguments: map[string]any{"db": "notes", "key": "k", "values": []string{"v"}},
	})
	if err == nil || !strings.Contains(err.Error(), "requires append access") {
		t.Fatalf("expected access error, got %v", err)
	}
}

func TestServeDBReadonlyForcesReadAccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	session, cleanup := connectTestMCP(t, newDBSDKServer([]ir.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: ir.DBAccessAdmin,
	}}, true))
	defer cleanup()
	_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      DBSetToolName,
		Arguments: map[string]any{"db": "notes", "key": "k", "values": []string{"v"}},
	})
	if err == nil || !strings.Contains(err.Error(), "requires write access") {
		t.Fatalf("expected readonly access error, got %v", err)
	}
}

func TestDBConfigRejectsUnknownFields(t *testing.T) {
	_, err := parseDBServerConfig([]byte(`{"databases":[],"databasez":[]}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "databasez"`) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
	if _, err := parseDBServerConfig([]byte(`{"databases":[{"name":"notes","path":"notes.json","scope":"global","persist":"run","access":"read","extra":true}]}`)); err == nil || !strings.Contains(err.Error(), `unknown field "extra"`) {
		t.Fatalf("expected nested unknown field error, got %v", err)
	}
}

func TestDBWriteRejectsMissingValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	server := newDBServer([]ir.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: ir.DBAccessAdmin,
	}}, false)
	cases := []struct {
		name string
		tool string
		args string
		want string
	}{
		{
			name: "singular value field",
			tool: DBSetToolName,
			args: `{"db":"notes","key":"k","value":"v"}`,
			want: `unknown field "value"`,
		},
		{
			name: "missing values",
			tool: DBSetToolName,
			args: `{"db":"notes","key":"k"}`,
			want: "values is required",
		},
		{
			name: "null values",
			tool: DBAppendToolName,
			args: `{"db":"notes","key":"k","values":null}`,
			want: "values is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := server.callTool(tc.tool, json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
	data, err := readDBFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("invalid writes should not create DB entries, got %#v", data)
	}
	if _, err := server.callTool(DBSetToolName, json.RawMessage(`{"db":"notes","key":"empty","values":[]}`)); err != nil {
		t.Fatalf("empty values array should be accepted: %v", err)
	}
	values, found, err := readDBKey(dbPath, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if !found || values == nil || len(values) != 0 {
		t.Fatalf("expected empty values array, found=%v values=%#v", found, values)
	}
}

func hasTool(tools []*mcpsdk.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func contentText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected text content")
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}
