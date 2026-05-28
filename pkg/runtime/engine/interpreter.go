package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/expr"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"github.com/chinaykc/atm/pkg/runtime/store"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	templateVarRef      = regexp.MustCompile(`{{[^}]*}}`)
	templateQuotedVar   = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	templateLegacyVar   = regexp.MustCompile(`{{[ \t]*([A-Za-z_][A-Za-z0-9_-]*)[ \t]*}}`)
	templateLegacyField = regexp.MustCompile(`{{[ \t]*([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)[ \t]*}}`)
	exprIdentifierToken = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_-]*\b`)
)

type execContext struct {
	vars       map[string]any
	options    compiler.RunOptions
	defRef     definitionScopeRef
	waitLimit  int
	loopOp     int
	loopRun    int
	agent      string
	agentID    int
	background bool
	poolPrefix string
	callState  *callState
}

type definitionScopeRef struct {
	SourcePath string
	Scope      []string
	Line       int
}

type taskExecution struct {
	engine *Engine
	task   compiler.Task
	lease  store.BlockLease
	start  time.Time
	runs   int
	pool   string

	stdout       io.Writer
	stderr       io.Writer
	file         interface{ Close() error }
	logPath      string
	taskDir      string
	reportID     string
	reportSource string
	reportPath   string
	orphan       bool

	branches           *branchCollector
	branchSeq          int
	messages           []compiler.OutputMessage
	structuredOutputs  [][]byte
	eventPaths         []string
	renderedPromptHash string
	planHash           string
	returnValue        any
	returnSet          bool
	writeState         bool
	mu                 sync.Mutex
}

type lazyProviderKind string

const (
	lazyProviderBash lazyProviderKind = "bash"
	lazyProviderCall lazyProviderKind = "call"
)

type lazyProvider struct {
	kind    lazyProviderKind
	bash    compiler.BashCommand
	call    compiler.Call
	vars    map[string]any
	options compiler.RunOptions
	defRef  definitionScopeRef

	mu        sync.Mutex
	resolved  bool
	resolving bool
	value     any
}

type callState struct {
	mu       sync.Mutex
	messages []compiler.OutputMessage
	outputs  [][]byte
}

func (s *callState) addMessages(messages []compiler.OutputMessage) {
	if s == nil || len(messages) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, messages...)
}

func (s *callState) addOutput(output []byte) {
	if s == nil || len(output) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputs = append(s.outputs, slices.Clone(output))
}

func (s *callState) snapshotMessages() []compiler.OutputMessage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]compiler.OutputMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

type branchCollector struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	runs int
	err  error
}

func (e *Engine) runTask(ctx context.Context, lease store.BlockLease, task compiler.Task, options compiler.RunOptions) error {
	exec := &taskExecution{
		engine:     e,
		task:       task,
		lease:      lease,
		start:      time.Now(),
		branches:   &branchCollector{},
		writeState: true,
	}
	if task.Cursor.Active {
		exec.start = task.Cursor.Start
		exec.runs = task.Cursor.TotalRuns
		if id, _, _, err := store.LeaseReportIdentity(e.filePath, exec.lease); err == nil {
			source := exec.sourcePromptHash()
			exec.setReportIdentity(id, source, e.taskReportPath(task.BlockIndex, id))
		}
	} else if exec.writeState && !isWaitOnly(task) {
		updated, err := store.SaveRunning(e.filePath, exec.lease, compiler.RunningInfo{Active: true, Start: exec.start, Source: exec.sourcePromptHash()})
		if err != nil {
			return err
		}
		exec.lease = updated
		if id, _, _, err := store.LeaseReportIdentity(e.filePath, exec.lease); err != nil {
			return err
		} else {
			source := exec.sourcePromptHash()
			report := e.taskReportPath(task.BlockIndex, id)
			exec.setReportIdentity(id, source, report)
		}
	}
	exec.taskDir = e.taskArtifactDir(task.BlockIndex, exec.reportID)
	stdout, stderr, file, path, err := e.taskWriters(task.BlockIndex, exec.taskDir)
	if err != nil {
		return err
	}
	exec.stdout = stdout
	exec.stderr = stderr
	exec.file = file
	exec.logPath = path
	writeATMEvent(stderr, "log", "task %d %s", task.BlockIndex+1, path)
	if exec.writeState && !isWaitOnly(task) && exec.reportID != "" {
		if err := e.updateTaskState(task.BlockIndex, taskStateUpdate{
			ID:               exec.reportID,
			Status:           "running",
			SourcePromptHash: exec.reportSource,
			StartedAt:        exec.start,
			UpdatedAt:        time.Now(),
			Runs:             exec.runs,
			Report:           exec.reportPath,
			Logs:             []string{path},
		}); err != nil {
			_ = file.Close()
			return err
		}
	}

	base := execContext{
		vars:    ir.CloneVars(task.Vars),
		loopOp:  -1,
		options: options,
		defRef:  definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line},
	}
	_, _, background, err := exec.executeFlow(ctx, base, task.Flow, 0, true)
	if err != nil {
		if !errors.Is(err, store.ErrObsolete) {
			_ = exec.markFailed(err)
		}
		_ = file.Close()
		return err
	}
	if background {
		key := inFlightKey{index: lease.Index, hash: lease.BodyHash}
		asyncTask := e.async.register(key, exec.pool, path)
		go func() {
			err := exec.waitBranchesAndMarkDone()
			_ = file.Close()
			e.async.complete(asyncTask, err)
		}()
		return nil
	}
	defer file.Close()
	return exec.markDone()
}

func isWaitOnly(task compiler.Task) bool {
	if strings.TrimSpace(task.Prompt) != "" {
		return false
	}
	return flowIsWaitOnly(task.Flow)
}

func (x *taskExecution) waitBranchesAndMarkDone() error {
	x.branches.wg.Wait()
	x.branches.mu.Lock()
	err := x.branches.err
	x.branches.mu.Unlock()
	if err != nil {
		if !errors.Is(err, store.ErrObsolete) {
			_ = x.markFailed(err)
		}
		return err
	}
	return x.markDone()
}

func (x *taskExecution) setReportIdentity(id, source, report string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.reportID = id
	x.reportSource = source
	x.reportPath = report
	if x.taskDir == "" {
		x.taskDir = x.engine.taskArtifactDir(x.task.BlockIndex, id)
	}
}

func (x *taskExecution) sourcePromptHash() string {
	if strings.TrimSpace(x.task.SourcePromptHash) != "" {
		return x.task.SourcePromptHash
	}
	return hashStateText(x.task.Prompt)
}

func (x *taskExecution) reportIdentity() (id, source, report string, orphan bool, err error) {
	id, _, _, err = store.LeaseReportIdentity(x.engine.filePath, x.lease)
	if err == nil {
		source = x.sourcePromptHash()
		report = x.engine.taskReportPath(x.task.BlockIndex, id)
		x.setReportIdentity(id, source, report)
		return id, source, report, false, nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !errors.Is(err, store.ErrObsolete) && x.reportID == "" {
		return "", "", "", false, err
	}
	x.orphan = true
	if x.reportID == "" {
		return "", "", "", true, err
	}
	return x.reportID, x.reportSource, x.reportPath, true, nil
}

func (x *taskExecution) reportIdentityLocked() (id, source, report string, orphan bool, err error) {
	id, _, _, err = store.LeaseReportIdentity(x.engine.filePath, x.lease)
	if err == nil {
		source = x.sourcePromptHash()
		report = x.engine.taskReportPath(x.task.BlockIndex, id)
		x.reportID = id
		x.reportSource = source
		x.reportPath = report
		if x.taskDir == "" {
			x.taskDir = x.engine.taskArtifactDir(x.task.BlockIndex, id)
		}
		return id, source, report, false, nil
	}
	if !errors.Is(err, store.ErrObsolete) && x.reportID == "" {
		return "", "", "", false, err
	}
	x.orphan = true
	if x.reportID == "" {
		return "", "", "", true, err
	}
	return x.reportID, x.reportSource, x.reportPath, true, nil
}

func (x *taskExecution) markDone() error {
	if !x.writeState || x.engine.isAbandoningBackground() {
		return nil
	}
	x.mu.Lock()
	runs := x.runs
	messages := x.recentMessagesLocked()
	renderedPromptHash := x.renderedPromptHash
	planHash := x.planHash
	x.mu.Unlock()
	end := time.Now()
	id, source, report, orphan, err := x.reportIdentity()
	if err != nil {
		return err
	}
	info := compiler.DoneInfo{Start: x.start, End: end, Runs: runs, ID: id, Source: source, Rendered: renderedPromptHash, Report: report, Messages: messages}
	if err := x.engine.writeDetailReport(x.task.BlockIndex, "done", detailReportInfo{
		ID:                 info.ID,
		Source:             info.Source,
		RenderedPromptHash: renderedPromptHash,
		PlanHash:           planHash,
		Report:             info.Report,
		Start:              info.Start,
		End:                info.End,
		Runs:               info.Runs,
		Messages:           info.Messages,
		Orphan:             orphan,
	}); err != nil {
		return err
	}
	if orphan {
		writeATMEvent(x.stderr, "orphan", "task %d completed after its todo block changed or disappeared; detail report %s", x.task.BlockIndex+1, report)
	} else {
		if err := store.MarkDone(x.engine.filePath, x.lease, info); err != nil {
			return err
		}
	}
	return x.engine.updateTaskState(x.task.BlockIndex, taskStateUpdate{
		ID:                 id,
		Status:             "done",
		SourcePromptHash:   source,
		RenderedPromptHash: renderedPromptHash,
		PlanHash:           planHash,
		StartedAt:          x.start,
		UpdatedAt:          end,
		Runs:               runs,
		Report:             report,
		Logs:               x.stateLogPaths(),
		Orphan:             orphan,
	})
}

func (x *taskExecution) markFailed(cause error) error {
	if !x.writeState || x.engine.isAbandoningBackground() {
		return nil
	}
	x.mu.Lock()
	runs := x.runs
	messages := x.recentMessagesLocked()
	renderedPromptHash := x.renderedPromptHash
	planHash := x.planHash
	x.mu.Unlock()
	end := time.Now()
	id, source, report, orphan, err := x.reportIdentity()
	if err != nil {
		return err
	}
	info := compiler.FailedInfo{
		Start:    x.start,
		End:      end,
		Runs:     runs,
		Error:    cause.Error(),
		ID:       id,
		Source:   source,
		Rendered: renderedPromptHash,
		Report:   report,
		Messages: messages,
	}
	if err := x.engine.writeDetailReport(x.task.BlockIndex, "failed", detailReportInfo{
		ID:                 info.ID,
		Source:             info.Source,
		RenderedPromptHash: renderedPromptHash,
		PlanHash:           planHash,
		Report:             info.Report,
		Start:              info.Start,
		End:                info.End,
		Runs:               info.Runs,
		Error:              info.Error,
		Messages:           info.Messages,
		Orphan:             orphan,
	}); err != nil {
		return err
	}
	if orphan {
		writeATMEvent(x.stderr, "orphan", "task %d failed after its todo block changed or disappeared; detail report %s", x.task.BlockIndex+1, report)
	} else {
		if err := store.MarkFailed(x.engine.filePath, x.lease, info); err != nil {
			return err
		}
	}
	return x.engine.updateTaskState(x.task.BlockIndex, taskStateUpdate{
		ID:                 id,
		Status:             "failed",
		SourcePromptHash:   source,
		RenderedPromptHash: renderedPromptHash,
		PlanHash:           planHash,
		StartedAt:          x.start,
		UpdatedAt:          end,
		Runs:               runs,
		Report:             report,
		Logs:               x.stateLogPaths(),
		Orphan:             orphan,
	})
}

func (x *taskExecution) executeFlow(ctx context.Context, current execContext, node compiler.FlowNode, offset int, allowGo bool) (execContext, int, bool, error) {
	switch node.Kind {
	case compiler.FlowSeq:
		return x.executeFlowSeq(ctx, current, node.Children, offset, allowGo)
	case compiler.FlowCd:
		next, err := x.changeWorkdir(ctx, current, node.Cd)
		return next, 0, false, err
	case compiler.FlowBash:
		if node.Bash.Name != "" {
			current.vars[node.Bash.Name] = &lazyProvider{
				kind:    lazyProviderBash,
				bash:    node.Bash,
				vars:    ir.CloneVars(current.vars),
				options: current.options,
			}
			return current, 0, false, nil
		}
		next, err := x.runBash(ctx, current, node.Bash)
		return next, 0, false, err
	case compiler.FlowWait:
		pool := resolvePool(current, node.Pool)
		if pool != "" {
			if _, err := x.engine.pools.pool(pool); err != nil {
				return current, 0, false, err
			}
		}
		limit := current.waitLimit
		if limit == 0 {
			limit = x.engine.async.currentMaxID()
		}
		if err := x.engine.async.waitUpTo(limit, pool); err != nil {
			return current, 0, false, err
		}
		return current, 0, false, nil
	case compiler.FlowFor:
		return x.executeFlowFor(ctx, current, node.For, node.Children, offset+1, allowGo, offset)
	case compiler.FlowIf:
		return x.executeFlowIf(ctx, current, node.If, node.Children, node.ElseChildren, offset+1, allowGo)
	case compiler.FlowGo:
		if !allowGo {
			return current, 0, false, nil
		}
		branch := current
		branch.waitLimit = x.engine.async.currentMaxID()
		if err := x.startFlowBranch(ctx, branch, node.Children, offset+1, resolvePool(current, node.Pool)); err != nil {
			return current, 0, false, err
		}
		return current, 0, true, nil
	case compiler.FlowCall:
		if node.Call.Assign != "" {
			providerCall := node.Call
			assign := providerCall.Assign
			if err := x.resolveLazyVar(ctx, &current, assign); err != nil {
				return current, 0, false, err
			}
			providerCall.Assign = ""
			current.vars[assign] = &lazyProvider{
				kind:    lazyProviderCall,
				call:    providerCall,
				vars:    ir.CloneVars(current.vars),
				options: current.options,
				defRef:  current.defRef,
			}
			return current, 0, false, nil
		}
		value, ok, err := x.callDefinition(ctx, current, node.Call, node.Call.Assign != "")
		if err != nil {
			return current, 0, false, err
		}
		if node.Call.Assign != "" {
			if !ok {
				return current, 0, false, fmt.Errorf("/call %s returned no value", node.Call.Name)
			}
			current.vars[node.Call.Assign] = normalizeReturnValue(value)
		}
		return current, 0, false, nil
	case compiler.FlowWebhook:
		if err := x.sendWebhook(ctx, current, node.Webhook); err != nil {
			return current, 0, false, err
		}
		return current, 0, false, nil
	case compiler.FlowReturn:
		value, err := x.evaluateReturn(ctx, current, node.Return)
		if err != nil {
			return current, 0, false, err
		}
		x.mu.Lock()
		x.returnValue = value
		x.returnSet = true
		x.mu.Unlock()
		return current, 0, false, nil
	case compiler.FlowExecute:
		next, runs, err := x.executePrompt(ctx, current, node.ExecuteOptions, node.Prompt, offset)
		return next, runs, false, err
	default:
		return current, 0, false, fmt.Errorf("unsupported flow kind %q", node.Kind)
	}
}

func (x *taskExecution) executeFlowIf(ctx context.Context, current execContext, branch compiler.If, thenChildren, elseChildren []compiler.FlowNode, childOffset int, allowGo bool) (execContext, int, bool, error) {
	passed, err := x.evaluateFlowIfCondition(ctx, current, branch.Condition)
	if err != nil {
		return current, 0, false, fmt.Errorf("task %d /if condition failed: %w", x.task.BlockIndex+1, err)
	}
	if passed {
		return x.executeFlowSeq(ctx, current, thenChildren, childOffset, allowGo)
	}
	if len(elseChildren) == 0 {
		return current, 0, false, nil
	}
	return x.executeFlowSeq(ctx, current, elseChildren, childOffset, allowGo)
}

func (x *taskExecution) evaluateFlowIfCondition(ctx context.Context, current execContext, condition compiler.Condition) (bool, error) {
	switch condition.Kind {
	case compiler.ConditionExpr:
		if err := x.resolveLazyVarsInExpr(ctx, &current, condition.Text); err != nil {
			return false, err
		}
		return expr.EvalBool(condition.Text, expr.Context{
			Vars:      current.vars,
			TodoFile:  x.engine.filePath,
			Root:      x.evalRoot(current),
			OutputDir: x.engine.outputs.dirPath(),
		})
	default:
		renderedCondition, err := x.renderTemplate(ctx, &current, condition.Text, "condition")
		if err != nil {
			return false, err
		}
		prompt, err := x.renderTemplate(ctx, &current, x.task.Prompt, "prompt")
		if err != nil {
			return false, err
		}
		renderedOptions, err := x.renderRunOptions(ctx, &current, current.options)
		if err != nil {
			return false, fmt.Errorf("args template failed: %w", err)
		}
		if err := x.resolveSessionOptions(&renderedOptions); err != nil {
			return false, err
		}
		return x.engine.checkAgent(ctx, x.engine.filePath, prompt, renderedCondition, renderedOptions, x.stdout, x.stderr)
	}
}

func (x *taskExecution) executeFlowSeq(ctx context.Context, current execContext, nodes []compiler.FlowNode, offset int, allowGo bool) (execContext, int, bool, error) {
	total := 0
	nextOffset := offset
	for i := 0; i < len(nodes); i++ {
		node := nodes[i]
		if node.Kind == compiler.FlowWait && i+1 < len(nodes) && nodes[i+1].Kind == compiler.FlowExecute {
			executeNode := nodes[i+1]
			if strings.TrimSpace(x.executePromptText(executeNode.Prompt)) != "" {
				next, runs, err := x.executeWaitAgent(ctx, current, node, executeNode, nextOffset)
				total += runs
				if err != nil {
					return next, total, false, err
				}
				current = next
				nextOffset += flowLinearOpCount(node) + flowLinearOpCount(executeNode)
				i++
				if x.returnSet {
					return current, total, false, nil
				}
				continue
			}
		}
		next, runs, background, err := x.executeFlow(ctx, current, node, nextOffset, allowGo)
		total += runs
		if err != nil {
			return next, total, background, err
		}
		current = next
		if background || x.returnSet {
			return current, total, background, nil
		}
		nextOffset += flowLinearOpCount(node)
	}
	return current, total, false, nil
}

func (x *taskExecution) executePromptText(promptOverride string) string {
	if promptOverride != "" {
		return promptOverride
	}
	return x.task.Prompt
}

func (x *taskExecution) executeWaitAgent(ctx context.Context, current execContext, waitNode, executeNode compiler.FlowNode, opIndex int) (execContext, int, error) {
	pool := resolvePool(current, waitNode.Pool)
	if pool != "" {
		if _, err := x.engine.pools.pool(pool); err != nil {
			return current, 0, err
		}
	}
	limit := current.waitLimit
	if limit == 0 {
		limit = x.engine.async.currentMaxID()
	}
	prompt := waitAgentPrompt(x.executePromptText(executeNode.Prompt), pool, x.waitAgentTaskSnapshots(limit, pool))
	next, runs, err := x.executePrompt(ctx, current, executeNode.ExecuteOptions, prompt, opIndex+1)
	if err != nil {
		return next, runs, err
	}
	if err := x.engine.async.waitUpTo(limit, pool); err != nil {
		return next, runs, err
	}
	return next, runs, nil
}

func (x *taskExecution) waitAgentTaskSnapshots(limit int, pool string) []asyncTaskSnapshot {
	tasks := x.engine.async.snapshotUpTo(limit, pool)
	if len(tasks) == 0 {
		return tasks
	}
	blocks, err := store.ReadBlocks(x.engine.filePath)
	if err != nil {
		return tasks
	}
	for i := range tasks {
		blockIndex := tasks[i].Block - 1
		if blockIndex < 0 || blockIndex >= len(blocks) {
			continue
		}
		tasks[i].Report = marker.VisibleATMReport(blocks[blockIndex].Body)
	}
	return tasks
}

func waitAgentPrompt(prompt, pool string, tasks []asyncTaskSnapshot) string {
	var b strings.Builder
	b.WriteString("ATM wait coordination context.\n\n")
	if pool == "" {
		b.WriteString("Waiting for: all previously started background tasks.\n")
	} else {
		b.WriteString("Waiting for pool: ")
		b.WriteString(pool)
		b.WriteString(".\n")
	}
	if len(tasks) == 0 {
		b.WriteString("Pending background tasks at coordination start: none.\n")
	} else {
		b.WriteString("Pending background tasks at coordination start:\n")
		for _, task := range tasks {
			b.WriteString("- async #")
			b.WriteString(fmt.Sprint(task.ID))
			b.WriteString(", block ")
			b.WriteString(fmt.Sprint(task.Block))
			if task.Pool != "" {
				b.WriteString(", pool ")
				b.WriteString(task.Pool)
			} else {
				b.WriteString(", default pool")
			}
			b.WriteString(", status ")
			b.WriteString(task.Status)
			b.WriteByte('\n')
			if task.LogPath != "" {
				b.WriteString("  log: ")
				b.WriteString(task.LogPath)
				b.WriteByte('\n')
			}
			if task.Error != "" {
				b.WriteString("  error: ")
				b.WriteString(task.Error)
				b.WriteByte('\n')
			}
			if strings.TrimSpace(task.Report) != "" {
				b.WriteString("  visible report:\n")
				for _, line := range strings.Split(strings.TrimRight(task.Report, "\r\n"), "\n") {
					b.WriteString("  ")
					b.WriteString(line)
					b.WriteByte('\n')
				}
			}
		}
	}
	b.WriteString("\nCancellation capability: not currently available in this runtime; report when cancellation would be appropriate.\n\n")
	b.WriteString("Coordinate the wait: monitor the ATM file reports and logs if available, summarize completed, failed, cancelled, and follow-up work, and report clearly if intervention is needed.\n\n")
	b.WriteString(prompt)
	return b.String()
}

func (x *taskExecution) executeFlowFor(ctx context.Context, current execContext, loop compiler.For, children []compiler.FlowNode, childOffset int, allowGo bool, opIndex int) (execContext, int, bool, error) {
	total := 0
	background := false
	startRun := 0
	if x.task.Cursor.Active && x.task.Cursor.OpIndex == opIndex {
		startRun = x.task.Cursor.RunIndex
	}
	values, err := x.loopValues(ctx, current, loop)
	if err != nil {
		return current, 0, false, fmt.Errorf("task %d /for source failed: %w", x.task.BlockIndex+1, err)
	}
	maxRuns := loop.MaxRuns
	if loop.Source.Kind != "" {
		maxRuns = len(values)
		if maxRuns == 0 {
			writeATMEvent(x.stderr, "warning", "task %d /for %s in %s produced an empty sequence; skipping loop", x.task.BlockIndex+1, loop.VarName, loop.Source.Text)
			return current, 0, false, nil
		}
	}
	unbounded := loop.Source.Kind == "" && loop.MaxRuns == 0 && loop.Condition.Kind == compiler.ConditionExpr
	if loop.MaxRuns == 0 && loop.Source.Kind == "" && !unbounded {
		return current, 0, false, fmt.Errorf("task %d /for has no runs", x.task.BlockIndex+1)
	}
	if unbounded && flowContainsGo(children) {
		return current, 0, false, fmt.Errorf("task %d unbounded /for until(expr) cannot launch background branches; write /go /for until(...) to run the loop inside one background branch", x.task.BlockIndex+1)
	}
	for runIndex := startRun; unbounded || runIndex < maxRuns; runIndex++ {
		child := execContext{
			vars:       ir.CloneVars(current.vars),
			options:    ir.MergeRunOptions(current.options, loop.Options),
			defRef:     current.defRef,
			waitLimit:  current.waitLimit,
			loopOp:     opIndex,
			loopRun:    runIndex + 1,
			agent:      current.agent,
			agentID:    current.agentID,
			background: current.background,
			poolPrefix: current.poolPrefix,
			callState:  current.callState,
		}
		if loop.VarName != "" && runIndex < len(values) {
			child.vars[loop.VarName] = values[runIndex]
			child.agent = appendAgentLabel(current.agent, loop.VarName+"="+loopLabel(values[runIndex]))
		} else if loop.VarName != "" && unbounded {
			value := fmt.Sprintf("%d", runIndex)
			child.vars[loop.VarName] = value
			child.agent = appendAgentLabel(current.agent, loop.VarName+"="+value)
		}

		next, runs, bg, err := x.executeFlowSeq(ctx, child, children, childOffset, allowGo)
		total += runs
		if err != nil {
			return current, total, background, err
		}
		background = background || bg
		if bg {
			if unbounded {
				return current, total, background, fmt.Errorf("task %d unbounded /for until(expr) cannot launch background branches; write /go /for until(...) to run the loop inside one background branch", x.task.BlockIndex+1)
			}
			continue
		}
		current.vars = ir.CloneVars(next.vars)
		if x.returnSet {
			return current, total, false, nil
		}
		if loop.Condition.Text != "" {
			condition, err := x.renderTemplate(ctx, &next, loop.Condition.Text, "condition")
			if err != nil {
				return current, total, false, fmt.Errorf("task %d %w", x.task.BlockIndex+1, err)
			}
			passed, err := x.checkLoopCondition(ctx, next, loop.Condition.Kind, condition)
			if err != nil {
				return current, total, false, fmt.Errorf("task %d condition check failed: %w", x.task.BlockIndex+1, err)
			}
			if passed {
				return current, total, false, nil
			}
		}
	}
	if loop.Condition.Text != "" && !background {
		return current, total, false, fmt.Errorf("task %d condition not satisfied after %d run(s)", x.task.BlockIndex+1, loop.MaxRuns)
	}
	return current, total, background, nil
}

func flowContainsGo(nodes []compiler.FlowNode) bool {
	for _, node := range nodes {
		for _, op := range ir.FlattenFlow(node) {
			if op.Kind == compiler.FlatOpGo {
				return true
			}
		}
	}
	return false
}

func (x *taskExecution) startFlowBranch(ctx context.Context, current execContext, children []compiler.FlowNode, childOffset int, pool string) error {
	current.background = true
	if current.agentID == 0 {
		current.agentID = x.nextAgentID()
	}
	if current.agent == "" {
		current.agent = fmt.Sprintf("agent=%d", current.agentID)
	}
	x.mu.Lock()
	if x.pool == "" {
		x.pool = pool
	}
	x.mu.Unlock()
	x.branches.wg.Add(1)
	err := x.engine.pools.submit(ctx, pool, func() {
		defer x.branches.wg.Done()
		_, runs, _, err := x.executeFlowSeq(ctx, current, children, childOffset, false)
		x.branches.mu.Lock()
		defer x.branches.mu.Unlock()
		x.branches.runs += runs
		if err != nil && x.branches.err == nil {
			x.branches.err = err
		}
	})
	if err != nil {
		x.branches.wg.Done()
		return err
	}
	return nil
}

func flowLinearOpCount(node compiler.FlowNode) int {
	count := 0
	if node.Kind != "" && node.Kind != compiler.FlowSeq {
		count = 1
	}
	for _, child := range node.Children {
		count += flowLinearOpCount(child)
	}
	for _, child := range node.ElseChildren {
		count += flowLinearOpCount(child)
	}
	return count
}

func flowIsWaitOnly(node compiler.FlowNode) bool {
	switch node.Kind {
	case "":
		return true
	case compiler.FlowSeq:
	case compiler.FlowWait, compiler.FlowExecute:
	default:
		return false
	}
	for _, child := range node.Children {
		if !flowIsWaitOnly(child) {
			return false
		}
	}
	for _, child := range node.ElseChildren {
		if !flowIsWaitOnly(child) {
			return false
		}
	}
	return true
}

func (x *taskExecution) loopValues(ctx context.Context, current execContext, loop compiler.For) ([]any, error) {
	if loop.Source.Kind == compiler.ConditionCall {
		call, err := compiler.ParseCallExpression(loop.Source.Text)
		if err != nil {
			return nil, err
		}
		value, ok, err := x.callDefinition(ctx, current, call, true)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s returned no value", loop.Source.Text)
		}
		return coerceLoopList(normalizeReturnValue(value))
	}
	if loop.Source.Kind == compiler.ConditionExpr {
		if err := x.resolveLazyVarsInExpr(ctx, &current, loop.Source.Text); err != nil {
			return nil, err
		}
		source, err := x.renderTemplate(ctx, &current, loop.Source.Text, "source")
		if err != nil {
			return nil, err
		}
		return expr.EvalList(source, expr.Context{
			Vars:      current.vars,
			TodoFile:  x.engine.filePath,
			Root:      x.evalRoot(current),
			OutputDir: x.engine.outputs.dirPath(),
		})
	}
	if len(loop.Values) == 0 {
		return nil, nil
	}
	values := make([]any, len(loop.Values))
	for i, value := range loop.Values {
		values[i] = value
	}
	return values, nil
}

func coerceLoopList(value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return v, nil
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out, nil
	case map[string]any:
		if plans, ok := v["plans"]; ok {
			return coerceLoopList(plans)
		}
		var found []any
		for _, item := range v {
			list, err := coerceLoopList(item)
			if err == nil {
				if found != nil {
					return nil, fmt.Errorf("loop call return object has multiple array fields; return an array or use expression field access")
				}
				found = list
			}
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, fmt.Errorf("loop source must return list, got %T", value)
}

func loopLabel(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"name", "id", "key", "slug"} {
			if label, ok := v[key]; ok {
				return ir.StringValue(label)
			}
		}
	case map[string]string:
		for _, key := range []string{"name", "id", "key", "slug"} {
			if label, ok := v[key]; ok {
				return label
			}
		}
	}
	return ir.StringValue(value)
}

func (x *taskExecution) checkLoopCondition(ctx context.Context, current execContext, kind compiler.ConditionKind, condition string) (bool, error) {
	switch kind {
	case compiler.ConditionExpr:
		if err := x.resolveLazyVarsInExpr(ctx, &current, condition); err != nil {
			return false, err
		}
		return expr.EvalBool(condition, expr.Context{
			Vars:      current.vars,
			TodoFile:  x.engine.filePath,
			Root:      x.evalRoot(current),
			OutputDir: x.engine.outputs.dirPath(),
		})
	default:
		prompt, err := x.renderTemplate(ctx, &current, x.task.Prompt, "prompt")
		if err != nil {
			return false, err
		}
		renderedOptions, err := x.renderRunOptions(ctx, &current, current.options)
		if err != nil {
			return false, fmt.Errorf("args template failed: %w", err)
		}
		if err := x.resolveSessionOptions(&renderedOptions); err != nil {
			return false, err
		}
		return x.engine.checkAgent(ctx, x.engine.filePath, prompt, condition, renderedOptions, x.stdout, x.stderr)
	}
}

func (x *taskExecution) nextAgentID() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.branchSeq++
	return x.branchSeq
}

func (x *taskExecution) runBash(ctx context.Context, current execContext, command compiler.BashCommand) (execContext, error) {
	script, err := x.renderTemplate(ctx, &current, command.Script, "bash")
	if err != nil {
		return current, err
	}
	workdir := x.evalRoot(current)
	if command.Name == "" {
		if err := agent.RunBash(ctx, x.engine.filePath, script, workdir, x.stdout, x.stderr); err != nil {
			return current, err
		}
		return current, nil
	}
	value, err := agent.CaptureBash(ctx, x.engine.filePath, script, workdir, x.stderr)
	if err != nil {
		return current, err
	}
	current.vars[command.Name] = value
	return current, nil
}

func (x *taskExecution) changeWorkdir(ctx context.Context, current execContext, command compiler.CdCommand) (execContext, error) {
	rendered, err := x.renderTemplate(ctx, &current, command.Path, "/cd")
	if err != nil {
		return current, fmt.Errorf("task %d %w", x.task.BlockIndex+1, err)
	}
	path, err := x.resolveTaskWorkdir(current, rendered)
	if err != nil {
		return current, fmt.Errorf("task %d /cd %q failed: %w", x.task.BlockIndex+1, rendered, err)
	}
	if command.MustExist {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return current, fmt.Errorf("task %d /cd %q failed: directory does not exist", x.task.BlockIndex+1, rendered)
			}
			return current, fmt.Errorf("task %d /cd %q failed: %w", x.task.BlockIndex+1, rendered, err)
		}
		if !info.IsDir() {
			return current, fmt.Errorf("task %d /cd %q failed: path is not a directory", x.task.BlockIndex+1, rendered)
		}
		current.options.Workdir = path
		return current, nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return current, fmt.Errorf("task %d /cd %q failed: create directory: %w", x.task.BlockIndex+1, rendered, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return current, fmt.Errorf("task %d /cd %q failed: %w", x.task.BlockIndex+1, rendered, err)
	}
	if !info.IsDir() {
		return current, fmt.Errorf("task %d /cd %q failed: path is not a directory", x.task.BlockIndex+1, rendered)
	}
	current.options.Workdir = path
	return current, nil
}

func (x *taskExecution) resolveTaskWorkdir(current execContext, rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	base := current.options.Workdir
	if base == "" {
		base = x.engine.root
	}
	if !filepath.IsAbs(base) {
		abs, err := filepath.Abs(base)
		if err != nil {
			return "", err
		}
		base = abs
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(filepath.Clean(x.engine.root))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root %s", root)
	}
	return candidate, nil
}

func (x *taskExecution) evalRoot(current execContext) string {
	if current.options.Workdir != "" {
		return current.options.Workdir
	}
	return x.engine.root
}

func (x *taskExecution) renderTemplate(ctx context.Context, current *execContext, input, label string) (string, error) {
	if err := x.resolveLazyVarsInText(ctx, current, input); err != nil {
		return "", err
	}
	rendered, err := compiler.RenderTemplate(input, current.vars)
	if err != nil {
		return "", fmt.Errorf("%s template failed: %w", label, err)
	}
	return rendered, nil
}

func (x *taskExecution) resolveLazyVarsInText(ctx context.Context, current *execContext, input string) error {
	for _, name := range lazyNamesInTemplate(input) {
		if err := x.resolveLazyVar(ctx, current, name); err != nil {
			return err
		}
	}
	return nil
}

func (x *taskExecution) resolveLazyVarsInExpr(ctx context.Context, current *execContext, input string) error {
	for _, name := range exprIdentifierToken.FindAllString(input, -1) {
		if err := x.resolveLazyVar(ctx, current, name); err != nil {
			return err
		}
	}
	return nil
}

func (x *taskExecution) resolveOutputLazyVars(ctx context.Context, current *execContext) error {
	if x.task.Output != nil {
		if err := x.resolveLazyVarsInText(ctx, current, x.task.Output.FileName); err != nil {
			return err
		}
		if err := x.resolveLazyVarsInText(ctx, current, x.task.Output.Schema); err != nil {
			return err
		}
	}
	if x.task.Return != nil && x.task.Return.Output != nil {
		if err := x.resolveLazyVarsInText(ctx, current, x.task.Return.Output.FileName); err != nil {
			return err
		}
		if err := x.resolveLazyVarsInText(ctx, current, x.task.Return.Output.Schema); err != nil {
			return err
		}
	}
	return nil
}

func lazyNamesInTemplate(input string) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, match := range templateVarRef.FindAllString(input, -1) {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		if parts := templateLegacyField.FindStringSubmatch(match); len(parts) == 3 {
			add(parts[1])
			continue
		}
		if parts := templateLegacyVar.FindStringSubmatch(match); len(parts) == 2 {
			add(parts[1])
			continue
		}
		if strings.HasPrefix(inner, ".") {
			name := strings.TrimLeft(inner, ".")
			if i := strings.IndexAny(name, " |)}]\t\r\n"); i >= 0 {
				name = name[:i]
			}
			add(name)
		}
		for _, quoted := range templateQuotedVar.FindAllString(inner, -1) {
			name := strings.Trim(quoted, `"`)
			add(name)
		}
	}
	return names
}

