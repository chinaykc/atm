package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type DefinitionSet struct {
	Definitions map[string]Definition
	Items       []Definition
	Imports     []ImportDecl
}

func LoadDefinitionSet(sourcePath, content string, opts CompileOptions) (DefinitionSet, error) {
	opts = normalizeCompileOptions(sourcePath, opts)
	loader := definitionLoader{
		defs:    make(map[string]Definition),
		visited: make(map[string]struct{}),
		active:  make(map[string]int),
	}
	if err := loader.load(sourcePath, content, opts, "", nil); err != nil {
		return DefinitionSet{}, err
	}
	if err := detectDefinitionCycles(loader.defs); err != nil {
		return DefinitionSet{}, err
	}
	if err := validateDefinitionReturnSyntax(loader.defs); err != nil {
		return DefinitionSet{}, err
	}
	return DefinitionSet{Definitions: loader.defs, Items: loader.items, Imports: loader.imports}, nil
}

type definitionLoader struct {
	defs    map[string]Definition
	items   []Definition
	imports []ImportDecl
	visited map[string]struct{}
	active  map[string]int
	stack   []string
}

type definitionVisibility struct {
	SourcePath string
	Scope      []string
	Line       int
}

func (l *definitionLoader) load(sourcePath, content string, opts CompileOptions, namespace string, visibility *definitionVisibility) error {
	pathID := definitionImportIdentity(sourcePath)
	if start, ok := l.active[pathID]; ok {
		cycle := slices.Concat(l.stack[start:], []string{pathID})
		return fmt.Errorf("recursive import: %s", strings.Join(cycle, " -> "))
	}
	key := sourcePath + "\x00" + namespace
	if visibility != nil {
		key += "\x00" + visibility.SourcePath + "\x00" + strings.Join(visibility.Scope, "\x1f") + "\x00" + fmt.Sprint(visibility.Line)
	}
	if _, ok := l.visited[key]; ok {
		return nil
	}
	l.visited[key] = struct{}{}
	l.active[pathID] = len(l.stack)
	l.stack = append(l.stack, pathID)
	defer func() {
		l.stack = l.stack[:len(l.stack)-1]
		delete(l.active, pathID)
	}()

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
		if visibility != nil {
			def.VisibleSourcePath = visibility.SourcePath
			def.VisibleScope = slices.Clone(visibility.Scope)
			def.VisibleLine = visibility.Line
		} else {
			def.VisibleSourcePath = def.SourcePath
			def.VisibleScope = slices.Clone(def.Scope)
			def.VisibleLine = def.Line
		}
		if _, exists := l.defs[name]; exists {
			return fmt.Errorf("definition %q already exists", name)
		}
		l.defs[name] = def
		l.items = append(l.items, def)
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
		declVisibility := definitionVisibility{SourcePath: decl.SourcePath, Scope: decl.Scope, Line: decl.Line}
		if err := l.load(path, string(data), childOpts, decl.Namespace, &declVisibility); err != nil {
			return err
		}
	}
	return nil
}

func definitionImportIdentity(path string) string {
	clean := filepath.Clean(path)
	if abs, err := filepath.Abs(clean); err == nil {
		return abs
	}
	return clean
}

func ParseLocalDefinitions(sourcePath, content string, opts CompileOptions) ([]Definition, []ImportDecl, error) {
	opts = normalizeCompileOptions(sourcePath, opts)
	var defs []Definition
	var imports []ImportDecl
	v2Defs, err := parseV2DefinitionBlocks(sourcePath, content)
	if err != nil {
		return nil, nil, err
	}
	defs = append(defs, v2Defs...)
	blocks := ParseBlocks(content)
	for i, block := range blocks {
		decls, ok, err := ParseGlobalImportBlock(block.Body)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			for _, decl := range decls {
				decl.BlockIndex = i
				decl.SourcePath = sourcePath
				decl.Scope = slices.Clone(block.Scope)
				decl.Line = block.StartLine
				imports = append(imports, decl)
			}
		}
	}
	return defs, imports, nil
}

