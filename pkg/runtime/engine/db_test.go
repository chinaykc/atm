package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinaykc/atm/pkg/lang/compiler"
)

func TestTaskDBsAppliesVisibilityAndAccess(t *testing.T) {
	outputs, err := newOutputRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		root:    t.TempDir(),
		outputs: outputs,
		dbs: map[string]compiler.DBDecl{
			"global": {Name: "global", Scope: compiler.DBScopeGlobal, Persist: compiler.DBPersistProject, Access: compiler.DBAccessAdmin},
			"local":  {Name: "local", Scope: compiler.DBScopeLocal, Persist: compiler.DBPersistRun, Access: compiler.DBAccessWrite},
		},
		dbItems: []compiler.DBDecl{
			{Name: "global", Scope: compiler.DBScopeGlobal, Persist: compiler.DBPersistProject, Access: compiler.DBAccessAdmin},
			{Name: "local", Scope: compiler.DBScopeLocal, Persist: compiler.DBPersistRun, Access: compiler.DBAccessWrite},
		},
	}
	dbs, err := e.taskDBs(compiler.Task{
		DB: compiler.DBTaskConfig{
			Use:    []compiler.DBUse{{Names: []string{"local"}, Access: compiler.DBAccessAppend}},
			Access: []compiler.DBAccessRule{{Names: []string{"global"}, Access: compiler.DBAccessRead}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected two visible dbs, got %#v", dbs)
	}
	byName := map[string]compiler.DBRuntime{}
	for _, db := range dbs {
		byName[db.Name] = db
	}
	if byName["global"].Access != compiler.DBAccessRead || byName["local"].Access != compiler.DBAccessAppend {
		t.Fatalf("unexpected access: %#v", byName)
	}
	if !strings.Contains(byName["global"].Path, filepath.Join(".atm", "db", "global.json")) {
		t.Fatalf("unexpected project path: %s", byName["global"].Path)
	}
}

func TestTaskDBsRejectsAccessElevation(t *testing.T) {
	outputs, err := newOutputRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		root:    t.TempDir(),
		outputs: outputs,
		dbs: map[string]compiler.DBDecl{
			"notes": {Name: "notes", Scope: compiler.DBScopeGlobal, Persist: compiler.DBPersistRun, Access: compiler.DBAccessRead},
		},
		dbItems: []compiler.DBDecl{
			{Name: "notes", Scope: compiler.DBScopeGlobal, Persist: compiler.DBPersistRun, Access: compiler.DBAccessRead},
		},
	}
	_, err = e.taskDBs(compiler.Task{DB: compiler.DBTaskConfig{Access: []compiler.DBAccessRule{{Names: []string{"notes"}, Access: compiler.DBAccessWrite}}}})
	if err == nil || !strings.Contains(err.Error(), "exceeds declared access") {
		t.Fatalf("expected access elevation error, got %v", err)
	}
}
