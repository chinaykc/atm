package engine

import (
	"atm/pkg/dsl"
	"atm/pkg/expr"
	"atm/pkg/store"
	"atm/pkg/tools"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Options struct {
	FilePath     string
	Runner       tools.Runner
	ToolName     string
	CodexPath    string
	ClaudePath   string
	Stdout       io.Writer
	Stderr       io.Writer
	MessageLimit int
	OutputDir    string
	GlobalJobs   int
}

type Engine struct {
	filePath   string
	root       string
	runner     tools.Runner
	toolName   string
	codexPath  string
	claudePath string
	stdout     io.Writer
	stderr     io.Writer
	outputs    *outputRegistry
	async      *asyncGroup
	pools      *poolManager
	defs       map[string]dsl.Definition
	dbs        map[string]dsl.DBDecl
	skills     map[string]dsl.SkillDecl
	mcps       map[string]dsl.MCPDecl
	bashVars   map[string]string
	callSeq    int
	start      time.Time
	messages   int
	mu         sync.Mutex
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
		defs:       make(map[string]dsl.Definition),
		dbs:        make(map[string]dsl.DBDecl),
		skills:     make(map[string]dsl.SkillDecl),
		mcps:       make(map[string]dsl.MCPDecl),
		bashVars:   make(map[string]string),
		start:      time.Now(),
		messages:   opts.MessageLimit,
	}, nil
}

func (e *Engine) Run(ctx context.Context) (runErr error) {
	workspace, err := store.PrepareWorkspace(e.filePath, e.stderr)
	if err != nil {
		return err
	}
	e.filePath = workspace.Active
	e.root = filepath.Dir(workspace.Original)
	if err := e.loadDefinitions(workspace.Original); err != nil {
		return err
	}

	stopSignals := store.SetupRestoreSignals(workspace, e.stderr)
	defer stopSignals()
	defer func() {
		if e.async.hasPending() && runErr == nil {
			runErr = e.async.waitAll()
		}
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

	if _, err := store.ReadBlocks(e.filePath); err != nil {
		return err
	}
	for {
		blocks, err := store.ReadBlocks(e.filePath)
		if err != nil {
			return err
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
				if err := e.async.waitAll(); errors.Is(err, store.ErrObsolete) {
					continue
				} else if err != nil {
					return err
				}
				continue
			}
			return nil
		}

		task, err := dsl.ParseTaskForFile(e.filePath, index, blocks[index].Body, globals, dsl.CompileOptions{Root: e.root})
		if err != nil {
			return err
		}
		dbs, err := e.taskDBs(task.DB)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		skills, err := e.taskSkills(task.Skill)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		mcps, err := e.taskMCPs(task.MCP)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		defMCP, err := e.taskDefMCP(task.MCP, dbs, skills, mcps)
		if err != nil {
			return fmt.Errorf("task %d: %w", task.BlockIndex+1, err)
		}
		lease := store.NewBlockLease(index, blocks[index].Body)
		err = e.runTask(ctx, lease, task, dsl.RunOptions{DBs: dbs, Skills: skills, MCPs: mcps, DefMCP: defMCP, DefDepth: 1})
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
	set, err := dsl.LoadDefinitionSet(sourcePath, string(data), dsl.CompileOptions{Root: filepath.Dir(sourcePath)})
	if err != nil {
		return err
	}
	e.defs = set.Definitions
	return nil
}

func (e *Engine) loadGlobalDeclarations() error {
	data, err := os.ReadFile(e.filePath)
	if err != nil {
		return err
	}
	blocks := dsl.ParseBlocks(string(data))
	for i, block := range blocks {
		body := block.Body
		if dsl.IsDone(body) || strings.TrimSpace(body) == "" {
			continue
		}
		if pools, ok, err := dsl.ParseGlobalPoolBlock(body); err != nil {
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
		if db, ok, err := dsl.ParseGlobalDBBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			db.BlockIndex = i
			if existing, exists := e.dbs[db.Name]; exists && existing.BlockIndex != i {
				return fmt.Errorf("task %d: duplicate db %q", i+1, db.Name)
			}
			e.dbs[db.Name] = db
			continue
		}
		if skill, ok, err := dsl.ParseGlobalSkillBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			skill.BlockIndex = i
			if existing, exists := e.skills[skill.Name]; exists && existing.BlockIndex != i {
				return fmt.Errorf("task %d: duplicate skill %q", i+1, skill.Name)
			}
			e.skills[skill.Name] = skill
			continue
		}
		if mcp, ok, err := dsl.ParseGlobalMCPBlock(body); err != nil {
			return fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			mcp.BlockIndex = i
			if isBuiltinMCPName(mcp.Name) {
				return fmt.Errorf("task %d: mcp name %q conflicts with an ATM builtin MCP server", i+1, mcp.Name)
			}
			if existing, exists := e.mcps[mcp.Name]; exists && existing.BlockIndex != i {
				return fmt.Errorf("task %d: duplicate mcp %q", i+1, mcp.Name)
			}
			e.mcps[mcp.Name] = mcp
			continue
		}
	}
	return nil
}

