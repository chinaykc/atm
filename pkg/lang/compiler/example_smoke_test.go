package compiler_test

import (
	"os"
	"testing"

	"github.com/chinaykc/atm/pkg/lang/compiler"
)

func TestSmokeExampleCoversCoreDSLFeatures(t *testing.T) {
	path := "../../../examples/en/smoke.md"
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.CompileProgram(path, string(content))
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Flags) != 1 || plan.Flags[0].Name != "who" {
		t.Fatalf("expected who flag, got %#v", plan.Flags)
	}
	if len(plan.Tasks) != 5 {
		t.Fatalf("expected five smoke tasks, got %d task(s)", len(plan.Tasks))
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "tag" {
		t.Fatalf("expected tag definition, got %#v", plan.Definitions)
	}
	if len(plan.Pools) != 1 || plan.Pools[0].Name != "smoke" {
		t.Fatalf("expected smoke pool, got %#v", plan.Pools)
	}
	if len(plan.DBs) != 1 || plan.DBs[0].Name != "board" {
		t.Fatalf("expected board DB, got %#v", plan.DBs)
	}
	if len(plan.Controls) != 1 || plan.Controls[0].Kind != "if" {
		t.Fatalf("expected local expression if control, got %#v", plan.Controls)
	}

	var hasOutput, hasDBUse bool
	for _, task := range plan.Tasks {
		if task.Output != nil {
			hasOutput = true
		}
		if len(task.DB.Use) > 0 {
			hasDBUse = true
		}
	}
	for name, ok := range map[string]bool{
		"output": hasOutput,
		"db use": hasDBUse,
	} {
		if !ok {
			t.Fatalf("smoke example no longer covers %s", name)
		}
	}
}
