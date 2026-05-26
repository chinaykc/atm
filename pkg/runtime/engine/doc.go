// Package engine executes compiled ATM task plans.
//
// It owns scheduling, /go branches, /wait coordination, definition execution,
// resource resolution, state updates, and report generation. It does not parse
// command syntax directly; parsing and validation belong to pkg/lang/compiler.
package engine
