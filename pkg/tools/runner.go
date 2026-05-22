package tools

import (
	"atm/pkg/dsl"
	"atm/pkg/mcp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	TodoFileEnv   = "ATM_TODO_FILE"
	mcpServerName = "atm_check"
	outputMCPName = "atm_output"
	dbMCPName     = "atm_db"
	defsMCPName   = "atm_defs"
)

type Runner interface {
	Name() string
	Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (ExecuteResult, error)
	Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error)
}

type ExecuteResult struct {
	Messages         []dsl.OutputMessage
	RawEvents        string
	StructuredOutput []byte
}

type Config struct {
	CodexPath  string
	ClaudePath string
}

type factory func(Config) (Runner, error)

var factories = map[string]factory{
	"claude":      newClaudeRunner,
	"claude-code": newClaudeRunner,
	"codex":       newCodexRunner,
}

func NewRunner(name string, config Config) (Runner, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		key = "codex"
	}
	factory, ok := factories[key]
	if !ok {
		return nil, fmt.Errorf("unsupported tool adapter %q; currently supported: %s", name, strings.Join(SupportedRunnerNames(), ", "))
	}
	return factory(config)
}

func SupportedRunnerNames() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type codexRunner struct {
	path string
}

func newCodexRunner(config Config) (Runner, error) {
	path := strings.TrimSpace(config.CodexPath)
	if path == "" {
		path = "codex"
	}
	return codexRunner{path: path}, nil
}

func (r codexRunner) Name() string {
	return "codex"
}

func (r codexRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (ExecuteResult, error) {
	skillCleanup, err := prepareSkills("codex", opts)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer skillCleanup()
	resultFile, schemaFile, cleanup, err := prepareOutputMCP(opts.Output)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer cleanup()
	dbConfigFile, dbCleanup, err := prepareDBMCP(opts.DBs)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer dbCleanup()
	defConfigFile, defCleanup, err := prepareDefMCP(opts.DefMCP)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer defCleanup()
	if err := validateRuntimeMCPs(opts.MCPs); err != nil {
		return ExecuteResult{}, err
	}
	if opts.Output != nil && opts.Output.IsStructured() {
		prompt = appendOutputToolInstruction(prompt)
	}
	if len(opts.DBs) > 0 {
		prompt = appendDBToolInstruction(prompt)
	}
	if opts.DefMCP != nil && len(opts.DefMCP.Definitions) > 0 && opts.DefMCP.Depth > 0 {
		prompt = appendDefToolInstruction(prompt, opts.DefMCP.Definitions)
	}
	cmd := exec.CommandContext(ctx, r.path, codexArgs(opts, resultFile, schemaFile, dbConfigFile, defConfigFile, false)...)
	cmd.Env = toolEnv(todoPath)
	cmd.Dir = opts.Workdir
	cmd.Stdin = strings.NewReader(prompt)
	stream, err := runAgentCommand(cmd, r.Name(), stdout, stderr)
	if err != nil {
		return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw}, err
	}
	structuredOutput, err := readOutputMCPResult(resultFile)
	if err != nil {
		return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw}, err
	}
	return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw, StructuredOutput: structuredOutput}, nil
}

func (r codexRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return runCheckCommand(ctx, r.path, func(_ string, resultFile, dbConfigFile string) []string {
		return codexCheckArgs(opts, resultFile, dbConfigFile)
	}, todoPath, buildCheckPrompt, prompt, condition, opts, true, true, r.Name(), stdout, stderr)
}

func codexArgs(opts dsl.RunOptions, resultFile, schemaFile, dbConfigFile, defConfigFile string, readonlyDB bool) []string {
	args := []string{"exec", "--json"}
	if opts.Resume {
		args = append(args, "resume", "--last")
	}
	if opts.Output != nil && opts.Output.IsStructured() {
		args = append(args, codexOutputMCPArgs(opts.Output, resultFile, schemaFile)...)
	}
	if dbConfigFile != "" {
		args = append(args, codexDBMCPArgs(dbConfigFile, readonlyDB)...)
	}
	args = append(args, codexExternalMCPArgs(opts.MCPs)...)
	if defConfigFile != "" {
		args = append(args, codexDefsMCPArgs(defConfigFile, opts.DefMCP)...)
	}
	args = append(args, opts.Args...)
	args = append(args, "-")
	return args
}