func (e *Engine) nextCallID() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callSeq++
	return e.callSeq
}

func (e *Engine) firstRunnableBlock(ctx context.Context, blocks []dsl.Block) (int, map[string]any, error) {
	globals := make(map[string]any)
	for i := range blocks {
		body := blocks[i].Body
		if dsl.IsDone(body) || strings.TrimSpace(body) == "" {
			continue
		}
		if _, ok, err := dsl.ParseLegacyDefinitionBlock(e.filePath, i, body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			continue
		}
		if _, ok, err := dsl.ParseGlobalImportBlock(body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			continue
		}
		key := inFlightKey{index: i, hash: store.HashBody(body)}
		if e.async.hasPendingKey(key) {
			continue
		}
		bindings, ok, err := dsl.ParseGlobalLetBlock(body)
		if err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			if err := e.applyGlobalLetBindings(ctx, i, bindings, globals); err != nil {
				return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
			}
			continue
		}
		pools, ok, err := dsl.ParseGlobalPoolBlock(body)
		if err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			for _, pool := range pools {
				pool.BlockIndex = i
				if err := e.pools.declare(pool); err != nil {
					return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
				}
			}
			continue
		}
		db, ok, err := dsl.ParseGlobalDBBlock(body)
		if err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			db.BlockIndex = i
			if existing, exists := e.dbs[db.Name]; exists && existing.BlockIndex != i {
				return -1, nil, fmt.Errorf("task %d: duplicate db %q", i+1, db.Name)
			}
			e.dbs[db.Name] = db
			continue
		}
		if skill, ok, err := dsl.ParseGlobalSkillBlock(body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			skill.BlockIndex = i
			if existing, exists := e.skills[skill.Name]; exists && existing.BlockIndex != i {
				return -1, nil, fmt.Errorf("task %d: duplicate skill %q", i+1, skill.Name)
			}
			e.skills[skill.Name] = skill
			continue
		}
		if mcp, ok, err := dsl.ParseGlobalMCPBlock(body); err != nil {
			return -1, nil, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			mcp.BlockIndex = i
			if isBuiltinMCPName(mcp.Name) {
				return -1, nil, fmt.Errorf("task %d: mcp name %q conflicts with an ATM builtin MCP server", i+1, mcp.Name)
			}
			if existing, exists := e.mcps[mcp.Name]; exists && existing.BlockIndex != i {
				return -1, nil, fmt.Errorf("task %d: duplicate mcp %q", i+1, mcp.Name)
			}
			e.mcps[mcp.Name] = mcp
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
	return -1, globals, nil
}

type conditionalGroup struct {
	ifIndex   int
	ifBlock   dsl.IfBlock
	thenStart int
	thenEnd   int
	hasElse   bool
	elseIndex int
	elseBlock dsl.ElseBlock
	elseStart int
	elseEnd   int
	end       int
}

