package engine

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"maps"
	"path/filepath"
	"slices"
)

func (e *Engine) taskDBs(task compiler.Task) ([]compiler.DBRuntime, error) {
	config := task.DB
	ref := definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	var base []compiler.DBRuntime
	decls := e.visibleDBDecls(ref)
	for _, name := range slices.Sorted(maps.Keys(decls)) {
		decl := decls[name]
		if decl.Scope != compiler.DBScopeGlobal {
			continue
		}
		base = append(base, e.runtimeDB(decl, decl.Access))
	}
	return e.applyDBConfig(base, config, decls)
}

func (e *Engine) applyDBConfig(base []compiler.DBRuntime, config compiler.DBTaskConfig, decls map[string]compiler.DBDecl) ([]compiler.DBRuntime, error) {
	if config.IgnoreAll {
		return nil, nil
	}
	visible := make(map[string]compiler.DBRuntime, len(base))
	for _, db := range base {
		visible[db.Name] = db
	}
	for _, use := range config.Use {
		for _, name := range use.Names {
			decl, ok := decls[name]
			if !ok {
				return nil, fmt.Errorf("unknown db %q", name)
			}
			access := decl.Access
			if use.Access != "" {
				if !dbAccessAllowed(use.Access, decl.Access) {
					return nil, fmt.Errorf("/db use %s access %s exceeds declared access %s", name, use.Access, decl.Access)
				}
				access = use.Access
			}
			visible[name] = e.runtimeDB(decl, access)
		}
	}
	for _, rule := range config.Access {
		for _, name := range expandDBAccessNames(rule.Names, visible) {
			db, ok := visible[name]
			if !ok {
				return nil, fmt.Errorf("/db access references unavailable db %q", name)
			}
			decl := decls[name]
			if !dbAccessAllowed(rule.Access, decl.Access) {
				return nil, fmt.Errorf("/db access %s %s exceeds declared access %s", name, rule.Access, decl.Access)
			}
			db.Access = rule.Access
			visible[name] = db
		}
	}
	for _, name := range config.Ignore {
		delete(visible, name)
	}
	names := slices.Sorted(maps.Keys(visible))
	out := make([]compiler.DBRuntime, 0, len(names))
	for _, name := range names {
		out = append(out, visible[name])
	}
	return out, nil
}

func (e *Engine) visibleDBDecls(ref definitionScopeRef) map[string]compiler.DBDecl {
	out := make(map[string]compiler.DBDecl)
	for _, db := range e.dbItems {
		if !runtimeDBVisibleAt(db, ref) {
			continue
		}
		out[db.Name] = db
	}
	return out
}

func runtimeDBVisibleAt(db compiler.DBDecl, ref definitionScopeRef) bool {
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

func (e *Engine) declsForRuntimeDBs(dbs []compiler.DBRuntime) map[string]compiler.DBDecl {
	out := make(map[string]compiler.DBDecl, len(dbs))
	for _, db := range dbs {
		if decl, ok := e.dbs[db.Name]; ok {
			out[db.Name] = decl
		}
	}
	return out
}

func (e *Engine) runtimeDB(decl compiler.DBDecl, access compiler.DBAccess) compiler.DBRuntime {
	return compiler.DBRuntime{
		Name:    decl.Name,
		Path:    e.dbPath(decl),
		Scope:   decl.Scope,
		Persist: decl.Persist,
		Access:  access,
		Usage:   decl.Usage,
	}
}

func (e *Engine) dbPath(decl compiler.DBDecl) string {
	switch decl.Persist {
	case compiler.DBPersistProject:
		return filepath.Join(e.root, ".atm", "db", decl.Name+".json")
	default:
		return filepath.Join(e.outputs.dirPath(), "db", decl.Name+".json")
	}
}

func expandDBAccessNames(names []string, visible map[string]compiler.DBRuntime) []string {
	for _, name := range names {
		if name == "*" {
			return slices.Sorted(maps.Keys(visible))
		}
	}
	return slices.Clone(names)
}

func dbAccessAllowed(requested, max compiler.DBAccess) bool {
	return dbAccessRank(requested) <= dbAccessRank(max)
}

func dbAccessRank(access compiler.DBAccess) int {
	switch access {
	case compiler.DBAccessRead:
		return 1
	case compiler.DBAccessAppend:
		return 2
	case compiler.DBAccessWrite:
		return 3
	case compiler.DBAccessAdmin:
		return 4
	default:
		return 0
	}
}
