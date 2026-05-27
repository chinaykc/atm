package compiler

import (
	"fmt"
	exprpkg "github.com/chinaykc/atm/pkg/lang/expr"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

func validateProgram(source string, plan Plan, opts CompileOptions, blocks []Block, spans []SourceSpan) error {
	symbols, err := BuildSymbolTable(plan)
	if err != nil {
		return diagnosticError(source, err)
	}
	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, validateConditionalBlockStructure(source, blocks, spans)...)
	for _, control := range plan.Controls {
		if err := validateConditionExpression(fmt.Sprintf("block %d /%s", control.BlockIndex+1, control.Kind), control.Condition); err != nil {
			diagnostics = append(diagnostics, diagnosticsFromError(source, blockDiagnosticError(source, spans, control.BlockIndex, err))...)
		}
	}
	for i, task := range plan.Tasks {
		if task.Return != nil {
			diagnostics = append(diagnostics, diagnosticsFromError(source, blockDiagnosticError(source, spans, task.BlockIndex, fmt.Errorf("task %d: /return is only allowed inside /def", i+1)))...)
			continue
		}
		if err := validateTaskIR(fmt.Sprintf("task %d", i+1), task, symbols, nil, definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}); err != nil {
			diagnostics = append(diagnostics, diagnosticsFromError(source, blockDiagnosticError(source, spans, task.BlockIndex, err))...)
		}
	}
	if err := validateDefinitions(plan, symbols, opts); err != nil {
		diagnostics = append(diagnostics, diagnosticsFromError(source, err)...)
	}
	if len(diagnostics) > 0 {
		return DiagnosticError{Diagnostics: diagnostics}
	}
	return nil
}

func validateConditionalBlockStructure(source string, blocks []Block, spans []SourceSpan) []Diagnostic {
	var diagnostics []Diagnostic
	for i := 0; i < len(blocks); i++ {
		if _, ok, err := ParseIfBlock(blocks[i].Body); err != nil {
			diagnostics = append(diagnostics, diagnosticsFromError(source, blockDiagnosticError(source, spans, i, err))...)
		} else if ok {
			if _, errIndex, err := conditionalGroupEnd(blocks, i); err != nil {
				diagnostics = append(diagnostics, diagnosticsFromError(source, blockDiagnosticError(source, spans, errIndex, err))...)
			}
		}
	}
	return diagnostics
}

func conditionalGroupEnd(blocks []Block, index int) (int, int, error) {
	if index < 0 || index >= len(blocks) {
		return index, closestBlockIndex(blocks, index), fmt.Errorf("conditional branch is missing a task block")
	}
	ifBlock, ok, err := ParseIfBlock(blocks[index].Body)
	if err != nil || !ok {
		return index + 1, index, err
	}
	if !ifBlock.HeaderOnly {
		if index+1 < len(blocks) && !marker.IsDone(blocks[index+1].Body) {
			elseBlock, ok, err := ParseElseBlock(blocks[index+1].Body)
			if err != nil {
				return index + 1, index + 1, err
			}
			if ok && elseBlock.HeaderOnly {
				if index+2 >= len(blocks) {
					return index + 2, index + 1, nil
				}
				return conditionalNodeEnd(blocks, index+2)
			}
		}
		return index + 1, index, nil
	}

	thenEnd, errIndex, err := conditionalNodeEnd(blocks, index+1)
	if err != nil {
		return thenEnd, errIndex, err
	}
	if thenEnd >= len(blocks) {
		return thenEnd, closestBlockIndex(blocks, thenEnd), fmt.Errorf("header-only /if requires a matching /else")
	}
	elseBlock, ok, err := ParseElseBlock(blocks[thenEnd].Body)
	if err != nil {
		return thenEnd, thenEnd, err
	}
	if !ok {
		return thenEnd, thenEnd, fmt.Errorf("header-only /if requires a matching /else")
	}
	if elseBlock.HeaderOnly {
		if thenEnd+1 >= len(blocks) {
			return thenEnd + 1, thenEnd, nil
		}
		return conditionalNodeEnd(blocks, thenEnd+1)
	}
	return thenEnd + 1, thenEnd, nil
}

