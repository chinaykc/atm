package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/agent"
	atmmcp "github.com/chinaykc/atm/pkg/integration/mcp"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/runtime/store"
	"io"
	"os"
	"slices"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	var config compiler.DefMCPRuntime
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse defs config: %w", err)
	}
	return ServeDefsMCP(context.Background(), stdin, stdout, stderr, config)
}

func ServeDefsMCP(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, config compiler.DefMCPRuntime) error {
	if config.Depth <= 0 {
		config.Depth = 1
	}
	runner, err := agent.NewRunner(config.Tool, agent.Config{CodexPath: config.CodexPath, ClaudePath: config.ClaudePath})
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
	return server.run(ctx, stdin, stdout)
}

func (e *Engine) registerNetworkDefsMCP(ctx context.Context, config *compiler.DefMCPRuntime) error {
	if config == nil || config.Depth <= 0 || (len(config.Definitions) == 0 && len(config.Defs) == 0) {
		return nil
	}
	server := defsMCPServer{engine: e, config: *config, stderr: e.stderr}
	url, err := server.registerNetwork(ctx)
	if err != nil {
		return err
	}
	config.URL = url
	return nil
}

type defsMCPServer struct {
	engine *Engine
	config compiler.DefMCPRuntime
	stderr io.Writer
}

func (s defsMCPServer) run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return atmmcp.ServeSDKServer(ctx, s.mcpServer(), stdin, stdout)
}

func (s defsMCPServer) registerNetwork(ctx context.Context) (string, error) {
	manager, err := atmmcp.DefaultNetworkManager()
	if err != nil {
		return "", err
	}
	endpoint, err := manager.Register(s.mcpServer())
	if err != nil {
		return "", err
	}
	go func() {
		<-ctx.Done()
		endpoint.Close()
	}()
	return endpoint.URL, nil
}

func (s defsMCPServer) mcpServer() *mcpsdk.Server {
	return atmmcp.NewDefsSDKServer(s.definitionRefs(), s.callTool)
}

func (s defsMCPServer) definitionRefs() []compiler.DefinitionRef {
	if len(s.config.Defs) > 0 {
		refs := slices.Clone(s.config.Defs)
		slices.SortFunc(refs, func(a, b compiler.DefinitionRef) int { return cmp.Compare(a.Name, b.Name) })
		return refs
	}
	var refs []compiler.DefinitionRef
	for _, name := range s.config.Definitions {
		if def, ok := s.engine.defs[name]; ok {
			refs = append(refs, compiler.DefinitionRef{Name: name, Params: slices.Clone(def.Params)})
		}
	}
	slices.SortFunc(refs, func(a, b compiler.DefinitionRef) int { return cmp.Compare(a.Name, b.Name) })
	return refs
}

func (s defsMCPServer) callTool(ctx context.Context, toolName string, arguments json.RawMessage) (any, error) {
	name, ok := s.toolDefinitionName(toolName)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", toolName)
	}
	def, ok := s.engine.defs[name]
	if !ok {
		return nil, fmt.Errorf("unknown definition %q", name)
	}
	var args map[string]string
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
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
	value, returned, err := s.engine.ExecuteDefinition(ctx, compiler.Call{Name: name, Args: callArgs}, ExecuteDefinitionOptions{
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
	return map[string]any{"ok": true, "definition": name, "returned": returned, "value": value}, nil
}

func (s defsMCPServer) toolDefinitionName(tool string) (string, bool) {
	return atmmcp.DefNameForTool(s.definitionRefs(), tool)
}

type ExecuteDefinitionOptions struct {
	Vars      map[string]any
	Workdir   string
	DBs       []compiler.DBRuntime
	Skills    []compiler.SkillRuntime
	MCPs      []compiler.MCPRuntime
	Depth     int
	Stdout    io.Writer
	Stderr    io.Writer
	StartedAt time.Time
}

func (e *Engine) ExecuteDefinition(ctx context.Context, call compiler.Call, opts ExecuteDefinitionOptions) (any, bool, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.StartedAt.IsZero() {
		opts.StartedAt = time.Now()
	}
	baseOptions := compiler.RunOptions{
		Workdir: opts.Workdir,
		DBs:     slices.Clone(opts.DBs),
		Skills:  slices.Clone(opts.Skills),
		MCPs:    slices.Clone(opts.MCPs),
	}
	if opts.Depth > 0 {
		// Reserved for future nested def-MCP support; v1 intentionally disables
		// dynamic def-MCP inside definitions invoked through def-MCP.
		baseOptions.DefDepth = opts.Depth
	}
	x := &taskExecution{
		engine:     e,
		task:       compiler.Task{BlockIndex: -1},
		lease:      store.BlockLease{},
		start:      opts.StartedAt,
		stdout:     opts.Stdout,
		stderr:     opts.Stderr,
		file:       noopFile{},
		branches:   &branchCollector{},
		writeState: false,
	}
	vars := ir.CloneVars(opts.Vars)
	if vars == nil {
		vars = map[string]any{}
	}
	current := execContext{vars: vars, options: baseOptions, loopOp: -1}
	return x.callDefinition(ctx, current, call, true)
}

type noopFile struct{}

func (noopFile) Close() error { return nil }