func (x *taskExecution) resolveLazyVar(ctx context.Context, current *execContext, name string) error {
	provider, ok := current.vars[name].(*lazyProvider)
	if !ok || provider == nil {
		return nil
	}
	value, err := provider.resolve(ctx, x)
	if err != nil {
		return fmt.Errorf("resolve lazy /let %s failed: %w", name, err)
	}
	current.vars[name] = value
	return nil
}

func (p *lazyProvider) resolve(ctx context.Context, x *taskExecution) (any, error) {
	p.mu.Lock()
	if p.resolved {
		value := p.value
		p.mu.Unlock()
		return value, nil
	}
	if p.resolving {
		p.mu.Unlock()
		return nil, fmt.Errorf("recursive lazy provider")
	}
	p.resolving = true
	p.mu.Unlock()

	value, err := p.evaluate(ctx, x)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolving = false
	if err != nil {
		return nil, err
	}
	p.value = normalizeReturnValue(value)
	p.resolved = true
	return p.value, nil
}

func (p *lazyProvider) evaluate(ctx context.Context, x *taskExecution) (any, error) {
	current := execContext{vars: ir.CloneVars(p.vars), options: p.options, loopOp: -1, defRef: p.defRef}
	switch p.kind {
	case lazyProviderBash:
		script, err := x.renderTemplate(ctx, &current, p.bash.Script, "/let "+p.bash.Name+" /bash")
		if err != nil {
			return nil, err
		}
		return agent.CaptureBash(ctx, x.engine.filePath, script, x.evalRoot(current), x.stderr)
	case lazyProviderCall:
		call := p.call
		call.Args = slices.Clone(p.call.Args)
		value, ok, err := x.callDefinition(ctx, current, call, true)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("/call %s returned no value", p.call.Name)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported lazy provider %q", p.kind)
	}
}

