package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"atm/pkg/dsl"
)

func TestTaskDBsAppliesVisibilityAndAccess(t *testing.T) {
	outputs, err := newOutputRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		root:    t.TempDir(),
		outputs: outputs,
		dbs: map[string]dsl.DBDecl{
			"global": {Name: "global", Scope: dsl.DBScopeGlobal, Persist: dsl.DBPersistProject, Access: dsl.DBAccessAdmin},
			"local":  {Name: "local", Scope: dsl.DBScopeLocal, Persist: dsl.DBPersistRun, Access: dsl.DBAccessWrite},
		},
	}
	dbs, err := e.taskDBs(dsl.DBTaskConfig{
		Use:    []dsl.DBUse{{Names: []string{"local"}, Access: dsl.DBAccessAppend}},
		Access: []dsl.DBAccessRule{{Names: []string{"global"}, Access: dsl.DBAccessRead}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected two visible dbs, got %#v", dbs)
	}
	byName := map[string]dsl.DBRuntime{}
	for _, db := range dbs {
		byName[db.Name] = db
	}
	if byName["global"].Access != dsl.DBAccessRead || byName["local"].Access != dsl.DBAccessAppend {
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
		dbs: map[string]dsl.DBDecl{
			"notes": {Name: "notes", Scope: dsl.DBScopeGlobal, Persist: dsl.DBPersistRun, Access: dsl.DBAccessRead},
		},
	}
	_, err = e.taskDBs(dsl.DBTaskConfig{Access: []dsl.DBAccessRule{{Names: []string{"notes"}, Access: dsl.DBAccessWrite}}})
	if err == nil || !strings.Contains(err.Error(), "exceeds declared access") {
		t.Fatalf("expected access elevation error, got %v", err)
	}
}