func conditionalNodeEnd(blocks []Block, index int) (int, int, error) {
	if index >= len(blocks) {
		return index, closestBlockIndex(blocks, index), fmt.Errorf("conditional branch is missing a task block")
	}
	if marker.IsDone(blocks[index].Body) {
		return index + 1, index, nil
	}
	if _, ok, err := ParseElseBlock(blocks[index].Body); err != nil {
		return index, index, err
	} else if ok {
		return index, index, fmt.Errorf("/else appears before a branch body")
	}
	if _, ok, err := ParseIfBlock(blocks[index].Body); err != nil {
		return index, index, err
	} else if ok {
		return index, index, fmt.Errorf("nested /if is not supported; wrap complex branches in /def and /call it")
	}
	return index + 1, index, nil
}

func closestBlockIndex(blocks []Block, index int) int {
	if len(blocks) == 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= len(blocks) {
		return len(blocks) - 1
	}
	return index
}

func collectPlanWarnings(source string, plan Plan, opts CompileOptions, spans []SourceSpan) []Diagnostic {
	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, collectUnwaitedGoWarnings(source, plan.Tasks, spans)...)
	diagnostics = append(diagnostics, collectLazyProviderWarnings(source, plan, spans)...)
	diagnostics = append(diagnostics, collectDefinitionHeadingWarnings(plan.Definitions)...)
	diagnostics = append(diagnostics, collectDefinitionUnwaitedGoWarnings(plan, opts)...)
	diagnostics = append(diagnostics, collectDefinitionLazyProviderWarnings(plan, opts)...)
	return diagnostics
}

func collectDefinitionHeadingWarnings(definitions []Definition) []Diagnostic {
	var diagnostics []Diagnostic
	for _, def := range definitions {
		for i, block := range def.Blocks {
			if span, ok := firstMarkdownHeadingSpan(block); ok {
				diagnostics = append(diagnostics, warningDiagnosticAt(def.SourcePath, fmt.Sprintf("definition %s block %d contains a Markdown heading; headings inside /def are prompt text and do not end the definition", def.Name, i+1), span))
			}
		}
	}
	return diagnostics
}

func firstMarkdownHeadingSpan(block DefinitionBlock) (SourceSpan, bool) {
	fence := outputFenceInfo{}
	for i, line := range SplitLines(block.Body) {
		if fence.marker != "" {
			if isFenceClose(line, fence) {
				fence = outputFenceInfo{}
			}
			continue
		}
		if nextFence, ok := parseAnyFenceStart(line); ok {
			fence = nextFence
			continue
		}
		if _, _, ok := parseMarkdownHeading(line); !ok {
			continue
		}
		span := block.Span
		if span.Line > 0 {
			span.Line += i
			span.Column = 1
		}
		return span, true
	}
	return SourceSpan{}, false
}

func collectLazyProviderWarnings(source string, plan Plan, spans []SourceSpan) []Diagnostic {
	var diagnostics []Diagnostic
	for _, binding := range plan.Globals {
		if strings.TrimSpace(binding.BashScript) == "" {
			continue
		}
		diagnostics = append(diagnostics, warningDiagnosticAt(source, fmt.Sprintf("global /let %s /bash is a lazy provider with possible side effects; plan/check will not execute it unless plan --preview is used", binding.Name), spanAtIndex(spans, binding.BlockIndex)))
	}
	for _, task := range plan.Tasks {
		diagnostics = append(diagnostics, collectFlowLazyProviderWarnings(source, fmt.Sprintf("task %d", task.BlockIndex+1), task.Flow, spanAtIndex(spans, task.BlockIndex))...)
	}
	return diagnostics
}

