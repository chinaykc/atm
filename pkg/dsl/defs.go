package dsl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DefinitionSet struct {
	Definitions map[string]Definition
	Imports     []ImportDecl
}

func LoadDefinitionSet(sourcePath, content string, opts CompileOptions) (DefinitionSet, error) {
	opts = normalizeCompileOptions(sourcePath, opts)
	loader := definitionLoader{
		defs:    make(map[string]Definition),
		visited: make(map[string]struct{}),
	}
	if err := loader.load(sourcePath, content, opts, ""); err != nil {
		return DefinitionSet{}, err
	}
	if err := detectDefinitionCycles(loader.defs); err != nil {
		return DefinitionSet{}, err
	}
	return DefinitionSet{Definitions: loader.defs, Imports: loader.imports}, nil
}

type definitionLoader struct {
	defs    map[string]Definition
	imports []ImportDecl
	visited map[string]struct{}
}

func (l *definitionLoader) load(sourcePath, content string, opts CompileOptions, namespace string) error {
	key := sourcePath + "\x00" + namespace
	if _, ok := l.visited[key]; ok {
		return nil
	}
	l.visited[key] = struct{}{}

	defs, imports, err := ParseLocalDefinitions(sourcePath, content, opts)
	if err != nil {
		return err
	}
	for _, def := range defs {
		name := def.Name
		if namespace != "" {
			name = namespace + "." + name
			def.Name = name
		}
		if _, exists := l.defs[name]; exists {
			return fmt.Errorf("definition %q already exists", name)
		}
		l.defs[name] = def
	}
	for _, decl := range imports {
		l.imports = append(l.imports, decl)
		path := decl.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(opts.Root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read import %q: %w", decl.Path, err)
		}
		childOpts := normalizeCompileOptions(path, CompileOptions{})
		if err := l.load(path, string(data), childOpts, decl.Namespace); err != nil {
			return err
		}
	}
	return nil
}

func ParseLocalDefinitions(sourcePath, content string, opts CompileOptions) ([]Definition, []ImportDecl, error) {
	opts = normalizeCompileOptions(sourcePath, opts)
	lines := SplitLines(content)
	var defs []Definition
	var imports []ImportDecl
	if hasSlashHeading(lines) {
		parsed, err := parseMarkdownDefinitions(sourcePath, content, lines)
		if err != nil {
			return nil, nil, err
		}
		defs = append(defs, parsed...)
	}
	blocks := ParseBlocks(content)
	for i, block := range blocks {
		def, ok, err := ParseLegacyDefinitionBlock(sourcePath, i, block.Body)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			defs = append(defs, def)
			continue
		}
		decls, ok, err := ParseGlobalImportBlock(block.Body)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			for _, decl := range decls {
				decl.BlockIndex = i
				imports = append(imports, decl)
			}
		}
	}
	return defs, imports, nil
}

func parseMarkdownDefinitions(sourcePath, content string, rawLines []string) ([]Definition, error) {
	lines := sourceLines(rawLines)
	var defs []Definition
	for i := 0; i < len(lines); {
		heading, ok := slashHeading(lines[i].text)
		if !ok || !isDefinitionHeadingText(heading.text) {
			i++
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			nextLevel, ok := markdownAnyHeading(lines[j].text)
			if ok && nextLevel <= heading.level {
				end = j
				break
			}
		}
		sectionStart := lines[i].end
		sectionEnd := len(content)
		if end < len(lines) {
			sectionEnd = lines[end].start
		}
		def, err := definitionFromHeader(sourcePath, len(defs), heading.text, content[sectionStart:sectionEnd], heading.list)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
		i = end
	}
	return defs, nil
}

func ParseLegacyDefinitionBlock(sourcePath string, index int, body string) (Definition, bool, error) {
	lines := SplitLines(body)
	if len(lines) == 0 {
		return Definition{}, false, nil
	}
	first := strings.TrimSpace(lines[0])
	if first != "/def" && !strings.HasPrefix(first, "/def ") {
		return Definition{}, false, nil
	}
	section := strings.Join(lines[1:], "")
	def, err := definitionFromHeader(sourcePath, index, first, section, false)
	return def, true, err
}

