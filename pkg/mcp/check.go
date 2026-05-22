package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
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
	scanner := bufio.NewScanner(stdin)
	writer := json.NewEncoder(stdout)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		resp := handleRequest(req, resultFile)
		if err := writer.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func handleRequest(req request, resultFile string) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "atm-check",
				"version": "1",
			},
		}
	case "tools/list":
		resp.Result = map[string]any{
			"tools": []any{
				map[string]any{
					"name":        CheckToolName,
					"description": "Report whether an ATM until condition is satisfied.",
					"inputSchema": map[string]any{
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
				},
			},
		}
	case "tools/call":
		result, err := handleToolCall(req.Params, resultFile)
		if err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return resp
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func handleToolCall(raw json.RawMessage, resultFile string) (any, error) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Name != CheckToolName {
		return nil, fmt.Errorf("unknown tool %q", params.Name)
	}
	var result CheckResult
	if err := json.Unmarshal(params.Arguments, &result); err != nil {
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
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "ATM check result recorded: " + status,
			},
		},
	}, nil
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