func (x *taskExecution) callDefinition(ctx context.Context, current execContext, call compiler.Call, needReturn bool) (any, bool, error) {
	def, ok := x.engine.resolveDefinition(call.Name, current.defRef)
	if !ok {
		return nil, false, fmt.Errorf("unknown definition %q", call.Name)
	}
	if len(call.Args) != len(def.Params) {
		return nil, false, fmt.Errorf("/call %s expects %d argument(s), got %d", call.Name, len(def.Params), len(call.Args))
	}
	callID := x.engine.nextCallID()
	state := &callState{}
	vars := ir.CloneVars(current.vars)
	for i, param := range def.Params {
		value, err := x.renderTemplate(ctx, &current, call.Args[i], "/call "+call.Name+" argument")
		if err != nil {
			return nil, false, err
		}
		vars[param] = value
	}
	poolPrefix := fmt.Sprintf("__call_%d_", callID)
	asyncPool := fmt.Sprintf("__call_%d", callID)
	base := execContext{
		vars:       vars,
		options:    compiler.RunOptions{Workdir: current.options.Workdir, Skills: slices.Clone(current.options.Skills), MCPs: slices.Clone(current.options.MCPs), DefMCP: cloneDefMCPRuntime(current.options.DefMCP), DefDepth: current.options.DefDepth},
		defRef:     definitionScopeRef{SourcePath: def.SourcePath, Scope: def.Scope, Line: def.Line},
		loopOp:     -1,
		agent:      appendAgentLabel(current.agent, "call="+call.Name),
		poolPrefix: poolPrefix,
		callState:  state,
	}
	callAsyncPools := map[string]struct{}{}
	for i, block := range def.Blocks {
		body := block.Body
		if pools, ok, err := compiler.ParseGlobalPoolBlock(body); err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		} else if ok {
			for _, pool := range pools {
				pool.Name = poolPrefix + pool.Name
				if err := x.engine.pools.declare(pool); err != nil {
					return nil, false, err
				}
			}
			continue
		}
		if bindings, ok, err := compiler.ParseGlobalLetBlock(body); err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		} else if ok {
			x.applyLocalLetBindings(bindings, vars, base.options)
			base.vars = vars
			continue
		}
		task, err := compiler.ParseTaskForFile(def.SourcePath, x.task.BlockIndex, body, vars, compiler.CompileOptions{Root: defRoot(def)})
		if err != nil {
			return nil, false, err
		}
		blockCtx := base
		blockCtx.vars = ir.CloneVars(task.Vars)
		blockDBs, err := x.engine.applyDBConfig(current.options.DBs, task.DB, x.engine.declsForRuntimeDBs(current.options.DBs))
		if err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		}
		blockCtx.options = ir.MergeRunOptions(base.options, compiler.RunOptions{DBs: blockDBs})
		child := &taskExecution{
			engine:     x.engine,
			task:       task,
			lease:      x.lease,
			start:      x.start,
			stdout:     x.stdout,
			stderr:     x.stderr,
			file:       x.file,
			branches:   &branchCollector{},
			writeState: false,
		}
		_, _, background, err := child.executeFlow(ctx, blockCtx, task.Flow, 0, true)
		if err != nil {
			return nil, false, err
		}
		if background {
			key := inFlightKey{index: -callID*1000 - i - 1, hash: fmt.Sprintf("%s:%d", call.Name, i)}
			taskPool := child.pool
			if taskPool == "" {
				taskPool = asyncPool
			}
			callAsyncPools[taskPool] = struct{}{}
			asyncTask := x.engine.async.register(key, taskPool, "")
			go func(exec *taskExecution) {
				err := exec.waitBranchesAndMarkDone()
				x.mergeChildCallState(exec, state)
				x.engine.async.complete(asyncTask, err)
			}(child)
			continue
		}
		x.mergeChildCallState(child, state)
		vars = blockCtx.vars
		base.vars = vars
		if child.returnSet {
			return child.returnValue, true, nil
		}
	}
	for pool := range callAsyncPools {
		if err := x.engine.async.waitUpTo(0, pool); err != nil {
			return nil, false, err
		}
	}
	return nil, false, nil
}

