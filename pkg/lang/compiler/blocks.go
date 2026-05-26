package compiler

import "strings"

func ParseBlocks(content string) []Block {
	lines := SplitLines(content)
	return parseMarkdownTaskBlocks(content, lines)
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
