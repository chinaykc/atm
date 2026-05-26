package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/document"
	"github.com/chinaykc/atm/pkg/lang/expr"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"github.com/chinaykc/atm/pkg/runtime/store"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

type Options struct {
	FilePath     string
	Runner       agent.Runner
	ToolName     string
	CodexPath    string
	ClaudePath   string
	Stdout       io.Writer
	Stderr       io.Writer
	MessageLimit int
	OutputDir    string
	GlobalJobs   int
	Snapshot     bool
}

type Engine struct {
	filePath   string
	document   string
	root       string
	runner     agent.Runner
	toolName   string
	codexPath  string
	claudePath string
	stdout     io.Writer
	stderr     io.Writer
	outputs    *outputRegistry
	async      *asyncGroup
	pools      *poolManager
	defs       map[string]compiler.Definition
	defItems   []compiler.Definition
	dbs        map[string]compiler.DBDecl
	dbItems    []compiler.DBDecl
	skills     map[string]compiler.SkillDecl
	skillItems []compiler.SkillDecl
	mcps       map[string]compiler.MCPDecl
	mcpItems   []compiler.MCPDecl
	bashVars   map[string]string
	callSeq    int
	start      time.Time
	messages   int
	snapshot   bool
	blockLimit int
	abandoning bool
	mu         sync.Mutex
	stateMu    sync.Mutex
}

type scopedRuntimeLet struct {
	binding compiler.LetBinding
	value   any
	scope   []string
	line    int
}

type scopedRuntimePool struct {
	decl  compiler.PoolDecl
	scope []string
	line  int
}

func Run(ctx context.Context, opts Options) error {
	e, err := New(opts)
	if err != nil {
		return err
	}
	return e.Run(ctx)
}

func New(opts Options) (*Engine, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.MessageLimit <= 0 {
		opts.MessageLimit = 1
	}
	if opts.GlobalJobs <= 0 {
		opts.GlobalJobs = runtime.NumCPU()
	}
	outputs, err := newOutputRegistry(opts.OutputDir)
	if err != nil {
		return nil, err
	}
	var stdMu sync.Mutex
	return &Engine{
		filePath:   opts.FilePath,
		document:   opts.FilePath,
		root:       filepath.Dir(opts.FilePath),
		runner:     opts.Runner,
		toolName:   opts.ToolName,
		codexPath:  opts.CodexPath,
		claudePath: opts.ClaudePath,
		stdout:     lockedWriter{w: opts.Stdout, mu: &stdMu},
		stderr:     lockedWriter{w: opts.Stderr, mu: &stdMu},
		outputs:    outputs,
		async:      newAsyncGroup(),
		pools:      newPoolManager(opts.GlobalJobs),
		defs:       make(map[string]compiler.Definition),
		dbs:        make(map[string]compiler.DBDecl),
		skills:     make(map[string]compiler.SkillDecl),
		mcps:       make(map[string]compiler.MCPDecl),
		bashVars:   make(map[string]string),
		start:      time.Now(),
		messages:   opts.MessageLimit,
		snapshot:   opts.Snapshot,
	}, nil
}