func (x *taskExecution) mergeChildCallState(child *taskExecution, state *callState) {
	child.mu.Lock()
	messages := slices.Clone(child.messages)
	child.mu.Unlock()
	x.mu.Lock()
	x.messages = append(x.messages, messages...)
	x.mu.Unlock()
}

func (x *taskExecution) applyLocalLetBindings(bindings []compiler.LetBinding, vars map[string]any, options compiler.RunOptions) {
	for _, binding := range bindings {
		if binding.BashScript == "" {
			vars[binding.Name] = binding.Value
			continue
		}
		vars[binding.Name] = &lazyProvider{
			kind:    lazyProviderBash,
			bash:    compiler.BashCommand{Name: binding.Name, Script: binding.BashScript},
			vars:    ir.CloneVars(vars),
			options: options,
			defRef:  definitionScopeRef{SourcePath: x.task.SourcePath, Scope: x.task.Scope, Line: x.task.Line},
		}
	}
}

func (x *taskExecution) evaluateReturn(ctx context.Context, current execContext, spec compiler.ReturnSpec) (any, error) {
	vars := ir.CloneVars(current.vars)
	vars["agent"] = agentReturnData(current.callState.snapshotMessages(), x.engine.messages)
	returnCtx := current
	returnCtx.vars = vars
	switch spec.Kind {
	case compiler.ReturnBash:
		script, err := x.renderTemplate(ctx, &returnCtx, spec.Script, "/return /bash")
		if err != nil {
			return nil, err
		}
		return agent.CaptureBash(ctx, x.engine.filePath, script, current.options.Workdir, x.stderr)
	case compiler.ReturnStructured:
		if len(x.structuredOutputs) == 0 {
			return nil, fmt.Errorf("/return structured output missing MCP result")
		}
		return parseJSONReturn(x.structuredOutputs[len(x.structuredOutputs)-1])
	default:
		return x.renderTemplate(ctx, &returnCtx, spec.Text, "/return")
	}
}

