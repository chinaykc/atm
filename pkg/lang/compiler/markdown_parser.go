package compiler

import (
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strings"

	"github.com/yuin/goldmark"
	mdast "github.com/yuin/goldmark/ast"
	mdtext "github.com/yuin/goldmark/text"
)

type markdownHeadingSection struct {
	level      int
	slash      bool
	list       bool
	text       string
	private    bool
	start, end int
}

type MarkdownHeading struct {
	Level int
	Text  string
	Start int
	End   int
}

func parseMarkdownTaskBlocks(content string, _ []string) []Block {
	lines := SplitLines(content)
	offsets := lineOffsets(lines)
	contexts := markdownSectionContextMap(lines)
	var blocks []Block
	var headings []markdownHeadingSection
	var sectionContext strings.Builder
	prefixStart := 0
	blockStart := -1
	blockContext := ""
	pendingInheritedPrompt := ""
	pendingInheritedLevel := 0
	pendingParentIndex := -1
	activeParentRootPrompt := ""
	activeParentLevel := 0
	activeParentIndex := -1
	blockNestedHeading := false
	blockScopeLevel := 0
	blockHasParent := false
	blockParentIndex := -1
	skipHeadingLevel := 0
	heredocDelim := ""
	fence := outputFenceInfo{}
	fencePending := false

	finalize := func(endLine int) {
		if blockStart < 0 {
			return
		}
		start := offsets[blockStart]
		end := offsets[endLine]
		body, sep := splitTrailingBlankLines(content[start:end])
		body, explicitContext := extractMarkdownContextRefs(body, contexts)
		if strings.TrimSpace(body) != "" {
			context := strings.TrimSpace(blockContext)
			if strings.TrimSpace(explicitContext) != "" {
				if context != "" {
					context += "\n\n"
				}
				context += strings.TrimSpace(explicitContext)
			}
			blocks = append(blocks, Block{
				Prefix:      content[prefixStart:start],
				Body:        body,
				Sep:         sep,
				Context:     context,
				Scope:       markdownScopePath(headings),
				StartLine:   blockStart + 1,
				HasParent:   blockHasParent,
				ParentIndex: blockParentIndex,
			})
		}
		prefixStart = end
		blockStart = -1
		blockContext = ""
		blockScopeLevel = 0
		blockHasParent = false
		blockParentIndex = -1
		blockNestedHeading = false
		heredocDelim = ""
		fence = outputFenceInfo{}
		fencePending = false
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if blockStart >= 0 {
			if fence.marker != "" {
				if isFenceClose(line, fence) {
					fence = outputFenceInfo{}
				}
				i++
				continue
			}
			if fencePending {
				if parsed, ok := parseAnyFenceStart(line); ok {
					fence = parsed
				}
				fencePending = false
				i++
				continue
			}
		}
		if level, text, ok := parseMarkdownHeading(line); ok {
			if skipHeadingLevel > 0 && level <= skipHeadingLevel {
				skipHeadingLevel = 0
			}
			if blockStart >= 0 {
				if isCompleteMarkdownGlobalDeclaration(content[offsets[blockStart]:offsets[i]]) {
					finalize(i)
					continue
				}
				currentLevel := blockScopeLevel
				if currentLevel == 0 && len(headings) > 0 {
					currentLevel = headings[len(headings)-1].level
				}
				if level <= currentLevel {
					finalize(i)
					continue
				}
				blockNestedHeading = true
			}
			if blockStart < 0 {
				if activeParentLevel > 0 && level <= activeParentLevel {
					activeParentRootPrompt = ""
					activeParentLevel = 0
					activeParentIndex = -1
				}
				headings = updateMarkdownHeadingStack(headings, markdownHeadingSection{level: level, text: text})
				sectionContext.Reset()
			} else {
				blockNestedHeading = true
			}
			i++
			continue
		}
		if blockStart < 0 {
			if skipHeadingLevel > 0 {
				i++
				continue
			}
			if isDefCommandLine(trimmed) {
				end, err := findDefinitionEnd(lines, i+1)
				if err != nil {
					end = len(lines)
				}
				i = end
				continue
			}
			if next, ok := parseDocBlock(lines, i); ok {
				i = next
				continue
			}
			if isMarkdownV2SingleLineGlobal(trimmed) {
				start := offsets[i]
				end := offsets[i+1]
				blocks = append(blocks, Block{Prefix: content[prefixStart:start], Body: content[start:end]})
				blocks[len(blocks)-1].Scope = markdownScopePath(headings)
				blocks[len(blocks)-1].StartLine = i + 1
				prefixStart = end
				i++
				continue
			}
			if isMarkdownV2BlockStartLine(trimmed) {
				blockStart = i
				blockScopeLevel = currentMarkdownLevel(headings)
				blockContext = markdownContext(headings, sectionContext.String())
				if strings.TrimSpace(pendingInheritedPrompt) != "" {
					if strings.TrimSpace(blockContext) != "" {
						blockContext = strings.TrimSpace(blockContext) + "\n\n"
					}
					blockContext += strings.TrimSpace(pendingInheritedPrompt) + "\n"
					pendingInheritedPrompt = ""
					if pendingInheritedLevel > 0 {
						blockScopeLevel = pendingInheritedLevel
					}
					pendingInheritedLevel = 0
					if pendingParentIndex >= 0 {
						blockHasParent = true
						blockParentIndex = pendingParentIndex
					}
					pendingParentIndex = -1
				} else if strings.TrimSpace(activeParentRootPrompt) != "" && activeParentApplies(headings, activeParentLevel) {
					blockContext = markdownContextWithParentRoot(headings, sectionContext.String(), activeParentRootPrompt, activeParentLevel)
					if activeParentIndex >= 0 {
						blockHasParent = true
						blockParentIndex = activeParentIndex
					}
				}
				i++
				continue
			}
			if !IsIgnoredLine(line) {
				sectionContext.WriteString(line)
			}
			i++
			continue
		}
		if heredocDelim != "" {
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			i++
			continue
		}
		if previousMarkdownLineBlank(lines, i) && !isMarkdownV2BlockStartLine(trimmed) && isCompleteMarkdownGlobalDeclaration(content[offsets[blockStart]:offsets[i]]) {
			finalize(i)
			continue
		}
		if previousMarkdownLineBlank(lines, i) && isCompleteATMStateBlock(content[offsets[blockStart]:offsets[i]]) {
			finalize(i)
			continue
		}
		if previousMarkdownLineBlank(lines, i) && isMarkdownV2BlockStartLine(trimmed) && isCompleteMarkdownGlobalDeclaration(content[offsets[blockStart]:offsets[i]]) {
			finalize(i)
			continue
		}
		if previousMarkdownLineBlank(lines, i) && isMarkdownV2BlockStartLine(trimmed) {
			if blockNestedHeading {
				start := offsets[blockStart]
				end := offsets[i]
				body := content[start:end]
				pendingInheritedPrompt = inheritedPromptFromTaskBody(body)
				pendingInheritedLevel = lastNestedHeadingLevelFromTaskBody(body, blockScopeLevel)
				pendingParentIndex = len(blocks)
				if root := parentRootPromptFromTaskBody(body); strings.TrimSpace(root) != "" {
					activeParentRootPrompt = root
					activeParentLevel = blockScopeLevel
					activeParentIndex = len(blocks)
				}
			}
			finalize(i)
			continue
		}
		if startsFencedPayloadCommand(trimmed) {
			fencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
		if parsed, ok := parseAnyFenceStart(line); ok {
			fence = parsed
			i++
			continue
		}
		if isDefCommandLine(trimmed) && previousMarkdownLineBlank(lines, i) {
			finalize(i)
			continue
		}
		i++
	}
	finalize(len(lines))
	if len(blocks) > 0 && prefixStart < len(content) {
		blocks[len(blocks)-1].Sep += content[prefixStart:]
	}
	return blocks
}

func inheritedPromptFromTaskBody(body string) string {
	return strings.TrimSpace(strings.Join(promptLinesFromTaskBody(body), ""))
}

func parentRootPromptFromTaskBody(body string) string {
	lines := promptLinesFromTaskBody(body)
	var root strings.Builder
	for _, line := range lines {
		if _, _, ok := parseMarkdownHeading(line); ok {
			break
		}
		root.WriteString(line)
	}
	return strings.TrimSpace(root.String())
}

func lastNestedHeadingLevelFromTaskBody(body string, parentLevel int) int {
	level := 0
	for _, line := range promptLinesFromTaskBody(body) {
		if current, _, ok := parseMarkdownHeading(line); ok && current > parentLevel {
			level = current
		}
	}
	return level
}

func promptLinesFromTaskBody(body string) []string {
	lines := SplitLines(body)
	start := 0
	outputFence := outputFenceInfo{}
	outputFencePending := false
	heredocDelim := ""
	for start < len(lines) {
		line := lines[start]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			start++
			continue
		}
		if outputFence.marker != "" {
			if isFenceClose(line, outputFence) {
				outputFence = outputFenceInfo{}
			}
			start++
			continue
		}
		if outputFencePending {
			if fence, ok := parseAnyFenceStart(line); ok {
				outputFence = fence
			}
			outputFencePending = false
			start++
			continue
		}
		if heredocDelim != "" {
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			start++
			continue
		}
		if !strings.HasPrefix(trimmed, "/") {
			break
		}
		if startsFencedPayloadCommand(trimmed) {
			outputFencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
		start++
	}
	return lines[start:]
}

func previousMarkdownLineBlank(lines []string, index int) bool {
	if index <= 0 {
		return true
	}
	return strings.TrimSpace(lines[index-1]) == ""
}

func lineOffsets(lines []string) []int {
	offsets := make([]int, len(lines)+1)
	for i, line := range lines {
		offsets[i+1] = offsets[i] + len(line)
	}
	return offsets
}

func updateMarkdownHeadingStack(stack []markdownHeadingSection, heading markdownHeadingSection) []markdownHeadingSection {
	for len(stack) > 0 && stack[len(stack)-1].level >= heading.level {
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 && stack[len(stack)-1].private {
		heading.private = true
	}
	return append(stack, heading)
}

func markdownScopePath(headings []markdownHeadingSection) []string {
	if len(headings) == 0 {
		return nil
	}
	out := make([]string, 0, len(headings))
	for _, heading := range headings {
		out = append(out, markdownScopeSegment(heading))
	}
	return out
}

func markdownScopeSegment(heading markdownHeadingSection) string {
	return strings.Join([]string{
		strings.Repeat("#", heading.level),
		strings.TrimSpace(heading.text),
	}, " ")
}

func MarkdownScopeSegment(heading MarkdownHeading) string {
	return strings.Join([]string{
		strings.Repeat("#", heading.Level),
		strings.TrimSpace(heading.Text),
	}, " ")
}

func markdownContext(headings []markdownHeadingSection, section string) string {
	var b strings.Builder
	for _, heading := range headings {
		if heading.private {
			continue
		}
		b.WriteString(strings.Repeat("#", heading.level))
		b.WriteByte(' ')
		b.WriteString(heading.text)
		b.WriteByte('\n')
	}
	if len(headings) > 0 && headings[len(headings)-1].private {
		return b.String()
	}
	if strings.TrimSpace(section) != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(section))
		b.WriteByte('\n')
	}
	return b.String()
}

