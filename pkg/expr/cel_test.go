package expr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalBoolReadsJSONAndText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "result.json"), []byte(`{"passed":true,"items":["a","b"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ready"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := EvalBool(`exists("result.json") && json("result.json").passed && len(json("result.json").items) == 2 && len(read("note.txt")) == 5`, Context{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected expression to pass")
	}
}

func TestEvalBoolReadsOutputDirectory(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "gate.json"), []byte(`{"passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := EvalBool(`existsOutput("gate.json") && jsonOutput("gate.json").passed`, Context{Root: root, OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected output expression to pass")
	}
}

func TestEvalListReadsOutputArray(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "release-plan.json"), []byte(`{"areas":[{"name":"api","owner":"payments"},{"name":"docs","owner":"support"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	values, err := EvalList(`jsonOutput("release-plan.json").areas`, Context{Root: root, OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("expected two values, got %#v", values)
	}
	first, ok := values[0].(map[string]any)
	if !ok || first["name"] != "api" || first["owner"] != "payments" {
		t.Fatalf("unexpected first value: %#v", values[0])
	}
}

func TestEvalBoolExposesVarsAndNumericN(t *testing.T) {
	ok, err := EvalBool(`N >= 2 && gate.passed && vars["name-with-dash"] == "ok"`, Context{
		Root: ".",
		Vars: map[string]any{
			"N":              "2",
			"gate":           map[string]any{"passed": true},
			"name-with-dash": "ok",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected vars expression to pass")
	}
}

func TestEvalBoolRequiresBoolAndHintsQuotedPaths(t *testing.T) {
	if _, err := EvalBool(`"not bool"`, Context{Root: "."}); err == nil || !strings.Contains(err.Error(), "must return bool") {
		t.Fatalf("expected bool type error, got %v", err)
	}
	if _, err := EvalBool(`read(result.json) != ""`, Context{Root: "."}); err == nil || !strings.Contains(err.Error(), `read("result.json")`) {
		t.Fatalf("expected quoted path hint, got %v", err)
	}
}

func TestEvalBoolPreventsPathEscape(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(parentFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := EvalBool(`exists("../outside.txt")`, Context{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected path escape to be hidden")
	}
}
