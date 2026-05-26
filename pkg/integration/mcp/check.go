package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const CheckToolName = "atm_report_check"

type CheckResult struct {
	Passed  bool   `json:"passed"`
	Summary string `json:"summary,omitempty"`
}

func RunCheckServerCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var resultFile string
	flags := flag.NewFlagSet("atm mcp check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&resultFile, "result-file", "", "path where the check result is written")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm mcp check runs a temporary stdio MCP server for until checks.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage of atm mcp check:")
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
	return ServeCheck(stdin, stdout, resultFile)
}

func ServeCheck(stdin io.Reader, stdout io.Writer, resultFile string) error {
	return runSDKServer(context.Background(), newCheckSDKServer(resultFile), stdin, stdout)
}

func RegisterNetworkCheck(resultFile string) (NetworkEndpoint, error) {
	manager, err := DefaultNetworkManager()
	if err != nil {
		return NetworkEndpoint{}, err
	}
	return manager.Register(newCheckSDKServer(resultFile))
}

func newCheckSDKServer(resultFile string) *mcpsdk.Server {
	server := NewSDKServer("atm-check")
	AddTool(server, checkToolDefinition(), func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return recordCheckResult(resultFile, req.Params.Arguments)
	})
	return server
}

func checkToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        CheckToolName,
		Description: "Report whether an ATM until condition is satisfied.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"passed": map[string]any{
					"type":        "boolean",
					"description": "true only when the condition is fully satisfied",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "brief evidence for the decision",
				},
			},
			"required": []string{"passed"},
		},
	}
}

func recordCheckResult(resultFile string, arguments json.RawMessage) (*mcpsdk.CallToolResult, error) {
	var result CheckResult
	if err := json.Unmarshal(arguments, &result); err != nil {
		return nil, err
	}
	if err := WriteCheckResult(resultFile, result); err != nil {
		return nil, err
	}
	_ = appendDebugLog(result)
	status := "FAIL"
	if result.Passed {
		status = "PASS"
	}
	return textResult("ATM check result recorded: " + status), nil
}

func appendDebugLog(result CheckResult) error {
	path := os.Getenv("ATM_MCP_CHECK_LOG")
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	status := "FAIL"
	if result.Passed {
		status = "PASS"
	}
	_, err = fmt.Fprintf(file, "%s %s %s\n", time.Now().Format(time.RFC3339), status, result.Summary)
	return err
}

func WriteCheckResult(path string, result CheckResult) error {
	if path == "" {
		return errors.New("result file is required")
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(result)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func ReadCheckResult(path string) (CheckResult, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CheckResult{}, false, nil
		}
		return CheckResult{}, false, err
	}
	defer file.Close()
	var result CheckResult
	if err := json.NewDecoder(file).Decode(&result); err != nil {
		return CheckResult{}, false, err
	}
	return result, true, nil
}