func agentReturnData(messages []compiler.OutputMessage, limit int) map[string]any {
	if limit <= 0 {
		limit = 1
	}
	var texts []string
	for i := len(messages) - 1; i >= 0 && len(texts) < limit; i-- {
		if strings.TrimSpace(messages[i].Text) == "" {
			continue
		}
		texts = append(texts, messages[i].Text)
	}
	for i, j := 0, len(texts)-1; i < j; i, j = i+1, j-1 {
		texts[i], texts[j] = texts[j], texts[i]
	}
	last := ""
	if len(texts) > 0 {
		last = texts[len(texts)-1]
	}
	data, _ := json.Marshal(texts)
	return map[string]any{
		"message":       last,
		"last_message":  last,
		"messages":      strings.Join(texts, "\n"),
		"messages_json": string(data),
	}
}

func parseJSONReturn(data []byte) (any, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("parse structured return: %w", err)
	}
	return value, nil
}

func normalizeReturnValue(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return value
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return value
	}
	return parsed
}

func latestMessageText(messages []compiler.OutputMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if text := strings.TrimSpace(messages[i].Text); text != "" {
			return text
		}
	}
	return ""
}

func defRoot(def compiler.Definition) string {
	if def.SourcePath == "" {
		return "."
	}
	return filepath.Dir(def.SourcePath)
}

