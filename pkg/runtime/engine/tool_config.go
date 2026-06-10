package engine

import (
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (e *Engine) taskSkills(task compiler.Task) ([]compiler.SkillRuntime, error) {
	config := task.Skill
	if config.IgnoreAll {
		return nil, nil
	}
	ref := definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	ignored := stringSet(config.Ignore)
	var out []compiler.SkillRuntime
	seen := map[string]struct{}{}
	for _, item := range config.Use {
		runtime, err := e.resolveSkill(item, ref)
		if err != nil {
			return nil, err
		}
		if _, skip := ignored[runtime.Name]; skip {
			continue
		}
		if _, exists := seen[runtime.Name]; exists {
			continue
		}
		seen[runtime.Name] = struct{}{}
		out = append(out, runtime)
	}
	return out, nil
}

func (e *Engine) resolveSkill(item string, ref definitionScopeRef) (compiler.SkillRuntime, error) {
	if item == "" {
		return compiler.SkillRuntime{}, fmt.Errorf("skill name or path is required")
	}
	if decl, ok := e.resolveVisibleSkill(item, ref); ok {
		path := decl.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(e.root, path)
		}
		return validateSkillRuntime(compiler.SkillRuntime{Name: decl.Name, Path: filepath.Clean(path)})
	}
	path := item
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.root, path)
	}
	name := filepath.Base(filepath.Clean(path))
	if !isPortableName(name) {
		return compiler.SkillRuntime{}, fmt.Errorf("invalid skill name %q derived from %q", name, item)
	}
	return validateSkillRuntime(compiler.SkillRuntime{Name: name, Path: filepath.Clean(path)})
}

func validateSkillRuntime(runtime compiler.SkillRuntime) (compiler.SkillRuntime, error) {
	info, err := os.Stat(runtime.Path)
	if err != nil {
		return compiler.SkillRuntime{}, fmt.Errorf("skill %q path %q is not available: %w", runtime.Name, runtime.Path, err)
	}
	if !info.IsDir() {
		return compiler.SkillRuntime{}, fmt.Errorf("skill %q path %q is not a directory", runtime.Name, runtime.Path)
	}
	if _, err := os.Stat(filepath.Join(runtime.Path, "SKILL.md")); err != nil {
		return compiler.SkillRuntime{}, fmt.Errorf("skill %q path %q must contain SKILL.md: %w", runtime.Name, runtime.Path, err)
	}
	return runtime, nil
}

func (e *Engine) taskMCPs(task compiler.Task) ([]compiler.MCPRuntime, error) {
	config := task.MCP
	if config.IgnoreAll {
		return nil, nil
	}
	ref := definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	ignored := stringSet(config.Ignore)
	var out []compiler.MCPRuntime
	seen := map[string]struct{}{}
	for _, name := range config.Use {
		if _, skip := ignored[name]; skip {
			continue
		}
		if isBuiltinMCPName(name) {
			return nil, fmt.Errorf("mcp name %q conflicts with an ATM builtin MCP server", name)
		}
		decl, ok := e.resolveVisibleMCP(name, ref)
		if !ok {
			return nil, fmt.Errorf("unknown mcp %q", name)
		}
		if !json.Valid([]byte(decl.Config)) {
			return nil, fmt.Errorf("mcp %q config must be valid JSON", name)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, compiler.MCPRuntime{Name: name, Config: decl.Config})
	}
	return out, nil
}

func (e *Engine) resolveVisibleSkill(name string, ref definitionScopeRef) (compiler.SkillDecl, bool) {
	var best compiler.SkillDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, skill := range e.skillItems {
		if skill.Name != name || !runtimeSkillVisibleAt(skill, ref) {
			continue
		}
		depth := len(skill.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && skill.Line > bestLine) {
			best = skill
			found = true
			bestDepth = depth
			bestLine = skill.Line
		}
	}
	return best, found
}

func runtimeSkillVisibleAt(skill compiler.SkillDecl, ref definitionScopeRef) bool {
	if skill.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(skill.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && skill.Line > 0 && skill.Line >= ref.Line {
		return false
	}
	if len(skill.Scope) > len(ref.Scope) {
		return false
	}
	for i := range skill.Scope {
		if skill.Scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func (e *Engine) resolveVisibleMCP(name string, ref definitionScopeRef) (compiler.MCPDecl, bool) {
	var best compiler.MCPDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, mcp := range e.mcpItems {
		if mcp.Name != name || !runtimeMCPVisibleAt(mcp, ref) {
			continue
		}
		depth := len(mcp.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && mcp.Line > bestLine) {
			best = mcp
			found = true
			bestDepth = depth
			bestLine = mcp.Line
		}
	}
	return best, found
}

func runtimeMCPVisibleAt(mcp compiler.MCPDecl, ref definitionScopeRef) bool {
	if mcp.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(mcp.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && mcp.Line > 0 && mcp.Line >= ref.Line {
		return false
	}
	if len(mcp.Scope) > len(ref.Scope) {
		return false
	}
	for i := range mcp.Scope {
		if mcp.Scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func (e *Engine) taskDefMCP(task compiler.Task, dbs []compiler.DBRuntime, skills []compiler.SkillRuntime, mcps []compiler.MCPRuntime) (*compiler.DefMCPRuntime, error) {
	config := task.MCP
	if config.IgnoreAll || len(config.DefUse) == 0 {
		return nil, nil
	}
	ref := definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	seen := map[string]struct{}{}
	var names []string
	var refs []compiler.DefinitionRef
	for _, name := range config.DefUse {
		if _, exists := seen[name]; exists {
			continue
		}
		def, ok := e.resolveDefinition(name, ref)
		if !ok {
			return nil, fmt.Errorf("unknown definition %q", name)
		}
		seen[name] = struct{}{}
		names = append(names, name)
		refs = append(refs, compiler.DefinitionRef{Name: name, Params: slices.Clone(def.Params)})
	}
	slices.Sort(names)
	tool := e.toolName
	if strings.TrimSpace(tool) == "" && e.runner != nil {
		tool = e.runner.Name()
	}
	return &compiler.DefMCPRuntime{
		TodoPath:    e.filePath,
		Definitions: names,
		Tool:        tool,
		CodexPath:   e.codexPath,
		ClaudePath:  e.claudePath,
		Danger:      e.danger,
		DBs:         slices.Clone(dbs),
		Skills:      slices.Clone(skills),
		MCPs:        slices.Clone(mcps),
		OutputDir:   e.outputs.dirPath(),
		Messages:    e.messages,
		Depth:       1,
		Defs:        refs,
	}, nil
}

func stringSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func isBuiltinMCPName(name string) bool {
	switch name {
	case "atm_check", "atm_output", "atm_db", "atm_defs", "atm_webhook":
		return true
	default:
		return false
	}
}

func isPortableName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