func codexOutputMCPArgs(spec *dsl.OutputSpec, resultFile, schemaFile string) []string {
	server := outputMCPServer(resultFile, schemaFile, spec.SchemaFormat)
	return []string{
		"-c", "mcp_servers." + outputMCPName + ".command=" + tomlString(server.command),
		"-c", "mcp_servers." + outputMCPName + ".args=" + tomlStringArray(server.args),
		"-c", "mcp_servers." + outputMCPName + ".tools." + mcp.OutputToolName + ".approval_mode=\"approve\"",
	}
}

func codexCheckArgs(opts dsl.RunOptions, resultFile string, dbConfigFiles ...string) []string {
	server := checkMCPServer(resultFile)
	dbConfigFile := ""
	if len(dbConfigFiles) > 0 {
		dbConfigFile = dbConfigFiles[0]
	}
	configArgs := []string{
		"-c", "mcp_servers." + mcpServerName + ".command=" + tomlString(server.command),
		"-c", "mcp_servers." + mcpServerName + ".args=" + tomlStringArray(server.args),
		"-c", "mcp_servers." + mcpServerName + ".tools." + mcp.CheckToolName + ".approval_mode=\"approve\"",
	}
	if dbConfigFile != "" {
		configArgs = append(configArgs, codexDBMCPArgs(dbConfigFile, true)...)
	}
	for key, value := range checkMCPServerEnv() {
		configArgs = append(configArgs, "-c", "mcp_servers."+mcpServerName+".env."+key+"="+tomlString(value))
	}

	args := []string{"exec", "--json"}
	if opts.Resume {
		args = append(args, "resume", "--last")
	}
	args = append(args, configArgs...)
	args = append(args, opts.Args...)
	args = append(args, "-")
	return args
}

func codexDBMCPArgs(configFile string, readonly bool) []string {
	server := dbMCPServer(configFile, readonly)
	args := []string{
		"-c", "mcp_servers." + dbMCPName + ".command=" + tomlString(server.command),
		"-c", "mcp_servers." + dbMCPName + ".args=" + tomlStringArray(server.args),
	}
	for _, tool := range mcp.DBToolNames() {
		args = append(args, "-c", "mcp_servers."+dbMCPName+".tools."+tool+".approval_mode=\"approve\"")
	}
	return args
}

func codexDefsMCPArgs(configFile string, spec *dsl.DefMCPRuntime) []string {
	server := defsMCPServer(configFile)
	args := []string{
		"-c", "mcp_servers." + defsMCPName + ".command=" + tomlString(server.command),
		"-c", "mcp_servers." + defsMCPName + ".args=" + tomlStringArray(server.args),
	}
	for _, tool := range defMCPToolNames(spec) {
		args = append(args, "-c", "mcp_servers."+defsMCPName+".tools."+tool+".approval_mode=\"approve\"")
	}
	return args
}

func defMCPToolNames(spec *dsl.DefMCPRuntime) []string {
	if spec == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, ref := range spec.Defs {
		tool := dsl.DefMCPToolName(ref.Name)
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		out = append(out, tool)
	}
	for _, name := range spec.Definitions {
		tool := dsl.DefMCPToolName(name)
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		out = append(out, tool)
	}
	sort.Strings(out)
	return out
}