func resolvePool(current execContext, pool string) string {
	if pool == "" {
		return ""
	}
	if current.poolPrefix != "" {
		return current.poolPrefix + pool
	}
	return pool
}

func (x *taskExecution) executePrompt(ctx context.Context, current execContext, opts compiler.RunOptions, promptOverride string, opIndex int) (execContext, int, error) {
	renderedOptions, err := x.renderRunOptions(ctx, &current, ir.MergeRunOptions(current.options, opts))
	if err != nil {
		return current, 0, fmt.Errorf("task %d args template failed: %w", x.task.BlockIndex+1, err)
	}
	if err := x.resolveSessionOptions(&renderedOptions); err != nil {
		return current, 0, fmt.Errorf("task %d %w", x.task.BlockIndex+1, err)
	}
	rawPrompt := x.task.Prompt
	if promptOverride != "" {
		rawPrompt = promptOverride
	}
	prompt, err := x.renderTemplate(ctx, &current, rawPrompt, "prompt")
	if err != nil {
		return current, 0, fmt.Errorf("task %d %w", x.task.BlockIndex+1, err)
	}
	renderedPromptHash := hashStateText(prompt)
	planHash := hashTaskPlan(x.task)
	x.mu.Lock()
	x.renderedPromptHash = renderedPromptHash
	x.planHash = planHash
	x.mu.Unlock()
	if x.writeState && !x.engine.isAbandoningBackground() && strings.TrimSpace(prompt) != "" {
		id, source, report, orphan, identityErr := x.reportIdentity()
		if identityErr != nil {
			return current, 0, identityErr
		}
		if stateErr := x.engine.updateTaskState(x.task.BlockIndex, taskStateUpdate{
			ID:                 id,
			Status:             "running",
			SourcePromptHash:   source,
			RenderedPromptHash: renderedPromptHash,
			PlanHash:           planHash,
			StartedAt:          x.start,
			UpdatedAt:          time.Now(),
			Runs:               x.currentRuns(),
			Report:             report,
			Logs:               x.stateLogPaths(),
			Orphan:             orphan,
		}); stateErr != nil {
			return current, 0, stateErr
		}
	}
	if err := x.resolveOutputLazyVars(ctx, &current); err != nil {
		return current, 0, fmt.Errorf("task %d output template failed: %w", x.task.BlockIndex+1, err)
	}
	outputVars := outputTemplateVars(current)
	outputSpec, err := renderOutputSpec(x.task.Output, outputVars)
	if err != nil {
		return current, 0, fmt.Errorf("task %d output template failed: %w", x.task.BlockIndex+1, err)
	}
	returnOutputSpec, err := renderReturnOutputSpec(x.task.Return, outputVars)
	if err != nil {
		return current, 0, fmt.Errorf("task %d return output template failed: %w", x.task.BlockIndex+1, err)
	}
	activeOutputSpec := outputSpec
	outputWritesFile := outputSpec != nil
	if activeOutputSpec == nil && returnOutputSpec != nil {
		activeOutputSpec = returnOutputSpec
	}
	if activeOutputSpec != nil && activeOutputSpec.IsStructured() {
		renderedOptions.Output = activeOutputSpec
	}
	if err := x.engine.registerNetworkDefsMCP(ctx, renderedOptions.DefMCP); err != nil {
		return current, 0, fmt.Errorf("task %d def MCP setup failed: %w", x.task.BlockIndex+1, err)
	}
	if strings.TrimSpace(prompt) == "" {
		return current, 0, nil
	}
	start := time.Now()
	agentDetail := ""
	if current.agent != "" {
		agentDetail = " [" + current.agent + "]"
	}
	writeATMEvent(x.stderr, "run", "task %d%s step %d via %s%s", x.task.BlockIndex+1, x.engine.taskLineRangeLabel(x.task.BlockIndex), runningOpIndex(current, opIndex)+1, x.engine.runner.Name(), agentDetail)
	result, err := x.engine.executeAgent(ctx, x.engine.filePath, prompt, renderedOptions, x.stdout, x.stderr)
	if err != nil {
		return current, 0, fmt.Errorf("task %d run failed: %w", x.task.BlockIndex+1, err)
	}

	x.mu.Lock()
	x.runs++
	runNumber := x.runs
	annotatedMessages := annotateMessages(result.Messages, current.agent)
	x.messages = append(x.messages, annotatedMessages...)
	if current.callState != nil {
		current.callState.addMessages(annotatedMessages)
	}
	messages := x.recentMessagesLocked()
	eventPath, eventErr := x.engine.outputs.writeEvents(x.taskDir, x.task.BlockIndex, runNumber, x.engine.runner.Name(), current.agent, result.RawEvents)
	if eventErr != nil {
		x.mu.Unlock()
		return current, 1, eventErr
	}
	if eventPath != "" {
		x.eventPaths = append(x.eventPaths, eventPath)
	}
	structuredOutputPath := ""
	if activeOutputSpec != nil && activeOutputSpec.IsStructured() {
		if len(result.StructuredOutput) == 0 {
			x.mu.Unlock()
			return current, 1, fmt.Errorf("task %d structured output missing MCP result", x.task.BlockIndex+1)
		}
		suffix := ""
		if current.background {
			suffix = outputAgentSuffix(current)
		}
		if outputWritesFile {
			structuredOutputPath, err = x.engine.outputs.writeStructuredOutput(x.taskDir, x.task.BlockIndex, runNumber, activeOutputSpec.FileName, suffix, result.StructuredOutput)
			if err != nil {
				x.mu.Unlock()
				return current, 1, err
			}
		}
		x.structuredOutputs = append(x.structuredOutputs, slices.Clone(result.StructuredOutput))
		if current.callState != nil {
			current.callState.addOutput(result.StructuredOutput)
		}
	} else if outputSpec != nil {
		suffix := ""
		if current.background {
			suffix = outputAgentSuffix(current)
		}
		text := latestMessageText(result.Messages)
		if text == "" {
			text = strings.TrimSpace(result.RawEvents)
		}
		if text != "" {
			structuredOutputPath, err = x.engine.outputs.writeTextOutput(x.taskDir, x.task.BlockIndex, runNumber, outputSpec.FileName, suffix, []byte(text+"\n"))
			if err != nil {
				x.mu.Unlock()
				return current, 1, err
			}
		}
	}
	x.engine.recordResult(annotatedMessages, result.StructuredOutput)
	if x.writeState && !x.engine.isAbandoningBackground() && x.task.Name != "" && result.SessionID != "" {
		id, _, _, _, identityErr := x.reportIdentityLocked()
		if identityErr != nil {
			x.mu.Unlock()
			return current, 1, identityErr
		}
		if stateErr := x.engine.updateSessionState(x.task.Name, x.engine.runner.Name(), result.SessionID, id, time.Now()); stateErr != nil {
			x.mu.Unlock()
			return current, 1, stateErr
		}
	}
	if x.writeState && !x.engine.isAbandoningBackground() {
		id, source, report, orphan, identityErr := x.reportIdentityLocked()
		if identityErr != nil {
			x.mu.Unlock()
			return current, 1, identityErr
		}
		x.lease, err = store.SaveRunning(x.engine.filePath, x.lease, compiler.RunningInfo{
			Active:    true,
			Start:     x.start,
			StepIndex: runningOpIndex(current, opIndex),
			StepRuns:  runningStepRuns(current, runNumber),
			TotalRuns: runNumber,
			ID:        id,
			Source:    source,
			Rendered:  renderedPromptHash,
			Report:    report,
			Messages:  messages,
		})
		if orphan && err == nil {
			x.orphan = true
		}
	}
	stateLogs := x.stateLogPathsLocked()
	x.mu.Unlock()
	if err != nil {
		if errors.Is(err, store.ErrObsolete) || strings.Contains(err.Error(), "no tasks found") {
			x.mu.Lock()
			x.orphan = true
			x.mu.Unlock()
			err = nil
		} else {
			return current, 1, err
		}
	}
	if x.writeState && !x.engine.isAbandoningBackground() {
		id, source, report, orphan, identityErr := x.reportIdentity()
		if identityErr != nil {
			return current, 1, identityErr
		}
		if stateErr := x.engine.updateTaskState(x.task.BlockIndex, taskStateUpdate{
			ID:                 id,
			Status:             "running",
			SourcePromptHash:   source,
			RenderedPromptHash: renderedPromptHash,
			PlanHash:           planHash,
			StartedAt:          x.start,
			UpdatedAt:          time.Now(),
			Runs:               runNumber,
			Report:             report,
			Logs:               stateLogs,
			Orphan:             orphan,
		}); stateErr != nil {
			return current, 1, stateErr
		}
	}
	finished := time.Now()
	writeATMEvent(x.stderr, "done", "task %d run %d at %s in %s", x.task.BlockIndex+1, runNumber, finished.Format(time.RFC3339), finished.Sub(start).Round(time.Millisecond))
	if structuredOutputPath != "" {
		writeATMEvent(x.stderr, "output", "task %d run %d %s", x.task.BlockIndex+1, runNumber, structuredOutputPath)
	}
	return current, 1, nil
}