func markdownContextWithParentRoot(headings []markdownHeadingSection, section, parentRoot string, parentLevel int) string {
	var b strings.Builder
	insertedParent := false
	for _, heading := range headings {
		if heading.private {
			continue
		}
		b.WriteString(strings.Repeat("#", heading.level))
		b.WriteByte(' ')
		b.WriteString(heading.text)
		b.WriteByte('\n')
		if !insertedParent && heading.level == parentLevel && strings.TrimSpace(parentRoot) != "" {
			b.WriteByte('\n')
			b.WriteString(strings.TrimSpace(parentRoot))
			b.WriteString("\n\n")
			insertedParent = true
		}
	}
	if !insertedParent && strings.TrimSpace(parentRoot) != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(parentRoot))
		b.WriteByte('\n')
	}
	if len(headings) > 0 && headings[len(headings)-1].private {
		return b.String()
	}
	if strings.TrimSpace(section) != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(section))
		b.WriteByte('\n')
	}
	return b.String()
}

func currentMarkdownLevel(headings []markdownHeadingSection) int {
	if len(headings) == 0 {
		return 0
	}
	return headings[len(headings)-1].level
}

func activeParentApplies(headings []markdownHeadingSection, parentLevel int) bool {
	return parentLevel > 0 && currentMarkdownLevel(headings) > parentLevel
}

