package expr

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEvalBoolOpensAndParsesLocalFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "result.json"), `{"passed":true,"items":["a","b"]}`)
	mustWrite(t, filepath.Join(dir, "release.yaml"), "ready: true\n")
	mustWrite(t, filepath.Join(dir, "config.toml"), "[release]\nenabled = true\n")
	mustWrite(t, filepath.Join(dir, "note.txt"), "ready")

	ok, err := EvalBool(`exist("result.json") &&
		json(open("result.json")).passed &&
		len(json(open("result.json")).items) == 2 &&
		yaml(open("release.yaml")).ready &&
		toml(open("config.toml")).release.enabled &&
		len(open("note.txt")) == 5`, Context{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected expression to pass")
	}
}

func TestParsersParseTextOnly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "gate.json"), `{"passed":true}`)

	if _, err := Eval(`json("gate.json")`, Context{Root: dir}); err == nil {
		t.Fatal("expected json literal parse to fail instead of reading gate.json")
	}
	ok, err := EvalBool(`json("true") &&
		yaml("true") &&
		yaml("[a, b]")[1] == "b" &&
		yaml("ready: true").ready &&
		toml("items = [1, 2]").items[1] == 2`, Context{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected parser expression to pass")
	}
}

func TestEvalBoolReadsOutputDirectory(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	mustWrite(t, filepath.Join(out, "gate.json"), `{"passed":true}`)

	ok, err := EvalBool(`exist(outputDir("gate.json")) && json(open(outputDir("gate.json"))).passed`, Context{Root: app, OutputDir: out})
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
	mustWrite(t, filepath.Join(out, "release-plan.json"), `{"areas":[{"name":"api","owner":"payments"},{"name":"docs","owner":"support"}]}`)

	values, err := EvalList(`json(open(outputDir("release-plan.json"))).areas`, Context{Root: root, OutputDir: out})
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

func TestEvalListSupportsRangeHelper(t *testing.T) {
	values, err := EvalList(`range(1, 6, 2)`, Context{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 || values[0] != 1 || values[1] != 3 || values[2] != 5 {
		t.Fatalf("unexpected range values: %#v", values)
	}
	descending, err := EvalList(`range(3, 0, -1)`, Context{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(descending) != 3 || descending[0] != 3 || descending[1] != 2 || descending[2] != 1 {
		t.Fatalf("unexpected descending range values: %#v", descending)
	}
	if _, err := EvalList(`range(1, 3, 0)`, Context{Root: "."}); err == nil || !strings.Contains(err.Error(), "step cannot be 0") {
		t.Fatalf("expected zero-step range error, got %v", err)
	}
}

func TestEvalListSupportsFileAndDirHelpers(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "api", "internal"))
	mustMkdir(t, filepath.Join(root, "docs"))
	mustMkdir(t, filepath.Join(root, ".git"))
	mustMkdir(t, filepath.Join(root, "node_modules", "pkg"))
	mustWrite(t, filepath.Join(root, "README.md"), "readme")
	mustWrite(t, filepath.Join(root, "z.txt"), "z")
	mustWrite(t, filepath.Join(root, "api", "server.go"), "package api\n")
	mustWrite(t, filepath.Join(root, "api", "internal", "detail.go"), "package internal\n")
	mustWrite(t, filepath.Join(root, "docs", "guide.md"), "guide")
	mustWrite(t, filepath.Join(root, ".git", "config"), "hidden")
	mustWrite(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "hidden")

	assertEvalStrings(t, `dirs()`, Context{Root: root}, []string{"api", "docs"})
	assertEvalStrings(t, `files()`, Context{Root: root}, []string{"README.md", "z.txt"})
	assertEvalStrings(t, `walkDirs()`, Context{Root: root}, []string{"api", "api/internal", "docs"})
	assertEvalStrings(t, `walkFiles()`, Context{Root: root}, []string{"README.md", "api/internal/detail.go", "api/server.go", "docs/guide.md", "z.txt"})
	assertEvalStrings(t, `walkFiles("api")`, Context{Root: root}, []string{"internal/detail.go", "server.go"})
	assertEvalStrings(t, `filter(walkFiles("api"), {# endsWith ".go"})`, Context{Root: root}, []string{"internal/detail.go", "server.go"})
}

func TestEvalListEnumeratesOutputDirRoot(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	mustMkdir(t, filepath.Join(out, "reports", "nested"))
	mustWrite(t, filepath.Join(out, "reports", "a.json"), `{"ok":true}`)
	mustWrite(t, filepath.Join(out, "reports", "nested", "b.json"), `{"ok":true}`)

	assertEvalStrings(t, `walkFiles(outputDir("reports"))`, Context{Root: root, OutputDir: out}, []string{"a.json", "nested/b.json"})
}

func TestEvalBoolExposesVarsAndNumericLoopIndex(t *testing.T) {
	ok, err := EvalBool(`n >= 2 && gate.passed && vars["name-with-dash"] == "ok"`, Context{
		Root: ".",
		Vars: map[string]any{
			"n":              "2",
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
	if _, err := EvalBool(`open(result.json) != ""`, Context{Root: "."}); err == nil || !strings.Contains(err.Error(), `open("result.json")`) {
		t.Fatalf("expected quoted path hint, got %v", err)
	}
}

func TestValidateSyntaxRejectsInvalidAndRemovedFunctions(t *testing.T) {
	if err := ValidateSyntax(`exist("gate.json") && json(open("gate.json")).passed`); err != nil {
		t.Fatalf("expected valid expression syntax, got %v", err)
	}
	if err := ValidateSyntax(`filter(walkFiles("api"), {# endsWith ".go"})`); err != nil {
		t.Fatalf("expected expr built-ins to remain valid, got %v", err)
	}
	if err := ValidateSyntax(`exist("gate.json") &&`); err == nil {
		t.Fatal("expected invalid expression syntax to fail")
	}
	for _, expression := range []string{
		`read("gate.json")`,
		`exists("gate.json")`,
		`read_output("gate.json")`,
		`json_output("gate.json")`,
		`exists_output("gate.json")`,
		`readOutput("gate.json")`,
		`jsonOutput("gate.json")`,
		`existsOutput("gate.json")`,
	} {
		if err := ValidateSyntax(expression); err == nil || !strings.Contains(err.Error(), "unknown function") {
			t.Fatalf("expected removed function error for %s, got %v", expression, err)
		}
	}
	if err := ValidateSyntax(`typo("gate.json")`); err == nil || !strings.Contains(err.Error(), `unknown function "typo"`) {
		t.Fatalf("expected unsupported function error, got %v", err)
	}
}

func TestEvalPreventsPathEscape(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(dir), "outside.txt")
	mustWrite(t, parentFile, "secret")
	ok, err := EvalBool(`exist("../outside.txt")`, Context{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected path escape to be hidden")
	}
	if _, err := Eval(`open("../outside.txt")`, Context{Root: dir}); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("expected open path escape error, got %v", err)
	}
	if _, err := EvalList(`walkFiles("../")`, Context{Root: dir}); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("expected walk path escape error, got %v", err)
	}
}

func assertEvalStrings(t *testing.T, expression string, ctx Context, want []string) {
	t.Helper()
	values, err := EvalList(expression, ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(values))
	for i, value := range values {
		got[i] = value.(string)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %#v want %#v", expression, got, want)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
