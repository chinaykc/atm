// Package syntax defines ATM's public source-level AST.
//
// The syntax tree preserves document blocks, slash commands, source positions,
// and diagnostics for editors, linters, and other tools that need to understand
// source structure without lowering it to executable IR.
package syntax
