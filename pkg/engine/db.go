package engine

import (
	"atm/pkg/dsl"
	"fmt"
	"path/filepath"
	"sort"
)

func (e *Engine) taskDBs(config dsl.DBTaskConfig) ([]dsl.DBRuntime, error) {
	var base []dsl.DBRuntime
	names := make([]string, 0, len(e.dbs))
	for name := range e.dbs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		decl := e.dbs[name]
		if decl.Scope != dsl.DBScopeGlobal {
			continue
		}
		base = append(base, e.runtimeDB(decl, decl.Access))
	}
	return e.applyDBConfig(base, config)
}

func (e *Engine) applyDBConfig(base []dsl.DBRuntime, config dsl.DBTaskConfig) ([]dsl.DBRuntime, error) {
	if config.IgnoreAll {
		return nil, nil
	}
	visible := make(map[string]dsl.DBRuntime, len(base))
	for _, db := range base {
		visible[db.Name] = db
	}
	for _, use := range config.Use {
		for _, name := range use.Names {
			decl, ok := e.dbs[name]
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
			decl := e.dbs[name]
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
	names := make([]string, 0, len(visible))
	for name := range visible {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]dsl.DBRuntime, 0, len(names))
	for _, name := range names {
		out = append(out, visible[name])
	}
	return out, nil
}

func (e *Engine) runtimeDB(decl dsl.DBDecl, access dsl.DBAccess) dsl.DBRuntime {
	return dsl.DBRuntime{
		Name:    decl.Name,
		Path:    e.dbPath(decl),
		Scope:   decl.Scope,
		Persist: decl.Persist,
		Access:  access,
		Usage:   decl.Usage,
	}
}

func (e *Engine) dbPath(decl dsl.DBDecl) string {
	switch decl.Persist {
	case dsl.DBPersistProject:
		return filepath.Join(e.root, ".atm", "db", decl.Name+".json")
	default:
		return filepath.Join(e.outputs.dirPath(), "db", decl.Name+".json")
	}
}

func expandDBAccessNames(names []string, visible map[string]dsl.DBRuntime) []string {
	for _, name := range names {
		if name == "*" {
			out := make([]string, 0, len(visible))
			for visibleName := range visible {
				out = append(out, visibleName)
			}
			sort.Strings(out)
			return out
		}
	}
	return append([]string{}, names...)
}

func dbAccessAllowed(requested, max dsl.DBAccess) bool {
	return dbAccessRank(requested) <= dbAccessRank(max)
}

func dbAccessRank(access dsl.DBAccess) int {
	switch access {
	case dsl.DBAccessRead:
		return 1
	case dsl.DBAccessAppend:
		return 2
	case dsl.DBAccessWrite:
		return 3
	case dsl.DBAccessAdmin:
		return 4
	default:
		return 0
	}
}
