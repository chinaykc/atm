package compiler

import (
	"errors"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"path/filepath"
	"slices"
)

func ParseTask(index int, body string, globals map[string]any, opts CompileOptions) (Task, error) {
	t, err := parseTask(index, body, globals, normalizeCompileOptions("", opts))
	if err != nil {
		return Task{}, err
	}
	task := lowerTaskASTToIR(index, t)
	task.Context = opts.Context
	return task, nil
}

func ParseTaskForFile(filePath string, index int, body string, globals map[string]any, opts CompileOptions) (Task, error) {
	task, err := ParseTask(index, body, globals, normalizeCompileOptions(filePath, opts))
	if err != nil {
		return Task{}, fmt.Errorf("parse todo file %q: %w", filePath, err)
	}
	return task, nil
}

func CompileProgram(sourcePath, content string) (Plan, error) {
	return CompileProgramWithOptions(sourcePath, content, CompileOptions{})
}

func CompileProgramDiagnostics(sourcePath, content string) (Plan, []Diagnostic) {
	plan, err := CompileProgramWithOptions(sourcePath, content, CompileOptions{})
	if err == nil {
		return plan, plan.Diagnostics
	}
	var diagnosticErr DiagnosticError
	if errors.As(err, &diagnosticErr) {
		return Plan{}, diagnosticErr.Diagnostics
	}
	return Plan{}, []Diagnostic{errorDiagnostic(sourcePath, err)}
}

func CompileProgramWithOptions(sourcePath, content string, opts CompileOptions) (Plan, error) {
	opts = normalizeCompileOptions(sourcePath, opts)
	blocks := ParseBlocks(content)
	spans := blockSourceSpans(content, blocks)
	plan := Plan{SourcePath: sourcePath}
	defs, err := LoadDefinitionSet(sourcePath, content, opts)
	if err != nil {
		return Plan{}, err
	}
	for _, decl := range defs.Imports {
		plan.Imports = append(plan.Imports, decl)
	}
	for _, def := range defs.Items {
		plan.Definitions = append(plan.Definitions, def)
	}
	if len(blocks) == 0 {
		if len(plan.Definitions) > 0 || len(plan.Imports) > 0 {
			return plan, nil
		}
		return Plan{}, fmt.Errorf("no tasks found in todo file %q", sourcePath)
	}
	diagnostics := diagnosticCollector{source: sourcePath, spans: spans}
	var warnings []Diagnostic
	for _, item := range duplicateATMReportIDs(blocks) {
		diagnostics.addBlock(item.index, fmt.Errorf("duplicate ATM report id %q also appears in block %d", item.id, item.first+1))
	}
	taskVarsByBlock := make(map[int]map[string]any)
	for i := 0; i < len(blocks); i++ {
		body := blocks[i].Body
		if marker.IsDone(body) || IsBlankLine(body) {
			continue
		}
		imports, ok, err := ParseGlobalImportBlock(body)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		if ok {
			_ = imports
			continue
		}
		bindings, ok, err := ParseGlobalLetBlock(body)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		if ok {
			for _, binding := range bindings {
				plan.Globals = append(plan.Globals, GlobalBinding{
					BlockIndex: i,
					Name:       binding.Name,
					Value:      binding.Value,
					BashScript: binding.BashScript,
					SourcePath: sourcePath,
					Scope:      slices.Clone(blocks[i].Scope),
					Line:       blocks[i].StartLine,
				})
			}
			continue
		}
		pools, ok, err := ParseGlobalPoolBlock(body)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		if ok {
			for _, pool := range pools {
				pool.BlockIndex = i
				pool.SourcePath = sourcePath
				pool.Scope = slices.Clone(blocks[i].Scope)
				pool.Line = blocks[i].StartLine
				plan.Pools = append(plan.Pools, pool)
			}
			continue
		}
		db, ok, err := ParseGlobalDBBlock(body)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		if ok {
			db.BlockIndex = i
			db.SourcePath = sourcePath
			db.ScopePath = slices.Clone(blocks[i].Scope)
			db.Line = blocks[i].StartLine
			plan.DBs = append(plan.DBs, db)
			continue
		}
		skill, ok, err := ParseGlobalSkillBlock(body)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		if ok {
			skill.BlockIndex = i
			skill.SourcePath = sourcePath
			skill.Scope = slices.Clone(blocks[i].Scope)
			skill.Line = blocks[i].StartLine
			plan.Skills = append(plan.Skills, skill)
			continue
		}
		mcp, ok, err := ParseGlobalMCPBlock(body)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		if ok {
			mcp.BlockIndex = i
			mcp.SourcePath = sourcePath
			mcp.Scope = slices.Clone(blocks[i].Scope)
			mcp.Line = blocks[i].StartLine
			plan.MCPs = append(plan.MCPs, mcp)
			continue
		}
		if info, ok, err := ParseIfBlock(body); err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		} else if ok {
			plan.Controls = append(plan.Controls, ControlBlock{
				BlockIndex: i,
				Kind:       "if",
				Condition:  info.Condition,
				HeaderOnly: info.HeaderOnly,
			})
			if info.HeaderOnly {
				continue
			}
		}
		if info, ok, err := ParseElseBlock(body); err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		} else if ok {
			plan.Controls = append(plan.Controls, ControlBlock{
				BlockIndex: i,
				Kind:       "else",
				HeaderOnly: info.HeaderOnly,
			})
			if info.HeaderOnly {
				continue
			}
		}

		blockOpts := opts
		blockOpts.Context = blocks[i].Context
		globals := visibleGlobalVars(plan.Globals, scopeRef{SourcePath: sourcePath, Scope: blocks[i].Scope, Line: blocks[i].StartLine})
		globals = inheritParentTaskVars(globals, taskVarsByBlock, blocks[i])
		task, err := ParseTask(i, body, globals, blockOpts)
		if err != nil {
			diagnostics.addBlock(i, fmt.Errorf("task %d: %w", i+1, err))
			continue
		}
		task.HasParent = blocks[i].HasParent
		task.ParentIndex = blocks[i].ParentIndex
		task.SourcePath = sourcePath
		task.Scope = slices.Clone(blocks[i].Scope)
		task.Line = blocks[i].StartLine
		task.Context = blocks[i].Context
		if i+1 < len(blocks) && TaskHasFlowIf(task) {
			if elseInfo, ok, err := ParseElseBlock(blocks[i+1].Body); err != nil {
				diagnostics.addBlock(i+1, fmt.Errorf("task %d: %w", i+2, err))
				continue
			} else if ok {
				elseOpts := opts
				elseOpts.Context = blocks[i+1].Context
				var elseTask Task
				if elseInfo.HeaderOnly {
					elseTask = EmptyTask(i+1, globals)
					warnings = append(warnings, warningDiagnosticAt(sourcePath, "empty /else is a no-op; omit it for clarity", spanAtIndex(spans, i+1)))
				} else {
					elseGlobals := visibleGlobalVars(plan.Globals, scopeRef{SourcePath: sourcePath, Scope: blocks[i+1].Scope, Line: blocks[i+1].StartLine})
					elseGlobals = inheritParentTaskVars(elseGlobals, taskVarsByBlock, blocks[i+1])
					elseTask, err = ParseTask(i+1, blocks[i+1].Body, elseGlobals, elseOpts)
					if err != nil {
						diagnostics.addBlock(i+1, fmt.Errorf("task %d: %w", i+2, err))
						continue
					}
				}
				elseTask.HasParent = blocks[i+1].HasParent
				elseTask.ParentIndex = blocks[i+1].ParentIndex
				elseTask.SourcePath = sourcePath
				elseTask.Scope = slices.Clone(blocks[i+1].Scope)
				elseTask.Line = blocks[i+1].StartLine
				elseTask.Context = blocks[i+1].Context
				task, err = AttachElseTask(task, elseTask)
				if err != nil {
					diagnostics.addBlock(i+1, fmt.Errorf("task %d: %w", i+2, err))
					continue
				}
				i++
			}
		}
		plan.Tasks = append(plan.Tasks, task)
		taskVarsByBlock[task.BlockIndex] = inheritableTaskVars(task)
	}
	if err := validateProgram(sourcePath, plan, opts, blocks, spans); err != nil {
		diagnostics.add(err)
	}
	if err := diagnostics.err(); err != nil {
		return Plan{}, err
	}
	warnings = append(warnings, collectPlanWarnings(sourcePath, plan, opts, spans)...)
	plan.Diagnostics = warnings
	return plan, nil
}