func (e *Engine) Run(ctx context.Context) (runErr error) {
	workspace, err := store.PrepareWorkspace(e.filePath, e.stderr)
	if err != nil {
		return err
	}
	e.filePath = workspace.Active
	e.document = workspace.Original
	e.root = filepath.Dir(workspace.Original)
	if err := e.loadDefinitions(workspace.Original); err != nil {
		return err
	}

	stopSignals := store.SetupRestoreSignals(workspace, e.stderr)
	defer stopSignals()
	defer func() {
		if resultPath, err := e.outputs.writeResultDocument(e.filePath); err == nil {
			writeATMEvent(e.stderr, "result", "%s", resultPath)
		} else if runErr == nil {
			runErr = err
		}
		if reportErr := e.report(); runErr == nil {
			runErr = reportErr
		}
		if err := workspace.Restore(); runErr == nil {
			runErr = err
		}
	}()

	initialBlocks, err := store.ReadBlocks(e.filePath)
	if err != nil {
		return err
	}
	if e.snapshot {
		e.blockLimit = len(initialBlocks)
	}
	for {
		blocks, err := store.ReadBlocks(e.filePath)
		if err != nil {
			if errors.Is(err, store.ErrNoTasks) || strings.Contains(err.Error(), "no tasks found") {
				if e.async.hasPending() {
					e.abandonBackgroundWork()
				}
				return nil
			}
			return err
		}
		if e.blockLimit > 0 && len(blocks) > e.blockLimit {
			blocks = blocks[:e.blockLimit]
		}
		index, globals, err := e.firstRunnableBlock(ctx, blocks)
		if err != nil {
			if errors.Is(err, store.ErrObsolete) {
				continue
			}
			return fmt.Errorf("parse todo file %q: %w", e.filePath, err)
		}
		if index == -1 {
			if e.async.hasPending() {
				e.abandonBackgroundWork()
			}
			return nil
		}

		blockContext := e.blockExecutionContext(blocks, index)
		task, err := compiler.ParseTaskForFile(e.filePath, index, blocks[index].Body, globals, compiler.CompileOptions{Root: e.root, Context: blockContext})
		if err != nil {
			return err
		}
		task.SourcePromptHash = marker.SourcePromptHash(blocks[index].Body, blocks[index].Context)
		task.SourcePath = workspace.Original
		task.Scope = slices.Clone(blocks[index].Scope)
		task.Line = blocks[index].StartLine
		if task.Return != nil {
			return fmt.Errorf("task %d: /return is only allowed inside /def", index+1)
		}
		if index+1 < len(blocks) && compiler.TaskHasFlowIf(task) {
			if elseInfo, ok, err := compiler.ParseElseBlock(blocks[index+1].Body); err != nil {
				return fmt.Errorf("task %d: %w", index+2, err)
			} else if ok {
				var elseTask compiler.Task
				if elseInfo.HeaderOnly {
					elseTask = compiler.EmptyTask(index+1, globals)
				} else {
					elseTask, err = compiler.ParseTaskForFile(e.filePath, index+1, blocks[index+1].Body, globals, compiler.CompileOptions{Root: e.root, Context: blocks[index+1].Context})
					if err != nil {
						return err
					}
					elseTask.SourcePromptHash = marker.SourcePromptHash(blocks[index+1].Body, blocks[index+1].Context)
				}
				elseTask.SourcePath = workspace.Original
				elseTask.Scope = slices.Clone(blocks[index+1].Scope)
				elseTask.Line = blocks[index+1].StartLine
				task, err = compiler.AttachElseTask(task, elseTask)
				if err != nil {
					return fmt.Errorf("task %d: %w", index+2, err)
				}
			}
		}
		dbs, err := e.taskDBs(task)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		skills, err := e.taskSkills(task)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		mcps, err := e.taskMCPs(task)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		defMCP, err := e.taskDefMCP(task, dbs, skills, mcps)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		lease := store.NewBlockLease(index, blocks[index].Body)
		err = e.runTask(ctx, lease, task, compiler.RunOptions{DBs: dbs, Skills: skills, MCPs: mcps, DefMCP: defMCP, DefDepth: 1})
		if errors.Is(err, store.ErrObsolete) {
			continue
		}
		if err != nil {
			return err
		}
	}
}

func (e *Engine) loadDefinitions(sourcePath string) error {
	data, err := os.ReadFile(e.filePath)
	if err != nil {
		return err
	}
	set, err := compiler.LoadDefinitionSet(sourcePath, string(data), compiler.CompileOptions{Root: filepath.Dir(sourcePath)})
	if err != nil {
		return err
	}
	e.defs = set.Definitions
	e.defItems = slices.Clone(set.Items)
	return nil
}

