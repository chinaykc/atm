package engine

import (
	"atm/pkg/dsl"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (e *Engine) taskSkills(config dsl.SkillTaskConfig) ([]dsl.SkillRuntime, error) {
	if config.IgnoreAll {
		return nil, nil
	}
	ignored := stringSet(config.Ignore)
	var out []dsl.SkillRuntime
	seen := map[string]struct{}{}
	for _, item := range config.Use {
		runtime, err := e.resolveSkill(item)
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

func (e *Engine) resolveSkill(item string) (dsl.SkillRuntime, error) {
	if item == "" {
		return dsl.SkillRuntime{}, fmt.Errorf("skill name or path is required")
	}
	if decl, ok := e.skills[item]; ok {
		path := decl.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(e.root, path)
		}
		return validateSkillRuntime(dsl.SkillRuntime{Name: decl.Name, Path: filepath.Clean(path)})
	}
	path := item
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.root, path)
	}
	name := filepath.Base(filepath.Clean(path))
	if !isPortableName(name) {
		return dsl.SkillRuntime{}, fmt.Errorf("invalid skill name %q derived from %q", name, item)
	}
	return validateSkillRuntime(dsl.SkillRuntime{Name: name, Path: filepath.Clean(path)})
}

func validateSkillRuntime(runtime dsl.SkillRuntime) (dsl.SkillRuntime, error) {
	info, err := os.Stat(runtime.Path)
	if err != nil {
		return dsl.SkillRuntime{}, fmt.Errorf("skill %q path %q is not available: %w", runtime.Name, runtime.Path, err)
	}
	if !info.IsDir() {
		return dsl.SkillRuntime{}, fmt.Errorf("skill %q path %q is not a directory", runtime.Name, runtime.Path)
	}
	if _, err := os.Stat(filepath.Join(runtime.Path, "SKILL.md")); err != nil {
		return dsl.SkillRuntime{}, fmt.Errorf("skill %q path %q must contain SKILL.md: %w", runtime.Name, runtime.Path, err)
	}
	return runtime, nil
}

func (e *Engine) taskMCPs(config dsl.MCPTaskConfig) ([]dsl.MCPRuntime, error) {
	if config.IgnoreAll {
		return nil, nil
	}
	ignored := stringSet(config.Ignore)
	var out []dsl.MCPRuntime
	seen := map[string]struct{}{}
	for _, name := range config.Use {
		if _, skip := ignored[name]; skip {
			continue
		}
		if isBuiltinMCPName(name) {
			return nil, fmt.Errorf("mcp name %q conflicts with an ATM builtin MCP server", name)
		}
		decl, ok := e.mcps[name]
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
		out = append(out, dsl.MCPRuntime{Name: name, Config: decl.Config})
	}
	return out, nil
}

func (e *Engine) taskDefMCP(config dsl.MCPTaskConfig, dbs []dsl.DBRuntime, skills []dsl.SkillRuntime, mcps []dsl.MCPRuntime) (*dsl.DefMCPRuntime, error) {
	if config.IgnoreAll || len(config.DefUse) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var names []string
	var refs []dsl.DefinitionRef
	for _, name := range config.DefUse {
		if _, exists := seen[name]; exists {
			continue
		}
		def, ok := e.defs[name]
		if !ok {
			return nil, fmt.Errorf("unknown definition %q", name)
		}
		seen[name] = struct{}{}
		names = append(names, name)
		refs = append(refs, dsl.DefinitionRef{Name: name, Params: append([]string{}, def.Params...)})
	}
	sort.Strings(names)
	tool := e.toolName
	if strings.TrimSpace(tool) == "" && e.runner != nil {
		tool = e.runner.Name()
	}
	return &dsl.DefMCPRuntime{
		TodoPath:    e.filePath,
		Definitions: names,
		Tool:        tool,
		CodexPath:   e.codexPath,
		ClaudePath:  e.claudePath,
		DBs:         append([]dsl.DBRuntime{}, dbs...),
		Skills:      append([]dsl.SkillRuntime{}, skills...),
		MCPs:        append([]dsl.MCPRuntime{}, mcps...),
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
	case "atm_check", "atm_output", "atm_db", "atm_defs":
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