func parseV2DefinitionBlocks(sourcePath, content string) ([]Definition, error) {
	lines := SplitLines(content)
	offsets := lineOffsets(lines)
	var defs []Definition
	var headings []markdownHeadingSection
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if level, text, ok := parseMarkdownHeading(lines[i]); ok {
			headings = updateMarkdownHeadingStack(headings, markdownHeadingSection{level: level, text: text})
			continue
		}
		if !isDefCommandLine(line) {
			continue
		}
		end, err := findDefinitionEnd(lines, i+1)
		if err != nil {
			return nil, err
		}
		section := content[offsets[i+1]:offsets[end]]
		base := SourceSpan{Line: i + 2, Column: 1}
		def, err := definitionFromHeader(sourcePath, len(defs), line, section, true, base)
		if err != nil {
			return nil, err
		}
		def.Scope = markdownScopePath(headings)
		def.Line = i + 1
		def.VisibleSourcePath = def.SourcePath
		def.VisibleScope = slices.Clone(def.Scope)
		def.VisibleLine = def.Line
		defs = append(defs, def)
		i = end - 1
	}
	return defs, nil
}

func isDefCommandLine(line string) bool {
	fields := strings.Fields(line)
	return len(fields) >= 1 && fields[0] == "/def"
}

func findDefinitionEnd(lines []string, start int) (int, error) {
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		fields := strings.Fields(trimmed)
		if len(fields) == 0 || fields[0] != "/return" {
			continue
		}
		spec, next, _, err := parseReturnAt(lines, i, trimmed)
		if err != nil {
			return next, err
		}
		if spec != nil && spec.Kind == ReturnStructured {
			next = findStructuredReturnDefinitionEnd(lines, next)
		}
		return next, nil
	}
	return len(lines), fmt.Errorf("/def requires /return")
}

func findStructuredReturnDefinitionEnd(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			if i+1 >= len(lines) {
				return i
			}
			next := strings.TrimSpace(lines[i+1])
			if isDefCommandLine(next) || isMarkdownHeadingLine(next) || isMarkdownV2BlockStartLine(next) {
				return i
			}
			continue
		}
		if isDefCommandLine(trimmed) {
			return i
		}
	}
	return len(lines)
}

func isMarkdownHeadingLine(line string) bool {
	_, _, ok := parseMarkdownHeading(line)
	return ok
}

func definitionFromHeader(sourcePath string, index int, header, section string, list bool, base SourceSpan) (Definition, error) {
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
	blocks := parseDefinitionBlocks(section, list, base)
	return Definition{Name: name, Params: params, Blocks: blocks, SourcePath: sourcePath, BlockIndex: index, List: list}, nil
}

func parseDefinitionBlocks(section string, list bool, base SourceSpan) []DefinitionBlock {
	if list {
		return parseV2DefinitionTaskBlocks(section, base)
	}
	body := filterSingleTaskSection(section)
	body, _ = splitTrailingBlankLines(body)
	if strings.TrimSpace(body) == "" {
		return nil
	}
	span := SourceSpan{}
	if base.Line > 0 {
		span = shiftSourceSpan(base, sourceSpanAtOffset(section, firstSectionBodyOffset(section)))
		span.Block = 1
	}
	return []DefinitionBlock{{Body: body, Span: span}}
}