func (e *Engine) loadGlobalDeclarations() error {
	data, err := os.ReadFile(e.filePath)
	if err != nil {
		return err
	}
	blocks := document.ParseBlocks(string(data))
	for i, block := range blocks {
		body := block.Body
		if marker.IsDone(body) || strings.TrimSpace(body) == "" {
			continue
		}
		if pools, ok, err := compiler.ParseGlobalPoolBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			for _, pool := range pools {
				pool.BlockIndex = i
				if err := e.pools.declare(pool); err != nil {
					return fmt.Errorf("task %d: %w", i+1, err)
				}
			}
			continue
		}
		if db, ok, err := compiler.ParseGlobalDBBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			db.BlockIndex = i
			db.SourcePath = e.document
			db.ScopePath = slices.Clone(block.Scope)
			db.Line = block.StartLine
			if err := e.registerDBDecl(db); err != nil {
				return fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		if skill, ok, err := compiler.ParseGlobalSkillBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			skill.BlockIndex = i
			skill.SourcePath = e.document
			skill.Scope = slices.Clone(block.Scope)
			skill.Line = block.StartLine
			if err := e.registerSkillDecl(skill); err != nil {
				return fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		if mcp, ok, err := compiler.ParseGlobalMCPBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			mcp.BlockIndex = i
			if isBuiltinMCPName(mcp.Name) {
				return fmt.Errorf("task %d: mcp name %q conflicts with an ATM builtin MCP server", i+1, mcp.Name)
			}
			mcp.SourcePath = e.document
			mcp.Scope = slices.Clone(block.Scope)
			mcp.Line = block.StartLine
			if err := e.registerMCPDecl(mcp); err != nil {
				return fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
	}
	return nil
}

func (e *Engine) registerDBDecl(db compiler.DBDecl) error {
	if existing, exists := e.dbs[db.Name]; exists && existing.BlockIndex != db.BlockIndex {
		return fmt.Errorf("duplicate db %q", db.Name)
	}
	e.dbs[db.Name] = db
	for i, item := range e.dbItems {
		if item.Name == db.Name && item.BlockIndex == db.BlockIndex {
			e.dbItems[i] = db
			return nil
		}
	}
	e.dbItems = append(e.dbItems, db)
	return nil
}

func (e *Engine) registerSkillDecl(skill compiler.SkillDecl) error {
	if existing, exists := e.skills[skill.Name]; exists && existing.BlockIndex != skill.BlockIndex {
		return fmt.Errorf("duplicate skill %q", skill.Name)
	}
	e.skills[skill.Name] = skill
	for i, item := range e.skillItems {
		if item.Name == skill.Name && item.BlockIndex == skill.BlockIndex {
			e.skillItems[i] = skill
			return nil
		}
	}
	e.skillItems = append(e.skillItems, skill)
	return nil
}

func (e *Engine) registerMCPDecl(mcp compiler.MCPDecl) error {
	if existing, exists := e.mcps[mcp.Name]; exists && existing.BlockIndex != mcp.BlockIndex {
		return fmt.Errorf("duplicate mcp %q", mcp.Name)
	}
	e.mcps[mcp.Name] = mcp
	for i, item := range e.mcpItems {
		if item.Name == mcp.Name && item.BlockIndex == mcp.BlockIndex {
			e.mcpItems[i] = mcp
			return nil
		}
	}
	e.mcpItems = append(e.mcpItems, mcp)
	return nil
}

func (e *Engine) nextCallID() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callSeq++
	return e.callSeq
}

func (e *Engine) resolveDefinition(name string, ref definitionScopeRef) (compiler.Definition, bool) {
	var best compiler.Definition
	found := false
	bestDepth := -1
	bestLine := -1
	for _, def := range e.defItems {
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
	if found {
		return best, true
	}
	def, ok := e.defs[name]
	return def, ok && definitionVisibleAt(def, ref)
}

func definitionVisibleAt(def compiler.Definition, ref definitionScopeRef) bool {
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

func (e *Engine) abandonBackgroundWork() {
	e.mu.Lock()
	e.abandoning = true
	e.mu.Unlock()
	for _, task := range e.async.abandonAll() {
		pool := task.Pool
		if pool == "" {
			pool = "default"
		}
		if task.LogPath != "" {
			writeATMEvent(e.stderr, "background", "leaving async #%d block %d pool %s running without /wait; log %s", task.ID, task.Block, pool, task.LogPath)
		} else {
			writeATMEvent(e.stderr, "background", "leaving async #%d block %d pool %s running without /wait", task.ID, task.Block, pool)
		}
	}
}

func (e *Engine) isAbandoningBackground() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.abandoning
}

func (e *Engine) firstRunnableBlock(ctx context.Context, blocks []compiler.Block) (int, map[string]any, error) {
	var lets []scopedRuntimeLet
	var pools []scopedRuntimePool
	parentVars := make(map[int]map[string]any)
	for i := range blocks {
		body := blocks[i].Body
		if marker.IsDone(body) || strings.TrimSpace(body) == "" {
			continue
		}
		if _, ok, err := compiler.ParseGlobalImportBlock(body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			continue
		}
		key := inFlightKey{index: i, hash: store.HashBody(body)}
		if e.async.hasPendingKey(key) {
			continue
		}
		bindings, ok, err := compiler.ParseGlobalLetBlock(body)
		if err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			var err error
			lets, err = e.appendGlobalLetBindings(ctx, blocks[i], bindings, lets)
			if err != nil {
				return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		poolDecls, ok, err := compiler.ParseGlobalPoolBlock(body)
		if err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			for _, pool := range poolDecls {
				pool.BlockIndex = i
				pool.SourcePath = e.document
				pool.Scope = slices.Clone(blocks[i].Scope)
				pool.Line = blocks[i].StartLine
				pools = append(pools, scopedRuntimePool{decl: pool, scope: slices.Clone(blocks[i].Scope), line: blocks[i].StartLine})
			}
			continue
		}
		db, ok, err := compiler.ParseGlobalDBBlock(body)
		if err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			db.BlockIndex = i
			db.SourcePath = e.document
			db.ScopePath = slices.Clone(blocks[i].Scope)
			db.Line = blocks[i].StartLine
			if err := e.registerDBDecl(db); err != nil {
				return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		if skill, ok, err := compiler.ParseGlobalSkillBlock(body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			skill.BlockIndex = i
			skill.SourcePath = e.document
			skill.Scope = slices.Clone(blocks[i].Scope)
			skill.Line = blocks[i].StartLine
			if err := e.registerSkillDecl(skill); err != nil {
				return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		if mcp, ok, err := compiler.ParseGlobalMCPBlock(body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			mcp.BlockIndex = i
			if isBuiltinMCPName(mcp.Name) {
				return -1, nil, fmt.Errorf("task %d: mcp name %q conflicts with an ATM builtin MCP server", i+1, mcp.Name)
			}
			mcp.SourcePath = e.document
			mcp.Scope = slices.Clone(blocks[i].Scope)
			mcp.Line = blocks[i].StartLine
			if err := e.registerMCPDecl(mcp); err != nil {
				return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		globals := visibleRuntimeLetVars(lets, blocks[i])
		globals = inheritRuntimeParentTaskVars(globals, parentVars, blocks[i])
		if err := e.declareVisiblePools(blocks[i], pools); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if e.hasPendingChildTask(blocks, i) {
			task, err := compiler.ParseTaskForFile(e.filePath, i, body, globals, compiler.CompileOptions{Root: e.root, Context: blocks[i].Context})
			if err != nil {
				return -1, nil, err
			}
			task.SourcePromptHash = marker.SourcePromptHash(blocks[i].Body, blocks[i].Context)
			task.SourcePath = e.document
			task.Scope = slices.Clone(blocks[i].Scope)
			task.Line = blocks[i].StartLine
			parentVars[i] = e.inheritableRuntimeTaskVars(task)
			continue
		}
		if index, handled, err := e.handleConditionalBlock(ctx, blocks, i, globals); err != nil {
			return -1, nil, err
		} else if handled {
			if index >= 0 {
				return index, globals, nil
			}
			continue
		}
		return i, globals, nil
	}
	return -1, map[string]any{}, nil
}

func (e *Engine) declareVisiblePools(block compiler.Block, pools []scopedRuntimePool) error {
	for _, item := range pools {
		if !runtimePoolVisibleAt(item, block) {
			continue
		}
		if err := e.pools.declare(item.decl); err != nil {
			return err
		}
	}
	return nil
}

func runtimePoolVisibleAt(item scopedRuntimePool, block compiler.Block) bool {
	if block.StartLine > 0 && item.line > 0 && item.line >= block.StartLine {
		return false
	}
	if len(item.scope) > len(block.Scope) {
		return false
	}
	for i := range item.scope {
		if item.scope[i] != block.Scope[i] {
			return false
		}
	}
	return true
}

func (e *Engine) hasPendingChildTask(blocks []compiler.Block, parent int) bool {
	for i := parent + 1; i < len(blocks); i++ {
		block := blocks[i]
		if !block.HasParent || block.ParentIndex != parent {
			continue
		}
		body := strings.TrimSpace(block.Body)
		if body == "" || marker.IsDone(block.Body) {
			continue
		}
		key := inFlightKey{index: i, hash: store.HashBody(block.Body)}
		if e.async.hasPendingKey(key) {
			continue
		}
		return true
	}
	return false
}

func (e *Engine) blockExecutionContext(blocks []compiler.Block, index int) string {
	context := strings.TrimSpace(blocks[index].Context)
	childReports := childTaskReportsContext(blocks, index)
	if childReports == "" {
		return context
	}
	if context == "" {
		return childReports
	}
	return context + "\n\n" + childReports
}

func childTaskReportsContext(blocks []compiler.Block, parent int) string {
	var reports []string
	for i := parent + 1; i < len(blocks); i++ {
		block := blocks[i]
		if !block.HasParent || block.ParentIndex != parent || marker.IsSkipped(block.Body) {
			continue
		}
		report := marker.VisibleATMReport(block.Body)
		if report == "" {
			continue
		}
		childContext := strings.TrimSpace(block.Context)
		if childContext != "" {
			reports = append(reports, fmt.Sprintf("### Child task block %d\n\n%s\n\n%s", i+1, childContext, report))
			continue
		}
		reports = append(reports, fmt.Sprintf("### Child task block %d\n\n%s", i+1, report))
	}
	if len(reports) == 0 {
		return ""
	}
	return "## Completed child task reports\n\n" + strings.Join(reports, "\n\n")
}

type conditionalGroup struct {
	ifIndex   int
	ifBlock   compiler.IfBlock
	thenStart int
	thenEnd   int
	hasElse   bool
	elseIndex int
	elseBlock compiler.ElseBlock
	elseStart int
	elseEnd   int
	end       int
}

func (e *Engine) handleConditionalBlock(ctx context.Context, blocks []compiler.Block, index int, globals map[string]any) (int, bool, error) {
	body := blocks[index].Body
	if elseBlock, ok, err := compiler.ParseElseBlock(body); err != nil {
		return -1, false, fmt.Errorf("task %d: %w", index+1, err)
	} else if ok {
		if index > 0 && marker.IsSkipped(blocks[index-1].Body) {
			if elseBlock.HeaderOnly {
				if err := e.markControlDone(index, body); err != nil {
					return -1, true, err
				}
				return -1, true, store.ErrObsolete
			}
			return index, true, nil
		}
		if index > 0 && marker.IsDone(blocks[index-1].Body) {
			previous, err := compiler.ParseTaskForFile(e.filePath, index-1, blocks[index-1].Body, globals, compiler.CompileOptions{Root: e.root, Context: blocks[index-1].Context})
			if err == nil && compiler.TaskHasFlowIf(previous) {
				if err := e.markControlDone(index, body); err != nil {
					return -1, true, err
				}
				return -1, true, store.ErrObsolete
			}
		}
		return -1, true, fmt.Errorf("task %d: /else has no matching false /if branch", index+1)
	}

	group, ok, err := e.parseConditionalGroup(blocks, index)
	if err != nil {
		return -1, false, fmt.Errorf("task %d: %w", index+1, err)
	}
	if !ok {
		return -1, false, nil
	}
	passed, err := e.evaluateIfCondition(ctx, group.ifBlock, globals)
	if err != nil {
		return -1, true, fmt.Errorf("task %d /if condition failed: %w", index+1, err)
	}
	if passed {
		if group.ifBlock.HeaderOnly {
			if err := e.markControlDone(group.ifIndex, blocks[group.ifIndex].Body); err != nil {
				return -1, true, err
			}
		} else if group.hasElse {
			if err := e.skipRange(blocks, group.elseIndex, group.elseEnd, "if condition evaluated true"); err != nil {
				return -1, true, err
			}
			return index, true, nil
		}
		if group.hasElse {
			if err := e.skipRange(blocks, group.elseIndex, group.elseEnd, "if condition evaluated true"); err != nil {
				return -1, true, err
			}
		}
		return -1, true, store.ErrObsolete
	}
	if group.ifBlock.HeaderOnly {
		if err := e.markSkipped(group.ifIndex, blocks[group.ifIndex], "if condition evaluated false"); err != nil {
			return -1, true, err
		}
		if err := e.skipRange(blocks, group.thenStart, group.thenEnd, "if condition evaluated false"); err != nil {
			return -1, true, err
		}
		return -1, true, store.ErrObsolete
	}
	if err := e.markSkipped(index, blocks[index], "if condition evaluated false"); err != nil {
		return -1, true, err
	}
	return -1, true, store.ErrObsolete
}

func (e *Engine) parseConditionalGroup(blocks []compiler.Block, index int) (conditionalGroup, bool, error) {
	ifBlock, ok, err := compiler.ParseIfBlock(blocks[index].Body)
	if err != nil || !ok {
		return conditionalGroup{}, ok, err
	}
	group := conditionalGroup{ifIndex: index, ifBlock: ifBlock}
	if !ifBlock.HeaderOnly {
		group.thenStart = index
		group.thenEnd = index + 1
		group.end = index + 1
		if index+1 < len(blocks) && !marker.IsDone(blocks[index+1].Body) {
			if elseBlock, ok, err := compiler.ParseElseBlock(blocks[index+1].Body); err != nil {
				return conditionalGroup{}, true, err
			} else if ok {
				group.hasElse = true
				group.elseIndex = index + 1
				group.elseBlock = elseBlock
				if elseBlock.HeaderOnly {
					elseEnd, err := e.nodeEnd(blocks, index+2)
					if err != nil {
						return conditionalGroup{}, true, err
					}
					group.elseStart = index + 2
					group.elseEnd = elseEnd
					group.end = elseEnd
				} else {
					group.elseStart = index + 1
					group.elseEnd = index + 2
					group.end = index + 2
				}
			}
		}
		return group, true, nil
	}
	thenEnd, err := e.nodeEnd(blocks, index+1)
	if err != nil {
		return conditionalGroup{}, true, err
	}
	group.thenStart = index + 1
	group.thenEnd = thenEnd
	if thenEnd >= len(blocks) {
		return conditionalGroup{}, true, fmt.Errorf("header-only /if requires a matching /else")
	}
	elseBlock, ok, err := compiler.ParseElseBlock(blocks[thenEnd].Body)
	if err != nil {
		return conditionalGroup{}, true, err
	}
	if !ok {
		return conditionalGroup{}, true, fmt.Errorf("header-only /if requires a matching /else")
	}
	group.hasElse = true
	group.elseIndex = thenEnd
	group.elseBlock = elseBlock
	if elseBlock.HeaderOnly {
		elseEnd, err := e.nodeEnd(blocks, thenEnd+1)
		if err != nil {
			return conditionalGroup{}, true, err
		}
		group.elseStart = thenEnd + 1
		group.elseEnd = elseEnd
		group.end = elseEnd
	} else {
		group.elseStart = thenEnd
		group.elseEnd = thenEnd + 1
		group.end = thenEnd + 1
	}
	return group, true, nil
}

func (e *Engine) nodeEnd(blocks []compiler.Block, index int) (int, error) {
	if index >= len(blocks) {
		return index, fmt.Errorf("conditional branch is missing a task block")
	}
	if marker.IsDone(blocks[index].Body) {
		return index + 1, nil
	}
	if _, ok, err := compiler.ParseElseBlock(blocks[index].Body); err != nil {
		return index, err
	} else if ok {
		return index, fmt.Errorf("/else appears before a branch body")
	}
	if _, ok, err := compiler.ParseIfBlock(blocks[index].Body); err != nil {
		return index, err
	} else if ok {
		return index, fmt.Errorf("nested /if is not supported; wrap complex branches in /def and /call it")
	}
	return index + 1, nil
}

func (e *Engine) evaluateIfCondition(ctx context.Context, block compiler.IfBlock, globals map[string]any) (bool, error) {
	condition, err := compiler.RenderTemplate(block.Condition.Text, globals)
	if err != nil {
		return false, fmt.Errorf("condition template failed: %w", err)
	}
	switch block.Condition.Kind {
	case compiler.ConditionExpr:
		return expr.EvalBool(condition, expr.Context{
			Vars:      globals,
			TodoFile:  e.filePath,
			Root:      e.root,
			OutputDir: e.outputs.dirPath(),
		})
	default:
		prompt, err := compiler.RenderTemplate(block.Body, globals)
		if err != nil {
			return false, fmt.Errorf("prompt template failed: %w", err)
		}
		return e.runner.Check(ctx, e.filePath, prompt, condition, compiler.RunOptions{}, e.stdout, e.stderr)
	}
}

func (e *Engine) skipRange(blocks []compiler.Block, start, end int, reason string) error {
	for i := start; i < end && i < len(blocks); i++ {
		if marker.IsDone(blocks[i].Body) || strings.TrimSpace(blocks[i].Body) == "" {
			continue
		}
		if err := e.markSkipped(i, blocks[i], reason); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) markSkipped(index int, block compiler.Block, reason string) error {
	body := block.Body
	lease := store.NewBlockLease(index, body)
	id, _, report, err := store.LeaseReportIdentity(e.filePath, lease)
	if err != nil {
		return err
	}
	source := marker.SourcePromptHash(block.Body, block.Context)
	now := time.Now()
	info := compiler.SkippedInfo{Time: now, Reason: reason, ID: id, Source: source, Report: report}
	if err := store.MarkSkipped(e.filePath, lease, info); err != nil {
		return err
	}
	return e.updateTaskState(index, taskStateUpdate{
		ID:               id,
		Status:           "skipped",
		SourcePromptHash: source,
		StartedAt:        now,
		UpdatedAt:        now,
		Runs:             0,
		Report:           report,
	})
}

func (e *Engine) markControlDone(index int, body string) error {
	lease := store.NewBlockLease(index, body)
	now := time.Now()
	id, source, report, err := store.LeaseReportIdentity(e.filePath, lease)
	if err != nil {
		return err
	}
	info := compiler.DoneInfo{Start: now, End: now, Runs: 0, ID: id, Source: source, Report: report}
	if err := store.MarkDone(e.filePath, lease, info); err != nil {
		return err
	}
	return e.updateTaskState(index, taskStateUpdate{
		ID:               id,
		Status:           "done",
		SourcePromptHash: source,
		StartedAt:        now,
		UpdatedAt:        now,
		Runs:             0,
		Report:           report,
	})
}

func (e *Engine) appendGlobalLetBindings(ctx context.Context, block compiler.Block, bindings []compiler.LetBinding, lets []scopedRuntimeLet) ([]scopedRuntimeLet, error) {
	_ = ctx
	vars := visibleRuntimeLetVars(lets, block)
	for _, binding := range bindings {
		value := any(binding.Value)
		if binding.BashScript == "" {
			value = binding.Value
		} else {
			value = &lazyProvider{
				kind:    lazyProviderBash,
				bash:    compiler.BashCommand{Name: binding.Name, Script: binding.BashScript},
				vars:    ir.CloneVars(vars),
				options: compiler.RunOptions{},
				defRef:  definitionScopeRef{SourcePath: e.document, Scope: block.Scope, Line: block.StartLine},
			}
		}
		lets = append(lets, scopedRuntimeLet{
			binding: binding,
			value:   value,
			scope:   slices.Clone(block.Scope),
			line:    block.StartLine,
		})
		vars[binding.Name] = value
	}
	return lets, nil
}

func visibleRuntimeLetVars(lets []scopedRuntimeLet, block compiler.Block) map[string]any {
	vars := make(map[string]any)
	for _, item := range lets {
		if !runtimeLetVisibleAt(item, block) {
			continue
		}
		vars[item.binding.Name] = item.value
	}
	return vars
}

func inheritRuntimeParentTaskVars(globals map[string]any, parentVars map[int]map[string]any, block compiler.Block) map[string]any {
	if !block.HasParent || block.ParentIndex < 0 {
		return globals
	}
	vars, ok := parentVars[block.ParentIndex]
	if !ok {
		return globals
	}
	out := ir.CloneVars(vars)
	for name, value := range globals {
		out[name] = value
	}
	return out
}

func (e *Engine) inheritableRuntimeTaskVars(task compiler.Task) map[string]any {
	vars := ir.CloneVars(task.Vars)
	ref := definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	e.collectInheritableRuntimeFlowVars(task.Flow, vars, compiler.RunOptions{}, ref)
	return vars
}

func (e *Engine) collectInheritableRuntimeFlowVars(node compiler.FlowNode, vars map[string]any, options compiler.RunOptions, ref definitionScopeRef) {
	switch node.Kind {
	case compiler.FlowBash:
		if node.Bash.Name != "" {
			vars[node.Bash.Name] = &lazyProvider{
				kind:    lazyProviderBash,
				bash:    node.Bash,
				vars:    ir.CloneVars(vars),
				options: options,
				defRef:  ref,
			}
		}
	case compiler.FlowCall:
		if node.Call.Assign != "" {
			call := node.Call
			assign := call.Assign
			call.Assign = ""
			vars[assign] = &lazyProvider{
				kind:    lazyProviderCall,
				call:    call,
				vars:    ir.CloneVars(vars),
				options: options,
				defRef:  ref,
			}
		}
	}
	for _, child := range node.Children {
		e.collectInheritableRuntimeFlowVars(child, vars, options, ref)
	}
	for _, child := range node.ElseChildren {
		e.collectInheritableRuntimeFlowVars(child, vars, options, ref)
	}
}

func runtimeLetVisibleAt(item scopedRuntimeLet, block compiler.Block) bool {
	if block.StartLine > 0 && item.line > 0 && item.line >= block.StartLine {
		return false
	}
	if len(item.scope) > len(block.Scope) {
		return false
	}
	for i := range item.scope {
		if item.scope[i] != block.Scope[i] {
			return false
		}
	}
	return true
}

func (e *Engine) report() error {
	finished := time.Now()
	files := e.outputs.list()
	writeATMEvent(e.stderr, "done", "run finished at %s in %s", finished.Format(time.RFC3339), finished.Sub(e.start).Round(time.Millisecond))
	writeATMEvent(e.stderr, "output", "directory %s", e.outputs.dirPath())
	if len(files) == 0 {
		writeATMEvent(e.stderr, "artifacts", "none")
		return nil
	}
	writeATMEvent(e.stderr, "artifacts", "%d file(s)", len(files))
	for _, file := range files {
		writeATMEvent(e.stderr, "artifact", "%s", file)
	}
	return nil
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