func collectFlowLazyProviderWarnings(source, label string, node FlowNode, span SourceSpan) []Diagnostic {
	var diagnostics []Diagnostic
	switch node.Kind {
	case FlowBash:
		if node.Bash.Name != "" {
			diagnostics = append(diagnostics, warningDiagnosticAt(source, fmt.Sprintf("%s /let %s /bash is a lazy provider with possible side effects; plan/check will not execute it unless plan --preview is used", label, node.Bash.Name), span))
		}
	case FlowCall:
		if node.Call.Assign != "" {
			diagnostics = append(diagnostics, warningDiagnosticAt(source, fmt.Sprintf("%s /let %s /call %s is a lazy definition provider; static plan/check record it as a render-time dependency, and plan --preview may execute it when it can return without running an agent", label, node.Call.Assign, node.Call.Name), span))
		}
	case FlowReturn:
		if node.Return.Kind == ReturnBash {
			diagnostics = append(diagnostics, warningDiagnosticAt(source, fmt.Sprintf("%s /return /bash is a value provider with possible side effects; plan/check will not execute it", label), span))
		}
	}
	for _, child := range node.Children {
		diagnostics = append(diagnostics, collectFlowLazyProviderWarnings(source, label, child, span)...)
	}
	for _, child := range node.ElseChildren {
		diagnostics = append(diagnostics, collectFlowLazyProviderWarnings(source, label, child, span)...)
	}
	return diagnostics
}

func collectDefinitionLazyProviderWarnings(plan Plan, opts CompileOptions) []Diagnostic {
	var diagnostics []Diagnostic
	for _, def := range plan.Definitions {
		vars := visibleGlobalVars(plan.Globals, scopeRef{SourcePath: def.SourcePath, Scope: def.Scope, Line: def.Line})
		for _, param := range def.Params {
			vars[param] = "{{" + param + "}}"
		}
		for i, block := range def.Blocks {
			if bindings, ok, _ := ParseGlobalLetBlock(block.Body); ok {
				for _, binding := range bindings {
					if binding.BashScript != "" {
						diagnostics = append(diagnostics, warningDiagnosticAt(def.SourcePath, fmt.Sprintf("definition %s block %d /let %s /bash is a lazy provider with possible side effects; plan/check will not execute it unless plan --preview is used", def.Name, i+1, binding.Name), block.Span))
						vars[binding.Name] = "{{" + binding.Name + "}}"
					} else {
						vars[binding.Name] = binding.Value
					}
				}
				continue
			}
			task, err := ParseTaskForFile(def.SourcePath, def.BlockIndex, block.Body, vars, normalizeCompileOptions(def.SourcePath, opts))
			if err != nil {
				continue
			}
			diagnostics = append(diagnostics, collectFlowLazyProviderWarnings(def.SourcePath, fmt.Sprintf("definition %s block %d", def.Name, i+1), task.Flow, block.Span)...)
			vars = task.Vars
		}
	}
	return diagnostics
}

func spanAtIndex(spans []SourceSpan, index int) SourceSpan {
	if index >= 0 && index < len(spans) {
		return spans[index]
	}
	return SourceSpan{}
}

type pendingGoWarning struct {
	pool  string
	span  SourceSpan
	label string
}

func collectUnwaitedGoWarnings(source string, tasks []Task, spans []SourceSpan) []Diagnostic {
	var pending []pendingGoWarning
	for _, task := range tasks {
		span := SourceSpan{}
		if task.BlockIndex >= 0 && task.BlockIndex < len(spans) {
			span = spans[task.BlockIndex]
		}
		pending = updatePendingGoWarnings(pending, FlattenTaskFlow(task), span, fmt.Sprintf("task %d", task.BlockIndex+1))
	}
	return diagnosticsFromPendingGo(source, pending)
}

func collectDefinitionUnwaitedGoWarnings(plan Plan, opts CompileOptions) []Diagnostic {
	var diagnostics []Diagnostic
	for _, def := range plan.Definitions {
		vars := visibleGlobalVars(plan.Globals, scopeRef{SourcePath: def.SourcePath, Scope: def.Scope, Line: def.Line})
		for _, param := range def.Params {
			vars[param] = "{{" + param + "}}"
		}
		var pending []pendingGoWarning
		for i, block := range def.Blocks {
			if _, ok, _ := ParseGlobalPoolBlock(block.Body); ok {
				continue
			}
			if bindings, ok, _ := ParseGlobalLetBlock(block.Body); ok {
				for _, binding := range bindings {
					vars[binding.Name] = binding.Value
					if binding.BashScript != "" {
						vars[binding.Name] = "{{" + binding.Name + "}}"
					}
				}
				continue
			}
			task, err := ParseTaskForFile(def.SourcePath, def.BlockIndex, block.Body, vars, normalizeCompileOptions(def.SourcePath, opts))
			if err != nil {
				continue
			}
			pending = updatePendingGoWarnings(pending, FlattenTaskFlow(task), block.Span, fmt.Sprintf("definition %s block %d", def.Name, i+1))
		}
		diagnostics = append(diagnostics, diagnosticsFromPendingGo(def.SourcePath, pending)...)
	}
	return diagnostics
}