func (x *taskExecution) resolveSessionOptions(opts *compiler.RunOptions) error {
	if opts == nil || (!opts.Resume && !opts.Fork) {
		return nil
	}
	label := "/resume"
	target := strings.TrimSpace(opts.ResumeTarget)
	if opts.Fork {
		label = "/fork"
		target = strings.TrimSpace(opts.ForkTarget)
	}
	if target == "" {
		return fmt.Errorf("%s requires a task name", label)
	}
	session, ok, err := x.engine.resolveSessionState(target)
	if err != nil {
		return fmt.Errorf("read %s session %q: %w", strings.TrimPrefix(label, "/"), target, err)
	}
	if !ok || strings.TrimSpace(session.ID) == "" {
		return fmt.Errorf("no recorded agent session for %s %s", label, target)
	}
	if session.Tool != "" && session.Tool != x.engine.runner.Name() {
		return fmt.Errorf("%s %s recorded a %s session, current tool is %s", label, target, session.Tool, x.engine.runner.Name())
	}
	opts.ResumeSessionID = session.ID
	return nil
}

func (x *taskExecution) recentMessagesLocked() []compiler.OutputMessage {
	limit := x.engine.messages
	if limit <= 0 || len(x.messages) == 0 {
		return nil
	}
	if !hasAgentLabels(x.messages) {
		start := len(x.messages) - limit
		if start < 0 {
			start = 0
		}
		recent := make([]compiler.OutputMessage, len(x.messages[start:]))
		copy(recent, x.messages[start:])
		return recent
	}

	counts := make(map[string]int)
	var reversed []compiler.OutputMessage
	for i := len(x.messages) - 1; i >= 0; i-- {
		message := x.messages[i]
		key := message.Agent
		if key == "" {
			key = "_"
		}
		if counts[key] >= limit {
			continue
		}
		counts[key]++
		reversed = append(reversed, message)
	}
	recent := make([]compiler.OutputMessage, len(reversed))
	for i := range reversed {
		recent[len(reversed)-1-i] = reversed[i]
	}
	return recent
}