func parseV2DefinitionTaskBlocks(section string, base SourceSpan) []DefinitionBlock {
	var blocks []DefinitionBlock
	lines := SplitLines(section)
	offsets := lineOffsets(lines)
	var body strings.Builder
	blockStartOffset := -1
	seenPrompt := false
	heredocDelim := ""
	outputFence := outputFenceInfo{}
	outputFencePending := false

	flush := func(nextOffset int) {
		if strings.TrimSpace(body.String()) == "" {
			body.Reset()
			blockStartOffset = -1
			seenPrompt = false
			return
		}
		blockBody := body.String()
		blockBody, _ = splitTrailingBlankLines(blockBody)
		if strings.TrimSpace(blockBody) == "" {
			body.Reset()
			blockStartOffset = -1
			seenPrompt = false
			return
		}
		span := SourceSpan{}
		if base.Line > 0 && blockStartOffset >= 0 {
			span = shiftSourceSpan(base, sourceSpanAtOffset(section, blockStartOffset))
			span.Block = len(blocks) + 1
		}
		blocks = append(blocks, DefinitionBlock{Body: blockBody, Span: span})
		body.Reset()
		blockStartOffset = nextOffset
		seenPrompt = false
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if blockStartOffset < 0 && IsBlankLine(line) {
			continue
		}
		if blockStartOffset < 0 && !IsBlankLine(line) {
			blockStartOffset = offsets[i]
		}
		if outputFence.marker != "" {
			body.WriteString(line)
			if isFenceClose(line, outputFence) {
				outputFence = outputFenceInfo{}
			}
			continue
		}
		if outputFencePending {
			body.WriteString(line)
			if fence, ok := parseAnyFenceStart(line); ok {
				outputFence = fence
			}
			outputFencePending = false
			continue
		}
		if heredocDelim != "" {
			body.WriteString(line)
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if seenPrompt && isDefinitionTaskBoundary(trimmed) {
			flush(offsets[i])
			if blockStartOffset < 0 {
				blockStartOffset = offsets[i]
			}
		} else if !seenPrompt && isDefinitionStandaloneBoundary(body.String(), trimmed) {
			flush(offsets[i])
			if blockStartOffset < 0 {
				blockStartOffset = offsets[i]
			}
		}
		body.WriteString(line)
		if startsFencedPayloadCommand(trimmed) {
			outputFencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
		if !IsBlankLine(line) && !isDefinitionHeaderCommandLine(trimmed) {
			seenPrompt = true
		}
	}
	flush(len(section))
	return blocks
}

func isDefinitionStandaloneBoundary(body, line string) bool {
	if strings.TrimSpace(body) == "" || strings.TrimSpace(line) == "" {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(body))
	if len(fields) == 0 || fields[0] != "/pool" {
		return false
	}
	lineFields := strings.Fields(line)
	return len(lineFields) == 0 || lineFields[0] != "/pool"
}

func isDefinitionTaskBoundary(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] == "/return" {
		return false
	}
	return isDefinitionHeaderCommandLine(line)
}

func isDefinitionHeaderCommandLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	token := fields[0]
	if token == "/return" || isCommandToken(token) || isIfCommandToken(token) || token == "/else" {
		return true
	}
	if strings.HasPrefix(token, "/") {
		name := strings.TrimPrefix(token, "/")
		return len(fields) == 1 && isVariableName(name)
	}
	return false
}

func firstSectionBodyOffset(section string) int {
	offset := 0
	inHTMLComment := false
	for _, line := range SplitLines(section) {
		trimmed := strings.TrimSpace(line)
		if inHTMLComment {
			offset += len(line)
			if isHTMLCommentEndLine(trimmed) {
				inHTMLComment = false
			}
			continue
		}
		if isHTMLCommentStartLine(trimmed) {
			offset += len(line)
			if !isHTMLCommentEndLine(trimmed) {
				inHTMLComment = true
			}
			continue
		}
		if IsBlankLine(line) || (IsIgnoredLine(line) && !isPreservedPromptHeading(line)) {
			offset += len(line)
			continue
		}
		return offset
	}
	return offset
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
			cycle := slices.Concat(stack[start:], []string{name})
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

func validateDefinitionReturnSyntax(defs map[string]Definition) error {
	for _, def := range defs {
		count := 0
		returnBlock := -1
		for i, block := range def.Blocks {
			if !blockHasReturnCommand(block.Body) {
				continue
			}
			count++
			returnBlock = i
		}
		if count == 0 {
			return fmt.Errorf("definition %s requires /return", def.Name)
		}
		if count > 1 {
			return fmt.Errorf("definition %s: /return can only appear once", def.Name)
		}
		if returnBlock != len(def.Blocks)-1 {
			return fmt.Errorf("definition %s: /return must be the final definition block", def.Name)
		}
	}
	return nil
}

func definitionCalls(def Definition) []string {
	var calls []string
	for _, block := range def.Blocks {
		task, err := parseTask(def.BlockIndex, block.Body, nil, normalizeCompileOptions(def.SourcePath, CompileOptions{}))
		if err != nil {
			continue
		}
		for _, op := range task.flow {
			if op.kind == astOpCall {
				calls = append(calls, op.Call.Name)
			}
		}
	}
	return calls
}

func DefinitionCalls(def Definition) []string {
	return definitionCalls(def)
}