func codexExternalMCPArgs(mcps []dsl.MCPRuntime) []string {
	var args []string
	for _, item := range mcps {
		server, err := parseRuntimeMCP(item)
		if err != nil {
			continue
		}
		prefix := "mcp_servers." + item.Name
		args = append(args,
			"-c", prefix+".command="+tomlString(server.command),
			"-c", prefix+".args="+tomlStringArray(server.args),
		)
		for key, value := range server.env {
			args = append(args, "-c", prefix+".env."+key+"="+tomlString(value))
		}
	}
	return args
}

type claudeRunner struct {
	path string
}

func newClaudeRunner(config Config) (Runner, error) {
	path := strings.TrimSpace(config.ClaudePath)
	if path == "" {
		path = "claude"
	}
	return claudeRunner{path: path}, nil
}

func (r claudeRunner) Name() string {
	return "claude"
}

func (r claudeRunner) Execute(ctx context.Context, todoPath, prompt string, opts dsl.RunOptions, stdout, stderr io.Writer) (ExecuteResult, error) {
	skillCleanup, err := prepareSkills("claude", opts)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer skillCleanup()
	resultFile, schemaFile, cleanup, err := prepareOutputMCP(opts.Output)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer cleanup()
	dbConfigFile, dbCleanup, err := prepareDBMCP(opts.DBs)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer dbCleanup()
	defConfigFile, defCleanup, err := prepareDefMCP(opts.DefMCP)
	if err != nil {
		return ExecuteResult{}, err
	}
	defer defCleanup()
	if err := validateRuntimeMCPs(opts.MCPs); err != nil {
		return ExecuteResult{}, err
	}
	if opts.Output != nil && opts.Output.IsStructured() {
		prompt = appendOutputToolInstruction(prompt)
	}
	if len(opts.DBs) > 0 {
		prompt = appendDBToolInstruction(prompt)
	}
	if opts.DefMCP != nil && len(opts.DefMCP.Definitions) > 0 && opts.DefMCP.Depth > 0 {
		prompt = appendDefToolInstruction(prompt, opts.DefMCP.Definitions)
	}
	cmd := exec.CommandContext(ctx, r.path, claudeArgs(prompt, opts, resultFile, schemaFile, dbConfigFile, defConfigFile, false)...)
	cmd.Env = toolEnv(todoPath)
	cmd.Dir = opts.Workdir
	stream, err := runAgentCommand(cmd, r.Name(), stdout, stderr)
	if err != nil {
		return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw}, err
	}
	structuredOutput, err := readOutputMCPResult(resultFile)
	if err != nil {
		return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw}, err
	}
	return ExecuteResult{Messages: stream.messages, RawEvents: stream.raw, StructuredOutput: structuredOutput}, nil
}

func (r claudeRunner) Check(ctx context.Context, todoPath, prompt, condition string, opts dsl.RunOptions, stdout, stderr io.Writer) (bool, error) {
	return runCheckCommand(ctx, r.path, func(checkPrompt, resultFile, dbConfigFile string) []string {
		return claudeCheckArgs(checkPrompt, opts, resultFile, dbConfigFile)
	}, todoPath, buildCheckPrompt, prompt, condition, opts, false, true, r.Name(), stdout, stderr)
}

func claudeArgs(prompt string, opts dsl.RunOptions, resultFile, schemaFile, dbConfigFile, defConfigFile string, readonlyDB bool) []string {
	args := make([]string, 0, len(opts.Args)+3)
	if opts.Resume {
		args = append(args, "-c")
	}
	args = append(args, opts.Args...)
	if opts.Output != nil && opts.Output.IsStructured() || dbConfigFile != "" || len(opts.MCPs) > 0 || defConfigFile != "" {
		args = append(args, "--mcp-config", executeMCPConfigJSON(opts.Output, resultFile, schemaFile, dbConfigFile, defConfigFile, opts.MCPs, readonlyDB))
	}
	args = append(args, claudeAllowedToolsArgs(opts.Output, dbConfigFile != "", opts.DefMCP)...)
	args = append(args, "--output-format", "stream-json", "--verbose")
	args = append(args, "-p", prompt)
	return args
}

