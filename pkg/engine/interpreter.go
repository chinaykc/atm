package engine

import (
	"atm/pkg/dsl"
	"atm/pkg/expr"
	"atm/pkg/store"
	"atm/pkg/tools"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type execContext struct {
	vars       map[string]any
	options    dsl.RunOptions
	waitLimit  int
	loopOp     int
	loopRun    int
	agent      string
	agentID    int
	background bool
	poolPrefix string
	callState  *callState
}

type taskExecution struct {
	engine *Engine
	task   dsl.Task
	lease  store.BlockLease
	start  time.Time
	runs   int
	pool   string

	stdout io.Writer
	stderr io.Writer
	file   interface{ Close() error }

	branches          *branchCollector
	branchSeq         int
	messages          []dsl.OutputMessage
	structuredOutputs [][]byte
	returnValue       any
	returnSet         bool
	writeState        bool
	mu                sync.Mutex
}

type callState struct {
	mu       sync.Mutex
	messages []dsl.OutputMessage
	outputs  [][]byte
}

func (s *callState) addMessages(messages []dsl.OutputMessage) {
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
	s.outputs = append(s.outputs, append([]byte{}, output...))
}

func (s *callState) snapshotMessages() []dsl.OutputMessage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dsl.OutputMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *callState) snapshotOutputs() [][]byte {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.outputs))
	for i := range s.outputs {
		out[i] = append([]byte{}, s.outputs[i]...)
	}
	return out
}

type branchCollector struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	runs int
	err  error
}