func definitionFromHeader(sourcePath string, index int, header, section string, list bool) (Definition, error) {
	fields := strings.Fields(header)
	if len(fields) < 2 {
		return Definition{}, fmt.Errorf("%s requires a name", fields[0])
	}
	name := fields[1]
	if !isDefinitionName(name) || strings.Contains(name, ".") {
		return Definition{}, fmt.Errorf("invalid definition name %q", name)
	}
	var params []string
	for _, param := range fields[2:] {
		if !isVariableName(param) {
			return Definition{}, fmt.Errorf("invalid definition parameter %q", param)
		}
		params = append(params, param)
	}
	var blocks []string
	if list {
		for _, block := range parseLegacyBlocks(SplitLines(section), "") {
			blocks = append(blocks, block.Body)
		}
	} else {
		body := filterSingleTaskSection(section)
		body, _ = splitTrailingBlankLines(body)
		if strings.TrimSpace(body) != "" {
			blocks = append(blocks, body)
		}
	}
	return Definition{Name: name, Params: params, Blocks: blocks, SourcePath: sourcePath, BlockIndex: index, List: list}, nil
}

func ParseGlobalImportBlock(body string) ([]ImportDecl, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return nil, false, err
	}
	lines := SplitLines(body)
	var imports []ImportDecl
	seen := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "/import" {
			return nil, true, fmt.Errorf("/import requires a path")
		}
		if !strings.HasPrefix(line, "/import ") {
			return nil, false, nil
		}
		fields := strings.Fields(line)
		switch len(fields) {
		case 2:
			imports = append(imports, ImportDecl{Path: fields[1]})
		case 4:
			if fields[2] != "from" {
				return nil, true, fmt.Errorf("/import namespace form is /import name from path")
			}
			if !isVariableName(fields[1]) {
				return nil, true, fmt.Errorf("invalid import namespace %q", fields[1])
			}
			imports = append(imports, ImportDecl{Namespace: fields[1], Path: fields[3]})
		default:
			return nil, true, fmt.Errorf("/import requires a path or namespace from path")
		}
		seen = true
	}
	return imports, seen, nil
}

func isDefinitionName(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, ".") {
		if !isVariableName(part) {
			return false
		}
	}
	return true
}

func detectDefinitionCycles(defs map[string]Definition) error {
	state := make(map[string]int)
	var stack []string
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			start := 0
			for i, item := range stack {
				if item == name {
					start = i
					break
				}
			}
			cycle := append(append([]string{}, stack[start:]...), name)
			return fmt.Errorf("recursive definition call: %s", strings.Join(cycle, " -> "))
		case 2:
			return nil
		}
		state[name] = 1
		stack = append(stack, name)
		for _, dep := range definitionCalls(defs[name]) {
			if _, ok := defs[dep]; !ok {
				return fmt.Errorf("definition %q calls unknown definition %q", name, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for name := range defs {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func definitionCalls(def Definition) []string {
	var calls []string
	for _, body := range def.Blocks {
		task, err := parseTask(def.BlockIndex, body, nil, normalizeCompileOptions(def.SourcePath, CompileOptions{}))
		if err != nil {
			continue
		}
		for _, op := range task.flow {
			if op.kind == astOpCall {
				calls = append(calls, op.Call.Name)
			}
		}
		for _, line := range SplitLines(task.prompt) {
			if call, ok := ParseInlineCallLine(line); ok {
				calls = append(calls, call.Name)
			}
		}
	}
	return calls
}

func ParseInlineCallLine(line string) (Call, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/call" {
		return Call{}, false
	}
	call, err := parseCallFields(fields, 0)
	if err != nil {
		return Call{}, false
	}
	return call, true
}
