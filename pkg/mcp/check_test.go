package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeCheckReportsToolAndWritesResult(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "result.json")
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"atm_report_check","arguments":{"passed":true,"summary":"done"}}}`,
		"",
	}, "\n")
	var out strings.Builder
	if err := ServeCheck(strings.NewReader(input), &out, resultFile); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name":"atm_report_check"`) {
		t.Fatalf("expected tool listing in output:\n%s", out.String())
	}
	result, ok, err := ReadCheckResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !result.Passed || result.Summary != "done" {
		t.Fatalf("unexpected result: ok=%v result=%#v", ok, result)
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

func TestServeOutputReportsSchemaAndWritesResult(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "output.json")
	schema := `{"type":"object","required":["reason"],"properties":{"reason":{"type":"string"}}}`
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"atm_report_output","arguments":{"reason":"done"}}}`,
		"",
	}, "\n")
	var out strings.Builder
	if err := ServeOutput(strings.NewReader(input), &out, resultFile, schema, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name":"atm_report_output"`) || !strings.Contains(out.String(), `"reason"`) {
		t.Fatalf("expected output tool and schema in response:\n%s", out.String())
	}
	data, ok, err := ReadOutputResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.Contains(string(data), `"reason": "done"`) {
		t.Fatalf("unexpected output result ok=%v data=%s", ok, data)
	}
}

func TestServeOutputAcceptsYAMLSchema(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "output.json")
	schema := "type: object\nrequired:\n  - weather\nproperties:\n  weather:\n    type: string\n"
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"atm_report_output","arguments":{"weather":"sunny"}}}`,
		"",
	}, "\n")
	var out strings.Builder
	if err := ServeOutput(strings.NewReader(input), &out, resultFile, schema, "yaml"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"weather"`) {
		t.Fatalf("expected YAML schema converted into tool schema:\n%s", out.String())
	}
	data, ok, err := ReadOutputResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !strings.Contains(string(data), `"weather": "sunny"`) {
		t.Fatalf("unexpected output result ok=%v data=%s", ok, data)
	}
}
