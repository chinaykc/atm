// Package agent adapts external coding agents and local shell execution to the
// ATM runtime.
//
// The Runner interface is the runtime boundary. Implementations translate ATM
// run options into Codex, Claude Code, bash, and MCP configuration without
// changing the language or execution model.
package agent