func updatePendingGoWarnings(pending []pendingGoWarning, ops []FlatOp, span SourceSpan, label string) []pendingGoWarning {
	for _, op := range ops {
		switch op.Kind {
		case FlatOpGo:
			pending = append(pending, pendingGoWarning{pool: op.Pool, span: span, label: label})
		case FlatOpWait:
			if op.Pool == "" {
				pending = nil
				continue
			}
			pending = filterPendingGoWarnings(pending, op.Pool)
		}
	}
	return pending
}

func filterPendingGoWarnings(pending []pendingGoWarning, pool string) []pendingGoWarning {
	out := pending[:0]
	for _, item := range pending {
		if item.pool != pool {
			out = append(out, item)
		}
	}
	return out
}

func diagnosticsFromPendingGo(source string, pending []pendingGoWarning) []Diagnostic {
	var diagnostics []Diagnostic
	for _, item := range pending {
		target := "default pool"
		if item.pool != "" {
			target = fmt.Sprintf("pool %q", item.pool)
		}
		diagnostics = append(diagnostics, warningDiagnosticAt(source, fmt.Sprintf("%s starts background work in %s without a later /wait", item.label, target), item.span))
	}
	return diagnostics
}

func validateDefinitions(plan Plan, symbols SymbolTable, opts CompileOptions) error {
	var diagnostics []Diagnostic
	for _, def := range plan.Definitions {
		vars := visibleGlobalVars(plan.Globals, scopeRef{SourcePath: def.SourcePath, Scope: def.Scope, Line: def.Line})
		for _, param := range def.Params {
			vars[param] = "{{" + param + "}}"
		}
		localPools := make(map[string]PoolDecl)
		returnCount := 0
		sawReturnSyntax := false
		for i, block := range def.Blocks {
			body := block.Body
			label := fmt.Sprintf("definition %s block %d", def.Name, i+1)
			if blockHasReturnCommand(body) {
				sawReturnSyntax = true
			}
			if returnCount > 0 {
				diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("definition %s: /return must be the final definition block", def.Name)))...)
				continue
			}
			if pools, ok, err := ParseGlobalPoolBlock(body); err != nil {
				diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("%s: %w", label, err)))...)
				continue
			} else if ok {
				for _, pool := range pools {
					if existing, exists := localPools[pool.Name]; exists {
						diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("%s: pool %q already declared in definition block %d", label, pool.Name, existing.BlockIndex+1)))...)
						continue
					}
					pool.BlockIndex = i
					localPools[pool.Name] = pool
				}
				continue
			}
			if bindings, ok, err := ParseGlobalLetBlock(body); err != nil {
				diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("%s: %w", label, err)))...)
				continue
			} else if ok {
				for _, binding := range bindings {
					vars[binding.Name] = binding.Value
					if binding.BashScript != "" {
						vars[binding.Name] = "{{" + binding.Name + "}}"
					}
				}
				continue
			}
			defOpts := normalizeCompileOptions(def.SourcePath, opts)
			task, err := ParseTaskForFile(def.SourcePath, def.BlockIndex, body, vars, defOpts)
			if err != nil {
				diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("%s: %w", label, err)))...)
				continue
			}
			if err := validateTaskIR(label, task, symbols, localPools, definitionScopeRef{SourcePath: def.SourcePath, Scope: def.Scope, Line: def.Line}); err != nil {
				diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, err))...)
			}
			if task.Return != nil {
				returnCount++
				if returnCount > 1 {
					diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("definition %s: /return can only appear once", def.Name)))...)
				}
				if i != len(def.Blocks)-1 {
					diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, i, fmt.Errorf("definition %s: /return must be the final definition block", def.Name)))...)
				}
			}
		}
		if returnCount == 0 && !sawReturnSyntax {
			diagnostics = append(diagnostics, diagnosticsFromError(def.SourcePath, definitionDiagnosticError(def, len(def.Blocks)-1, fmt.Errorf("definition %s requires /return", def.Name)))...)
		}
	}
	if len(diagnostics) > 0 {
		return DiagnosticError{Diagnostics: diagnostics}
	}
	return nil
}

