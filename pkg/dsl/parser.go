package dsl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	gotemplate "text/template"
)

var legacyTemplateVar = regexp.MustCompile(`{{[ \t]*([A-Za-z_][A-Za-z0-9_-]*)[ \t]*}}`)
var legacyTemplateField = regexp.MustCompile(`{{[ \t]*([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)[ \t]*}}`)

func ParseBlocks(content string) []Block {
	lines := SplitLines(content)
	if hasSlashHeading(lines) {
		return parseMarkdownTaskBlocks(content, lines)
	}
	return parseLegacyBlocks(lines, "")
}

func parseLegacyBlocks(lines []string, initialPrefix string) []Block {
	blocks := make([]Block, 0)
	var body strings.Builder
	heredocDelim := ""
	outputFence := outputFenceInfo{}
	outputFencePending := false
	inHTMLComment := false
	prefix := initialPrefix

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if outputFence.marker != "" {
			body.WriteString(line)
			if isFenceClose(line, outputFence) {
				outputFence = outputFenceInfo{}
			}
			continue
		}
		if outputFencePending {
			body.WriteString(line)
			if fence, ok := parseFenceStart(line); ok {
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
		if inHTMLComment {
			if isHTMLCommentEndLine(trimmed) {
				inHTMLComment = false
			}
			continue
		}
		if isHTMLCommentStartLine(trimmed) {
			if !isHTMLCommentEndLine(trimmed) {
				inHTMLComment = true
			}
			continue
		}
		if IsIgnoredLine(line) {
			continue
		}
		if IsBlankLine(line) {
			if body.Len() > 0 {
				blocks = append(blocks, Block{Prefix: prefix, Body: body.String(), Sep: line})
				prefix = ""
				body.Reset()
			} else if len(blocks) > 0 {
				blocks[len(blocks)-1].Sep += line
			} else {
				prefix += line
			}
			continue
		}
		body.WriteString(line)
		if startsFencedPayloadCommand(trimmed) {
			outputFencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
	}

	if body.Len() > 0 {
		blocks = append(blocks, Block{Prefix: prefix, Body: body.String()})
	}

	return blocks
}

func filterSingleTaskSection(section string) string {
	lines := SplitLines(section)
	var out strings.Builder
	heredocDelim := ""
	outputFence := outputFenceInfo{}
	outputFencePending := false
	inHTMLComment := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if outputFence.marker != "" {
			out.WriteString(line)
			if isFenceClose(line, outputFence) {
				outputFence = outputFenceInfo{}
			}
			continue
		}
		if outputFencePending {
			out.WriteString(line)
			if fence, ok := parseFenceStart(line); ok {
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
		if inHTMLComment {
			if isHTMLCommentEndLine(trimmed) {
				inHTMLComment = false
			}
			continue
		}
		if isHTMLCommentStartLine(trimmed) {
			if !isHTMLCommentEndLine(trimmed) {
				inHTMLComment = true
			}
			continue
		}
		if IsIgnoredLine(line) && !isPreservedPromptHeading(line) {
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
	return out.String()
}

type sourceLine struct {
	text       string
	start, end int
}

func parseMarkdownTaskBlocks(content string, rawLines []string) []Block {
	lines := sourceLines(rawLines)
	var blocks []Block
	cursor := 0
	for i := 0; i < len(lines); {
		heading, ok := slashHeading(lines[i].text)
		if !ok {
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
		prefix := content[cursor:sectionStart]
		section := content[sectionStart:sectionEnd]
		var sectionBlocks []Block
		if isDefinitionHeadingText(heading.text) {
			i = end
			continue
		}
		if heading.list {
			sectionBlocks = parseLegacyBlocks(SplitLines(section), prefix)
		} else if strings.TrimSpace(section) != "" {
			body := filterSingleTaskSection(section)
			body, sep := splitTrailingBlankLines(body)
			if strings.TrimSpace(body) != "" {
				sectionBlocks = []Block{{Prefix: prefix, Body: body, Sep: sep}}
			}
		}
		if len(sectionBlocks) > 0 {
			blocks = append(blocks, sectionBlocks...)
			cursor = sectionEnd
		}
		i = end
	}
	if len(blocks) > 0 && cursor < len(content) {
		blocks[len(blocks)-1].Sep += content[cursor:]
	}
	return blocks
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

func sourceLines(raw []string) []sourceLine {
	lines := make([]sourceLine, 0, len(raw))
	offset := 0
	for _, line := range raw {
		next := offset + len(line)
		lines = append(lines, sourceLine{text: line, start: offset, end: next})
		offset = next
	}
	return lines
}

func hasSlashHeading(lines []string) bool {
	for _, line := range lines {
		if _, ok := slashHeading(line); ok {
			return true
		}
	}
	return false
}

type slashHeadingInfo struct {
	level int
	list  bool
	text  string
}

func slashHeading(line string) (slashHeadingInfo, bool) {
	level, text, ok := parseMarkdownHeading(line)
	if !ok {
		return slashHeadingInfo{}, false
	}
	switch {
	case strings.HasPrefix(text, "//"):
		return slashHeadingInfo{level: level, list: true, text: text}, true
	case strings.HasPrefix(text, "/"):
		return slashHeadingInfo{level: level, text: text}, true
	default:
		return slashHeadingInfo{}, false
	}
}

func isDefinitionHeadingText(text string) bool {
	return text == "/def" || strings.HasPrefix(text, "/def ") || text == "//def" || strings.HasPrefix(text, "//def ")
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

func SplitLines(content string) []string {
	if content == "" {
		return nil
	}

	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		lines = append(lines, content[start:i+1])
		start = i + 1
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

func IsBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

func IsCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#") || isMarkdownReferenceCommentLine(trimmed)
}

func IsIgnoredLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return IsCommentLine(line) || isMarkdownRuleLine(trimmed)
}

func isMarkdownRuleLine(line string) bool {
	if len(line) < 3 {
		return false
	}
	var want rune
	for i, r := range line {
		if i == 0 {
			switch r {
			case '-', '=':
				want = r
			default:
				return false
			}
			continue
		}
		if r != want {
			return false
		}
	}
	return true
}

func isPreservedPromptHeading(line string) bool {
	_, ok := markdownAnyHeading(line)
	return ok
}

func isHTMLCommentStartLine(line string) bool {
	if !strings.HasPrefix(line, "<!--") {
		return false
	}
	end := strings.Index(line, "-->")
	return end < 0 || end == len(line)-3
}

func isHTMLCommentEndLine(line string) bool {
	return strings.HasSuffix(line, "-->")
}

func isMarkdownReferenceCommentLine(line string) bool {
	if strings.HasPrefix(line, "[//]: # (") && strings.HasSuffix(line, ")") {
		return true
	}
	if strings.HasPrefix(line, "[//]: # \"") && strings.HasSuffix(line, "\"") {
		return true
	}
	if strings.HasPrefix(line, "[comment]: <> (") && strings.HasSuffix(line, ")") {
		return true
	}
	return false
}

func IsDone(body string) bool {
	if _, fields, ok := extractATMBlock(body); ok && isTerminalStatus(fields["status"]) {
		return true
	}
	return doneSuffix.MatchString(strings.TrimSpace(body))
}

func IsSkipped(body string) bool {
	if _, fields, ok := extractATMBlock(body); ok && strings.EqualFold(fields["status"], "skipped") {
		return true
	}
	return false
}

func ParseIfBlock(body string) (IfBlock, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return IfBlock{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok || !isIfCommandLine(line) {
		return IfBlock{}, false, nil
	}
	condition, err := parseIfCondition(line)
	if err != nil {
		return IfBlock{}, true, err
	}
	rest = strings.TrimSpace(rest)
	return IfBlock{Condition: condition, HeaderOnly: rest == "", Body: rest}, true, nil
}

func ParseElseBlock(body string) (ElseBlock, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return ElseBlock{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok || line != "/else" {
		return ElseBlock{}, false, nil
	}
	rest = strings.TrimSpace(rest)
	return ElseBlock{HeaderOnly: rest == "", Body: rest}, true, nil
}

func firstNonBlankLine(body string) (string, bool) {
	for _, line := range SplitLines(body) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed, true
		}
	}
	return "", false
}

func firstNonBlankLineWithRest(body string) (string, string, bool) {
	lines := SplitLines(body)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed, strings.Join(lines[i+1:], ""), true
		}
	}
	return "", "", false
}

func firstField(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func isIfCommandLine(line string) bool {
	return line == "/if" || strings.HasPrefix(line, "/if ") || strings.HasPrefix(line, "/if(")
}

func isIfCommandToken(token string) bool {
	return token == "/if" || strings.HasPrefix(token, "/if(")
}

func parseIfCondition(line string) (Condition, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "/if"))
	if rest == "" {
		return Condition{}, fmt.Errorf("/if requires a condition")
	}
	if strings.HasPrefix(rest, "(") {
		expr, err := parseParenthesizedCommand(line, "/if")
		if err != nil {
			return Condition{}, err
		}
		return Condition{Kind: ConditionCEL, Text: expr}, nil
	}
	return Condition{Kind: ConditionNatural, Text: rest}, nil
}

func ParseGlobalLetBlock(body string) ([]LetBinding, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return nil, false, err
	}
	lines := SplitLines(body)
	var bindings []LetBinding
	seen := false
	for i := 0; i < len(lines); i++ {
		rawLine := lines[i]
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/let ") {
			return nil, false, nil
		}
		if defaults, next, ok, err := parseBashHeredocCommand(lines, i, line); ok || err != nil {
			if err != nil {
				return nil, true, err
			}
			if !defaults.hasLet {
				return nil, false, nil
			}
			bindings = append(bindings, LetBinding{Name: defaults.letName, BashScript: defaults.bashCommands[0].Script})
			seen = true
			i = next - 1
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil, true, fmt.Errorf("/let requires a name and value")
		}
		name := fields[1]
		if !isVariableName(name) {
			return nil, true, fmt.Errorf("invalid variable name %q", name)
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "/let "+name))
		if value == "/bash" || strings.HasPrefix(value, "/bash ") {
			script := strings.TrimSpace(strings.TrimPrefix(value, "/bash"))
			if script == "" {
				return nil, true, fmt.Errorf("/let %s /bash requires a script", name)
			}
			bindings = append(bindings, LetBinding{Name: name, BashScript: script})
		} else {
			bindings = append(bindings, LetBinding{Name: name, Value: value})
		}
		seen = true
	}
	return bindings, seen, nil
}

func ParseGlobalPoolBlock(body string) ([]PoolDecl, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return nil, false, err
	}
	lines := SplitLines(body)
	var pools []PoolDecl
	seen := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "/pool" {
			return nil, true, fmt.Errorf("/pool requires name, max concurrency, and optional buffer size")
		}
		if !strings.HasPrefix(line, "/pool ") {
			return nil, false, nil
		}
		fields := strings.Fields(line)
		if len(fields) != 3 && len(fields) != 4 {
			return nil, true, fmt.Errorf("/pool requires name, max concurrency, and optional buffer size")
		}
		name := fields[1]
		if !isVariableName(name) {
			return nil, true, fmt.Errorf("invalid pool name %q", name)
		}
		max := parsePositiveIntToken(fields[2])
		if max <= 0 {
			return nil, true, fmt.Errorf("/pool %s max concurrency must be a positive integer", name)
		}
		buffer := -1
		if len(fields) == 4 {
			n, err := strconv.Atoi(fields[3])
			if err != nil || n < 0 {
				return nil, true, fmt.Errorf("/pool %s buffer size must be a non-negative integer", name)
			}
			buffer = n
		}
		pools = append(pools, PoolDecl{Name: name, Max: max, Buffer: buffer})
		seen = true
	}
	return pools, seen, nil
}

