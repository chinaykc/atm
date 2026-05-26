// Package compiler turns ATM task documents into executable IR.
//
// It owns command parsing, Markdown scope handling, imports, definitions,
// resource declarations, static validation, and lowering to the IR model.
// Consumers that only need syntax trees, document blocks, status markers, or
// formatting should use the narrower sibling packages under pkg/lang.
package compiler
