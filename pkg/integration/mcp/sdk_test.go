package mcp

import (
	"context"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNetworkCheckWritesResult(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "result.json")
	endpoint, err := RegisterNetworkCheck(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "atm-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: endpoint.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if _, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      CheckToolName,
		Arguments: map[string]any{"passed": true, "summary": "network"},
	}); err != nil {
		t.Fatal(err)
	}
	result, ok, err := ReadCheckResult(resultFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !result.Passed || result.Summary != "network" {
		t.Fatalf("unexpected result ok=%v result=%#v", ok, result)
	}
}