func ParseGlobalDBBlock(body string) (DBDecl, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return DBDecl{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok {
		return DBDecl{}, false, nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/db" {
		return DBDecl{}, false, nil
	}
	if len(fields) < 3 || fields[1] != "new" {
		return DBDecl{}, false, nil
	}
	if len(fields) > 6 {
		return DBDecl{}, true, fmt.Errorf("/db new accepts name plus optional scope, persist, and access")
	}
	decl := DBDecl{
		Name:    fields[2],
		Scope:   DBScopeGlobal,
		Persist: DBPersistRun,
		Access:  DBAccessAdmin,
		Usage:   strings.TrimSpace(rest),
	}
	if !isVariableName(decl.Name) {
		return DBDecl{}, true, fmt.Errorf("invalid db name %q", decl.Name)
	}
	for _, field := range fields[3:] {
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			return DBDecl{}, true, fmt.Errorf("unsupported /db new option %q", field)
		}
		switch key {
		case "scope":
			scope, err := parseDBScope(value)
			if err != nil {
				return DBDecl{}, true, err
			}
			decl.Scope = scope
		case "persist":
			persist, err := parseDBPersistence(value)
			if err != nil {
				return DBDecl{}, true, err
			}
			decl.Persist = persist
		case "access":
			access, err := parseDBAccess(value)
			if err != nil {
				return DBDecl{}, true, err
			}
			decl.Access = access
		default:
			return DBDecl{}, true, fmt.Errorf("unsupported /db new option %q", field)
		}
	}
	return decl, true, nil
}

func ParseGlobalSkillBlock(body string) (SkillDecl, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return SkillDecl{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok {
		return SkillDecl{}, false, nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/skill" {
		return SkillDecl{}, false, nil
	}
	if len(fields) != 5 || fields[1] != "new" || fields[3] != "from" {
		return SkillDecl{}, true, fmt.Errorf("/skill new form is /skill new name from path")
	}
	if strings.TrimSpace(rest) != "" {
		return SkillDecl{}, true, fmt.Errorf("/skill new does not accept a body")
	}
	if !isVariableName(fields[2]) {
		return SkillDecl{}, true, fmt.Errorf("invalid skill name %q", fields[2])
	}
	return SkillDecl{Name: fields[2], Path: fields[4]}, true, nil
}

func ParseGlobalMCPBlock(body string) (MCPDecl, bool, error) {
	body, _, err := StripRunning(body)
	if err != nil {
		return MCPDecl{}, false, err
	}
	lines := SplitLines(body)
	first := -1
	for i, raw := range lines {
		if strings.TrimSpace(raw) != "" {
			first = i
			break
		}
	}
	if first < 0 {
		return MCPDecl{}, false, nil
	}
	line := strings.TrimSpace(lines[first])
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/mcp" {
		return MCPDecl{}, false, nil
	}
	if len(fields) != 3 || fields[1] != "new" {
		return MCPDecl{}, true, fmt.Errorf("/mcp new form is /mcp new name followed by a fenced JSON block")
	}
	if !isVariableName(fields[2]) {
		return MCPDecl{}, true, fmt.Errorf("invalid mcp name %q", fields[2])
	}
	next := first + 1
	for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
		next++
	}
	if next >= len(lines) {
		return MCPDecl{}, true, fmt.Errorf("/mcp new requires a fenced JSON block")
	}
	fence, ok := parseFenceStart(lines[next])
	if !ok || fence.lang != "json" {
		return MCPDecl{}, true, fmt.Errorf("/mcp new requires a fenced json block")
	}
	bodyText, end, err := collectRawFenceBlock(lines, next+1, fence)
	if err != nil {
		return MCPDecl{}, true, err
	}
	for i := end; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return MCPDecl{}, true, fmt.Errorf("/mcp new does not accept content after its fenced JSON block")
		}
	}
	return MCPDecl{Name: fields[2], Config: bodyText}, true, nil
}

func collectRawFenceBlock(lines []string, start int, fence outputFenceInfo) (string, int, error) {
	var body strings.Builder
	for i := start; i < len(lines); i++ {
		if isFenceClose(lines[i], fence) {
			return body.String(), i + 1, nil
		}
		body.WriteString(lines[i])
	}
	return "", len(lines), fmt.Errorf("fenced block is missing closing ```")
}

