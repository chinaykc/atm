package document

import "github.com/chinaykc/atm/pkg/lang/compiler"

type Block = compiler.Block
type MarkdownHeading = compiler.MarkdownHeading

func ParseBlocks(content string) []Block {
	return compiler.ParseBlocks(content)
}

func SplitLines(content string) []string {
	return compiler.SplitLines(content)
}

func IsBlankLine(line string) bool {
	return compiler.IsBlankLine(line)
}

func MarkdownHeadings(content string) []MarkdownHeading {
	return compiler.MarkdownHeadings(content)
}

func MarkdownScopeSegment(heading MarkdownHeading) string {
	return compiler.MarkdownScopeSegment(heading)
}
