package dsl

import (
	"fmt"
	"path/filepath"
)

func ParseTask(index int, body string, globals map[string]any, opts CompileOptions) (Task, error) {
	t, err := parseTask(index, body, globals, normalizeCompileOptions("", opts))
	if err != nil {
		return Task{}, err
	}
	return lowerTaskASTToIR(index, t), nil
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

func CompileProgramWithOptions(sourcePath, content string, opts CompileOptions) (Plan, error) {
	opts = normalizeCompileOptions(sourcePath, opts)
	blocks := ParseBlocks(content)
	plan := Plan{SourcePath: sourcePath}
	defs, err := LoadDefinitionSet(sourcePath, content, opts)
	if err != nil {
		return Plan{}, err
	}
	for _, decl := range defs.Imports {
		plan.Imports = append(plan.Imports, decl)
	}
	for _, def := range defs.Definitions {
		plan.Definitions = append(plan.Definitions, def)
	}
	if len(blocks) == 0 {
		if len(plan.Definitions) > 0 || len(plan.Imports) > 0 {
			return plan, nil
		}
		return Plan{}, fmt.Errorf("no tasks found in todo file %q", sourcePath)
	}
	globals := make(map[string]any)
	for i := range blocks {
		body := blocks[i].Body
		if IsDone(body) || IsBlankLine(body) {
			continue
		}
		if _, ok, err := ParseLegacyDefinitionBlock(sourcePath, i, body); err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		} else if ok {
			continue
		}
		imports, ok, err := ParseGlobalImportBlock(body)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			_ = imports
			continue
		}
		bindings, ok, err := ParseGlobalLetBlock(body)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			for _, binding := range bindings {
				plan.Globals = append(plan.Globals, GlobalBinding{
					BlockIndex: i,
					Name:       binding.Name,
					Value:      binding.Value,
					BashScript: binding.BashScript,
				})
				if binding.BashScript != "" {
					globals[binding.Name] = "{{" + binding.Name + "}}"
				} else {
					globals[binding.Name] = binding.Value
				}
			}
			continue
		}
		pools, ok, err := ParseGlobalPoolBlock(body)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			for _, pool := range pools {
				pool.BlockIndex = i
				plan.Pools = append(plan.Pools, pool)
			}
			continue
		}
		db, ok, err := ParseGlobalDBBlock(body)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			db.BlockIndex = i
			plan.DBs = append(plan.DBs, db)
			continue
		}
		skill, ok, err := ParseGlobalSkillBlock(body)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			skill.BlockIndex = i
			plan.Skills = append(plan.Skills, skill)
			continue
		}
		mcp, ok, err := ParseGlobalMCPBlock(body)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		if ok {
			mcp.BlockIndex = i
			plan.MCPs = append(plan.MCPs, mcp)
			continue
		}
		if info, ok, err := ParseIfBlock(body); err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
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
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
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

		task, err := ParseTask(i, body, globals, opts)
		if err != nil {
			return Plan{}, fmt.Errorf("task %d: %w", i+1, err)
		}
		plan.Tasks = append(plan.Tasks, task)
	}
	return plan, nil
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