func parseDBTaskLine(line string) (DBTaskConfig, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/db" {
		return DBTaskConfig{}, false, nil
	}
	if len(fields) < 2 {
		return DBTaskConfig{}, true, fmt.Errorf("/db requires subcommand")
	}
	var out DBTaskConfig
	switch fields[1] {
	case "use":
		names, access, err := parseDBNamesWithOptionalAccess(fields[2:])
		if err != nil {
			return DBTaskConfig{}, true, err
		}
		if len(names) == 0 {
			return DBTaskConfig{}, true, fmt.Errorf("/db use requires at least one name")
		}
		out.Use = append(out.Use, DBUse{Names: names, Access: access})
	case "access":
		if len(fields) < 4 {
			return DBTaskConfig{}, true, fmt.Errorf("/db access requires name(s) and access level")
		}
		access, err := parseDBAccess(fields[len(fields)-1])
		if err != nil {
			return DBTaskConfig{}, true, err
		}
		names := append([]string{}, fields[2:len(fields)-1]...)
		if err := validateDBNamesOrStar(names); err != nil {
			return DBTaskConfig{}, true, err
		}
		out.Access = append(out.Access, DBAccessRule{Names: names, Access: access})
	case "ignore":
		if len(fields) == 2 {
			out.IgnoreAll = true
			return out, true, nil
		}
		names := append([]string{}, fields[2:]...)
		if err := validateDBNames(names); err != nil {
			return DBTaskConfig{}, true, err
		}
		out.Ignore = append(out.Ignore, names...)
	case "new":
		return DBTaskConfig{}, true, fmt.Errorf("/db new must be written as a standalone global block")
	default:
		return DBTaskConfig{}, true, fmt.Errorf("unsupported /db subcommand %q", fields[1])
	}
	return out, true, nil
}

func parseSkillTaskLine(line string) (SkillTaskConfig, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/skill" {
		return SkillTaskConfig{}, false, nil
	}
	if len(fields) < 2 {
		return SkillTaskConfig{}, true, fmt.Errorf("/skill requires subcommand")
	}
	var out SkillTaskConfig
	switch fields[1] {
	case "use":
		if len(fields) < 3 {
			return SkillTaskConfig{}, true, fmt.Errorf("/skill use requires at least one name or path")
		}
		out.Use = append(out.Use, fields[2:]...)
	case "ignore":
		if len(fields) == 2 {
			out.IgnoreAll = true
			return out, true, nil
		}
		out.Ignore = append(out.Ignore, fields[2:]...)
	case "new":
		return SkillTaskConfig{}, true, fmt.Errorf("/skill new must be written as a standalone global block")
	default:
		return SkillTaskConfig{}, true, fmt.Errorf("unsupported /skill subcommand %q", fields[1])
	}
	return out, true, nil
}

func parseMCPTaskLine(line string) (MCPTaskConfig, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/mcp" {
		return MCPTaskConfig{}, false, nil
	}
	if len(fields) < 2 {
		return MCPTaskConfig{}, true, fmt.Errorf("/mcp requires subcommand")
	}
	var out MCPTaskConfig
	switch fields[1] {
	case "use":
		if len(fields) < 3 {
			return MCPTaskConfig{}, true, fmt.Errorf("/mcp use requires at least one name")
		}
		out.Use = append(out.Use, fields[2:]...)
	case "ignore":
		if len(fields) == 2 {
			out.IgnoreAll = true
			return out, true, nil
		}
		out.Ignore = append(out.Ignore, fields[2:]...)
	case "def":
		if len(fields) < 4 || fields[2] != "use" {
			return MCPTaskConfig{}, true, fmt.Errorf("/mcp def form is /mcp def use name...")
		}
		for _, name := range fields[3:] {
			if !isDefinitionName(name) {
				return MCPTaskConfig{}, true, fmt.Errorf("invalid definition name %q", name)
			}
		}
		out.DefUse = append(out.DefUse, fields[3:]...)
	case "new":
		return MCPTaskConfig{}, true, fmt.Errorf("/mcp new must be written as a standalone global block")
	default:
		return MCPTaskConfig{}, true, fmt.Errorf("unsupported /mcp subcommand %q", fields[1])
	}
	return out, true, nil
}

func parseDBNamesWithOptionalAccess(fields []string) ([]string, DBAccess, error) {
	var names []string
	var access DBAccess
	for _, field := range fields {
		if strings.HasPrefix(field, "access:") {
			if access != "" {
				return nil, "", fmt.Errorf("/db use accepts access only once")
			}
			parsed, err := parseDBAccess(strings.TrimPrefix(field, "access:"))
			if err != nil {
				return nil, "", err
			}
			access = parsed
			continue
		}
		names = append(names, field)
	}
	if err := validateDBNames(names); err != nil {
		return nil, "", err
	}
	return names, access, nil
}

func validateDBNames(names []string) error {
	for _, name := range names {
		if !isVariableName(name) {
			return fmt.Errorf("invalid db name %q", name)
		}
	}
	return nil
}

func validateDBNamesOrStar(names []string) error {
	for _, name := range names {
		if name == "*" {
			continue
		}
		if !isVariableName(name) {
			return fmt.Errorf("invalid db name %q", name)
		}
	}
	return nil
}

func parseDBScope(value string) (DBScope, error) {
	switch DBScope(value) {
	case DBScopeLocal:
		return DBScopeLocal, nil
	case DBScopeGlobal:
		return DBScopeGlobal, nil
	default:
		return "", fmt.Errorf("invalid db scope %q", value)
	}
}

func parseDBPersistence(value string) (DBPersistence, error) {
	switch DBPersistence(value) {
	case DBPersistRun:
		return DBPersistRun, nil
	case DBPersistProject:
		return DBPersistProject, nil
	default:
		return "", fmt.Errorf("invalid db persistence %q", value)
	}
}

func parseDBAccess(value string) (DBAccess, error) {
	switch DBAccess(value) {
	case DBAccessRead:
		return DBAccessRead, nil
	case DBAccessAppend:
		return DBAccessAppend, nil
	case DBAccessWrite:
		return DBAccessWrite, nil
	case DBAccessAdmin:
		return DBAccessAdmin, nil
	default:
		return "", fmt.Errorf("invalid db access %q", value)
	}
}