func (e *Engine) runTask(ctx context.Context, lease store.BlockLease, task dsl.Task, options dsl.RunOptions) error {
	stdout, stderr, file, path, err := e.taskWriters(task.BlockIndex)
	if err != nil {
		return err
	}
	writeATMEvent(stderr, "log", "task %d %s", task.BlockIndex+1, path)

	exec := &taskExecution{
		engine:     e,
		task:       task,
		lease:      lease,
		start:      time.Now(),
		stdout:     stdout,
		stderr:     stderr,
		file:       file,
		branches:   &branchCollector{},
		writeState: true,
	}
	if task.Cursor.Active {
		exec.start = task.Cursor.Start
		exec.runs = task.Cursor.TotalRuns
	} else if exec.writeState && !isWaitOnly(task) {
		updated, err := store.SaveRunning(e.filePath, exec.lease, dsl.RunningInfo{Active: true, Start: exec.start})
		if err != nil {
			_ = file.Close()
			return err
		}
		exec.lease = updated
	}

	base := execContext{vars: dsl.CloneVars(task.Vars), loopOp: -1, options: options}
	_, background, err := exec.execute(ctx, base, task.Ops, 0, true)
	if err != nil {
		_ = file.Close()
		return err
	}
	if background {
		key := inFlightKey{index: lease.Index, hash: lease.BodyHash}
		asyncTask := e.async.register(key, exec.pool)
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

func isWaitOnly(task dsl.Task) bool {
	if strings.TrimSpace(task.Prompt) != "" {
		return false
	}
	for _, op := range task.Ops {
		if op.Kind != dsl.OpWait && op.Kind != dsl.OpExecute {
			return false
		}
	}
	return true
}

func (x *taskExecution) waitBranchesAndMarkDone() error {
	x.branches.wg.Wait()
	x.branches.mu.Lock()
	err := x.branches.err
	x.branches.mu.Unlock()
	if err != nil {
		return err
	}
	return x.markDone()
}

func (x *taskExecution) markDone() error {
	if !x.writeState {
		return nil
	}
	x.mu.Lock()
	runs := x.runs
	messages := x.recentMessagesLocked()
	x.mu.Unlock()
	return store.MarkDone(x.engine.filePath, x.lease, dsl.DoneInfo{Start: x.start, End: time.Now(), Runs: runs, Messages: messages})
}

func (x *taskExecution) execute(ctx context.Context, current execContext, ops []dsl.Op, offset int, allowGo bool) (int, bool, error) {
	total := 0
	for i, op := range ops {
		absolute := offset + i
		switch op.Kind {
		case dsl.OpCd:
			var err error
			current, err = x.changeWorkdir(current, op.Cd)
			if err != nil {
				return total, false, err
			}
		case dsl.OpBash:
			var err error
			current, err = x.runBash(ctx, current, op.Bash)
			if err != nil {
				return total, false, err
			}
		case dsl.OpWait:
			pool := resolvePool(current, op.Pool)
			if pool != "" {
				if _, err := x.engine.pools.pool(pool); err != nil {
					return total, false, err
				}
			}
			limit := current.waitLimit
			if limit == 0 {
				limit = x.engine.async.currentMaxID()
			}
			if err := x.engine.async.waitUpTo(limit, pool); err != nil {
				return total, false, err
			}
		case dsl.OpFor:
			runs, background, err := x.executeFor(ctx, current, op.For, ops[i+1:], absolute+1, allowGo, absolute)
			return total + runs, background, err
		case dsl.OpGo:
			if !allowGo {
				continue
			}
			branch := current
			branch.waitLimit = x.engine.async.currentMaxID()
			if err := x.startBranch(ctx, branch, ops[i+1:], absolute+1, resolvePool(current, op.Pool)); err != nil {
				return total, false, err
			}
			return total, true, nil
		case dsl.OpCall:
			value, ok, err := x.callDefinition(ctx, current, op.Call, op.Call.Assign != "")
			if err != nil {
				return total, false, err
			}
			if op.Call.Assign != "" {
				if !ok {
					return total, false, fmt.Errorf("/call %s returned no value", op.Call.Name)
				}
				current.vars[op.Call.Assign] = normalizeReturnValue(value)
			}
		case dsl.OpReturn:
			value, err := x.evaluateReturn(ctx, current, op.Return)
			if err != nil {
				return total, false, err
			}
			x.mu.Lock()
			x.returnValue = value
			x.returnSet = true
			x.mu.Unlock()
			return total, false, nil
		case dsl.OpExecute:
			runs, err := x.executePrompt(ctx, current, op.ExecuteOptions, absolute)
			if err != nil {
				return total, false, err
			}
			total += runs
		default:
			return total, false, fmt.Errorf("unsupported op kind %q", op.Kind)
		}
	}
	return total, false, nil
}

func (x *taskExecution) executeFor(ctx context.Context, current execContext, loop dsl.For, suffix []dsl.Op, suffixOffset int, allowGo bool, opIndex int) (int, bool, error) {
	total := 0
	background := false
	startRun := 0
	if x.task.Cursor.Active && x.task.Cursor.OpIndex == opIndex {
		startRun = x.task.Cursor.RunIndex
	}
	values, err := x.loopValues(ctx, current, loop)
	if err != nil {
		return 0, false, fmt.Errorf("task %d /for source failed: %w", x.task.BlockIndex+1, err)
	}
	maxRuns := loop.MaxRuns
	if loop.Source.Kind != "" {
		maxRuns = len(values)
	}
	unbounded := loop.Source.Kind == "" && loop.MaxRuns == 0 && loop.Condition.Kind == dsl.ConditionCEL
	if loop.MaxRuns == 0 && loop.Source.Kind == "" && !unbounded {
		return 0, false, fmt.Errorf("task %d /for has no runs", x.task.BlockIndex+1)
	}
	for runIndex := startRun; unbounded || runIndex < maxRuns; runIndex++ {
		child := execContext{
			vars:       dsl.CloneVars(current.vars),
			options:    dsl.MergeRunOptions(current.options, loop.Options),
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
			value := fmt.Sprintf("%d", runIndex+1)
			child.vars[loop.VarName] = value
			child.agent = appendAgentLabel(current.agent, loop.VarName+"="+value)
		}

		runs, bg, err := x.execute(ctx, child, suffix, suffixOffset, allowGo)
		total += runs
		if err != nil {
			return total, background, err
		}
		background = background || bg
		if bg {
			if unbounded {
				return total, background, fmt.Errorf("task %d unbounded /for until(CEL) cannot launch background branches; write /go /for until(...) to run the loop inside one background branch", x.task.BlockIndex+1)
			}
			continue
		}
		current.vars = dsl.CloneVars(child.vars)
		if loop.Condition.Text != "" {
			condition, err := dsl.RenderTemplate(loop.Condition.Text, child.vars)
			if err != nil {
				return total, false, fmt.Errorf("task %d condition template failed: %w", x.task.BlockIndex+1, err)
			}
			passed, err := x.checkLoopCondition(ctx, child, loop.Condition.Kind, condition)
			if err != nil {
				return total, false, fmt.Errorf("task %d condition check failed: %w", x.task.BlockIndex+1, err)
			}
			if passed {
				return total, false, nil
			}
		}
	}
	if loop.Condition.Text != "" && !background {
		return total, false, fmt.Errorf("task %d condition not satisfied after %d run(s)", x.task.BlockIndex+1, loop.MaxRuns)
	}
	return total, background, nil
}

func (x *taskExecution) loopValues(ctx context.Context, current execContext, loop dsl.For) ([]any, error) {
	if loop.Source.Kind == dsl.ConditionCall {
		call, err := dsl.ParseCallExpression(loop.Source.Text)
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
	if loop.Source.Kind == dsl.ConditionCEL {
		source, err := dsl.RenderTemplate(loop.Source.Text, current.vars)
		if err != nil {
			return nil, fmt.Errorf("source template failed: %w", err)
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
					return nil, fmt.Errorf("loop call return object has multiple array fields; return an array or use a CEL field expression")
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
				return dsl.StringValue(label)
			}
		}
	case map[string]string:
		for _, key := range []string{"name", "id", "key", "slug"} {
			if label, ok := v[key]; ok {
				return label
			}
		}
	}
	return dsl.StringValue(value)
}

func (x *taskExecution) checkLoopCondition(ctx context.Context, current execContext, kind dsl.ConditionKind, condition string) (bool, error) {
	switch kind {
	case dsl.ConditionCEL:
		return expr.EvalBool(condition, expr.Context{
			Vars:      current.vars,
			TodoFile:  x.engine.filePath,
			Root:      x.evalRoot(current),
			OutputDir: x.engine.outputs.dirPath(),
		})
	default:
		prompt, err := dsl.RenderTemplate(x.task.Prompt, current.vars)
		if err != nil {
			return false, fmt.Errorf("prompt template failed: %w", err)
		}
		renderedOptions, err := renderRunOptions(current.options, current.vars)
		if err != nil {
			return false, fmt.Errorf("args template failed: %w", err)
		}
		return x.engine.runner.Check(ctx, x.engine.filePath, prompt, condition, renderedOptions, x.stdout, x.stderr)
	}
}

func (x *taskExecution) startBranch(ctx context.Context, current execContext, suffix []dsl.Op, suffixOffset int, pool string) error {
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
		runs, _, err := x.execute(ctx, current, suffix, suffixOffset, false)
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

func (x *taskExecution) nextAgentID() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.branchSeq++
	return x.branchSeq
}

func (x *taskExecution) runBash(ctx context.Context, current execContext, command dsl.BashCommand) (execContext, error) {
	script, err := dsl.RenderTemplate(command.Script, current.vars)
	if err != nil {
		return current, fmt.Errorf("bash template failed: %w", err)
	}
	if command.Name == "" {
		if err := tools.RunBash(ctx, x.engine.filePath, script, current.options.Workdir, x.stdout, x.stderr); err != nil {
			return current, err
		}
		return current, nil
	}
	value, err := tools.CaptureBash(ctx, x.engine.filePath, script, current.options.Workdir, x.stderr)
	if err != nil {
		return current, err
	}
	current.vars[command.Name] = value
	return current, nil
}

func (x *taskExecution) changeWorkdir(current execContext, command dsl.CdCommand) (execContext, error) {
	rendered, err := dsl.RenderTemplate(command.Path, current.vars)
	if err != nil {
		return current, fmt.Errorf("task %d /cd template failed: %w", x.task.BlockIndex+1, err)
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

func (x *taskExecution) expandInlineCalls(ctx context.Context, current execContext, prompt string) (string, error) {
	lines := dsl.SplitLines(prompt)
	if len(lines) == 0 {
		return prompt, nil
	}
	var out strings.Builder
	for _, line := range lines {
		call, ok := dsl.ParseInlineCallLine(line)
		if !ok {
			out.WriteString(line)
			continue
		}
		value, returned, err := x.callDefinition(ctx, current, call, true)
		if err != nil {
			return "", err
		}
		if !returned {
			return "", fmt.Errorf("/call %s returned no value", call.Name)
		}
		text := dsl.StringValue(value)
		out.WriteString(text)
		if strings.HasSuffix(line, "\n") && !strings.HasSuffix(text, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

func (x *taskExecution) callDefinition(ctx context.Context, current execContext, call dsl.Call, needReturn bool) (any, bool, error) {
	def, ok := x.engine.defs[call.Name]
	if !ok {
		return nil, false, fmt.Errorf("unknown definition %q", call.Name)
	}
	if len(call.Args) != len(def.Params) {
		return nil, false, fmt.Errorf("/call %s expects %d argument(s), got %d", call.Name, len(def.Params), len(call.Args))
	}
	callID := x.engine.nextCallID()
	state := &callState{}
	vars := dsl.CloneVars(current.vars)
	for i, param := range def.Params {
		value, err := dsl.RenderTemplate(call.Args[i], current.vars)
		if err != nil {
			return nil, false, fmt.Errorf("/call %s argument template failed: %w", call.Name, err)
		}
		vars[param] = value
	}
	poolPrefix := fmt.Sprintf("__call_%d_", callID)
	asyncPool := fmt.Sprintf("__call_%d", callID)
	base := execContext{
		vars:       vars,
		options:    dsl.RunOptions{Workdir: current.options.Workdir, Skills: append([]dsl.SkillRuntime{}, current.options.Skills...), MCPs: append([]dsl.MCPRuntime{}, current.options.MCPs...), DefMCP: cloneDefMCPRuntime(current.options.DefMCP), DefDepth: current.options.DefDepth},
		loopOp:     -1,
		agent:      appendAgentLabel(current.agent, "call="+call.Name),
		poolPrefix: poolPrefix,
		callState:  state,
	}
	var fallback []byte
	callAsyncPools := map[string]struct{}{}
	for i, body := range def.Blocks {
		if pools, ok, err := dsl.ParseGlobalPoolBlock(body); err != nil {
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
		if bindings, ok, err := dsl.ParseGlobalLetBlock(body); err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		} else if ok {
			if err := x.applyLocalLetBindings(ctx, bindings, vars, base.options.Workdir); err != nil {
				return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
			}
			base.vars = vars
			continue
		}
		task, err := dsl.ParseTaskForFile(def.SourcePath, x.task.BlockIndex, body, vars, dsl.CompileOptions{Root: defRoot(def)})
		if err != nil {
			return nil, false, err
		}
		blockCtx := base
		blockCtx.vars = dsl.CloneVars(task.Vars)
		blockDBs, err := x.engine.applyDBConfig(current.options.DBs, task.DB)
		if err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		}
		blockCtx.options = dsl.MergeRunOptions(base.options, dsl.RunOptions{DBs: blockDBs})
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
		_, background, err := child.execute(ctx, blockCtx, task.Ops, 0, true)
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
			asyncTask := x.engine.async.register(key, taskPool)
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
		if len(child.structuredOutputs) > 0 {
			fallback = child.structuredOutputs[len(child.structuredOutputs)-1]
		}
	}
	for pool := range callAsyncPools {
		if err := x.engine.async.waitUpTo(0, pool); err != nil {
			return nil, false, err
		}
	}
	if len(fallback) == 0 {
		outputs := state.snapshotOutputs()
		if len(outputs) > 0 {
			fallback = outputs[len(outputs)-1]
		}
	}
	if needReturn && len(fallback) > 0 {
		value, err := parseJSONReturn(fallback)
		if err != nil {
			return nil, false, err
		}
		return value, true, nil
	}
	return nil, false, nil
}

func (x *taskExecution) mergeChildCallState(child *taskExecution, state *callState) {
	child.mu.Lock()
	messages := append([]dsl.OutputMessage{}, child.messages...)
	child.mu.Unlock()
	x.mu.Lock()
	x.messages = append(x.messages, messages...)
	x.mu.Unlock()
}

func (x *taskExecution) applyLocalLetBindings(ctx context.Context, bindings []dsl.LetBinding, vars map[string]any, workdir string) error {
	for _, binding := range bindings {
		if binding.BashScript == "" {
			vars[binding.Name] = binding.Value
			continue
		}
		script, err := dsl.RenderTemplate(binding.BashScript, vars)
		if err != nil {
			return fmt.Errorf("/let %s /bash template failed: %w", binding.Name, err)
		}
		value, err := tools.CaptureBash(ctx, x.engine.filePath, script, workdir, x.stderr)
		if err != nil {
			return fmt.Errorf("/let %s /bash failed: %w", binding.Name, err)
		}
		vars[binding.Name] = value
	}
	return nil
}

func (x *taskExecution) evaluateReturn(ctx context.Context, current execContext, spec dsl.ReturnSpec) (any, error) {
	vars := dsl.CloneVars(current.vars)
	vars["agent"] = agentReturnData(current.callState.snapshotMessages(), x.engine.messages)
	switch spec.Kind {
	case dsl.ReturnBash:
		script, err := dsl.RenderTemplate(spec.Script, vars)
		if err != nil {
			return nil, fmt.Errorf("/return /bash template failed: %w", err)
		}
		return tools.CaptureBash(ctx, x.engine.filePath, script, current.options.Workdir, x.stderr)
	case dsl.ReturnStructured:
		if len(x.structuredOutputs) == 0 {
			return nil, fmt.Errorf("/return structured output missing MCP result")
		}
		return parseJSONReturn(x.structuredOutputs[len(x.structuredOutputs)-1])
	default:
		return dsl.RenderTemplate(spec.Text, vars)
	}
}

func agentReturnData(messages []dsl.OutputMessage, limit int) map[string]any {
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

func latestMessageText(messages []dsl.OutputMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if text := strings.TrimSpace(messages[i].Text); text != "" {
			return text
		}
	}
	return ""
}

func defRoot(def dsl.Definition) string {
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

func (x *taskExecution) executePrompt(ctx context.Context, current execContext, opts dsl.RunOptions, opIndex int) (int, error) {
	renderedOptions, err := renderRunOptions(dsl.MergeRunOptions(current.options, opts), current.vars)
	if err != nil {
		return 0, fmt.Errorf("task %d args template failed: %w", x.task.BlockIndex+1, err)
	}
	prompt, err := dsl.RenderTemplate(x.task.Prompt, current.vars)
	if err != nil {
		return 0, fmt.Errorf("task %d prompt template failed: %w", x.task.BlockIndex+1, err)
	}
	prompt, err = x.expandInlineCalls(ctx, current, prompt)
	if err != nil {
		return 0, err
	}
	outputVars := outputTemplateVars(current)
	outputSpec, err := renderOutputSpec(x.task.Output, outputVars)
	if err != nil {
		return 0, fmt.Errorf("task %d output template failed: %w", x.task.BlockIndex+1, err)
	}
	returnOutputSpec, err := renderReturnOutputSpec(x.task.Return, outputVars)
	if err != nil {
		return 0, fmt.Errorf("task %d return output template failed: %w", x.task.BlockIndex+1, err)
	}
	activeOutputSpec := outputSpec
	outputWritesFile := outputSpec != nil
	if activeOutputSpec == nil && returnOutputSpec != nil {
		activeOutputSpec = returnOutputSpec
	}
	if activeOutputSpec != nil && activeOutputSpec.IsStructured() {
		renderedOptions.Output = activeOutputSpec
	}
	if strings.TrimSpace(prompt) == "" {
		return 0, nil
	}
	start := time.Now()
	agentDetail := ""
	if current.agent != "" {
		agentDetail = " [" + current.agent + "]"
	}
	writeATMEvent(x.stderr, "run", "task %d%s step %d via %s%s", x.task.BlockIndex+1, x.engine.taskLineRangeLabel(x.task.BlockIndex), runningOpIndex(current, opIndex)+1, x.engine.runner.Name(), agentDetail)
	result, err := x.engine.runner.Execute(ctx, x.engine.filePath, prompt, renderedOptions, x.stdout, x.stderr)
	if err != nil {
		return 0, fmt.Errorf("task %d run failed: %w", x.task.BlockIndex+1, err)
	}

	x.mu.Lock()
	x.runs++
	runNumber := x.runs
	x.messages = append(x.messages, annotateMessages(result.Messages, current.agent)...)
	if current.callState != nil {
		current.callState.addMessages(annotateMessages(result.Messages, current.agent))
	}
	messages := x.recentMessagesLocked()
	_, eventErr := x.engine.outputs.writeEvents(x.task.BlockIndex, runNumber, x.engine.runner.Name(), current.agent, result.RawEvents)
	if eventErr != nil {
		x.mu.Unlock()
		return 1, eventErr
	}
	structuredOutputPath := ""
	if activeOutputSpec != nil && activeOutputSpec.IsStructured() {
		if len(result.StructuredOutput) == 0 {
			x.mu.Unlock()
			return 1, fmt.Errorf("task %d structured output missing MCP result", x.task.BlockIndex+1)
		}
		suffix := ""
		if current.background {
			suffix = outputAgentSuffix(current)
		}
		if outputWritesFile {
			structuredOutputPath, err = x.engine.outputs.writeStructuredOutput(x.task.BlockIndex, runNumber, activeOutputSpec.FileName, suffix, result.StructuredOutput)
			if err != nil {
				x.mu.Unlock()
				return 1, err
			}
		}
		x.structuredOutputs = append(x.structuredOutputs, append([]byte{}, result.StructuredOutput...))
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
			structuredOutputPath, err = x.engine.outputs.writeTextOutput(x.task.BlockIndex, runNumber, outputSpec.FileName, suffix, []byte(text+"\n"))
			if err != nil {
				x.mu.Unlock()
				return 1, err
			}
		}
	}
	if x.writeState {
		x.lease, err = store.SaveRunning(x.engine.filePath, x.lease, dsl.RunningInfo{
			Active:    true,
			Start:     x.start,
			StepIndex: runningOpIndex(current, opIndex),
			StepRuns:  runningStepRuns(current, runNumber),
			TotalRuns: runNumber,
			Messages:  messages,
		})
	}
	x.mu.Unlock()
	if err != nil {
		if errors.Is(err, store.ErrObsolete) {
			return 1, err
		}
		return 1, err
	}
	finished := time.Now()
	writeATMEvent(x.stderr, "done", "task %d run %d at %s in %s", x.task.BlockIndex+1, runNumber, finished.Format(time.RFC3339), finished.Sub(start).Round(time.Millisecond))
	if structuredOutputPath != "" {
		writeATMEvent(x.stderr, "output", "task %d run %d %s", x.task.BlockIndex+1, runNumber, structuredOutputPath)
	}
	return 1, nil
}

func (x *taskExecution) recentMessagesLocked() []dsl.OutputMessage {
	limit := x.engine.messages
	if limit <= 0 || len(x.messages) == 0 {
		return nil
	}
	if !hasAgentLabels(x.messages) {
		start := len(x.messages) - limit
		if start < 0 {
			start = 0
		}
		recent := make([]dsl.OutputMessage, len(x.messages[start:]))
		copy(recent, x.messages[start:])
		return recent
	}

	counts := make(map[string]int)
	var reversed []dsl.OutputMessage
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
	recent := make([]dsl.OutputMessage, len(reversed))
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

func annotateMessages(messages []dsl.OutputMessage, agent string) []dsl.OutputMessage {
	if agent == "" || len(messages) == 0 {
		return messages
	}
	out := make([]dsl.OutputMessage, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].Agent = agent
	}
	return out
}

func hasAgentLabels(messages []dsl.OutputMessage) bool {
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

func renderRunOptions(opts dsl.RunOptions, vars map[string]any) (dsl.RunOptions, error) {
	rendered := dsl.RunOptions{
		Resume:   opts.Resume,
		Output:   opts.Output,
		DBs:      append([]dsl.DBRuntime{}, opts.DBs...),
		Workdir:  opts.Workdir,
		Skills:   append([]dsl.SkillRuntime{}, opts.Skills...),
		MCPs:     append([]dsl.MCPRuntime{}, opts.MCPs...),
		DefMCP:   cloneDefMCPRuntime(opts.DefMCP),
		DefDepth: opts.DefDepth,
	}
	for _, arg := range opts.Args {
		value, err := dsl.RenderTemplate(arg, vars)
		if err != nil {
			return dsl.RunOptions{}, err
		}
		rendered.Args = append(rendered.Args, value)
	}
	if rendered.DefMCP != nil {
		rendered.DefMCP.Workdir = rendered.Workdir
		rendered.DefMCP.DBs = append([]dsl.DBRuntime{}, rendered.DBs...)
		rendered.DefMCP.Skills = append([]dsl.SkillRuntime{}, rendered.Skills...)
		rendered.DefMCP.MCPs = append([]dsl.MCPRuntime{}, rendered.MCPs...)
		rendered.DefMCP.Vars = dsl.CloneVars(vars)
		rendered.DefMCP.Depth = rendered.DefDepth
	}
	return rendered, nil
}

func cloneDefMCPRuntime(in *dsl.DefMCPRuntime) *dsl.DefMCPRuntime {
	if in == nil {
		return nil
	}
	out := *in
	out.Definitions = append([]string{}, in.Definitions...)
	out.DBs = append([]dsl.DBRuntime{}, in.DBs...)
	out.Skills = append([]dsl.SkillRuntime{}, in.Skills...)
	out.MCPs = append([]dsl.MCPRuntime{}, in.MCPs...)
	if in.Vars != nil {
		out.Vars = dsl.CloneVars(in.Vars)
	}
	out.Defs = append([]dsl.DefinitionRef{}, in.Defs...)
	return &out
}