func claudeCheckArgs(prompt string, opts dsl.RunOptions, resultFile string, dbConfigFiles ...string) []string {
	dbConfigFile := ""
	if len(dbConfigFiles) > 0 {
		dbConfigFile = dbConfigFiles[0]
	}
	args := make([]string, 0, len(opts.Args)+5)
	if opts.Resume {
		args = append(args, "-c")
	}
	args = append(args, opts.Args...)
	args = append(args, "--mcp-config", checkMCPConfigJSON(resultFile, dbConfigFile))
	args = append(args, claudeAllowedToolsForNames([]string{claudeMCPToolName(mcpServerName, mcp.CheckToolName)}, dbConfigFile != "")...)
	args = append(args, "--output-format", "stream-json", "--verbose")
	args = append(args, "-p", prompt)
	return args
}

func claudeAllowedToolsArgs(output *dsl.OutputSpec, hasDB bool, defMCP *dsl.DefMCPRuntime) []string {
	var names []string
	if output != nil && output.IsStructured() {
		names = append(names, claudeMCPToolName(outputMCPName, mcp.OutputToolName))
	}
	for _, tool := range defMCPToolNames(defMCP) {
		names = append(names, claudeMCPToolName(defsMCPName, tool))
	}
	return claudeAllowedToolsForNames(names, hasDB)
}