func parseTask(index int, body string, globals map[string]any, opts CompileOptions) (taskAST, error) {
	t := taskAST{vars: CloneVars(globals)}
	body, running, err := StripRunning(body)
	if err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	t.running = running

	lines := SplitLines(body)
	lines, t.output, err = extractOutputSpec(lines)
	if err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	lines, t.returnSpec, err = extractReturnSpec(lines)
	if err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	promptStart := 0
	var defaults RunOptions
	var prefixes []string
	seenCommand := false
	for ; promptStart < len(lines); promptStart++ {
		line := strings.TrimSpace(lines[promptStart])
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/") {
			break
		}
		if dbConfig, ok, err := parseDBTaskLine(line); ok || err != nil {
			if err != nil {
				return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if dbConfig.IgnoreAll && (len(t.db.Use) > 0 || len(t.db.Access) > 0) {
				return taskAST{}, fmt.Errorf("task %d: /db ignore cannot be combined with /db use or /db access", index+1)
			}
			if t.db.IgnoreAll && (len(dbConfig.Use) > 0 || len(dbConfig.Access) > 0) {
				return taskAST{}, fmt.Errorf("task %d: /db ignore cannot be combined with /db use or /db access", index+1)
			}
			t.db.IgnoreAll = t.db.IgnoreAll || dbConfig.IgnoreAll
			t.db.Ignore = append(t.db.Ignore, dbConfig.Ignore...)
			t.db.Use = append(t.db.Use, dbConfig.Use...)
			t.db.Access = append(t.db.Access, dbConfig.Access...)
			seenCommand = true
			continue
		}
		if skillConfig, ok, err := parseSkillTaskLine(line); ok || err != nil {
			if err != nil {
				return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if skillConfig.IgnoreAll && len(t.skill.Use) > 0 {
				return taskAST{}, fmt.Errorf("task %d: /skill ignore cannot be combined with /skill use", index+1)
			}
			if t.skill.IgnoreAll && len(skillConfig.Use) > 0 {
				return taskAST{}, fmt.Errorf("task %d: /skill ignore cannot be combined with /skill use", index+1)
			}
			t.skill.IgnoreAll = t.skill.IgnoreAll || skillConfig.IgnoreAll
			t.skill.Use = append(t.skill.Use, skillConfig.Use...)
			t.skill.Ignore = append(t.skill.Ignore, skillConfig.Ignore...)
			seenCommand = true
			continue
		}
		if mcpConfig, ok, err := parseMCPTaskLine(line); ok || err != nil {
			if err != nil {
				return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
			}
			if mcpConfig.IgnoreAll && (len(t.mcp.Use) > 0 || len(t.mcp.DefUse) > 0) {
				return taskAST{}, fmt.Errorf("task %d: /mcp ignore cannot be combined with /mcp use or /mcp def use", index+1)
			}
			if t.mcp.IgnoreAll && (len(mcpConfig.Use) > 0 || len(mcpConfig.DefUse) > 0) {
				return taskAST{}, fmt.Errorf("task %d: /mcp ignore cannot be combined with /mcp use or /mcp def use", index+1)
			}
			t.mcp.IgnoreAll = t.mcp.IgnoreAll || mcpConfig.IgnoreAll
			t.mcp.Use = append(t.mcp.Use, mcpConfig.Use...)
			t.mcp.Ignore = append(t.mcp.Ignore, mcpConfig.Ignore...)
			t.mcp.DefUse = append(t.mcp.DefUse, mcpConfig.DefUse...)
			seenCommand = true
			continue
		}
		token := firstField(line)
		if (isIfCommandToken(token) || token == "/else") && seenCommand {
			return taskAST{}, fmt.Errorf("task %d: %s must be the first command in its task block", index+1, token)
		}

		lineSteps, lineDefaults, nextPromptStart, err := parseCommandLineAt(lines, promptStart, t.vars, opts.Root)
		if err != nil {
			return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
		}
		seenCommand = true
		promptStart = nextPromptStart - 1
		t.goRun = t.goRun || lineDefaults.goRun
		t.wait = t.wait || lineDefaults.wait
		for _, op := range lineDefaults.flow {
			if op.kind == astOpFor {
				op.step.Options = MergeRunOptions(lineDefaults.Options, op.step.Options)
			}
			t.flow = append(t.flow, op)
		}
		if lineDefaults.hasLet {
			t.vars[lineDefaults.letName] = lineDefaults.letValue
		}
		if len(lineDefaults.bashCommands) > 0 {
			t.bashCommands = append(t.bashCommands, lineDefaults.bashCommands...)
		}
		if lineDefaults.prefixVar != "" {
			value, ok := t.vars[lineDefaults.prefixVar]
			if !ok {
				return taskAST{}, fmt.Errorf("unknown variable command %q", "/"+lineDefaults.prefixVar)
			}
			if StringValue(value) == "" && hasNamedBashCommand(t.bashCommands, lineDefaults.prefixVar) {
				value = "{{" + lineDefaults.prefixVar + "}}"
			}
			prefixes = append(prefixes, StringValue(value))
		}
		if len(lineSteps) == 0 {
			defaults = MergeRunOptions(defaults, lineDefaults.Options)
			continue
		}
		for _, step := range lineSteps {
			step.Options = MergeRunOptions(lineDefaults.Options, step.Options)
			t.steps = append(t.steps, step)
		}
	}

	t.prompt = prependPromptPrefixes(prefixes, strings.Join(lines[promptStart:], ""))
	if err := validateDBTaskConfig(t.db); err != nil {
		return taskAST{}, fmt.Errorf("task %d: %w", index+1, err)
	}
	if strings.TrimSpace(t.prompt) == "" {
		if (t.wait || t.returnSpec != nil) && !t.goRun && len(t.steps) == 0 && len(t.bashCommands) == 0 && !defaults.Resume && len(defaults.Args) == 0 {
			return t, nil
		}
		if len(t.bashCommands) == 0 && t.returnSpec == nil && len(t.flow) == 0 {
			return taskAST{}, fmt.Errorf("task %d: prompt is empty", index+1)
		}
	}

	if len(t.steps) == 0 {
		t.steps = []forAST{{MaxRuns: 1}}
	}
	for i := range t.steps {
		t.steps[i].Options = MergeRunOptions(defaults, t.steps[i].Options)
	}
	for i := range t.flow {
		if t.flow[i].kind == astOpFor {
			t.flow[i].step.Options = MergeRunOptions(defaults, t.flow[i].step.Options)
		}
	}
	return t, nil
}

func validateDBTaskConfig(config DBTaskConfig) error {
	seen := map[string]DBAccess{}
	record := func(name string, access DBAccess) error {
		if access == "" {
			return nil
		}
		if previous, ok := seen[name]; ok && previous != access {
			return fmt.Errorf("db %q has conflicting access overrides %s and %s", name, previous, access)
		}
		seen[name] = access
		return nil
	}
	for _, use := range config.Use {
		for _, name := range use.Names {
			if err := record(name, use.Access); err != nil {
				return err
			}
		}
	}
	for _, rule := range config.Access {
		for _, name := range rule.Names {
			if err := record(name, rule.Access); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseCommandLineAt(lines []string, index int, vars map[string]any, root string) ([]forAST, commandLineDefaults, int, error) {
	line := strings.TrimSpace(lines[index])
	if defaults, next, ok, err := parseBashHeredocCommand(lines, index, line); ok || err != nil {
		return nil, defaults, next, err
	}
	steps, defaults, err := parseCommandLine(line, vars, root)
	return steps, defaults, index + 1, err
}

func extractOutputSpec(lines []string) ([]string, *OutputSpec, error) {
	var out []string
	var spec *OutputSpec
	heredocDelim := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if heredocDelim != "" {
			out = append(out, line)
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if parsed, next, ok, err := parseOutputAt(lines, i, trimmed); ok || err != nil {
			if err != nil {
				return nil, nil, err
			}
			if spec != nil {
				return nil, nil, fmt.Errorf("/output can only appear once")
			}
			spec = parsed
			i = next - 1
			continue
		}
		out = append(out, line)
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
	}
	return out, spec, nil
}

func extractReturnSpec(lines []string) ([]string, *ReturnSpec, error) {
	var out []string
	var spec *ReturnSpec
	heredocDelim := ""
	outputFence := outputFenceInfo{}
	outputFencePending := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if outputFence.marker != "" {
			out = append(out, line)
			if isFenceClose(line, outputFence) {
				outputFence = outputFenceInfo{}
			}
			continue
		}
		if outputFencePending {
			out = append(out, line)
			if fence, ok := parseFenceStart(line); ok {
				outputFence = fence
			}
			outputFencePending = false
			continue
		}
		if heredocDelim != "" {
			out = append(out, line)
			if trimmed == heredocDelim {
				heredocDelim = ""
			}
			continue
		}
		if parsed, next, ok, err := parseReturnAt(lines, i, trimmed); ok || err != nil {
			if err != nil {
				return nil, nil, err
			}
			if spec != nil {
				return nil, nil, fmt.Errorf("/return can only appear once")
			}
			spec = parsed
			i = next - 1
			continue
		}
		out = append(out, line)
		if strings.HasPrefix(trimmed, "/output") {
			outputFencePending = true
		}
		if delim, ok := lineHeredocDelimiter(trimmed); ok {
			heredocDelim = delim
		}
	}
	return out, spec, nil
}

func parseReturnAt(lines []string, index int, line string) (*ReturnSpec, int, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/return" {
		return nil, index + 1, false, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "/return"))
	if rest == "" {
		if index+1 < len(lines) {
			if fence, ok := parseFenceStart(lines[index+1]); ok {
				schema, next, err := collectFenceBlock(lines, index+2, fence)
				if err != nil {
					return nil, next, true, err
				}
				return &ReturnSpec{
					Kind: ReturnStructured,
					Output: &OutputSpec{
						Schema:       schema.body,
						SchemaFormat: schema.format,
						Structured:   true,
					},
				}, next, true, nil
			}
		}
		var b strings.Builder
		for i := index + 1; i < len(lines); i++ {
			b.WriteString(lines[i])
		}
		return &ReturnSpec{Kind: ReturnTemplate, Text: strings.TrimRight(b.String(), "\r\n")}, len(lines), true, nil
	}
	if rest == "/bash" || strings.HasPrefix(rest, "/bash ") {
		script := strings.TrimSpace(strings.TrimPrefix(rest, "/bash"))
		if script == "" {
			return nil, index + 1, true, fmt.Errorf("/return /bash requires a script")
		}
		return &ReturnSpec{Kind: ReturnBash, Script: script}, index + 1, true, nil
	}
	return &ReturnSpec{Kind: ReturnTemplate, Text: rest}, index + 1, true, nil
}

func parseOutputAt(lines []string, index int, line string) (*OutputSpec, int, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/output" {
		return nil, index + 1, false, nil
	}
	if len(fields) > 2 {
		return nil, index + 1, true, fmt.Errorf("/output accepts at most one file name")
	}
	if index+1 >= len(lines) {
		fileName := ""
		if len(fields) == 2 {
			fileName = fields[1]
		}
		return &OutputSpec{FileName: fileName}, index + 1, true, nil
	}
	fence, ok := parseFenceStart(lines[index+1])
	if !ok {
		fileName := ""
		if len(fields) == 2 {
			fileName = fields[1]
		}
		return &OutputSpec{FileName: fileName}, index + 1, true, nil
	}
	schema, next, err := collectFenceBlock(lines, index+2, fence)
	if err != nil {
		return nil, next, true, err
	}
	fileName := ""
	if len(fields) == 2 {
		fileName = fields[1]
	}
	return &OutputSpec{FileName: fileName, Schema: schema.body, SchemaFormat: schema.format, Structured: true}, next, true, nil
}

