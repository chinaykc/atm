package compiler

import (
	"errors"
	"strings"

	"github.com/chinaykc/atm/pkg/lang/syntax"
)

type Diagnostic = syntax.Diagnostic
type SourceSpan = syntax.SourceSpan

type DiagnosticError struct {
	Diagnostics []Diagnostic
}

func (e DiagnosticError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "diagnostic error"
	}
	var messages []string
	for _, diagnostic := range e.Diagnostics {
		if diagnostic.Message != "" {
			messages = append(messages, diagnostic.Message)
		}
	}
	if len(messages) == 0 {
		return "diagnostic error"
	}
	return strings.Join(messages, "; ")
}

func errorDiagnostic(source string, err error) Diagnostic {
	return diagnosticAt(source, err, SourceSpan{})
}

func diagnosticError(source string, err error) error {
	if err == nil {
		return nil
	}
	return DiagnosticError{Diagnostics: []Diagnostic{errorDiagnostic(source, err)}}
}

func diagnosticErrorAt(source string, err error, span SourceSpan) error {
	if err == nil {
		return nil
	}
	return DiagnosticError{Diagnostics: []Diagnostic{diagnosticAt(source, err, span)}}
}

func diagnosticsFromError(source string, err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var diagnosticErr DiagnosticError
	if errors.As(err, &diagnosticErr) {
		return diagnosticErr.Diagnostics
	}
	return []Diagnostic{errorDiagnostic(source, err)}
}

func diagnosticAt(source string, err error, span SourceSpan) Diagnostic {
	return Diagnostic{
		Severity: "error",
		Source:   source,
		Block:    span.Block,
		Line:     span.Line,
		Column:   span.Column,
		Message:  err.Error(),
	}
}

func warningDiagnosticAt(source, message string, span SourceSpan) Diagnostic {
	return Diagnostic{
		Severity: "warning",
		Source:   source,
		Block:    span.Block,
		Line:     span.Line,
		Column:   span.Column,
		Message:  message,
	}
}

func blockSourceSpans(content string, blocks []Block) []SourceSpan {
	spans := make([]SourceSpan, len(blocks))
	line, column := 1, 1
	for i, block := range blocks {
		line, column = advancePosition(line, column, block.Prefix)
		spans[i] = SourceSpan{Block: i + 1, Line: line, Column: column}
		line, column = advancePosition(line, column, block.Body)
		line, column = advancePosition(line, column, block.Sep)
	}
	if len(blocks) == 0 && content != "" {
		_, _ = advancePosition(1, 1, content)
	}
	return spans
}

func sourceSpanAtOffset(content string, offset int) SourceSpan {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	line, column := advancePosition(1, 1, content[:offset])
	return SourceSpan{Line: line, Column: column}
}

func shiftSourceSpan(base SourceSpan, relative SourceSpan) SourceSpan {
	span := relative
	if span.Line <= 0 {
		span.Line = 1
	}
	if span.Column <= 0 {
		span.Column = 1
	}
	span.Line = base.Line + span.Line - 1
	if relative.Line <= 1 {
		span.Column = base.Column + span.Column - 1
	}
	return span
}

func advancePosition(line, column int, text string) (int, int) {
	for _, r := range text {
		if r == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}