func claudeAllowedToolsForNames(names []string, hasDB bool) []string {
	if hasDB {
		for _, tool := range mcp.DBToolNames() {
			names = append(names, claudeMCPToolName(dbMCPName, tool))
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	unique := names[:0]
	for _, name := range names {
		if len(unique) == 0 || unique[len(unique)-1] != name {
			unique = append(unique, name)
		}
	}
	return []string{"--allowedTools", strings.Join(unique, ",")}
}

func claudeMCPToolName(server, tool string) string {
	return "mcp__" + server + "__" + tool
}

func toolEnv(todoPath string, extra ...string) []string {
	env := append(os.Environ(), TodoFileEnv+"="+todoPath)
	env = append(env, extra...)
	return env
}

type checkPromptBuilder func(prompt, condition string, preferMCP bool) string

func buildCheckPrompt(prompt, condition string, preferMCP bool) string {
	return fmt.Sprintf(`You are performing a task completeness check.

This is a read-only check. Do not modify files, do not create files, do not delete files, do not apply patches, and do not run commands that write to disk. Only inspect the workspace and report whether the condition is already satisfied.

Condition:
%s

Task:
%s

Report the result by calling the MCP tool %q exactly once. The tool schema describes its arguments.
`, condition, prompt, mcp.CheckToolName)
}

func runCheckCommand(ctx context.Context, path string, argsForPrompt func(string, string, string) []string, todoPath string, builder checkPromptBuilder, prompt, condition string, opts dsl.RunOptions, useStdin, preferMCP bool, streamTool string, stdout, stderr io.Writer) (bool, error) {
	resultFile := ""
	cleanup := func() {}
	if preferMCP {
		var err error
		resultFile, cleanup, err = newCheckResultFile()
		if err != nil {
			return false, err
		}
	}
	defer cleanup()
	dbConfigFile, dbCleanup, err := prepareDBMCP(opts.DBs)
	if err != nil {
		return false, err
	}
	defer dbCleanup()

	stdin := builder(prompt, condition, preferMCP)
	cmd := exec.CommandContext(ctx, path, argsForPrompt(stdin, resultFile, dbConfigFile)...)
	cmd.Env = toolEnv(todoPath)
	cmd.Dir = opts.Workdir
	if useStdin {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if streamTool != "" {
		if _, err := runAgentCommand(cmd, streamTool, stdout, stderr); err != nil {
			return false, err
		}
	} else {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return false, err
		}
	}

	if resultFile != "" {
		if result, ok, err := mcp.ReadCheckResult(resultFile); err != nil {
			return false, fmt.Errorf("read MCP check result: %w", err)
		} else if ok {
			return result.Passed, nil
		}
		return false, fmt.Errorf("condition report missing MCP result")
	}

	return false, fmt.Errorf("condition report missing MCP result")
}

func newCheckResultFile() (string, func(), error) {
	dir := filepath.Join(os.TempDir(), "atm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create check result directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "check-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create check result file: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, err
	}
	if err := os.Remove(path); err != nil {
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func prepareOutputMCP(spec *dsl.OutputSpec) (resultFile, schemaFile string, cleanup func(), err error) {
	cleanup = func() {}
	if spec == nil || !spec.IsStructured() {
		return "", "", cleanup, nil
	}
	dir := filepath.Join(os.TempDir(), "atm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", nil, fmt.Errorf("create output MCP directory: %w", err)
	}
	result, err := os.CreateTemp(dir, "output-*.json")
	if err != nil {
		return "", "", nil, fmt.Errorf("create output result file: %w", err)
	}
	resultFile = result.Name()
	if err := result.Close(); err != nil {
		_ = os.Remove(resultFile)
		return "", "", nil, err
	}
	if err := os.Remove(resultFile); err != nil {
		return "", "", nil, err
	}
	schema, err := os.CreateTemp(dir, "output-schema-*.txt")
	if err != nil {
		_ = os.Remove(resultFile)
		return "", "", nil, fmt.Errorf("create output schema file: %w", err)
	}
	schemaFile = schema.Name()
	if _, err := schema.WriteString(spec.Schema); err != nil {
		schema.Close()
		_ = os.Remove(resultFile)
		_ = os.Remove(schemaFile)
		return "", "", nil, fmt.Errorf("write output schema file: %w", err)
	}
	if err := schema.Close(); err != nil {
		_ = os.Remove(resultFile)
		_ = os.Remove(schemaFile)
		return "", "", nil, err
	}
	return resultFile, schemaFile, func() {
		_ = os.Remove(resultFile)
		_ = os.Remove(schemaFile)
	}, nil
}

func prepareDBMCP(dbs []dsl.DBRuntime) (configFile string, cleanup func(), err error) {
	cleanup = func() {}
	if len(dbs) == 0 {
		return "", cleanup, nil
	}
	dir := filepath.Join(os.TempDir(), "atm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create db MCP directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "db-config-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create db MCP config: %w", err)
	}
	configFile = file.Name()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(map[string]any{"databases": dbs})
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(configFile)
		return "", nil, fmt.Errorf("write db MCP config: %w", encodeErr)
	}
	if closeErr != nil {
		_ = os.Remove(configFile)
		return "", nil, closeErr
	}
	return configFile, func() { _ = os.Remove(configFile) }, nil
}

func prepareDefMCP(spec *dsl.DefMCPRuntime) (configFile string, cleanup func(), err error) {
	cleanup = func() {}
	if spec == nil || len(spec.Definitions) == 0 || spec.Depth <= 0 {
		return "", cleanup, nil
	}
	dir := filepath.Join(os.TempDir(), "atm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create defs MCP directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "defs-config-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create defs MCP config: %w", err)
	}
	configFile = file.Name()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(spec)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(configFile)
		return "", nil, fmt.Errorf("write defs MCP config: %w", encodeErr)
	}
	if closeErr != nil {
		_ = os.Remove(configFile)
		return "", nil, closeErr
	}
	return configFile, func() { _ = os.Remove(configFile) }, nil
}

func prepareSkills(adapter string, opts dsl.RunOptions) (func(), error) {
	if len(opts.Skills) == 0 {
		return func() {}, nil
	}
	if opts.Workdir == "" {
		return func() {}, fmt.Errorf("skills require a task workdir; use /cd before /skill use")
	}
	var root string
	switch adapter {
	case "codex":
		root = filepath.Join(opts.Workdir, ".agents", "skills")
	default:
		root = filepath.Join(opts.Workdir, ".claude", "skills")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return func() {}, fmt.Errorf("create skill target directory: %w", err)
	}
	var created []string
	for _, skill := range opts.Skills {
		target := filepath.Join(root, skill.Name)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return func() {}, fmt.Errorf("stat skill target %q: %w", target, err)
		}
		if err := copyDir(skill.Path, target); err != nil {
			_ = os.RemoveAll(target)
			return func() {}, fmt.Errorf("materialize skill %q: %w", skill.Name, err)
		}
		created = append(created, target)
	}
	return func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.RemoveAll(created[i])
		}
	}, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func readOutputMCPResult(resultFile string) ([]byte, error) {
	if resultFile == "" {
		return nil, nil
	}
	data, ok, err := mcp.ReadOutputResult(resultFile)
	if err != nil {
		return nil, fmt.Errorf("read MCP output result: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("structured output report missing MCP result")
	}
	return data, nil
}

func appendOutputToolInstruction(prompt string) string {
	return strings.TrimRight(prompt, "\r\n") + fmt.Sprintf(`

Structured output:
Call the MCP tool %q exactly once when the task is complete. The tool schema describes the required JSON arguments. Do not report the structured result as ordinary prose; submit it through the tool.
`, mcp.OutputToolName)
}

func appendDBToolInstruction(prompt string) string {
	return strings.TrimRight(prompt, "\r\n") + `

Databases:
Use the available ATM DB MCP tools for task memory or blackboard state when useful. Respect each database's reported access level and usage description.
`
}

func appendDefToolInstruction(prompt string, names []string) string {
	return strings.TrimRight(prompt, "\r\n") + fmt.Sprintf(`

ATM definitions:
You may call the available ATM definition MCP tools when one of these reusable tasks is useful: %s. Treat tool results as the definition return value.
`, strings.Join(names, ", "))
}

func mcpServerCommand(resultFile string) string {
	server := checkMCPServer(resultFile)
	parts := append([]string{server.command}, server.args...)
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

type mcpServerSpec struct {
	command string
	args    []string
	env     map[string]string
}

func checkMCPServer(resultFile string) mcpServerSpec {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "atm"
	}
	return mcpServerSpec{
		command: exe,
		args:    []string{"mcp", "check", "-result-file", resultFile},
	}
}

func outputMCPServer(resultFile, schemaFile, schemaFormat string) mcpServerSpec {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "atm"
	}
	return mcpServerSpec{
		command: exe,
		args: []string{
			"mcp", "output",
			"-result-file", resultFile,
			"-schema-file", schemaFile,
			"-schema-format", schemaFormat,
		},
	}
}