func startsFencedPayloadCommand(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	return fields[0] == "/output" || fields[0] == "/return"
}

type parsedOutputSchema struct {
	format string
	body   string
}

type outputFenceInfo struct {
	marker string
	lang   string
}

func parseFenceStart(line string) (outputFenceInfo, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '`' {
		return outputFenceInfo{}, false
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == '`' {
		count++
	}
	if count < 3 {
		return outputFenceInfo{}, false
	}
	lang := strings.TrimSpace(trimmed[count:])
	switch strings.ToLower(lang) {
	case "", "json", "yaml", "yml":
		return outputFenceInfo{marker: strings.Repeat("`", count), lang: strings.ToLower(lang)}, true
	default:
		return outputFenceInfo{}, false
	}
}

func collectFenceBlock(lines []string, start int, fence outputFenceInfo) (parsedOutputSchema, int, error) {
	var body strings.Builder
	for i := start; i < len(lines); i++ {
		if isFenceClose(lines[i], fence) {
			schema, err := parseOutputSchema(fence.lang, body.String())
			return schema, i + 1, err
		}
		body.WriteString(lines[i])
	}
	return parsedOutputSchema{}, len(lines), fmt.Errorf("/output fenced schema block is missing closing ```")
}

func isFenceClose(line string, fence outputFenceInfo) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, fence.marker) {
		return false
	}
	for i := len(fence.marker); i < len(trimmed); i++ {
		if trimmed[i] != '`' {
			return false
		}
	}
	return true
}

func parseOutputSchema(lang, body string) (parsedOutputSchema, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return parsedOutputSchema{}, fmt.Errorf("/output schema block is empty")
	}
	switch lang {
	case "json":
		if !json.Valid([]byte(trimmed)) {
			return parsedOutputSchema{}, fmt.Errorf("/output json schema is not valid JSON")
		}
		return parsedOutputSchema{format: "json", body: trimmed}, nil
	case "yaml", "yml":
		return parsedOutputSchema{format: "yaml", body: trimmed}, nil
	default:
		if json.Valid([]byte(trimmed)) {
			return parsedOutputSchema{format: "json", body: trimmed}, nil
		}
		schema, err := parseSimpleOutputSchema(trimmed)
		if err != nil {
			return parsedOutputSchema{}, err
		}
		return parsedOutputSchema{format: "json", body: schema}, nil
	}
}

func parseSimpleOutputSchema(body string) (string, error) {
	properties := make(map[string]any)
	var required []string
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		name := strings.TrimSpace(parts[0])
		if !isVariableName(name) {
			return "", fmt.Errorf("/output field name %q is invalid", name)
		}
		typ := "string"
		description := ""
		switch len(parts) {
		case 1:
			description = ""
		case 2:
			description = strings.TrimSpace(parts[1])
		default:
			if candidate := strings.TrimSpace(parts[1]); candidate != "" {
				if strings.HasPrefix(candidate, "[]") {
					itemType := strings.TrimPrefix(candidate, "[]")
					if !isJSONSchemaScalarType(itemType) {
						return "", fmt.Errorf("/output field %q has unsupported array item type %q", name, itemType)
					}
					properties[name] = map[string]any{"type": "array", "items": map[string]string{"type": itemType}}
					description = strings.TrimSpace(parts[2])
					if description != "" {
						properties[name].(map[string]any)["description"] = description
					}
					required = append(required, name)
					continue
				}
				if !isJSONSchemaScalarType(candidate) {
					return "", fmt.Errorf("/output field %q has unsupported type %q", name, candidate)
				}
				typ = candidate
			}
			description = strings.TrimSpace(parts[2])
		}
		property := map[string]any{"type": typ}
		if description != "" {
			property["description"] = description
		}
		properties[name] = property
		required = append(required, name)
	}
	if len(required) == 0 {
		return "", fmt.Errorf("/output simple schema has no fields")
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func isJSONSchemaScalarType(value string) bool {
	switch value {
	case "string", "number", "integer", "boolean", "object", "array", "null":
		return true
	default:
		return false
	}
}

func parseBashHeredocCommand(lines []string, index int, line string) (commandLineDefaults, int, bool, error) {
	var defaults commandLineDefaults
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return defaults, index + 1, false, nil
	}

	if fields[0] == "/bash" {
		delim, ok := parseHeredocDelimiter(fields[1:])
		if !ok {
			return defaults, index + 1, false, nil
		}
		script, next, err := collectHeredocScript(lines, index+1, delim)
		if err != nil {
			return defaults, next, true, err
		}
		defaults.bashCommands = append(defaults.bashCommands, BashCommand{Script: script})
		defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: BashCommand{Script: script}})
		return defaults, next, true, nil
	}

	if len(fields) >= 4 && fields[0] == "/let" && fields[2] == "/bash" {
		name := fields[1]
		if !isVariableName(name) {
			return defaults, index + 1, true, fmt.Errorf("invalid variable name %q", name)
		}
		delim, ok := parseHeredocDelimiter(fields[3:])
		if !ok {
			return defaults, index + 1, false, nil
		}
		script, next, err := collectHeredocScript(lines, index+1, delim)
		if err != nil {
			return defaults, next, true, err
		}
		defaults.hasLet = true
		defaults.letName = name
		defaults.letValue = ""
		command := BashCommand{Name: name, Script: script}
		defaults.bashCommands = append(defaults.bashCommands, command)
		defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: command})
		return defaults, next, true, nil
	}

	return defaults, index + 1, false, nil
}

func lineHeredocDelimiter(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	if fields[0] == "/bash" {
		return parseHeredocDelimiter(fields[1:])
	}
	if len(fields) >= 4 && fields[0] == "/let" && fields[2] == "/bash" {
		return parseHeredocDelimiter(fields[3:])
	}
	return "", false
}

func parseHeredocDelimiter(fields []string) (string, bool) {
	if len(fields) == 1 && strings.HasPrefix(fields[0], "<<") {
		return normalizeHeredocDelimiter(strings.TrimPrefix(fields[0], "<<")), true
	}
	if len(fields) == 2 && fields[0] == "<<" {
		return normalizeHeredocDelimiter(fields[1]), true
	}
	return "", false
}

func normalizeHeredocDelimiter(delim string) string {
	delim = strings.TrimSpace(delim)
	if len(delim) >= 2 {
		first := delim[0]
		last := delim[len(delim)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return delim[1 : len(delim)-1]
		}
	}
	return delim
}

