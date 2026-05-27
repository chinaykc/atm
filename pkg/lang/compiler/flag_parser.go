package compiler

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

var supportedFlagTypes = map[string]struct{}{
	"string":   {},
	"int":      {},
	"number":   {},
	"bool":     {},
	"[]string": {},
	"[]int":    {},
	"[]number": {},
}

func ParseGlobalFlagBlock(body string) ([]FlagDecl, bool, error) {
	lines := SplitLines(body)
	var decls []FlagDecl
	seen := map[string]struct{}{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if trimmed != "/flag" && !strings.HasPrefix(trimmed, "/flag ") {
			return nil, false, nil
		}
		decl, err := parseFlagLine(trimmed)
		if err != nil {
			return nil, true, err
		}
		if _, ok := seen[decl.Name]; ok {
			return nil, true, fmt.Errorf("duplicate flag %q", decl.Name)
		}
		seen[decl.Name] = struct{}{}
		decls = append(decls, decl)
	}
	if len(decls) == 0 {
		return nil, false, nil
	}
	return decls, true, nil
}

func parseFlagLine(line string) (FlagDecl, error) {
	fields, err := commandFields(line)
	if err != nil {
		return FlagDecl{}, err
	}
	if len(fields) < 4 || fields[0] != "/flag" {
		return FlagDecl{}, fmt.Errorf("/flag requires type, name, and description")
	}
	typ := fields[1]
	if _, ok := supportedFlagTypes[typ]; !ok {
		return FlagDecl{}, fmt.Errorf("unsupported flag type %q", typ)
	}
	name := fields[2]
	if !isVariableName(name) {
		return FlagDecl{}, fmt.Errorf("invalid flag name %q", name)
	}
	descFields := slices.Clone(fields[3:])
	decl := FlagDecl{Type: typ, Name: name}
	if len(descFields) > 0 {
		last := descFields[len(descFields)-1]
		if strings.HasPrefix(last, "default:") {
			decl.HasDefault = true
			decl.Default = strings.TrimPrefix(last, "default:")
			descFields = descFields[:len(descFields)-1]
		}
	}
	if len(descFields) == 0 {
		return FlagDecl{}, fmt.Errorf("/flag %s requires a description", name)
	}
	decl.Description = strings.Join(descFields, " ")
	if decl.HasDefault {
		if _, err := CoerceFlagValue(decl, []string{decl.Default}); err != nil {
			return FlagDecl{}, fmt.Errorf("default for flag %s: %w", name, err)
		}
	}
	return decl, nil
}

func ParseFlagDecls(sourcePath, content string) ([]FlagDecl, error) {
	seen := map[string]int{}
	var out []FlagDecl
	for i, line := range SplitLines(content) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "/flag" && !strings.HasPrefix(trimmed, "/flag ") {
			continue
		}
		decl, err := parseFlagLine(trimmed)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if first, exists := seen[decl.Name]; exists {
			return nil, fmt.Errorf("duplicate flag %q also appears on line %d", decl.Name, first+1)
		}
		decl.BlockIndex = -1
		decl.SourcePath = sourcePath
		decl.Line = i + 1
		seen[decl.Name] = i
		out = append(out, decl)
	}
	return out, nil
}

func CoerceFlagValues(flags []FlagDecl, raw map[string][]string) (map[string]any, error) {
	vars := make(map[string]any, len(flags))
	seen := map[string]struct{}{}
	for _, flag := range flags {
		if _, ok := seen[flag.Name]; ok {
			return nil, fmt.Errorf("duplicate flag %q", flag.Name)
		}
		seen[flag.Name] = struct{}{}
		values, ok := raw[flag.Name]
		if !ok || len(values) == 0 {
			if flag.HasDefault {
				values = []string{flag.Default}
			} else if flag.Type == "bool" {
				values = []string{"false"}
			} else {
				return nil, fmt.Errorf("missing required flag %q", flag.Name)
			}
		}
		value, err := CoerceFlagValue(flag, values)
		if err != nil {
			return nil, fmt.Errorf("flag %s: %w", flag.Name, err)
		}
		vars[flag.Name] = value
	}
	for name := range raw {
		if _, ok := seen[name]; !ok {
			return nil, fmt.Errorf("unknown flag %q", name)
		}
	}
	return vars, nil
}

func CoerceFlagValue(flag FlagDecl, values []string) (any, error) {
	switch flag.Type {
	case "string":
		if len(values) != 1 {
			return nil, fmt.Errorf("expects one string")
		}
		return values[0], nil
	case "int":
		if len(values) != 1 {
			return nil, fmt.Errorf("expects one int")
		}
		v, err := strconv.Atoi(values[0])
		if err != nil {
			return nil, err
		}
		return v, nil
	case "number":
		if len(values) != 1 {
			return nil, fmt.Errorf("expects one number")
		}
		v, err := strconv.ParseFloat(values[0], 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "bool":
		if len(values) != 1 {
			return nil, fmt.Errorf("expects one bool")
		}
		v, err := strconv.ParseBool(values[0])
		if err != nil {
			return nil, err
		}
		return v, nil
	case "[]string":
		return splitFlagValues(values), nil
	case "[]int":
		var out []int
		for _, item := range splitFlagValues(values) {
			v, err := strconv.Atoi(item)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case "[]number":
		var out []float64
		for _, item := range splitFlagValues(values) {
			v, err := strconv.ParseFloat(item, 64)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported flag type %q", flag.Type)
	}
}

func splitFlagValues(values []string) []string {
	var out []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
	}
	return out
}

func MergeVars(base map[string]any, overlay map[string]any) map[string]any {
	out := CloneVars(base)
	maps.Copy(out, overlay)
	return out
}