type markdownSectionContext struct {
	level          int
	text           string
	private        bool
	seenExecutable bool
	body           strings.Builder
}

func markdownSectionContextMap(lines []string) map[string]string {
	var stack []*markdownSectionContext
	contexts := make(map[string]string)
	fence := outputFenceInfo{}
	flush := func(section *markdownSectionContext) {
		if section == nil || section.private {
			return
		}
		var b strings.Builder
		b.WriteString(strings.Repeat("#", section.level))
		b.WriteByte(' ')
		b.WriteString(section.text)
		if strings.TrimSpace(section.body.String()) != "" {
			b.WriteString("\n\n")
			b.WriteString(strings.TrimSpace(section.body.String()))
		}
		key := markdownContextKey(section.text)
		if key != "" {
			if _, exists := contexts[key]; !exists {
				contexts[key] = b.String()
			}
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if fence.marker != "" {
			if isFenceClose(line, fence) {
				fence = outputFenceInfo{}
			}
			if len(stack) > 0 {
				current := stack[len(stack)-1]
				if !current.private && !current.seenExecutable && !IsIgnoredLine(line) {
					current.body.WriteString(line)
				}
			}
			continue
		}
		if level, text, ok := parseMarkdownHeading(line); ok {
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				flush(stack[len(stack)-1])
				stack = stack[:len(stack)-1]
			}
			private := len(stack) > 0 && stack[len(stack)-1].private
			stack = append(stack, &markdownSectionContext{level: level, text: text, private: private})
			continue
		}
		if len(stack) == 0 {
			continue
		}
		current := stack[len(stack)-1]
		if next, ok := parseDocBlock(lines, i); ok {
			i = next - 1
			continue
		}
		if current.private || current.seenExecutable {
			continue
		}
		if parsed, ok := parseAnyFenceStart(line); ok {
			fence = parsed
		}
		if isDefCommandLine(trimmed) || isMarkdownV2BlockStartLine(trimmed) || isMarkdownV2SingleLineGlobal(trimmed) {
			current.seenExecutable = true
			continue
		}
		if !IsIgnoredLine(line) {
			current.body.WriteString(line)
		}
	}
	for len(stack) > 0 {
		flush(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
	}
	return contexts
}

func extractMarkdownContextRefs(body string, contexts map[string]string) (string, string) {
	lines := SplitLines(body)
	var out strings.Builder
	var extra strings.Builder
	header := true
	outputFence := outputFenceInfo{}
	outputFencePending := false
	heredocDelim := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !header {
			out.WriteString(line)
			continue
		}
		if outputFence.marker != "" {
			out.WriteString(line)
			if isFenceClose(line, outputFence) {
				outputFence = outputFenceInfo{}
			}
			continue
		}
		if outputFencePending {
			out.WriteString(line)
			if fence, ok := parseAnyFenceStart(line); ok {
				outputFence = fence
			}
			outputFencePending = false
			continue
		}
		if heredocDelim != "" {
			out.WriteString(line)
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			out.WriteString(line)
			continue
		}
		if ref, ok := parseContextLine(trimmed); ok {
			if ctx := contexts[markdownContextKey(ref)]; strings.TrimSpace(ctx) != "" {
				if extra.Len() > 0 {
					extra.WriteString("\n\n")
				}
				extra.WriteString(ctx)
				continue
			}
			out.WriteString(line)
			continue
		}
		if !strings.HasPrefix(trimmed, "/") {
			header = false
			out.WriteString(line)
			continue
		}
		out.WriteString(line)
		if startsFencedPayloadCommand(trimmed) {
			outputFencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
	}
	return out.String(), extra.String()
}

func parseContextLine(line string) (string, bool) {
	if line == "/context" {
		return "", true
	}
	if !strings.HasPrefix(line, "/context ") {
		return "", false
	}
	ref := strings.TrimSpace(strings.TrimPrefix(line, "/context"))
	return strings.TrimLeft(strings.TrimSpace(ref), "# \t"), true
}

func parseDocBlock(lines []string, index int) (int, bool) {
	if index < 0 || index >= len(lines) {
		return index, false
	}
	line := strings.TrimSpace(lines[index])
	if line == "" {
		return index, false
	}
	if !strings.HasPrefix(line, "/doc") {
		return index, false
	}
	if line != "/doc" && !strings.HasPrefix(line, "/doc ") {
		return index, false
	}
	if strings.TrimSpace(strings.TrimPrefix(line, "/doc")) != "" {
		return index + 1, true
	}
	if index+1 >= len(lines) {
		return index, false
	}
	fence, ok := parseAnyFenceStart(lines[index+1])
	if !ok {
		return index, false
	}
	_, next, err := collectRawFenceBlock(lines, index+2, fence)
	if err != nil {
		return index, false
	}
	return next, true
}

func markdownContextKey(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(strings.TrimLeft(text, "#")))), " ")
}