func appendAgentLabel(parent, label string) string {
	if parent == "" {
		return label
	}
	return parent + ", " + label
}

func annotateMessages(messages []compiler.OutputMessage, agent string) []compiler.OutputMessage {
	if agent == "" || len(messages) == 0 {
		return messages
	}
	out := make([]compiler.OutputMessage, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].Agent = agent
	}
	return out
}

func hasAgentLabels(messages []compiler.OutputMessage) bool {
	for _, message := range messages {
		if message.Agent != "" {
			return true
		}
	}
	return false
}

func runningOpIndex(current execContext, fallback int) int {
	if current.loopOp >= 0 {
		return current.loopOp
	}
	return fallback
}

func runningStepRuns(current execContext, fallback int) int {
	if current.loopRun > 0 {
		return current.loopRun
	}
	return fallback
}

func (x *taskExecution) renderRunOptions(ctx context.Context, current *execContext, opts compiler.RunOptions) (compiler.RunOptions, error) {
	for _, arg := range opts.Args {
		if err := x.resolveLazyVarsInText(ctx, current, arg); err != nil {
			return compiler.RunOptions{}, err
		}
	}
	return renderRunOptions(opts, current.vars)
}

func renderRunOptions(opts compiler.RunOptions, vars map[string]any) (compiler.RunOptions, error) {
	rendered := compiler.RunOptions{
		Resume:          opts.Resume,
		ResumeSessionID: opts.ResumeSessionID,
		Fork:            opts.Fork,
		Output:          opts.Output,
		DBs:             slices.Clone(opts.DBs),
		Workdir:         opts.Workdir,
		Skills:          slices.Clone(opts.Skills),
		MCPs:            slices.Clone(opts.MCPs),
		DefMCP:          cloneDefMCPRuntime(opts.DefMCP),
		DefDepth:        opts.DefDepth,
	}
	if opts.ResumeTarget != "" {
		value, err := compiler.RenderTemplate(opts.ResumeTarget, vars)
		if err != nil {
			return compiler.RunOptions{}, err
		}
		rendered.ResumeTarget = value
	}
	if opts.ForkTarget != "" {
		value, err := compiler.RenderTemplate(opts.ForkTarget, vars)
		if err != nil {
			return compiler.RunOptions{}, err
		}
		rendered.ForkTarget = value
	}
	for _, arg := range opts.Args {
		value, err := compiler.RenderTemplate(arg, vars)
		if err != nil {
			return compiler.RunOptions{}, err
		}
		rendered.Args = append(rendered.Args, value)
	}
	if rendered.DefMCP != nil {
		rendered.DefMCP.Workdir = rendered.Workdir
		rendered.DefMCP.DBs = slices.Clone(rendered.DBs)
		rendered.DefMCP.Skills = slices.Clone(rendered.Skills)
		rendered.DefMCP.MCPs = slices.Clone(rendered.MCPs)
		rendered.DefMCP.Vars = ir.CloneVars(vars)
		rendered.DefMCP.Depth = rendered.DefDepth
	}
	return rendered, nil
}

func cloneDefMCPRuntime(in *compiler.DefMCPRuntime) *compiler.DefMCPRuntime {
	if in == nil {
		return nil
	}
	out := *in
	out.Definitions = slices.Clone(in.Definitions)
	out.DBs = slices.Clone(in.DBs)
	out.Skills = slices.Clone(in.Skills)
	out.MCPs = slices.Clone(in.MCPs)
	if in.Vars != nil {
		out.Vars = ir.CloneVars(in.Vars)
	}
	out.Defs = slices.Clone(in.Defs)
	return &out
}