type duplicateReportID struct {
	id    string
	first int
	index int
}

func duplicateATMReportIDs(blocks []Block) []duplicateReportID {
	seen := make(map[string]int)
	var duplicates []duplicateReportID
	for i, block := range blocks {
		id, ok := marker.ATMReportID(block.Body)
		if !ok {
			continue
		}
		if first, exists := seen[id]; exists {
			duplicates = append(duplicates, duplicateReportID{id: id, first: first, index: i})
			continue
		}
		seen[id] = i
	}
	return duplicates
}

func inheritParentTaskVars(globals map[string]any, taskVarsByBlock map[int]map[string]any, block Block) map[string]any {
	if !block.HasParent || block.ParentIndex < 0 {
		return globals
	}
	parentVars, ok := taskVarsByBlock[block.ParentIndex]
	if !ok {
		return globals
	}
	out := CloneVars(parentVars)
	for name, value := range globals {
		out[name] = value
	}
	return out
}

func inheritableTaskVars(task Task) map[string]any {
	vars := CloneVars(task.Vars)
	collectInheritableFlowVars(task.Flow, vars)
	return vars
}

func collectInheritableFlowVars(node FlowNode, vars map[string]any) {
	switch node.Kind {
	case FlowBash:
		if node.Bash.Name != "" {
			vars[node.Bash.Name] = "{{" + node.Bash.Name + "}}"
		}
	case FlowCall:
		if node.Call.Assign != "" {
			vars[node.Call.Assign] = "{{" + node.Call.Assign + "}}"
		}
	}
	for _, child := range node.Children {
		collectInheritableFlowVars(child, vars)
	}
	for _, child := range node.ElseChildren {
		collectInheritableFlowVars(child, vars)
	}
}

func blockDiagnosticError(source string, spans []SourceSpan, index int, err error) error {
	if index >= 0 && index < len(spans) {
		return diagnosticErrorAt(source, err, spans[index])
	}
	return diagnosticError(source, err)
}

type diagnosticCollector struct {
	source      string
	spans       []SourceSpan
	diagnostics []Diagnostic
}

func (c *diagnosticCollector) add(err error) {
	c.diagnostics = append(c.diagnostics, diagnosticsFromError(c.source, err)...)
}

func (c *diagnosticCollector) addBlock(index int, err error) {
	c.add(blockDiagnosticError(c.source, c.spans, index, err))
}

func (c diagnosticCollector) err() error {
	if len(c.diagnostics) == 0 {
		return nil
	}
	return DiagnosticError{Diagnostics: c.diagnostics}
}

func normalizeCompileOptions(sourcePath string, opts CompileOptions) CompileOptions {
	if opts.Root == "" {
		if sourcePath != "" {
			opts.Root = filepath.Dir(sourcePath)
		} else {
			opts.Root = "."
		}
	}
	return opts
}