func (e *Engine) handleConditionalBlock(ctx context.Context, blocks []dsl.Block, index int, globals map[string]any) (int, bool, error) {
	body := blocks[index].Body
	if elseBlock, ok, err := dsl.ParseElseBlock(body); err != nil {
		return -1, false, fmt.Errorf("task %d: %w", index+1, err)
	} else if ok {
		if index > 0 && dsl.IsSkipped(blocks[index-1].Body) {
			if elseBlock.HeaderOnly {
				if err := e.markControlDone(index, body); err != nil {
					return -1, true, err
				}
				return -1, true, store.ErrObsolete
			}
			return index, true, nil
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
		if err := e.markSkipped(group.ifIndex, body, "if condition evaluated false"); err != nil {
			return -1, true, err
		}
		if err := e.skipRange(blocks, group.thenStart, group.thenEnd, "if condition evaluated false"); err != nil {
			return -1, true, err
		}
		return -1, true, store.ErrObsolete
	}
	if err := e.markSkipped(index, body, "if condition evaluated false"); err != nil {
		return -1, true, err
	}
	return -1, true, store.ErrObsolete
}

func (e *Engine) parseConditionalGroup(blocks []dsl.Block, index int) (conditionalGroup, bool, error) {
	ifBlock, ok, err := dsl.ParseIfBlock(blocks[index].Body)
	if err != nil || !ok {
		return conditionalGroup{}, ok, err
	}
	group := conditionalGroup{ifIndex: index, ifBlock: ifBlock}
	if !ifBlock.HeaderOnly {
		group.thenStart = index
		group.thenEnd = index + 1
		group.end = index + 1
		if index+1 < len(blocks) && !dsl.IsDone(blocks[index+1].Body) {
			if elseBlock, ok, err := dsl.ParseElseBlock(blocks[index+1].Body); err != nil {
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
	elseBlock, ok, err := dsl.ParseElseBlock(blocks[thenEnd].Body)
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

func (e *Engine) nodeEnd(blocks []dsl.Block, index int) (int, error) {
	if index >= len(blocks) {
		return index, fmt.Errorf("conditional branch is missing a task block")
	}
	if dsl.IsDone(blocks[index].Body) {
		return index + 1, nil
	}
	if _, ok, err := dsl.ParseElseBlock(blocks[index].Body); err != nil {
		return index, err
	} else if ok {
		return index, fmt.Errorf("/else appears before a branch body")
	}
	if _, ok, err := dsl.ParseIfBlock(blocks[index].Body); err != nil {
		return index, err
	} else if ok {
		group, _, err := e.parseConditionalGroup(blocks, index)
		if err != nil {
			return index, err
		}
		return group.end, nil
	}
	return index + 1, nil
}

func (e *Engine) evaluateIfCondition(ctx context.Context, block dsl.IfBlock, globals map[string]any) (bool, error) {
	condition, err := dsl.RenderTemplate(block.Condition.Text, globals)
	if err != nil {
		return false, fmt.Errorf("condition template failed: %w", err)
	}
	switch block.Condition.Kind {
	case dsl.ConditionCEL:
		return expr.EvalBool(condition, expr.Context{
			Vars:      globals,
			TodoFile:  e.filePath,
			Root:      e.root,
			OutputDir: e.outputs.dirPath(),
		})
	default:
		prompt, err := dsl.RenderTemplate(block.Body, globals)
		if err != nil {
			return false, fmt.Errorf("prompt template failed: %w", err)
		}
		return e.runner.Check(ctx, e.filePath, prompt, condition, dsl.RunOptions{}, e.stdout, e.stderr)
	}
}

func (e *Engine) skipRange(blocks []dsl.Block, start, end int, reason string) error {
	for i := start; i < end && i < len(blocks); i++ {
		if dsl.IsDone(blocks[i].Body) || strings.TrimSpace(blocks[i].Body) == "" {
			continue
		}
		if err := e.markSkipped(i, blocks[i].Body, reason); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) markSkipped(index int, body, reason string) error {
	lease := store.NewBlockLease(index, body)
	return store.MarkSkipped(e.filePath, lease, dsl.SkippedInfo{Time: time.Now(), Reason: reason})
}

func (e *Engine) markControlDone(index int, body string) error {
	lease := store.NewBlockLease(index, body)
	now := time.Now()
	return store.MarkDone(e.filePath, lease, dsl.DoneInfo{Start: now, End: now, Runs: 0})
}

func (e *Engine) applyGlobalLetBindings(ctx context.Context, blockIndex int, bindings []dsl.LetBinding, globals map[string]any) error {
	for _, binding := range bindings {
		if binding.BashScript == "" {
			globals[binding.Name] = binding.Value
			continue
		}
		script, err := dsl.RenderTemplate(binding.BashScript, globals)
		if err != nil {
			return fmt.Errorf("/let %s /bash template failed: %w", binding.Name, err)
		}
		cacheKey := fmt.Sprintf("%d:%s:%s", blockIndex, binding.Name, script)
		value, ok := e.bashVars[cacheKey]
		if !ok {
			value, err = tools.CaptureBash(ctx, e.filePath, script, "", e.stderr)
			if err != nil {
				return fmt.Errorf("/let %s /bash failed: %w", binding.Name, err)
			}
			e.bashVars[cacheKey] = value
		}
		globals[binding.Name] = value
	}
	return nil
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