func isMarkdownV2BlockStartLine(line string) bool {
	if line == "" || !strings.HasPrefix(line, "/") {
		return false
	}
	token := firstField(line)
	switch token {
	case "/task", "/for", "/go", "/resume", "/args", "/cd", "/call", "/bash", "/wait", "/if", "/else", "/output", "/return", "/def", "/import", "/pool", "/let", "/db", "/skill", "/mcp", "/context", "/doc":
		return true
	default:
		return isIfCommandToken(token)
	}
}

func isMarkdownV2SingleLineGlobal(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "/pool", "/import":
		return true
	case "/skill":
		return len(fields) >= 2 && (fields[1] == "new" || fields[1] == "ignore")
	default:
		return false
	}
}

func isCompleteMarkdownGlobalDeclaration(body string) bool {
	if strings.TrimSpace(body) == "" {
		return false
	}
	if _, ok, err := ParseGlobalLetBlock(body); ok && err == nil {
		return true
	}
	if _, ok, err := ParseGlobalPoolBlock(body); ok && err == nil {
		return true
	}
	if _, ok, err := ParseGlobalImportBlock(body); ok && err == nil {
		return true
	}
	if _, ok, err := ParseGlobalDBBlock(body); ok && err == nil {
		return true
	}
	if _, ok, err := ParseGlobalSkillBlock(body); ok && err == nil {
		return true
	}
	if _, ok, err := ParseGlobalMCPBlock(body); ok && err == nil {
		return true
	}
	return false
}