func blockHasReturnCommand(body string) bool {
	for _, line := range SplitLines(body) {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > 0 && fields[0] == "/return" {
			return true
		}
	}
	return false
}

func definitionDiagnosticError(def Definition, blockIndex int, err error) error {
	if err == nil {
		return nil
	}
	source := def.SourcePath
	if source == "" {
		source = "definition " + def.Name
	}
	if blockIndex >= 0 && blockIndex < len(def.Blocks) && def.Blocks[blockIndex].Span.Line > 0 {
		return diagnosticErrorAt(source, err, def.Blocks[blockIndex].Span)
	}
	return diagnosticError(source, err)
}

type definitionScopeRef struct {
	SourcePath string
	Scope      []string
	Line       int
}

func validateTaskIR(label string, task Task, symbols SymbolTable, localPools map[string]PoolDecl, ref definitionScopeRef) error {
	if err := validateTaskResources(label, task, symbols, ref); err != nil {
		return err
	}
	for _, name := range task.Webhook.Use {
		if _, ok := symbols.Webhooks[name]; !ok {
			return fmt.Errorf("%s: unknown webhook %q", label, name)
		}
	}
	return validateFlowNode(label, task.Flow, symbols, localPools, ref, false)
}

func validateFlowNode(label string, node FlowNode, symbols SymbolTable, localPools map[string]PoolDecl, ref definitionScopeRef, insideIf bool) error {
	switch node.Kind {
	case FlowFor:
		if err := validateConditionExpression(label+" /for source", node.For.Source); err != nil {
			return err
		}
		if err := validateConditionExpression(label+" /for until", node.For.Condition); err != nil {
			return err
		}
		if err := validateConditionCall(label+" /for source", node.For.Source, symbols, ref); err != nil {
			return err
		}
	case FlowIf:
		if insideIf {
			return fmt.Errorf("%s: nested /if is not supported; wrap complex branches in /def and /call it", label)
		}
		if err := validateConditionExpression(label+" /if", node.If.Condition); err != nil {
			return err
		}
	case FlowGo:
		if err := validatePoolReference(label+" /go", node.Pool, symbols, localPools, ref); err != nil {
			return err
		}
	case FlowWait:
		if err := validatePoolReference(label+" /wait", node.Pool, symbols, localPools, ref); err != nil {
			return err
		}
	case FlowCall:
		if err := validateCall(label, node.Call, symbols, ref); err != nil {
			return err
		}
	case FlowWebhook:
		if _, ok := symbols.Webhooks[node.Webhook.Name]; !ok {
			return fmt.Errorf("%s: unknown webhook %q", label, node.Webhook.Name)
		}
	}
	childInsideIf := insideIf || node.Kind == FlowIf
	for _, child := range node.Children {
		if err := validateFlowNode(label, child, symbols, localPools, ref, childInsideIf); err != nil {
			return err
		}
	}
	for _, child := range node.ElseChildren {
		if err := validateFlowNode(label, child, symbols, localPools, ref, childInsideIf); err != nil {
			return err
		}
	}
	return nil
}

func validateConditionExpression(label string, condition Condition) error {
	if condition.Kind != ConditionExpr {
		return nil
	}
	if err := exprpkg.ValidateSyntax(condition.Text); err != nil {
		return fmt.Errorf("%s expression: %w", label, err)
	}
	return nil
}

func validateConditionCall(label string, condition Condition, symbols SymbolTable, ref definitionScopeRef) error {
	if condition.Kind != ConditionCall {
		return nil
	}
	call, err := ParseCallExpression(condition.Text)
	if err != nil {
		return fmt.Errorf("%s call: %w", label, err)
	}
	return validateCall(label, call, symbols, ref)
}

func validateCall(label string, call Call, symbols SymbolTable, ref definitionScopeRef) error {
	def, ok := resolveVisibleDefinition(symbols.DefinitionItems, call.Name, ref)
	if !ok {
		return fmt.Errorf("%s: unknown definition %q", label, call.Name)
	}
	if len(call.Args) != len(def.Params) {
		return fmt.Errorf("%s: /call %s expects %d argument(s), got %d", label, call.Name, len(def.Params), len(call.Args))
	}
	return nil
}

