package compiler

import "path/filepath"

type scopeRef struct {
	SourcePath string
	Scope      []string
	Line       int
}

func visibleGlobalVars(bindings []GlobalBinding, ref scopeRef) map[string]any {
	vars := make(map[string]any)
	for _, binding := range bindings {
		if !globalBindingVisibleAt(binding, ref) {
			continue
		}
		if binding.BashScript != "" {
			vars[binding.Name] = "{{" + binding.Name + "}}"
			continue
		}
		vars[binding.Name] = binding.Value
	}
	return vars
}

func globalBindingVisibleAt(binding GlobalBinding, ref scopeRef) bool {
	if binding.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(binding.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && binding.Line > 0 && binding.Line >= ref.Line {
		return false
	}
	if len(binding.Scope) > len(ref.Scope) {
		return false
	}
	for i := range binding.Scope {
		if binding.Scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}