func collectHeredocScript(lines []string, start int, delim string) (string, int, error) {
	if delim == "" {
		return "", start, fmt.Errorf("/bash heredoc requires a delimiter")
	}
	var script strings.Builder
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == delim {
			return script.String(), i + 1, nil
		}
		script.WriteString(line)
	}
	return "", len(lines), fmt.Errorf("/bash heredoc missing delimiter %q", delim)
}

type commandLineDefaults struct {
	Options      RunOptions
	goRun        bool
	wait         bool
	flow         []astOp
	hasLet       bool
	letName      string
	letValue     string
	bashCommands []BashCommand
	prefixVar    string
	output       *OutputSpec
}

func parseCommandLine(line string, vars map[string]any, root string) ([]forAST, commandLineDefaults, error) {
	fields := strings.Fields(line)
	var steps []forAST
	var defaults commandLineDefaults

	for i := 0; i < len(fields); {
		token := fields[i]
		if !strings.HasPrefix(token, "/") {
			return nil, defaults, fmt.Errorf("unexpected command argument %q", token)
		}
		switch token {
		case "/resume":
			defaults.Options.Resume = true
			i++
		case "/go":
			pool, next, err := parseOptionalPoolName(fields, i+1)
			if err != nil {
				return nil, defaults, err
			}
			defaults.goRun = true
			defaults.flow = append(defaults.flow, astOp{kind: astOpGo, Pool: pool})
			i = next
		case "/wait":
			pool, next, err := parseOptionalPoolName(fields, i+1)
			if err != nil {
				return nil, defaults, err
			}
			defaults.wait = true
			defaults.flow = append(defaults.flow, astOp{kind: astOpWait, Pool: pool})
			i = next
		case "/args":
			args, next := collectCommandArgs(fields, i+1)
			if len(args) == 0 {
				return nil, defaults, fmt.Errorf("/args requires at least one argument")
			}
			defaults.Options.Args = append(defaults.Options.Args, args...)
			i = next
		case "/cd":
			command, next, err := parseCdCommand(fields, i)
			if err != nil {
				return nil, defaults, err
			}
			defaults.flow = append(defaults.flow, astOp{kind: astOpCd, CdCommand: command})
			i = next
		case "/let":
			if i != 0 {
				return nil, defaults, fmt.Errorf("/let must be the only command on its line")
			}
			if len(fields) < 3 {
				return nil, defaults, fmt.Errorf("/let requires a name and value")
			}
			name := fields[1]
			if !isVariableName(name) {
				return nil, defaults, fmt.Errorf("invalid variable name %q", name)
			}
			defaults.hasLet = true
			defaults.letName = name
			value := strings.TrimSpace(strings.TrimPrefix(line, "/let "+name))
			if value == "/call" || strings.HasPrefix(value, "/call ") {
				call, err := parseCallFields(strings.Fields(value), 0)
				if err != nil {
					return nil, defaults, err
				}
				call.Assign = name
				defaults.flow = append(defaults.flow, astOp{kind: astOpCall, Call: call})
				defaults.hasLet = false
				defaults.letValue = ""
			} else if value == "/bash" || strings.HasPrefix(value, "/bash ") {
				script := strings.TrimSpace(strings.TrimPrefix(value, "/bash"))
				if script == "" {
					return nil, defaults, fmt.Errorf("/let %s /bash requires a script", name)
				}
				command := BashCommand{Name: name, Script: script}
				defaults.bashCommands = append(defaults.bashCommands, command)
				defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: command})
				defaults.letValue = ""
			} else {
				defaults.letValue = value
			}
			return nil, defaults, nil
		case "/bash":
			if i != 0 {
				return nil, defaults, fmt.Errorf("/bash must be the only command on its line")
			}
			script := strings.TrimSpace(strings.TrimPrefix(line, "/bash"))
			if script == "" {
				return nil, defaults, fmt.Errorf("/bash requires a script")
			}
			command := BashCommand{Script: script}
			defaults.bashCommands = append(defaults.bashCommands, command)
			defaults.flow = append(defaults.flow, astOp{kind: astOpBash, BashCommand: command})
			return nil, defaults, nil
		case "/if":
			if i != 0 || len(fields) < 2 {
				return nil, defaults, fmt.Errorf("/if must be the first command on its line and requires a condition")
			}
			if _, err := parseIfCondition(line); err != nil {
				return nil, defaults, err
			}
			return nil, defaults, nil
		case "/else":
			if len(fields) != 1 {
				return nil, defaults, fmt.Errorf("/else must be the only command on its line")
			}
			return nil, defaults, nil
		case "/output":
			return nil, defaults, fmt.Errorf("/output must be the only command on its line and followed by a fenced schema block")
		case "/pool":
			return nil, defaults, fmt.Errorf("/pool must be written as a standalone global block")
		case "/def", "//def":
			return nil, defaults, fmt.Errorf("%s must be written as a standalone definition block or Markdown heading", token)
		case "/import":
			return nil, defaults, fmt.Errorf("/import must be written as a standalone global block")
		case "/db":
			return nil, defaults, fmt.Errorf("/db must be written as a standalone line")
		case "/skill":
			return nil, defaults, fmt.Errorf("/skill must be written as a standalone line")
		case "/mcp":
			return nil, defaults, fmt.Errorf("/mcp must be written as a standalone line")
		case "/return":
			return nil, defaults, fmt.Errorf("/return must be written as a standalone line")
		case "/call":
			call, err := parseCallFields(fields, i)
			if err != nil {
				return nil, defaults, err
			}
			defaults.flow = append(defaults.flow, astOp{kind: astOpCall, Call: call})
			i = len(fields)
		case "/for":
			step, next, err := parseForCommand(fields, i+1, root)
			if err != nil {
				return nil, defaults, err
			}
			steps = append(steps, step)
			defaults.flow = append(defaults.flow, astOp{kind: astOpFor, step: step})
			i = next
		default:
			if isIfCommandToken(token) {
				if i != 0 {
					return nil, defaults, fmt.Errorf("/if must be the first command on its line")
				}
				if _, err := parseIfCondition(line); err != nil {
					return nil, defaults, err
				}
				return nil, defaults, nil
			}
			name := strings.TrimPrefix(token, "/")
			if !isVariableName(name) {
				return nil, defaults, fmt.Errorf("unsupported command %q", token)
			}
			if _, ok := vars[name]; !ok {
				return nil, defaults, fmt.Errorf("unsupported command %q", token)
			}
			if len(fields) != 1 {
				return nil, defaults, fmt.Errorf("variable command %q must be the only command on its line", token)
			}
			defaults.prefixVar = name
			return nil, defaults, nil
		}
	}
	return steps, defaults, nil
}

func parseOptionalPoolName(fields []string, start int) (string, int, error) {
	if start >= len(fields) || isCommandToken(fields[start]) {
		return "", start, nil
	}
	name := fields[start]
	if !isVariableName(name) {
		return "", start, fmt.Errorf("invalid pool name %q", name)
	}
	return name, start + 1, nil
}

func parseCdCommand(fields []string, start int) (CdCommand, int, error) {
	if start >= len(fields) || fields[start] != "/cd" {
		return CdCommand{}, start, fmt.Errorf("expected /cd")
	}
	next := start + 1
	mustExist := false
	if next < len(fields) && fields[next] == "--must-exist" {
		mustExist = true
		next++
	}
	args, end := collectCommandArgs(fields, next)
	if len(args) == 0 {
		return CdCommand{}, end, fmt.Errorf("/cd requires a path")
	}
	if len(args) > 1 {
		return CdCommand{}, end, fmt.Errorf("/cd accepts exactly one path")
	}
	if strings.HasPrefix(args[0], "-") {
		return CdCommand{}, end, fmt.Errorf("unsupported /cd flag %q", args[0])
	}
	return CdCommand{Path: args[0], MustExist: mustExist}, end, nil
}

