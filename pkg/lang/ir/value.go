package ir

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

func CloneVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	maps.Copy(out, in)
	return out
}

func CloneStringMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func StringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		data, err := json.Marshal(v)
		if err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}

func MergeRunOptions(base, override RunOptions) RunOptions {
	output := cloneOutputSpec(base.Output)
	if override.Output != nil {
		output = cloneOutputSpec(override.Output)
	}
	workdir := base.Workdir
	if override.Workdir != "" {
		workdir = override.Workdir
	}
	return RunOptions{
		Resume:   base.Resume || override.Resume,
		Args:     slices.Concat(base.Args, override.Args),
		Output:   output,
		DBs:      slices.Concat(base.DBs, override.DBs),
		Workdir:  workdir,
		Skills:   slices.Concat(base.Skills, override.Skills),
		MCPs:     slices.Concat(base.MCPs, override.MCPs),
		DefMCP:   cloneMergedDefMCPRuntime(base.DefMCP, override.DefMCP),
		DefDepth: max(base.DefDepth, override.DefDepth),
	}
}

func cloneMergedDefMCPRuntime(base, override *DefMCPRuntime) *DefMCPRuntime {
	if override != nil {
		return CloneDefMCPRuntime(override)
	}
	return CloneDefMCPRuntime(base)
}

func CloneDefMCPRuntime(in *DefMCPRuntime) *DefMCPRuntime {
	if in == nil {
		return nil
	}
	out := *in
	out.Definitions = slices.Clone(in.Definitions)
	out.DBs = slices.Clone(in.DBs)
	out.Skills = slices.Clone(in.Skills)
	out.MCPs = slices.Clone(in.MCPs)
	if in.Vars != nil {
		out.Vars = CloneVars(in.Vars)
	}
	out.Defs = slices.Clone(in.Defs)
	return &out
}

func cloneOutputSpec(spec *OutputSpec) *OutputSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	return &out
}
