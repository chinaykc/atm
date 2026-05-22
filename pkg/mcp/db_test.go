package mcp

import (
	"path/filepath"
	"strings"
	"testing"

	"atm/pkg/dsl"
)

func TestServeDBSupportsReadWriteAndGlobScan(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"atm_db_append","arguments":{"db":"notes","key":"release/api","values":["a"]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"atm_db_append","arguments":{"db":"notes","key":"release/web","values":["b"]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"atm_db_scan","arguments":{"db":"notes","pattern":"release/*"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"atm_db_get","arguments":{"db":"notes","key":"release/api"}}}`,
		"",
	}, "\n")
	var out strings.Builder
	err := ServeDB(strings.NewReader(input), &out, []dsl.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: dsl.DBAccessAdmin,
		Usage:  "test notes",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `"name":"atm_db_append"`) || !strings.Contains(got, `release/api`) || !strings.Contains(got, `values\":[\"a\"]`) {
		t.Fatalf("unexpected db MCP output:\n%s", got)
	}
}

func TestServeDBRejectsWritesWithoutAccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"atm_db_append","arguments":{"db":"notes","key":"k","values":["v"]}}}`,
		"",
	}, "\n")
	var out strings.Builder
	err := ServeDB(strings.NewReader(input), &out, []dsl.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: dsl.DBAccessRead,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "requires append access") {
		t.Fatalf("expected access error, got:\n%s", out.String())
	}
}

func TestServeDBReadonlyForcesReadAccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "notes.json")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"atm_db_set","arguments":{"db":"notes","key":"k","values":["v"]}}}`,
		"",
	}, "\n")
	var out strings.Builder
	err := ServeDB(strings.NewReader(input), &out, []dsl.DBRuntime{{
		Name:   "notes",
		Path:   dbPath,
		Access: dsl.DBAccessAdmin,
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "requires write access") {
		t.Fatalf("expected readonly access error, got:\n%s", out.String())
	}
}