func isCompleteATMStateBlock(body string) bool {
	if _, ok := marker.RemoveDone(body); ok {
		return true
	}
	if _, running, err := marker.StripRunning(body); err == nil && running.Active {
		return true
	}
	return false
}

func markdownHeadings(content string) []markdownHeadingSection {
	source := []byte(content)
	doc := goldmark.DefaultParser().Parse(mdtext.NewReader(source))
	headings := make([]markdownHeadingSection, 0)
	_ = mdast.Walk(doc, func(n mdast.Node, entering bool) (mdast.WalkStatus, error) {
		if !entering || n.Kind() != mdast.KindHeading {
			return mdast.WalkContinue, nil
		}
		heading := n.(*mdast.Heading)
		lines := heading.Lines()
		if lines == nil || lines.Len() == 0 {
			return mdast.WalkContinue, nil
		}
		first := lines.At(0)
		last := lines.At(lines.Len() - 1)
		start := heading.Pos()
		if start < 0 {
			start = first.Start
		}
		rawLine := content[start:lineEndOffset(content, last.Stop)]
		_, text, ok := parseMarkdownHeading(rawLine)
		if !ok {
			return mdast.WalkContinue, nil
		}
		slash := true
		var list bool
		switch {
		case strings.HasPrefix(text, "//"):
			list = true
		case strings.HasPrefix(text, "/"):
		default:
			slash = false
		}
		headings = append(headings, markdownHeadingSection{
			level: heading.Level,
			slash: slash,
			list:  list,
			text:  text,
			start: start,
			end:   lineEndOffset(content, last.Stop),
		})
		return mdast.WalkContinue, nil
	})
	return headings
}

func MarkdownHeadings(content string) []MarkdownHeading {
	headings := markdownHeadings(content)
	out := make([]MarkdownHeading, 0, len(headings))
	for _, heading := range headings {
		out = append(out, MarkdownHeading{
			Level: heading.level,
			Text:  heading.text,
			Start: heading.start,
			End:   heading.end,
		})
	}
	return out
}

func lineEndOffset(content string, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > len(content) {
		return len(content)
	}
	if offset < len(content) && content[offset] == '\r' {
		offset++
	}
	if offset < len(content) && content[offset] == '\n' {
		offset++
	}
	return offset
}

func splitTrailingBlankLines(content string) (string, string) {
	lines := SplitLines(content)
	cut := len(lines)
	for cut > 0 && IsBlankLine(lines[cut-1]) {
		cut--
	}
	if cut == len(lines) {
		return content, ""
	}
	return strings.Join(lines[:cut], ""), strings.Join(lines[cut:], "")
}

func markdownAnyHeading(line string) (int, bool) {
	level, _, ok := parseMarkdownHeading(line)
	return level, ok
}

func parseMarkdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) {
		return 0, "", false
	}
	if trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, "", false
	}
	text := strings.TrimSpace(trimmed[level:])
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}