func resolveVisibleDefinition(definitions []Definition, name string, ref definitionScopeRef) (Definition, bool) {
	var best Definition
	found := false
	bestDepth := -1
	bestLine := -1
	for _, def := range definitions {
		if def.Name != name {
			continue
		}
		if !definitionVisibleAt(def, ref) {
			continue
		}
		depth := len(def.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && def.Line > bestLine) {
			best = def
			found = true
			bestDepth = depth
			bestLine = def.Line
		}
	}
	return best, found
}

func definitionVisibleAt(def Definition, ref definitionScopeRef) bool {
	sourcePath := def.VisibleSourcePath
	scope := def.VisibleScope
	line := def.VisibleLine
	if def.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(def.SourcePath) == filepath.Clean(ref.SourcePath) {
		sourcePath = def.SourcePath
		scope = def.Scope
		line = def.Line
	}
	if sourcePath != "" && ref.SourcePath != "" && filepath.Clean(sourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && line > 0 && line >= ref.Line {
		return false
	}
	if len(scope) > len(ref.Scope) {
		return false
	}
	for i := range scope {
		if scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func validatePoolReference(label, pool string, symbols SymbolTable, localPools map[string]PoolDecl, ref definitionScopeRef) error {
	if pool == "" {
		return nil
	}
	if localPools != nil {
		if _, ok := localPools[pool]; ok {
			return nil
		}
	}
	if _, ok := resolveVisiblePool(symbols.PoolItems, pool, ref); !ok {
		return fmt.Errorf("%s references unknown pool %q", label, pool)
	}
	return nil
}

func resolveVisiblePool(pools []PoolDecl, name string, ref definitionScopeRef) (PoolDecl, bool) {
	var best PoolDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, pool := range pools {
		if pool.Name != name || !poolVisibleAt(pool, ref) {
			continue
		}
		depth := len(pool.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && pool.Line > bestLine) {
			best = pool
			found = true
			bestDepth = depth
			bestLine = pool.Line
		}
	}
	return best, found
}

func poolVisibleAt(pool PoolDecl, ref definitionScopeRef) bool {
	if pool.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(pool.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && pool.Line > 0 && pool.Line >= ref.Line {
		return false
	}
	if len(pool.Scope) > len(ref.Scope) {
		return false
	}
	for i := range pool.Scope {
		if pool.Scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func validateTaskResources(label string, task Task, symbols SymbolTable, ref definitionScopeRef) error {
	if err := validateDBTaskReferences(label, task.DB, symbols.DBItems, ref); err != nil {
		return err
	}
	if err := validateSkillTaskReferences(label, task.Skill, symbols.SkillItems, ref); err != nil {
		return err
	}
	return validateMCPTaskReferences(label, task.MCP, symbols, ref)
}

func validateDBTaskReferences(label string, config DBTaskConfig, dbs []DBDecl, ref definitionScopeRef) error {
	if config.IgnoreAll {
		return nil
	}
	visible := make(map[string]DBDecl)
	decls := make(map[string]DBDecl)
	for _, db := range dbs {
		if !dbVisibleAt(db, ref) {
			continue
		}
		decls[db.Name] = db
		if db.Scope == DBScopeGlobal {
			visible[db.Name] = db
		}
	}
	for _, use := range config.Use {
		for _, name := range use.Names {
			decl, ok := decls[name]
			if !ok {
				return fmt.Errorf("%s: unknown db %q", label, name)
			}
			if use.Access != "" && !dbAccessAllowed(use.Access, decl.Access) {
				return fmt.Errorf("%s: /db use %s access %s exceeds declared access %s", label, name, use.Access, decl.Access)
			}
			visible[name] = decl
		}
	}
	for _, rule := range config.Access {
		for _, name := range expandVisibleDBNames(rule.Names, visible) {
			decl, ok := visible[name]
			if !ok {
				return fmt.Errorf("%s: /db access references unavailable db %q", label, name)
			}
			if !dbAccessAllowed(rule.Access, decl.Access) {
				return fmt.Errorf("%s: /db access %s %s exceeds declared access %s", label, name, rule.Access, decl.Access)
			}
		}
	}
	return nil
}

func dbVisibleAt(db DBDecl, ref definitionScopeRef) bool {
	if db.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(db.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && db.Line > 0 && db.Line >= ref.Line {
		return false
	}
	if len(db.ScopePath) > len(ref.Scope) {
		return false
	}
	for i := range db.ScopePath {
		if db.ScopePath[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func expandVisibleDBNames(names []string, visible map[string]DBDecl) []string {
	for _, name := range names {
		if name == "*" {
			return slices.Sorted(maps.Keys(visible))
		}
	}
	return slices.Clone(names)
}

func dbAccessAllowed(requested, max DBAccess) bool {
	return dbAccessRank(requested) <= dbAccessRank(max)
}

func dbAccessRank(access DBAccess) int {
	switch access {
	case DBAccessRead:
		return 1
	case DBAccessAppend:
		return 2
	case DBAccessWrite:
		return 3
	case DBAccessAdmin:
		return 4
	default:
		return 0
	}
}

func validateSkillTaskReferences(label string, config SkillTaskConfig, skills []SkillDecl, ref definitionScopeRef) error {
	if config.IgnoreAll {
		return nil
	}
	for _, item := range config.Use {
		if _, ok := resolveVisibleSkill(skills, item, ref); ok {
			continue
		}
		if looksLikePath(item) {
			continue
		}
		return fmt.Errorf("%s: unknown skill %q", label, item)
	}
	return nil
}

func validateMCPTaskReferences(label string, config MCPTaskConfig, symbols SymbolTable, ref definitionScopeRef) error {
	if config.IgnoreAll {
		return nil
	}
	for _, name := range config.Use {
		if isBuiltinMCPName(name) {
			return fmt.Errorf("%s: mcp name %q conflicts with an ATM builtin MCP server", label, name)
		}
		if _, ok := resolveVisibleMCP(symbols.MCPItems, name, ref); !ok {
			return fmt.Errorf("%s: unknown mcp %q", label, name)
		}
	}
	for _, name := range config.DefUse {
		if _, ok := resolveVisibleDefinition(symbols.DefinitionItems, name, ref); !ok {
			return fmt.Errorf("%s: unknown definition %q", label, name)
		}
	}
	return nil
}

func resolveVisibleSkill(skills []SkillDecl, name string, ref definitionScopeRef) (SkillDecl, bool) {
	var best SkillDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, skill := range skills {
		if skill.Name != name || !skillVisibleAt(skill, ref) {
			continue
		}
		depth := len(skill.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && skill.Line > bestLine) {
			best = skill
			found = true
			bestDepth = depth
			bestLine = skill.Line
		}
	}
	return best, found
}

func skillVisibleAt(skill SkillDecl, ref definitionScopeRef) bool {
	if skill.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(skill.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && skill.Line > 0 && skill.Line >= ref.Line {
		return false
	}
	if len(skill.Scope) > len(ref.Scope) {
		return false
	}
	for i := range skill.Scope {
		if skill.Scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func resolveVisibleMCP(mcps []MCPDecl, name string, ref definitionScopeRef) (MCPDecl, bool) {
	var best MCPDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, mcp := range mcps {
		if mcp.Name != name || !mcpVisibleAt(mcp, ref) {
			continue
		}
		depth := len(mcp.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && mcp.Line > bestLine) {
			best = mcp
			found = true
			bestDepth = depth
			bestLine = mcp.Line
		}
	}
	return best, found
}

func mcpVisibleAt(mcp MCPDecl, ref definitionScopeRef) bool {
	if mcp.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(mcp.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	if ref.Line > 0 && mcp.Line > 0 && mcp.Line >= ref.Line {
		return false
	}
	if len(mcp.Scope) > len(ref.Scope) {
		return false
	}
	for i := range mcp.Scope {
		if mcp.Scope[i] != ref.Scope[i] {
			return false
		}
	}
	return true
}

func looksLikePath(value string) bool {
	return filepath.Base(value) != value || value == "." || value == ".."
}

func isBuiltinMCPName(name string) bool {
	switch name {
	case "atm_check", "atm_output", "atm_db", "atm_defs":
		return true
	default:
		return false
	}
}