func dbMCPServer(configFile string, readonly bool) mcpServerSpec {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "atm"
	}
	args := []string{"mcp", "db", "-config-file", configFile}
	if readonly {
		args = append(args, "-readonly")
	}
	return mcpServerSpec{command: exe, args: args}
}

func defsMCPServer(configFile string) mcpServerSpec {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "atm"
	}
	return mcpServerSpec{command: exe, args: []string{"mcp", "defs", "-config-file", configFile}}
}

func mcpConfigJSON(resultFile string) string {
	server := checkMCPServer(resultFile)
	serverConfig := map[string]any{
		"type":    "stdio",
		"command": server.command,
		"args":    server.args,
	}
	if env := checkMCPServerEnv(); len(env) > 0 {
		serverConfig["env"] = env
	}
	config := map[string]any{
		"mcpServers": map[string]any{
			mcpServerName: serverConfig,
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func checkMCPConfigJSON(resultFile, dbConfigFile string) string {
	check := checkMCPServer(resultFile)
	servers := map[string]any{
		mcpServerName: map[string]any{
			"type":    "stdio",
			"command": check.command,
			"args":    check.args,
		},
	}
	if env := checkMCPServerEnv(); len(env) > 0 {
		servers[mcpServerName].(map[string]any)["env"] = env
	}
	if dbConfigFile != "" {
		db := dbMCPServer(dbConfigFile, true)
		servers[dbMCPName] = map[string]any{
			"type":    "stdio",
			"command": db.command,
			"args":    db.args,
		}
	}
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func executeMCPConfigJSON(spec *dsl.OutputSpec, resultFile, schemaFile, dbConfigFile, defConfigFile string, mcps []dsl.MCPRuntime, readonlyDB bool) string {
	servers := map[string]any{}
	if spec != nil && spec.IsStructured() {
		output := outputMCPServer(resultFile, schemaFile, spec.SchemaFormat)
		servers[outputMCPName] = map[string]any{
			"type":    "stdio",
			"command": output.command,
			"args":    output.args,
		}
	}
	if dbConfigFile != "" {
		db := dbMCPServer(dbConfigFile, readonlyDB)
		servers[dbMCPName] = map[string]any{
			"type":    "stdio",
			"command": db.command,
			"args":    db.args,
		}
	}
	if defConfigFile != "" {
		defs := defsMCPServer(defConfigFile)
		servers[defsMCPName] = map[string]any{
			"type":    "stdio",
			"command": defs.command,
			"args":    defs.args,
		}
	}
	for _, item := range mcps {
		serverConfig, err := runtimeMCPConfigMap(item)
		if err != nil {
			continue
		}
		servers[item.Name] = serverConfig
	}
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func outputMCPConfigJSON(spec *dsl.OutputSpec, resultFile, schemaFile string) string {
	server := outputMCPServer(resultFile, schemaFile, spec.SchemaFormat)
	serverConfig := map[string]any{
		"type":    "stdio",
		"command": server.command,
		"args":    server.args,
	}
	config := map[string]any{
		"mcpServers": map[string]any{
			outputMCPName: serverConfig,
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func parseRuntimeMCP(item dsl.MCPRuntime) (mcpServerSpec, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(item.Config), &raw); err != nil {
		return mcpServerSpec{}, fmt.Errorf("parse mcp %q config: %w", item.Name, err)
	}
	if serversRaw, ok := raw["mcpServers"]; ok {
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(serversRaw, &servers); err != nil {
			return mcpServerSpec{}, err
		}
		serverRaw, ok := servers[item.Name]
		if !ok && len(servers) == 1 {
			for _, value := range servers {
				serverRaw = value
			}
			ok = true
		}
		if !ok {
			return mcpServerSpec{}, fmt.Errorf("mcp %q config has no matching mcpServers entry", item.Name)
		}
		return parseMCPServerConfig(item.Name, serverRaw)
	}
	return parseMCPServerConfig(item.Name, json.RawMessage(item.Config))
}

func parseMCPServerConfig(name string, data json.RawMessage) (mcpServerSpec, error) {
	var cfg struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Type    string            `json:"type"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return mcpServerSpec{}, err
	}
	if cfg.Command == "" {
		return mcpServerSpec{}, fmt.Errorf("mcp %q command is required", name)
	}
	return mcpServerSpec{command: cfg.Command, args: append([]string{}, cfg.Args...), env: cfg.Env}, nil
}

func runtimeMCPConfigMap(item dsl.MCPRuntime) (map[string]any, error) {
	server, err := parseRuntimeMCP(item)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"type":    "stdio",
		"command": server.command,
		"args":    server.args,
	}
	if len(server.env) > 0 {
		out["env"] = server.env
	}
	return out, nil
}

func validateRuntimeMCPs(items []dsl.MCPRuntime) error {
	for _, item := range items {
		if _, err := parseRuntimeMCP(item); err != nil {
			return err
		}
	}
	return nil
}

func checkMCPServerEnv() map[string]string {
	env := map[string]string{}
	if value := os.Getenv("ATM_MCP_CHECK_LOG"); value != "" {
		env["ATM_MCP_CHECK_LOG"] = value
	}
	return env
}

func tomlString(value string) string {
	return strconv.Quote(value)
}

func tomlStringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, tomlString(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}();&|<>") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
