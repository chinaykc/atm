package mcp

import (
	"context"
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