func parseCallFields(fields []string, start int) (Call, error) {
	if start >= len(fields) || fields[start] != "/call" {
		return Call{}, fmt.Errorf("expected /call")
	}
	if start+1 >= len(fields) {
		return Call{}, fmt.Errorf("/call requires a definition name")
	}
	name := fields[start+1]
	if !isDefinitionName(name) {
		return Call{}, fmt.Errorf("invalid definition name %q", name)
	}
	return Call{Name: name, Args: append([]string{}, fields[start+2:]...)}, nil
}

func ParseCallExpression(text string) (Call, error) {
	fields := strings.Fields(strings.TrimSpace(text))
	return parseCallFields(fields, 0)
}

func collectCommandArgs(fields []string, start int) ([]string, int) {
	end := start
	for end < len(fields) && !isCommandToken(fields[end]) {
		end++
	}
	return fields[start:end], end
}

func isCommandToken(token string) bool {
	switch token {
	case "/resume", "/go", "/wait", "/args", "/cd", "/let", "/bash", "/for", "/if", "/else", "/output", "/pool", "/call", "/return", "/def", "//def", "/import", "/db", "/skill", "/mcp":
		return true
	default:
		return false
	}
}

func hasNamedBashCommand(commands []BashCommand, name string) bool {
	for _, command := range commands {
		if command.Name == name {
			return true
		}
	}
	return false
}

func parseForCommand(fields []string, start int, root string) (forAST, int, error) {
	if start >= len(fields) {
		return forAST{}, start, fmt.Errorf("/for requires an iterator")
	}

	step := forAST{}
	token := fields[start]
	next := start + 1
	if token == "until" || strings.HasPrefix(token, "until(") {
		condition, after, err := parseUntilCondition(fields, start)
		if err != nil {
			return forAST{}, start, err
		}
		if condition.Kind != ConditionCEL {
			return forAST{}, start, fmt.Errorf("/for until without a count requires a parenthesized CEL condition")
		}
		step.VarName = "N"
		step.Condition = condition
		return step, after, nil
	}
	switch {
	case parsePositiveIntToken(token) > 0:
		n := parsePositiveIntToken(token)
		step.MaxRuns = n
		step.VarName = "N"
		step.Values = make([]string, n)
		for i := range step.Values {
			step.Values[i] = strconv.Itoa(i + 1)
		}
	case token == "dir":
		values, err := listDirs(root)
		if err != nil {
			return forAST{}, start, err
		}
		step.VarName = "dir"
		step.Values = values
		step.MaxRuns = len(values)
	case token == "path":
		values, err := listPaths(root)
		if err != nil {
			return forAST{}, start, err
		}
		step.VarName = "path"
		step.Values = values
		step.MaxRuns = len(values)
	case isVariableName(token):
		if next >= len(fields) || !strings.HasPrefix(fields[next], "in") {
			return forAST{}, start, fmt.Errorf("unsupported /for iterator %q", token)
		}
		step.VarName = token
		inToken := fields[next]
		if inToken == "in" {
			sourceStart := next + 1
			if sourceStart >= len(fields) {
				return forAST{}, start, fmt.Errorf("/for in requires a bracketed list or parenthesized CEL expression")
			}
			if strings.HasPrefix(fields[sourceStart], "[") {
				values, after, err := parseForList(fields, sourceStart)
				if err != nil {
					return forAST{}, start, err
				}
				step.Values = values
				step.MaxRuns = len(values)
				next = after
			} else if strings.HasPrefix(fields[sourceStart], "(") {
				after, err := setDynamicForSource(&step, fields, sourceStart)
				if err != nil {
					return forAST{}, start, err
				}
				next = after
			} else {
				return forAST{}, start, fmt.Errorf("/for in requires a bracketed list or parenthesized CEL expression")
			}
		} else if strings.HasPrefix(inToken, "in(") {
			synthetic := append([]string{strings.TrimPrefix(inToken, "in")}, fields[next+1:]...)
			after, err := setDynamicForSource(&step, synthetic, 0)
			if err != nil {
				return forAST{}, start, err
			}
			next = next + after
		} else {
			return forAST{}, start, fmt.Errorf("/for in requires a bracketed list or parenthesized CEL expression")
		}
	default:
		return forAST{}, start, fmt.Errorf("unsupported /for iterator %q", token)
	}

	if next < len(fields) && (fields[next] == "until" || strings.HasPrefix(fields[next], "until(")) {
		condition, after, err := parseUntilCondition(fields, next)
		if err != nil {
			return forAST{}, start, err
		}
		step.Condition = condition
		next = after
	}
	return step, next, nil
}

func setDynamicForSource(step *forAST, fields []string, start int) (int, error) {
	parts, after, err := collectParenthesizedTokens(fields, start, "/for in")
	if err != nil {
		return start, err
	}
	source := trimCELParens(strings.Join(parts, " "))
	if source == "" {
		return start, fmt.Errorf("/for in requires a CEL expression")
	}
	kind := ConditionCEL
	if source == "/call" || strings.HasPrefix(source, "/call ") {
		if _, err := ParseCallExpression(source); err != nil {
			return start, err
		}
		kind = ConditionCall
	}
	step.Source = Condition{Kind: kind, Text: source}
	return after, nil
}

func collectParenthesizedTokens(fields []string, start int, label string) ([]string, int, error) {
	var parts []string
	depth := 0
	seen := false
	var quote rune
	escaped := false
	for i := start; i < len(fields); i++ {
		if isCommandToken(fields[i]) && !seen {
			return nil, start, fmt.Errorf("%s requires an expression", label)
		}
		token := fields[i]
		parts = append(parts, token)
		for _, r := range token {
			if escaped {
				escaped = false
				continue
			}
			if quote != 0 {
				if r == '\\' {
					escaped = true
					continue
				}
				if r == quote {
					quote = 0
				}
				continue
			}
			switch r {
			case '\'', '"':
				quote = r
			case '(':
				depth++
				seen = true
			case ')':
				depth--
				if depth == 0 && seen {
					return parts, i + 1, nil
				}
				if depth < 0 {
					return nil, start, fmt.Errorf("%s CEL expression has unmatched )", label)
				}
			}
		}
	}
	if !seen {
		return nil, start, fmt.Errorf("%s CEL expression must start with (", label)
	}
	return nil, start, fmt.Errorf("%s CEL expression missing closing )", label)
}

func parseUntilCondition(fields []string, start int) (Condition, int, error) {
	if start >= len(fields) {
		return Condition{}, start, fmt.Errorf("/for until requires a condition")
	}
	token := fields[start]
	if token != "until" && !strings.HasPrefix(token, "until(") {
		return Condition{}, start, fmt.Errorf("/for until requires a condition")
	}
	var parts []string
	next := start + 1
	if token == "until" {
		if next >= len(fields) || isCommandToken(fields[next]) {
			return Condition{}, start, fmt.Errorf("/for until requires a condition")
		}
		if strings.HasPrefix(fields[next], "(") {
			var err error
			parts, next, err = collectCELConditionTokens(fields, next)
			if err != nil {
				return Condition{}, start, err
			}
			return Condition{Kind: ConditionCEL, Text: trimCELParens(strings.Join(parts, " "))}, next, nil
		}
		conditionStart := next
		for next < len(fields) && !isCommandToken(fields[next]) {
			next++
		}
		if conditionStart == next {
			return Condition{}, start, fmt.Errorf("/for until requires a condition")
		}
		return Condition{Kind: ConditionNatural, Text: strings.Join(fields[conditionStart:next], " ")}, next, nil
	}

	first := strings.TrimPrefix(token, "until")
	if first == "" || !strings.HasPrefix(first, "(") {
		return Condition{}, start, fmt.Errorf("/for until requires a condition")
	}
	var err error
	parts, next, err = collectCELConditionTokens(append([]string{first}, fields[next:]...), 0)
	if err != nil {
		return Condition{}, start, err
	}
	return Condition{Kind: ConditionCEL, Text: trimCELParens(strings.Join(parts, " "))}, start + next, nil
}

