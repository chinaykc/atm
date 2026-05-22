package engine

import (
	"atm/pkg/dsl"
	"atm/pkg/store"
	"atm/pkg/tools"
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

func RunDefsMCPServerCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var configFile string
	flags := flag.NewFlagSet("atm mcp defs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&configFile, "config-file", "", "path to defs MCP config JSON")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm mcp defs runs a temporary stdio MCP server for ATM definitions.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage of atm mcp defs:")
		flags.PrintDefaults()
	}
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
		return fmt.Errorf("read defs config: %w", err)
	}
	var config dsl.DefMCPRuntime
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse defs config: %w", err)
	}
	return ServeDefsMCP(context.Background(), stdin, stdout, stderr, config)
}

func ServeDefsMCP(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, config dsl.DefMCPRuntime) error {
	if config.Depth <= 0 {
		config.Depth = 1
	}
	runner, err := tools.NewRunner(config.Tool, tools.Config{CodexPath: config.CodexPath, ClaudePath: config.ClaudePath})
	if err != nil {
		return err
	}
	engine, err := New(Options{
		FilePath:     config.TodoPath,
		Runner:       runner,
		ToolName:     config.Tool,
		CodexPath:    config.CodexPath,
		ClaudePath:   config.ClaudePath,
		Stdout:       io.Discard,
		Stderr:       stderr,
		MessageLimit: config.Messages,
		OutputDir:    config.OutputDir,
		GlobalJobs:   config.Jobs,
	})
	if err != nil {
		return err
	}
	if err := engine.loadDefinitions(config.TodoPath); err != nil {
		return err
	}
	if err := engine.loadGlobalDeclarations(); err != nil {
		return err
	}
	server := defsMCPServer{engine: engine, config: config, stderr: stderr}
	scanner := bufio.NewScanner(stdin)
	writer := json.NewEncoder(stdout)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		resp := server.handle(ctx, req)
		if err := writer.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type defsMCPServer struct {
	engine *Engine
	config dsl.DefMCPRuntime
	stderr io.Writer
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s defsMCPServer) handle(ctx context.Context, req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "atm-defs", "version": "1"},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools()}
	case "tools/call":
		result, err := s.call(ctx, req.Params)
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

func (s defsMCPServer) tools() []any {
	refs := s.definitionRefs()
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		properties := map[string]any{}
		required := make([]string, 0, len(ref.Params))
		for _, param := range ref.Params {
			properties[param] = map[string]any{"type": "string", "description": "Argument for definition parameter " + param}
			required = append(required, param)
		}
		out = append(out, map[string]any{
			"name":        dsl.DefMCPToolName(ref.Name),
			"description": "Run ATM definition " + ref.Name + " and return its result.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           properties,
				"required":             required,
				"additionalProperties": false,
			},
		})
	}
	return out
}

func (s defsMCPServer) definitionRefs() []dsl.DefinitionRef {
	if len(s.config.Defs) > 0 {
		refs := append([]dsl.DefinitionRef{}, s.config.Defs...)
		sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
		return refs
	}
	var refs []dsl.DefinitionRef
	for _, name := range s.config.Definitions {
		if def, ok := s.engine.defs[name]; ok {
			refs = append(refs, dsl.DefinitionRef{Name: name, Params: append([]string{}, def.Params...)})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

func (s defsMCPServer) call(ctx context.Context, raw json.RawMessage) (any, error) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	name, ok := s.toolDefinitionName(params.Name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", params.Name)
	}
	def, ok := s.engine.defs[name]
	if !ok {
		return nil, fmt.Errorf("unknown definition %q", name)
	}
	var args map[string]string
	if len(params.Arguments) > 0 {
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
	}
	callArgs := make([]string, 0, len(def.Params))
	for _, param := range def.Params {
		value, ok := args[param]
		if !ok {
			return nil, fmt.Errorf("missing argument %q", param)
		}
		callArgs = append(callArgs, value)
	}
	value, returned, err := s.engine.ExecuteDefinition(ctx, dsl.Call{Name: name, Args: callArgs}, ExecuteDefinitionOptions{
		Vars:      s.config.Vars,
		Workdir:   s.config.Workdir,
		DBs:       s.config.DBs,
		Skills:    s.config.Skills,
		MCPs:      s.config.MCPs,
		Depth:     s.config.Depth - 1,
		Stdout:    io.Discard,
		Stderr:    s.stderr,
		StartedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	return contentResult(map[string]any{"ok": true, "definition": name, "returned": returned, "value": value})
}

func (s defsMCPServer) toolDefinitionName(tool string) (string, bool) {
	for _, ref := range s.definitionRefs() {
		if dsl.DefMCPToolName(ref.Name) == tool {
			return ref.Name, true
		}
	}
	return "", false
}

func contentResult(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}}, nil
}

type ExecuteDefinitionOptions struct {
	Vars      map[string]any
	Workdir   string
	DBs       []dsl.DBRuntime
	Skills    []dsl.SkillRuntime
	MCPs      []dsl.MCPRuntime
	Depth     int
	Stdout    io.Writer
	Stderr    io.Writer
	StartedAt time.Time
}

func (e *Engine) ExecuteDefinition(ctx context.Context, call dsl.Call, opts ExecuteDefinitionOptions) (any, bool, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.StartedAt.IsZero() {
		opts.StartedAt = time.Now()
	}
	baseOptions := dsl.RunOptions{
		Workdir: opts.Workdir,
		DBs:     append([]dsl.DBRuntime{}, opts.DBs...),
		Skills:  append([]dsl.SkillRuntime{}, opts.Skills...),
		MCPs:    append([]dsl.MCPRuntime{}, opts.MCPs...),
	}
	if opts.Depth > 0 {
		// Reserved for future nested def-MCP support; v1 intentionally disables
		// dynamic def-MCP inside definitions invoked through def-MCP.
		baseOptions.DefDepth = opts.Depth
	}
	x := &taskExecution{
		engine:     e,
		task:       dsl.Task{BlockIndex: -1},
		lease:      store.BlockLease{},
		start:      opts.StartedAt,
		stdout:     opts.Stdout,
		stderr:     opts.Stderr,
		file:       noopFile{},
		branches:   &branchCollector{},
		writeState: false,
	}
	vars := dsl.CloneVars(opts.Vars)
	if vars == nil {
		vars = map[string]any{}
	}
	current := execContext{vars: vars, options: baseOptions, loopOp: -1}
	return x.callDefinition(ctx, current, call, true)
}

type noopFile struct{}

func (noopFile) Close() error { return nil }