func collectCELConditionTokens(fields []string, start int) ([]string, int, error) {
	var parts []string
	depth := 0
	seen := false
	var quote rune
	escaped := false
	for i := start; i < len(fields); i++ {
		if isCommandToken(fields[i]) && !seen {
			return nil, start, fmt.Errorf("/for until requires a condition")
		}
		token := fields[i]
		parts = append(parts, token)
		for _, r := range token {
			if escaped {
				escaped = false
				continue
			}
			if quote != 0 {
				if r == '\\' {
					escaped = true
					continue
				}
				if r == quote {
					quote = 0
				}
				continue
			}
			switch r {
			case '\'', '"':
				quote = r
			case '(':
				depth++
				seen = true
			case ')':
				depth--
				if depth == 0 && seen {
					return parts, i + 1, nil
				}
				if depth < 0 {
					return nil, start, fmt.Errorf("/for until CEL condition has unmatched )")
				}
			}
		}
	}
	if !seen {
		return nil, start, fmt.Errorf("/for until CEL condition must start with (")
	}
	return nil, start, fmt.Errorf("/for until CEL condition missing closing )")
}

func trimCELParens(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func parseParenthesizedCommand(line, command string) (string, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, command))
	if rest == line || rest == "" || !strings.HasPrefix(rest, "(") {
		return "", fmt.Errorf("%s requires a parenthesized CEL condition", command)
	}
	depth := 0
	var quote rune
	escaped := false
	for i, r := range rest {
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if strings.TrimSpace(rest[i+1:]) != "" {
					return "", fmt.Errorf("%s must be the only command on its line", command)
				}
				expr := strings.TrimSpace(rest[1:i])
				if expr == "" {
					return "", fmt.Errorf("%s requires a CEL condition", command)
				}
				return expr, nil
			}
			if depth < 0 {
				return "", fmt.Errorf("%s CEL condition has unmatched )", command)
			}
		}
	}
	return "", fmt.Errorf("%s CEL condition missing closing )", command)
}

func parseForList(fields []string, start int) ([]string, int, error) {
	if start >= len(fields) || !strings.HasPrefix(fields[start], "[") {
		return nil, start, fmt.Errorf("/for in requires a bracketed list")
	}
	var values []string
	for i := start; i < len(fields); i++ {
		token := fields[i]
		first := i == start
		last := strings.HasSuffix(token, "]")
		token = strings.TrimPrefix(token, "[")
		token = strings.TrimSuffix(token, "]")
		if token != "" {
			values = append(values, token)
		}
		if last {
			if first && strings.HasPrefix(fields[i], "[]") {
				return nil, i + 1, fmt.Errorf("/for in list cannot be empty")
			}
			if len(values) == 0 {
				return nil, i + 1, fmt.Errorf("/for in list cannot be empty")
			}
			return values, i + 1, nil
		}
	}
	return nil, start, fmt.Errorf("/for in list missing closing ]")
}

func parsePositiveIntToken(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func isVariableName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func CloneVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func CloneStringMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
		Args:     append(append([]string{}, base.Args...), override.Args...),
		Output:   output,
		DBs:      append(append([]DBRuntime{}, base.DBs...), override.DBs...),
		Workdir:  workdir,
		Skills:   append(append([]SkillRuntime{}, base.Skills...), override.Skills...),
		MCPs:     append(append([]MCPRuntime{}, base.MCPs...), override.MCPs...),
		DefMCP:   mergeDefMCP(base.DefMCP, override.DefMCP),
		DefDepth: maxInt(base.DefDepth, override.DefDepth),
	}
}

func mergeDefMCP(base, override *DefMCPRuntime) *DefMCPRuntime {
	if override != nil {
		return cloneDefMCPRuntime(override)
	}
	return cloneDefMCPRuntime(base)
}

func cloneDefMCPRuntime(in *DefMCPRuntime) *DefMCPRuntime {
	if in == nil {
		return nil
	}
	out := *in
	out.Definitions = append([]string{}, in.Definitions...)
	out.DBs = append([]DBRuntime{}, in.DBs...)
	out.Skills = append([]SkillRuntime{}, in.Skills...)
	out.MCPs = append([]MCPRuntime{}, in.MCPs...)
	if in.Vars != nil {
		out.Vars = CloneVars(in.Vars)
	}
	out.Defs = append([]DefinitionRef{}, in.Defs...)
	return &out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func prependPromptPrefixes(prefixes []string, prompt string) string {
	if len(prefixes) == 0 {
		return prompt
	}
	var b strings.Builder
	for _, prefix := range prefixes {
		b.WriteString(prefix)
		if !strings.HasSuffix(prefix, "\n") && !strings.HasSuffix(prefix, "\r") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(prompt)
	return b.String()
}

func RenderTemplate(input string, vars any) (string, error) {
	normalized := normalizeVars(vars)
	rewritten := rewriteLegacyTemplateVars(input)
	tpl, err := gotemplate.New("atm").
		Option("missingkey=zero").
		Funcs(templateFuncs(normalized)).
		Parse(rewritten)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, templateData(normalized)); err != nil {
		return "", err
	}
	return out.String(), nil
}

func normalizeVars(vars any) map[string]any {
	switch v := vars.(type) {
	case nil:
		return nil
	case map[string]any:
		return v
	case map[string]string:
		return CloneStringMap(v)
	default:
		return nil
	}
}

func rewriteLegacyTemplateVars(input string) string {
	input = legacyTemplateField.ReplaceAllStringFunc(input, func(match string) string {
		parts := legacyTemplateField.FindStringSubmatch(match)
		if len(parts) != 3 || isGoTemplateKeyword(parts[1]) {
			return match
		}
		return `{{index .` + parts[1] + ` "` + parts[2] + `"}}`
	})
	return legacyTemplateVar.ReplaceAllStringFunc(input, func(match string) string {
		parts := legacyTemplateVar.FindStringSubmatch(match)
		if len(parts) != 2 || isGoTemplateKeyword(parts[1]) {
			return match
		}
		return `{{var "` + parts[1] + `"}}`
	})
}

func templateFuncs(vars map[string]any) gotemplate.FuncMap {
	return gotemplate.FuncMap{
		"var": func(name string) string {
			if value, ok := vars[name]; ok {
				return StringValue(value)
			}
			return "{{" + name + "}}"
		},
		"has": func(name string) bool {
			_, ok := vars[name]
			return ok
		},
	}
}

func templateData(vars map[string]any) map[string]any {
	data := make(map[string]any, len(vars)+1)
	data["Vars"] = vars
	for name, value := range vars {
		if isTemplateIdentifier(name) {
			data[name] = value
		}
	}
	return data
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

func isTemplateIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isGoTemplateKeyword(word string) bool {
	switch word {
	case "if", "else", "end", "range", "with", "define", "template", "block", "break", "continue", "nil", "true", "false":
		return true
	default:
		return false
	}
}

func listDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list dirs: %w", err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func listPaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() && shouldSkipWalkDir(name) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list paths: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func shouldSkipWalkDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build":
		return true
	}
	return false
}

func FormatBlockBody(body string) string {
	body, tag, hasTag := extractTrailingTag(body)
	if hasTag {
		return appendTagLine(body, tag)
	}
	return body
}

func extractTrailingTag(body string) (string, string, bool) {
	if _, _, ok := extractATMBlock(body); ok {
		return body, "", false
	}
	if clean, tag, ok := extractTag(body, doneMarker, doneSuffix); ok {
		return clean, tag, true
	}
	if clean, tag, ok := extractTag(body, runningLineMarker, runningSuffix); ok {
		return clean, tag, true
	}
	return body, "", false
}

func extractTag(body string, markerRe, suffixRe *regexp.Regexp) (string, string, bool) {
	core, eol := splitTrailingLineEnding(body)

	lineStart := strings.LastIndexAny(core, "\r\n") + 1
	lastLine := core[lineStart:]
	if markerRe.MatchString(strings.TrimSpace(lastLine)) {
		prefix := strings.TrimRight(core[:lineStart], "\r\n")
		if prefix == "" {
			return eol, strings.TrimSpace(lastLine), true
		}
		return prefix + eol, strings.TrimSpace(lastLine), true
	}

	tag := suffixRe.FindString(core)
	if tag == "" {
		return body, "", false
	}
	return suffixRe.ReplaceAllString(core, "") + eol, strings.TrimSpace(tag), true
}
