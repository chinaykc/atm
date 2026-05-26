package compiler

import (
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompileProgramRejectsLegacyPathIterators(t *testing.T) {
	for _, body := range []string{"/for dir\nreview {{dir}}\n", "/for path\nreview {{path}}\n", "/for file\nreview {{file}}\n"} {
		if _, err := CompileProgram("todo.txt", body); err == nil || !strings.Contains(err.Error(), "unsupported /for iterator") {
			t.Fatalf("expected legacy iterator error for %q, got %v", body, err)
		}
	}
}

func TestCompileProgramParsesFilesExpressionIterator(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for file in files()\nreview {{file}}\n")
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(plan.Tasks[0])
	if len(plan.Tasks) != 1 || len(ops) != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if ops[0].For.VarName != "file" || ops[0].For.Source.Kind != ConditionExpr || ops[0].For.Source.Text != "files()" {
		t.Fatalf("unexpected files iterator: %#v", ops[0].For)
	}
}

func TestMarkdownLetScopeFollowsHeadingAncestry(t *testing.T) {
	content := "# Root\n\n/let root shared\n\n## A\n\n/let local alpha\n\n### Child\n\n/task\nUse {{root}} {{local}}.\n\n## B\n\n/task\nUse {{root}} only.\n"
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", plan.Tasks)
	}
	if _, ok := plan.Tasks[0].Vars["root"]; !ok {
		t.Fatalf("expected child task to inherit root variable: %#v", plan.Tasks[0].Vars)
	}
	if _, ok := plan.Tasks[0].Vars["local"]; !ok {
		t.Fatalf("expected child task to inherit heading-local variable: %#v", plan.Tasks[0].Vars)
	}
	if _, ok := plan.Tasks[1].Vars["root"]; !ok {
		t.Fatalf("expected sibling task to inherit root variable: %#v", plan.Tasks[1].Vars)
	}
	if _, ok := plan.Tasks[1].Vars["local"]; ok {
		t.Fatalf("did not expect sibling task to see heading-local variable: %#v", plan.Tasks[1].Vars)
	}
}

func TestMarkdownLetIsOnlyVisibleAfterDeclaration(t *testing.T) {
	content := "# Root\n\n/task\nUse future later.\n\n/let later value\n\n/task\nUse future {{later}}.\n"
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", plan.Tasks)
	}
	if _, ok := plan.Tasks[0].Vars["later"]; ok {
		t.Fatalf("did not expect first task to see future /let: %#v", plan.Tasks[0].Vars)
	}
	if _, ok := plan.Tasks[1].Vars["later"]; !ok {
		t.Fatalf("expected second task to see prior /let: %#v", plan.Tasks[1].Vars)
	}
}

func TestCompileProgramParsesDirsExpressionIterator(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for dir in dirs()\nreview {{dir}}\n")
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(plan.Tasks[0])
	if len(plan.Tasks) != 1 || len(ops) != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if ops[0].For.VarName != "dir" || ops[0].For.Source.Kind != ConditionExpr || ops[0].For.Source.Text != "dirs()" {
		t.Fatalf("unexpected dirs iterator: %#v", ops[0].For)
	}
}

func TestCompileProgramParsesBareRangeExpressionIterator(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for shard in range(1, 4) /go\nreview {{shard}}\n")
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(plan.Tasks[0])
	if len(ops) < 2 || ops[0].For.VarName != "shard" || ops[0].For.Source.Kind != ConditionExpr || ops[0].For.Source.Text != "range(1, 4)" || ops[1].Kind != FlatOpGo {
		t.Fatalf("unexpected bare range iterator: %#v", ops)
	}
}

func TestPlanPreservesForGoOrder(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 2 /go\nparallel {{n}}\n\n/go /for 2\nloop {{n}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != "For(n in [0 1]) -> Go -> Execute" {
		t.Fatalf("unexpected /for /go flow: %s", got)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "Go -> For(n in [0 1]) -> Execute" {
		t.Fatalf("unexpected /go /for flow: %s", got)
	}
}

func TestFlowTreePreservesCommandComposition(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 2 /go\nparallel {{n}}\n\n/go /for 2\nloop {{n}}\n")
	if err != nil {
		t.Fatal(err)
	}
	first := plan.Tasks[0].Flow
	if first.Kind != FlowSeq || len(first.Children) != 1 || first.Children[0].Kind != FlowFor {
		t.Fatalf("expected first task root For, got %#v", first)
	}
	firstFor := first.Children[0]
	if len(firstFor.Children) != 1 || firstFor.Children[0].Kind != FlowGo {
		t.Fatalf("expected For -> Go, got %#v", firstFor)
	}
	if child := firstFor.Children[0]; len(child.Children) != 1 || child.Children[0].Kind != FlowExecute {
		t.Fatalf("expected Go -> Execute, got %#v", child)
	}

	second := plan.Tasks[1].Flow
	if second.Kind != FlowSeq || len(second.Children) != 1 || second.Children[0].Kind != FlowGo {
		t.Fatalf("expected second task root Go, got %#v", second)
	}
	secondGo := second.Children[0]
	if len(secondGo.Children) != 1 || secondGo.Children[0].Kind != FlowFor {
		t.Fatalf("expected Go -> For, got %#v", secondGo)
	}
	if child := secondGo.Children[0]; len(child.Children) != 1 || child.Children[0].Kind != FlowExecute {
		t.Fatalf("expected For -> Execute, got %#v", child)
	}
}

func TestFlattenTaskFlowUsesFlowAsPrimaryIR(t *testing.T) {
	task := Task{
		Flow: FlowNode{Kind: FlowSeq, Children: []FlowNode{
			{Kind: FlowFor, For: For{VarName: "area", Values: []string{"api", "docs"}}, Children: []FlowNode{
				{Kind: FlowGo, Pool: "review", Children: []FlowNode{
					{Kind: FlowExecute},
				}},
			}},
		}},
	}
	if got := FormatTaskFlow(task); got != "For(area in [api docs]) -> Go(review) -> Execute" {
		t.Fatalf("expected flow-backed formatting, got %q", got)
	}
	ops := FlattenTaskFlow(task)
	if len(ops) != 3 || ops[0].Kind != FlatOpFor || ops[1].Kind != FlatOpGo || ops[2].Kind != FlatOpExecute {
		t.Fatalf("unexpected flow ops: %#v", ops)
	}
}

func TestCdCommandParsesAsOrderedFlow(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for area in [api docs] /cd work/{{area}} /go\nreview {{area}}\n\n/cd --must-exist backend\ncheck\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != "For(area in [api docs]) -> Cd(work/{{area}}) -> Go -> Execute" {
		t.Fatalf("unexpected /cd flow: %s", got)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "Cd(backend, must-exist) -> Execute" {
		t.Fatalf("unexpected /cd --must-exist flow: %s", got)
	}
}

func TestCommandLexerSupportsQuotedArgsPathsAndListItems(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/args \"--model fast lane\" /for area in [\"api docs\" tests] /cd \"work/{{area}}\" /go\nreview {{area}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one task, got %#v", plan.Tasks)
	}
	task := plan.Tasks[0]
	if got := FormatTaskFlow(task); got != "For(area in [api docs tests]) [args=--model fast lane] -> Cd(work/{{area}}) -> Go -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
	ops := FlattenTaskFlow(task)
	if got := strings.Join(ops[0].For.Values, "|"); got != "api docs|tests" {
		t.Fatalf("unexpected quoted list values: %q", got)
	}
}

func TestParseCommandSequenceBuildsCommandAST(t *testing.T) {
	commands, err := parseCommandSequence(`/args "--model fast" /for area in ["api docs" tests] /go reviewers`, nil, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 {
		t.Fatalf("expected three commands, got %#v", commands)
	}
	if commands[0].Kind != commandArgs || strings.Join(commands[0].Options.Args, " ") != "--model fast" {
		t.Fatalf("unexpected args command: %#v", commands[0])
	}
	if commands[1].Kind != commandFor || commands[1].For.VarName != "area" || strings.Join(commands[1].For.Values, "|") != "api docs|tests" {
		t.Fatalf("unexpected for command: %#v", commands[1])
	}
	if commands[2].Kind != commandGo || commands[2].Pool != "reviewers" {
		t.Fatalf("unexpected go command: %#v", commands[2])
	}
}

func TestComposableIfCommandFlow(t *testing.T) {
	task, err := ParseTask(0, "/for 10 /if(n % 2 == 0) /go even\nReview {{n}}.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTaskFlow(task); got != `For(n x 10) -> If(expr:n % 2 == 0) -> Go(even) -> Execute` {
		t.Fatalf("unexpected flow: %s", got)
	}
	flow := task.Flow.Children[0]
	if flow.Kind != FlowFor || len(flow.Children) != 1 || flow.Children[0].Kind != FlowIf {
		t.Fatalf("expected For -> If flow, got %#v", task.Flow)
	}
	if flow.Children[0].If.Condition.Text != "n % 2 == 0" {
		t.Fatalf("unexpected if condition: %#v", flow.Children[0].If.Condition)
	}
}

func TestComposableIfElseCommandFlow(t *testing.T) {
	task, err := ParseTask(0, "/for 2 /if(n == 0) /go even /else /go odd\nReview {{n}}.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	loop := task.Flow.Children[0]
	branch := loop.Children[0]
	if branch.Kind != FlowIf || len(branch.Children) != 1 || len(branch.ElseChildren) != 1 {
		t.Fatalf("expected if with then/else children, got %#v", branch)
	}
	if branch.Children[0].Kind != FlowGo || branch.Children[0].Pool != "even" {
		t.Fatalf("unexpected then branch: %#v", branch.Children)
	}
	if branch.ElseChildren[0].Kind != FlowGo || branch.ElseChildren[0].Pool != "odd" {
		t.Fatalf("unexpected else branch: %#v", branch.ElseChildren)
	}
}

func TestComposableIfUsesFollowingElseBlockAsSameTask(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 2 /if(n == 0)\nthen {{n}}\n\n/else\nelse {{n}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one conditional task, got %#v", plan.Tasks)
	}
	loop := plan.Tasks[0].Flow.Children[0]
	branch := loop.Children[0]
	if branch.Kind != FlowIf || len(branch.Children) != 1 || len(branch.ElseChildren) != 1 {
		t.Fatalf("expected merged if/else flow, got %#v", branch)
	}
	if branch.ElseChildren[0].Kind != FlowExecute || strings.TrimSpace(branch.ElseChildren[0].Prompt) != "else {{n}}" {
		t.Fatalf("expected else prompt override, got %#v", branch.ElseChildren)
	}
	if len(plan.Controls) != 0 {
		t.Fatalf("expected merged else not to appear as standalone control, got %#v", plan.Controls)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != `For(n in [0 1]) -> If(expr:n == 0) {then: Execute; else: Execute}` {
		t.Fatalf("unexpected merged if/else flow: %s", got)
	}
}

func TestComposableIfAllowsEmptyFollowingElseBlockWithWarning(t *testing.T) {
	plan, diagnostics := CompileProgramDiagnostics("todo.txt", "/if (false)\nthen\n\n/else\n")
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one conditional task, got %#v diagnostics=%#v", plan.Tasks, diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			t.Fatalf("expected warning-only diagnostics, got %#v", diagnostics)
		}
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != "warning" || !strings.Contains(diagnostics[0].Message, "empty /else") {
		t.Fatalf("expected empty /else warning, got %#v", diagnostics)
	}
	branch := plan.Tasks[0].Flow.Children[0]
	if branch.Kind != FlowIf || len(branch.ElseChildren) != 0 {
		t.Fatalf("expected empty else no-op branch, got %#v", branch)
	}
}

func TestCdCommandValidation(t *testing.T) {
	for _, body := range []string{
		"/cd\nrun\n",
		"/cd one two\nrun\n",
		"/cd --unknown path\nrun\n",
	} {
		if _, err := CompileProgram("todo.txt", body); err == nil {
			t.Fatalf("expected /cd validation error for %q", body)
		}
	}
}

func TestForUntilExprAndUnboundedExprParse(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 5 until (exist(\"result.json\") && len(open(\"result.json\")) > 0)\nretry {{n}}\n\n/for until(json(open(\"gate.json\")).passed)\nfinish {{n}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", plan.Tasks)
	}
	first := FlattenTaskFlow(plan.Tasks[0])[0].For
	if first.MaxRuns != 5 || first.Condition.Kind != ConditionExpr || first.Condition.Text != "exist(\"result.json\") && len(open(\"result.json\")) > 0" {
		t.Fatalf("unexpected bounded expression loop: %#v", first)
	}
	second := FlattenTaskFlow(plan.Tasks[1])[0].For
	if second.MaxRuns != 0 || second.VarName != "n" || second.Condition.Kind != ConditionExpr || second.Condition.Text != "json(open(\"gate.json\")).passed" {
		t.Fatalf("unexpected unbounded expression loop: %#v", second)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "For(n until expr(\"json(open(\\\"gate.json\\\")).passed\")) -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
}

func TestForUntilWithoutCountRequiresExpr(t *testing.T) {
	_, err := CompileProgram("todo.txt", "/for until tests pass\nretry\n")
	if err == nil || !strings.Contains(err.Error(), "requires a parenthesized expression") {
		t.Fatalf("expected unbounded natural-language until error, got %v", err)
	}
}

func TestCompileProgramValidatesExpressionSyntax(t *testing.T) {
	for _, body := range []string{
		"/if (exist(\"gate.json\") &&)\nCheck gate.\n",
		"/for until(json(\"gate.json\").)\nretry\n",
		"/for area in(json(open(outputDir(\"plan.json\"))).)\nreview {{area}}\n",
	} {
		if _, err := CompileProgram("todo.txt", body); err == nil || !strings.Contains(err.Error(), "expression") {
			t.Fatalf("expected expression validation error for %q, got %v", body, err)
		}
	}
}

func TestCompileProgramDiagnosticsLocateMarkdownDefinitionBody(t *testing.T) {
	content := strings.Join([]string{
		"# Runbook",
		"",
		"/def broken",
		"",
		"/for until tests pass",
		"retry",
		"/return done",
		"",
		"## /main",
		"",
		"/call broken",
		"use result",
		"",
	}, "\n")

	_, diagnostics := CompileProgramDiagnostics("todo.md", content)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", diagnostics)
	}
	got := diagnostics[0]
	if got.Source != "todo.md" || got.Line != 5 || got.Column != 1 || !strings.Contains(got.Message, "definition broken block 1") {
		t.Fatalf("unexpected diagnostic: %#v", got)
	}
}

func TestCompileProgramDiagnosticsUseImportedDefinitionSource(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "todo.md")
	libFile := filepath.Join(dir, "lib.todo.md")
	if err := os.WriteFile(libFile, []byte(strings.Join([]string{
		"/def broken",
		"",
		"/for until tests pass",
		"retry",
		"/return done",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	_, diagnostics := CompileProgramDiagnostics(mainFile, "/import lib.todo.md\n\n/call broken\nuse result\n")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", diagnostics)
	}
	got := diagnostics[0]
	if got.Source != libFile || got.Line != 3 || got.Column != 1 || !strings.Contains(got.Message, "requires a parenthesized expression") {
		t.Fatalf("unexpected diagnostic: %#v", got)
	}
}

func TestMarkdownImportScopeFollowsDeclarationHeading(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "todo.md")
	libFile := filepath.Join(dir, "lib.todo.md")
	if err := os.WriteFile(libFile, []byte("/def city\n/return Paris\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/import lib.todo.md",
		"",
		"### Child",
		"",
		"/task",
		"/call city",
		"Use imported city.",
		"",
		"## B",
		"",
		"/task",
		"/call city",
		"Should not see A import.",
		"",
	}, "\n")
	_, err := CompileProgram(mainFile, content)
	if err == nil || !strings.Contains(err.Error(), `unknown definition "city"`) {
		t.Fatalf("expected sibling import scope error, got %v", err)
	}
}

func TestMarkdownImportIsOnlyVisibleAfterDeclaration(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "todo.md")
	libFile := filepath.Join(dir, "lib.todo.md")
	if err := os.WriteFile(libFile, []byte("/def city\n/return Paris\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"# Root",
		"",
		"/task",
		"/call city",
		"Should not see future import.",
		"",
		"/import lib.todo.md",
		"",
		"/task",
		"/call city",
		"Can see prior import.",
		"",
	}, "\n")
	_, err := CompileProgram(mainFile, content)
	if err == nil || !strings.Contains(err.Error(), `unknown definition "city"`) {
		t.Fatalf("expected forward import scope error, got %v", err)
	}
}

func TestImportDoesNotLoadResourceDeclarations(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "todo.md")
	libFile := filepath.Join(dir, "lib.todo.md")
	lib := strings.Join([]string{
		"/db new imported_board scope:global persist:run access:write",
		"Imported board.",
		"",
		"/skill new imported_skill from skills/imported",
		"",
		"/mcp new imported_helper",
		"```json",
		`{"command":"helper"}`,
		"```",
		"",
		"/def city",
		"/return Paris",
		"",
	}, "\n")
	if err := os.WriteFile(libFile, []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "/import lib.todo.md\n\n/call city\nUse imported definition only.\n"
	plan, err := CompileProgram(mainFile, content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "city" {
		t.Fatalf("expected imported definition, got %#v", plan.Definitions)
	}
	if len(plan.DBs) != 0 || len(plan.Skills) != 0 || len(plan.MCPs) != 0 {
		t.Fatalf("imported resources leaked into main plan: db=%#v skills=%#v mcps=%#v", plan.DBs, plan.Skills, plan.MCPs)
	}

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "db",
			body: "/import lib.todo.md\n\n/db use imported_board\nShould not see imported DB.\n",
			want: `unknown db "imported_board"`,
		},
		{
			name: "skill",
			body: "/import lib.todo.md\n\n/skill use imported_skill\nShould not see imported skill.\n",
			want: `unknown skill "imported_skill"`,
		},
		{
			name: "mcp",
			body: "/import lib.todo.md\n\n/mcp use imported_helper\nShould not see imported MCP.\n",
			want: `unknown mcp "imported_helper"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileProgram(mainFile, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRecursiveImportIsRejected(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "todo.md")
	libFile := filepath.Join(dir, "lib.todo.md")
	if err := os.WriteFile(mainFile, []byte("/import lib.todo.md\n\n/task\nUse imports.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(libFile, []byte("/import todo.md\n\n/def city\n/return Paris\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(mainFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileProgram(mainFile, string(content))
	if err == nil || !strings.Contains(err.Error(), "recursive import") {
		t.Fatalf("expected recursive import error, got %v", err)
	}
}

func TestSelfImportIsRejected(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "todo.md")
	content := "/import todo.md\n\n/task\nUse imports.\n"
	if err := os.WriteFile(mainFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CompileProgram(mainFile, content)
	if err == nil || !strings.Contains(err.Error(), "recursive import") {
		t.Fatalf("expected self import error, got %v", err)
	}
}

func TestMarkdownDefinitionBlocksCarryTypedSpans(t *testing.T) {
	content := strings.Join([]string{
		"# Runbook",
		"",
		"/def pair name",
		"",
		"/pool local 1",
		"",
		"say {{name}}",
		"",
		"/return done",
		"",
		"## /main",
		"",
		"/call pair Bob",
		"",
	}, "\n")

	defs, imports, err := ParseLocalDefinitions("todo.md", content, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 0 {
		t.Fatalf("unexpected imports: %#v", imports)
	}
	if len(defs) != 1 || defs[0].Name != "pair" {
		t.Fatalf("unexpected definitions: %#v", defs)
	}
	blocks := defs[0].Blocks
	if len(blocks) != 2 {
		t.Fatalf("expected two definition blocks, got %#v", blocks)
	}
	want := []struct {
		body string
		line int
	}{
		{body: "/pool local 1\n", line: 5},
		{body: "say {{name}}\n\n/return done\n", line: 7},
	}
	for i, item := range want {
		if blocks[i].Body != item.body || blocks[i].Span.Line != item.line || blocks[i].Span.Column != 1 || blocks[i].Span.Block != i+1 {
			t.Fatalf("unexpected block %d: %#v", i+1, blocks[i])
		}
	}
}

func TestCompileProgramDiagnosticsCollectsValidationErrors(t *testing.T) {
	content := strings.Join([]string{
		"## /first",
		"",
		"/for until(false &&)",
		"retry",
		"",
		"## /second",
		"",
		"/call missing",
		"use result",
		"",
	}, "\n")

	_, diagnostics := CompileProgramDiagnostics("todo.md", content)
	if len(diagnostics) != 2 {
		t.Fatalf("expected two diagnostics, got %#v", diagnostics)
	}
	if diagnostics[0].Line != 3 || !strings.Contains(diagnostics[0].Message, "expression") {
		t.Fatalf("unexpected first diagnostic: %#v", diagnostics[0])
	}
	if diagnostics[1].Line != 8 || !strings.Contains(diagnostics[1].Message, `unknown definition "missing"`) {
		t.Fatalf("unexpected second diagnostic: %#v", diagnostics[1])
	}
}

func TestCompileProgramDiagnosticsCollectsParseErrorsAcrossBlocks(t *testing.T) {
	content := strings.Join([]string{
		"## /first",
		"",
		"/task",
		"/cd",
		"run",
		"",
		"## /second",
		"",
		"/for until tests pass",
		"retry",
		"",
	}, "\n")

	_, diagnostics := CompileProgramDiagnostics("todo.md", content)
	if len(diagnostics) != 2 {
		t.Fatalf("expected two diagnostics, got %#v", diagnostics)
	}
	if diagnostics[0].Line != 3 || !strings.Contains(diagnostics[0].Message, "/cd requires a path") {
		t.Fatalf("unexpected first diagnostic: %#v", diagnostics[0])
	}
	if diagnostics[1].Line != 9 || !strings.Contains(diagnostics[1].Message, "requires a parenthesized expression") {
		t.Fatalf("unexpected second diagnostic: %#v", diagnostics[1])
	}
}

func TestCompileProgramDiagnosticsWarnsForUnwaitedGo(t *testing.T) {
	plan, diagnostics := CompileProgramDiagnostics("todo.md", "/go\nReview docs.\n")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one warning, got %#v", diagnostics)
	}
	if diagnostics[0].Severity != "warning" || !strings.Contains(diagnostics[0].Message, "default pool") || !strings.Contains(diagnostics[0].Message, "without a later /wait") {
		t.Fatalf("unexpected warning: %#v", diagnostics[0])
	}
	if len(plan.Diagnostics) != 1 {
		t.Fatalf("expected warning on plan, got %#v", plan.Diagnostics)
	}
}

func TestCompileProgramDiagnosticsDoesNotWarnWhenGoIsWaited(t *testing.T) {
	_, diagnostics := CompileProgramDiagnostics("todo.md", "/go\nReview docs.\n\n/wait\n")
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestCompileProgramRejectsReturnOutsideDefinition(t *testing.T) {
	_, diagnostics := CompileProgramDiagnostics("todo.md", "/return done\n")
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].Severity != "error" || !strings.Contains(diagnostics[0].Message, "/return is only allowed inside /def") {
		t.Fatalf("unexpected diagnostic: %#v", diagnostics[0])
	}
}

func TestCompileProgramRejectsReturnInsideChildHeadingTask(t *testing.T) {
	content := strings.Join([]string{
		"# Release",
		"",
		"/task",
		"Prepare release notes.",
		"",
		"## Child",
		"",
		"/return done",
		"",
	}, "\n")
	_, diagnostics := CompileProgramDiagnostics("todo.md", content)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "/return is only allowed inside /def") {
		t.Fatalf("unexpected diagnostic: %#v", diagnostics[0])
	}
}

func TestReturnInsideMarkdownCodeFenceIsPromptText(t *testing.T) {
	content := strings.Join([]string{
		"# Release",
		"",
		"/task",
		"Show this literal command:",
		"",
		"```txt",
		"/return done",
		"```",
		"",
	}, "\n")
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || !strings.Contains(plan.Tasks[0].Prompt, "/return done") {
		t.Fatalf("expected literal /return in prompt, got %#v", plan.Tasks)
	}
}

func TestCompileProgramValidatesCalls(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown task call",
			body: "/call missing\nUse result.\n",
			want: `unknown definition "missing"`,
		},
		{
			name: "task call arity",
			body: "/def city name\n/return Paris\n\n## Use\n\n/call city\nUse result.\n",
			want: "/call city expects 1 argument(s), got 0",
		},
		{
			name: "definition call arity",
			body: "/def city name\n/return Paris\n\n/def use\n/call city\n/return done\n\n## Main\n\n/call use\n",
			want: "definition use block 1",
		},
		{
			name: "dynamic for call arity",
			body: "/def shards target\n/return\n```json\n{\"type\":\"array\"}\n```\n\n## Main\n\n/for shard in(/call shards)\nReview {{shard}}.\n",
			want: "/call shards expects 1 argument(s), got 0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileProgram("todo.md", tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDynamicForSourceParsesAsExpr(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/pool reviewer 2\n\n/for area in(json(open(outputDir(\"release-plan.json\"))).areas)\n/go reviewer\nReview {{area.name}}.\n")
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(plan.Tasks[0])
	if len(plan.Tasks) != 1 || len(ops) < 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	loop := ops[0].For
	if loop.VarName != "area" || loop.Source.Kind != ConditionExpr || loop.Source.Text != `json(open(outputDir("release-plan.json"))).areas` {
		t.Fatalf("unexpected dynamic loop: %#v", loop)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != `For(area in expr("json(open(outputDir(\"release-plan.json\"))).areas")) -> Go(reviewer) -> Execute` {
		t.Fatalf("unexpected flow: %s", got)
	}
}

func TestDynamicForSourceParsesRangeExpr(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for shard in(range(1, 4))\nReview shard {{shard}}.\n")
	if err != nil {
		t.Fatal(err)
	}
	loop := FlattenTaskFlow(plan.Tasks[0])[0].For
	if loop.VarName != "shard" || loop.Source.Kind != ConditionExpr || loop.Source.Text != `range(1, 4)` {
		t.Fatalf("unexpected dynamic loop: %#v", loop)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != `For(shard in expr("range(1, 4)")) -> Execute` {
		t.Fatalf("unexpected flow: %s", got)
	}
}

func TestDynamicForSourceParsesCall(t *testing.T) {
	plan, err := CompileProgram("todo.md", "/def plan_shards\n/return\n```json\n{\"type\":\"array\"}\n```\n\n## Main\n\n/pool reviewer 2\n\n/for plan in(/call plan_shards)\n/go reviewer\n{{plan}}\n")
	if err != nil {
		t.Fatal(err)
	}
	loop := FlattenTaskFlow(plan.Tasks[0])[0].For
	if loop.VarName != "plan" || loop.Source.Kind != ConditionCall || loop.Source.Text != `/call plan_shards` {
		t.Fatalf("unexpected call loop: %#v", loop)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != `For(plan in call("/call plan_shards")) -> Go(reviewer) -> Execute` {
		t.Fatalf("unexpected flow: %s", got)
	}
}

func TestOutputAndReturnModesParse(t *testing.T) {
	task, err := ParseTask(0, "/output note\nSummarize.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.IsStructured() || task.Output.FileName != "note" {
		t.Fatalf("unexpected non-structured output: %#v", task.Output)
	}
	defTask, err := ParseTask(0, "/return\n```json\n{\"type\":\"object\"}\n```\nAssess.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if defTask.Return == nil || defTask.Return.Kind != ReturnStructured || defTask.Return.Output == nil || !defTask.Return.Output.IsStructured() {
		t.Fatalf("expected structured return, got %#v", defTask.Return)
	}
	arrayTask, err := ParseTask(0, "/return\n```schema\nplans:[]string:计划\n```\nPlan.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(arrayTask.Return.Output.Schema, `"type": "array"`) || !strings.Contains(arrayTask.Return.Output.Schema, `"items"`) {
		t.Fatalf("expected array schema, got %s", arrayTask.Return.Output.Schema)
	}
	if strings.TrimSpace(defTask.Prompt) != "Assess." {
		t.Fatalf("unexpected structured return prompt: %q", defTask.Prompt)
	}
}

func TestReturnFencedTextAndBashParse(t *testing.T) {
	textTask, err := ParseTask(0, "/return\n```\nline one\nline two\n```\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if textTask.Return == nil || textTask.Return.Kind != ReturnTemplate || textTask.Return.Text != "line one\nline two" {
		t.Fatalf("unexpected fenced text return: %#v", textTask.Return)
	}

	bashTask, err := ParseTask(0, "/return /bash\n```\ngit diff --name-only\n```\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if bashTask.Return == nil || bashTask.Return.Kind != ReturnBash || bashTask.Return.Script != "git diff --name-only" {
		t.Fatalf("unexpected fenced bash return: %#v", bashTask.Return)
	}

	schemaTask, err := ParseTask(0, "/return\n```schema\nreason:string:why\n```\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if schemaTask.Return == nil || schemaTask.Return.Kind != ReturnStructured || !strings.Contains(schemaTask.Return.Output.Schema, `"reason"`) {
		t.Fatalf("unexpected schema return: %#v", schemaTask.Return)
	}
}

func TestReturnRejectsBareMultilineAndInlinePlusFence(t *testing.T) {
	if _, err := ParseTask(0, "/return\nline one\nline two\n", nil, CompileOptions{Root: "."}); err == nil || !strings.Contains(err.Error(), "fenced block") {
		t.Fatalf("expected bare multiline return error, got %v", err)
	}
	if _, err := ParseTask(0, "/return\n~~~\nline one\n~~~\n", nil, CompileOptions{Root: "."}); err == nil || !strings.Contains(err.Error(), "backticks") {
		t.Fatalf("expected tilde return fence error, got %v", err)
	}
	if _, err := ParseTask(0, "/return value\n```\nextra\n```\n", nil, CompileOptions{Root: "."}); err == nil || !strings.Contains(err.Error(), "cannot be followed") {
		t.Fatalf("expected inline plus fence return error, got %v", err)
	}
	if _, err := ParseTask(0, "/return /bash echo ok\n```\nextra\n```\n", nil, CompileOptions{Root: "."}); err == nil || !strings.Contains(err.Error(), "cannot be followed") {
		t.Fatalf("expected inline bash plus fence return error, got %v", err)
	}
}

func TestIfAndElseCommandsAreBlockLevelNoOpsInTaskIR(t *testing.T) {
	task, err := ParseTask(0, "/if (exist(\"gate.json\"))\nContinue.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(task.Prompt) != "Continue." {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
	if got := FormatTaskFlow(task); got != `If(expr:exist("gate.json")) -> Execute` {
		t.Fatalf("unexpected if flow: %s", got)
	}
	ifBlock, ok, err := ParseIfBlock("/if (exist(\"gate.json\"))\nContinue.\n")
	if err != nil || !ok || ifBlock.Condition.Text != `exist("gate.json")` || ifBlock.Condition.Kind != ConditionExpr || ifBlock.HeaderOnly {
		t.Fatalf("unexpected if parse ok=%v block=%#v err=%v", ok, ifBlock, err)
	}
	compact, ok, err := ParseIfBlock("/if(exist(\"gate.json\"))\nContinue.\n")
	if err != nil || !ok || compact.Condition.Text != `exist("gate.json")` || compact.Condition.Kind != ConditionExpr {
		t.Fatalf("unexpected compact if parse ok=%v block=%#v err=%v", ok, compact, err)
	}
	natural, ok, err := ParseIfBlock("/if release gate is open\nContinue.\n")
	if err != nil || !ok || natural.Condition.Text != "release gate is open" || natural.Condition.Kind != ConditionNatural {
		t.Fatalf("unexpected natural if parse ok=%v block=%#v err=%v", ok, natural, err)
	}
	fenced, ok, err := ParseIfBlock("/if\n```\nrelease gate is open\nand checks are green\n```\nContinue.\n")
	if err != nil || !ok || fenced.Condition.Text != "release gate is open\nand checks are green" || fenced.Condition.Kind != ConditionNatural {
		t.Fatalf("unexpected fenced if parse ok=%v block=%#v err=%v", ok, fenced, err)
	}
	elseTask, err := ParseTask(0, "/else\nFallback.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(elseTask.Prompt) != "Fallback." {
		t.Fatalf("unexpected else prompt: %q", elseTask.Prompt)
	}
	task, err = ParseTask(0, "/args --yolo\n/if (true)\nContinue.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTaskFlow(task); got != "If(expr:true) -> Execute [args=--yolo]" {
		t.Fatalf("unexpected modifier + if flow: %s", got)
	}
}

func TestCompileProgramRejectsNestedIf(t *testing.T) {
	for _, body := range []string{
		"/if (true) /if (false)\nNested then.\n",
		"/if (true)\n\n/if (false)\n\ninner then\n\n/else\ninner else\n\n/else\nouter else\n",
	} {
		_, err := CompileProgram("todo.txt", body)
		if err == nil || !(strings.Contains(err.Error(), "nested /if is not supported") || strings.Contains(err.Error(), "/if does not support nesting")) {
			t.Fatalf("expected nested /if error for %q, got %v", body, err)
		}
	}
}

func TestFencedNaturalIfAndUntilArguments(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/if\n```\nrelease gate is open\n\nand checks are green\n```\nDeploy.\n\n/for 3 until\n```\ntests pass\n\nand lint passes\n```\nFix {{n}}.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Controls) != 1 || plan.Controls[0].Condition.Kind != ConditionNatural || plan.Controls[0].Condition.Text != "release gate is open\n\nand checks are green" {
		t.Fatalf("unexpected fenced if condition: %#v", plan.Controls)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", plan.Tasks)
	}
	loop := FlattenTaskFlow(plan.Tasks[1])[0].For
	if loop.Condition.Kind != ConditionNatural || loop.Condition.Text != "tests pass\n\nand lint passes" {
		t.Fatalf("unexpected fenced until condition: %#v", loop.Condition)
	}
	if strings.TrimSpace(plan.Tasks[1].Prompt) != "Fix {{n}}." {
		t.Fatalf("unexpected prompt after fenced until: %q", plan.Tasks[1].Prompt)
	}
}

func TestPoolDeclarationsAndNamedGoWait(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/pool tester 5 10\n\n/go tester\nReview.\n\n/wait tester\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Pools) != 1 || plan.Pools[0].Name != "tester" || plan.Pools[0].Max != 5 || plan.Pools[0].Buffer != 10 {
		t.Fatalf("unexpected pool declarations: %#v", plan.Pools)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != "Go(tester) -> Execute" {
		t.Fatalf("unexpected named /go flow: %s", got)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "Wait(tester) -> Execute" {
		t.Fatalf("unexpected named /wait flow: %s", got)
	}
}

func TestMarkdownPoolScopeFollowsHeadingAncestry(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/pool scoped 1",
		"",
		"### Child",
		"",
		"/go scoped",
		"Can use scoped pool.",
		"",
		"## B",
		"",
		"/go scoped",
		"Should not see sibling pool.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown pool "scoped"`) {
		t.Fatalf("expected sibling pool scope error, got %v", err)
	}
}

func TestMarkdownPoolIsOnlyVisibleAfterDeclaration(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"/go later",
		"Should not see future pool.",
		"",
		"/pool later 1",
		"",
		"/go later",
		"Can use prior pool.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown pool "later"`) {
		t.Fatalf("expected forward pool scope error, got %v", err)
	}
}

func TestMarkdownSkillScopeFollowsHeadingAncestry(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/skill new reviewer from skills/reviewer",
		"",
		"### Child",
		"",
		"/skill use reviewer",
		"Can use scoped skill.",
		"",
		"## B",
		"",
		"/skill use reviewer",
		"Should not see sibling skill.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown skill "reviewer"`) {
		t.Fatalf("expected sibling skill scope error, got %v", err)
	}
}

func TestMarkdownSkillIsOnlyVisibleAfterDeclaration(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"/skill use reviewer",
		"Should not see future skill.",
		"",
		"/skill new reviewer from skills/reviewer",
		"",
		"/skill use reviewer",
		"Can use prior skill.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown skill "reviewer"`) {
		t.Fatalf("expected forward skill scope error, got %v", err)
	}
}

func TestMarkdownMCPScopeFollowsHeadingAncestry(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/mcp new helper",
		"```json",
		`{"command":"helper"}`,
		"```",
		"",
		"### Child",
		"",
		"/mcp use helper",
		"Can use scoped MCP.",
		"",
		"## B",
		"",
		"/mcp use helper",
		"Should not see sibling MCP.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown mcp "helper"`) {
		t.Fatalf("expected sibling mcp scope error, got %v", err)
	}
}

func TestMarkdownMCPIsOnlyVisibleAfterDeclaration(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"/mcp use helper",
		"Should not see future MCP.",
		"",
		"/mcp new helper",
		"```json",
		`{"command":"helper"}`,
		"```",
		"",
		"/mcp use helper",
		"Can use prior MCP.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown mcp "helper"`) {
		t.Fatalf("expected forward mcp scope error, got %v", err)
	}
}

func TestWaitWithPromptFormatsAsWaitAgent(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/go\nReview.\n\n/wait\nSummarize tester findings.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected go task plus wait agent task, got %#v", plan.Tasks)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "WaitAgent" {
		t.Fatalf("unexpected /wait prompt flow: %s", got)
	}
}

func TestPoolDeclarationDefaultsToUnlimitedBuffer(t *testing.T) {
	pools, ok, err := ParseGlobalPoolBlock("/pool tester 5\n")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || len(pools) != 1 || pools[0].Buffer != -1 {
		t.Fatalf("expected unlimited-buffer pool declaration, got ok=%v pools=%#v", ok, pools)
	}
}

func TestCompileProgramValidatesPoolReferences(t *testing.T) {
	for _, body := range []string{
		"/go missing\nReview.\n",
		"/wait missing\n",
	} {
		if _, err := CompileProgram("todo.txt", body); err == nil || !strings.Contains(err.Error(), "unknown pool") {
			t.Fatalf("expected unknown pool error for %q, got %v", body, err)
		}
	}
}

func TestDBDeclarationsAndTaskConfigParse(t *testing.T) {
	content := "/db new decisions scope:global persist:project access:write\nUse for release decisions.\n\n/db new notes scope:local persist:run access:append\nTask notes.\n\n/db use notes access:append\n/db access decisions read\nReview.\n\n/db ignore\nNo DB.\n"
	plan, err := CompileProgram("todo.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.DBs) != 2 {
		t.Fatalf("expected one db declaration, got %#v", plan.DBs)
	}
	db := plan.DBs[0]
	if db.Name != "decisions" || db.Scope != DBScopeGlobal || db.Persist != DBPersistProject || db.Access != DBAccessWrite || db.Usage != "Use for release decisions." {
		t.Fatalf("unexpected db declaration: %#v", db)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", plan.Tasks)
	}
	first := plan.Tasks[0].DB
	if len(first.Use) != 1 || first.Use[0].Names[0] != "notes" || first.Use[0].Access != DBAccessAppend {
		t.Fatalf("unexpected db use: %#v", first)
	}
	if len(first.Access) != 1 || first.Access[0].Names[0] != "decisions" || first.Access[0].Access != DBAccessRead {
		t.Fatalf("unexpected db access: %#v", first)
	}
	if !plan.Tasks[1].DB.IgnoreAll {
		t.Fatalf("expected ignore-all config: %#v", plan.Tasks[1].DB)
	}
}

func TestDBTaskConfigRejectsAmbiguousAccess(t *testing.T) {
	_, err := ParseTask(0, "/db use notes access:append\n/db access notes read\nReview.\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "conflicting access") {
		t.Fatalf("expected conflicting access error, got %v", err)
	}
	_, err = ParseTask(0, "/db ignore\n/db use notes\nReview.\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected ignore/use conflict, got %v", err)
	}
}

func TestMarkdownDBScopeFollowsHeadingAncestry(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/db new board scope:global persist:run access:append",
		"Scoped board.",
		"",
		"### Child",
		"",
		"/db access board read",
		"Can use scoped DB.",
		"",
		"## B",
		"",
		"/db access board read",
		"Should not see sibling DB.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unavailable db "board"`) {
		t.Fatalf("expected sibling db scope error, got %v", err)
	}
}

func TestMarkdownDBIsOnlyVisibleAfterDeclaration(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"/db access board read",
		"Should not see future DB.",
		"",
		"/db new board scope:global persist:run access:append",
		"Scoped board.",
		"",
		"/db access board read",
		"Can use prior DB.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unavailable db "board"`) {
		t.Fatalf("expected forward db scope error, got %v", err)
	}
}

func TestMarkdownLocalDBUseRequiresVisibleDeclaration(t *testing.T) {
	content := strings.Join([]string{
		"# Root",
		"",
		"## A",
		"",
		"/db new scratch scope:local persist:run access:append",
		"Scoped scratch.",
		"",
		"## B",
		"",
		"/db use scratch access:append",
		"Should not see sibling local DB.",
		"",
	}, "\n")
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown db "scratch"`) {
		t.Fatalf("expected sibling local db scope error, got %v", err)
	}
}

func TestCompileProgramValidatesResourceReferences(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown db",
			body: "/db use missing\nReview.\n",
			want: `unknown db "missing"`,
		},
		{
			name: "db access elevation",
			body: "/db new notes scope:local persist:run access:read\nNotes.\n\n/db use notes access:write\nReview.\n",
			want: "exceeds declared access read",
		},
		{
			name: "unknown mcp",
			body: "/mcp use helper\nReview.\n",
			want: `unknown mcp "helper"`,
		},
		{
			name: "unknown def mcp",
			body: "/mcp def use missing\nReview.\n",
			want: `unknown definition "missing"`,
		},
		{
			name: "invalid mcp json",
			body: "/mcp new helper\n```json\n{\n```\n",
			want: "config must be valid JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileProgram("todo.txt", tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSkillMCPAndDefMCPParse(t *testing.T) {
	content := strings.Join([]string{
		"/skill new release_reviewer from skills/release-reviewer",
		"",
		"/mcp new helper",
		"```json",
		`{"command":"helper-mcp","args":["--stdio"]}`,
		"```",
		"",
		"/def inspect_area area",
		"Inspect {{area}}.",
		"/return {{agent.last_message}}",
		"",
		"",
		"/cd work",
		"/skill use release_reviewer",
		"/mcp use helper",
		"/mcp def use inspect_area",
		"Inspect api and docs.",
		"",
	}, "\n")
	plan, err := CompileProgram("todo.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Skills) != 1 || plan.Skills[0].Name != "release_reviewer" {
		t.Fatalf("unexpected skill declarations: %#v", plan.Skills)
	}
	if len(plan.MCPs) != 1 || plan.MCPs[0].Name != "helper" || !strings.Contains(plan.MCPs[0].Config, "helper-mcp") {
		t.Fatalf("unexpected mcp declarations: %#v", plan.MCPs)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "inspect_area" {
		t.Fatalf("unexpected definitions: %#v", plan.Definitions)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one runnable task, got %#v", plan.Tasks)
	}
	task := plan.Tasks[0]
	if got := FormatTaskFlow(task); got != "Cd(work) -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
	if len(task.Skill.Use) != 1 || task.Skill.Use[0] != "release_reviewer" {
		t.Fatalf("unexpected skill task config: %#v", task.Skill)
	}
	if len(task.MCP.Use) != 1 || task.MCP.Use[0] != "helper" || len(task.MCP.DefUse) != 1 || task.MCP.DefUse[0] != "inspect_area" {
		t.Fatalf("unexpected mcp task config: %#v", task.MCP)
	}
}

func TestMarkdownDefinitionsAreLoadedButNotRunnableTasks(t *testing.T) {
	content := "/def whereami\nReturn city.\n\n/return Paris\n\n## Weather\n\nWeather for\n/call whereami\n"
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "whereami" {
		t.Fatalf("unexpected definitions: %#v", plan.Definitions)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected only runnable weather task, got %#v", plan.Tasks)
	}
}

func TestMarkdownDefinitionScopeFollowsHeadingAncestry(t *testing.T) {
	content := "# Root\n\n/def root_city\n/return Paris\n\n## A\n\n/def local_city\n/return Berlin\n\n### Child\n\n/task\n/call root_city\n/call local_city\nUse child definitions.\n\n## B\n\n/task\n/call local_city\nShould not see A-local definition.\n"
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown definition "local_city"`) {
		t.Fatalf("expected sibling definition scope error, got %v", err)
	}
}

func TestMarkdownDefinitionIsOnlyVisibleAfterDeclaration(t *testing.T) {
	content := "# Root\n\n/task\n/call later\nShould not see future definition.\n\n/def later\n/return done\n"
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), `unknown definition "later"`) {
		t.Fatalf("expected forward definition scope error, got %v", err)
	}
}

func TestDefinitionCycleDetectionIncludesInlineCalls(t *testing.T) {
	content := "/def a\n/call b\n/return a\n\n/def b\n/call a\n/return b\n"
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), "recursive definition call") {
		t.Fatalf("expected recursive definition error, got %v", err)
	}
}

func TestCallOpsAndReturnOpsAreParsed(t *testing.T) {
	task, err := ParseTask(0, "/let city /call whereami\nWeather for {{city}}.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(task)
	if len(ops) < 2 || ops[0].Kind != FlatOpCall || ops[0].Call.Assign != "city" {
		t.Fatalf("expected assigned call op, got %#v", ops)
	}
	defTask, err := ParseTask(0, "Find city.\n/return\n```\nCity: {{agent.last_message}}\n```\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if defTask.Return == nil || defTask.Return.Text != "City: {{agent.last_message}}" {
		t.Fatalf("expected multiline return, got %#v", defTask.Return)
	}
}

func TestTaskIRKeepsBashOnlyAsOrderedOps(t *testing.T) {
	task, err := ParseTask(0, "/bash echo setup\n/let name /bash printf ok\nUse {{name}}\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	var bashOps int
	ops := FlattenTaskFlow(task)
	for _, op := range ops {
		if op.Kind == FlatOpBash {
			bashOps++
		}
	}
	if bashOps != 2 {
		t.Fatalf("expected two bash ops, got %#v", ops)
	}
}

func TestLoopOptionsDoNotAlsoAttachToExecuteOp(t *testing.T) {
	task, err := ParseTask(0, "/args --yolo /for 2\nreview\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(task)
	if len(ops) != 2 || ops[0].Kind != FlatOpFor || ops[1].Kind != FlatOpExecute {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if got := strings.Join(ops[0].For.Options.Args, " "); got != "--yolo" {
		t.Fatalf("expected loop args, got %q", got)
	}
	if len(ops[1].ExecuteOptions.Args) != 0 {
		t.Fatalf("did not expect duplicate execute args: %#v", ops[1].ExecuteOptions.Args)
	}
}

func TestParseBlocksSupportsWhitespaceBlankLinesAndWholeLineComments(t *testing.T) {
	content := strings.Join([]string{
		"<!-- hidden",
		"still hidden",
		"-->",
		"<!-- hidden single line -->",
		"[//]: # (hidden reference)",
		"[comment]: <> (hidden reference)",
		"---",
		"   ===   ",
		"first # inline hash stays visible",
		"   ",
		"",
		"second",
		"",
	}, "\n")
	blocks := ParseBlocks(content)
	if len(blocks) != 0 {
		t.Fatalf("expected root Markdown without task commands to be documentation-only, got %#v", blocks)
	}
}

func TestCommentsDoNotApplyInsideHeredoc(t *testing.T) {
	task, err := ParseTask(0, "/bash <<'SH'\n# shell comment\nprintf ok\nSH\nRun after bash.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	ops := FlattenTaskFlow(task)
	if len(ops) == 0 || ops[0].Kind != FlatOpBash {
		t.Fatalf("expected bash op: %#v", ops)
	}
	if !strings.Contains(ops[0].Bash.Script, "# shell comment") {
		t.Fatalf("expected heredoc comment preserved: %q", ops[0].Bash.Script)
	}
}

func TestMixedContentAndCommentSyntaxIsPromptText(t *testing.T) {
	blocks := ParseBlocks("visible <!-- not a todo comment -->\n\n<!-- comment --> visible text\n\nprompt --- still prompt\n\n--- not ignored\n")
	if len(blocks) != 0 {
		t.Fatalf("expected root Markdown without task commands to be documentation-only, got %#v", blocks)
	}
}

func TestUnusedLetBindingsAreAllowed(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/let bypass true\n\n/task\nRun without using the variable.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Globals) != 1 || plan.Globals[0].Name != "bypass" {
		t.Fatalf("expected unused global let binding, got %#v", plan.Globals)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected task after unused let binding, got %#v", plan.Tasks)
	}

	task, err := ParseTask(0, "/let local unused\nRun without using local.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Prompt != "Run without using local.\n" {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
}

func TestMultilineGlobalLetCanBeUsedAsPromptPrefix(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/let notes\nRead README.md.\nCheck docs/commands.md.\n\n/task\n/notes\nReview installation flow.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Globals) != 1 {
		t.Fatalf("expected one global binding, got %#v", plan.Globals)
	}
	if got, want := plan.Globals[0].Value, "Read README.md.\nCheck docs/commands.md."; got != want {
		t.Fatalf("unexpected global value:\ngot  %q\nwant %q", got, want)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one task, got %#v", plan.Tasks)
	}
	want := "Read README.md.\nCheck docs/commands.md.\nReview installation flow.\n"
	if got := plan.Tasks[0].Prompt; got != want {
		t.Fatalf("unexpected prompt:\ngot  %q\nwant %q", got, want)
	}
}

func TestMultilineLocalLetCanBeUsedAsPromptPrefix(t *testing.T) {
	task, err := ParseTask(0, "/let notes\nRead README.md.\nCheck docs/commands.md.\n/notes\nReview installation flow.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	want := "Read README.md.\nCheck docs/commands.md.\nReview installation flow.\n"
	if got := task.Prompt; got != want {
		t.Fatalf("unexpected prompt:\ngot  %q\nwant %q", got, want)
	}
}

func TestRenderTemplateKeepsLegacyVariablesCompatible(t *testing.T) {
	got, err := RenderTemplate("Review {{path}} pass {{n}}; keep {{future}}.", map[string]string{
		"path": "README.md",
		"n":    "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Review README.md pass 2; keep {{future}}."
	if got != want {
		t.Fatalf("unexpected render:\nwant %q\ngot  %q", want, got)
	}
}

func TestRenderTemplateSupportsGoTemplateActions(t *testing.T) {
	got, err := RenderTemplate("{{if .n}}Pass {{.n}}: {{end}}{{.path}} {{index .Vars \"path\"}} {{var \"name-with-dash\"}} {{has \"path\"}}", map[string]string{
		"n":              "3",
		"path":           "api/server.go",
		"name-with-dash": "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Pass 3: api/server.go api/server.go ok true"
	if got != want {
		t.Fatalf("unexpected render:\nwant %q\ngot  %q", want, got)
	}
}

func TestRenderTemplateReportsInvalidGoTemplateSyntax(t *testing.T) {
	if _, err := RenderTemplate("{{if .n}}missing end", map[string]string{"n": "1"}); err == nil {
		t.Fatal("expected template error")
	}
}

func TestOutputCommandParsesJSONSchemaFence(t *testing.T) {
	task, err := ParseTask(0, "/output result.json\n```json\n{\"type\":\"object\"}\n```\nSummarize.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.FileName != "result.json" || task.Output.SchemaFormat != "json" {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	if strings.TrimSpace(task.Prompt) != "Summarize." {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
}

func TestOutputCommandAfterPromptIsRejected(t *testing.T) {
	_, err := ParseTask(0, "Summarize first.\n/output result\n```\nreason:string:why\n```\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "task header") {
		t.Fatalf("expected prompt /output rejection, got %v", err)
	}
}

func TestOutputCommandCanAppearBetweenFlowCommandsAndPrompt(t *testing.T) {
	task, err := ParseTask(0, "/for 2 /go\n/output result-{{n}}\n```\nreason:string:why\n```\nSummarize {{n}}.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.FileName != "result-{{n}}" {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	if got := FormatTaskFlow(task); got != "For(n in [0 1]) -> Go -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
	if strings.TrimSpace(task.Prompt) != "Summarize {{n}}." {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
}

func TestOutputFenceSupportsLongerMarkdownFence(t *testing.T) {
	body := "/output result\n" +
		"````json\n" +
		"{\"description\":\"schema can mention ``` safely\",\"type\":\"object\"}\n" +
		"````\n" +
		"Report.\n"
	task, err := ParseTask(0, body, nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || !strings.Contains(task.Output.Schema, "``` safely") {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	if strings.TrimSpace(task.Prompt) != "Report." {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
}

func TestParseBlocksKeepsOutputFenceBlankLinesInOneTask(t *testing.T) {
	content := "/output result.json\n" +
		"```json\n" +
		"{\n" +
		"  \"type\": \"object\",\n" +
		"\n" +
		"  \"properties\": {\n" +
		"    \"reason\": {\"type\": \"string\"}\n" +
		"  }\n" +
		"}\n" +
		"```\n" +
		"Summarize.\n" +
		"\n" +
		"Next task.\n"
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one Markdown task block, got %#v", blocks)
	}
	if !strings.Contains(blocks[0].Body, "\n\n  \"properties\"") {
		t.Fatalf("expected blank line inside output schema preserved: %q", blocks[0].Body)
	}
	task, err := ParseTask(0, blocks[0].Body, nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || !strings.Contains(task.Output.Schema, `"properties"`) {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
}

func TestOutputCommandParsesSimpleSchema(t *testing.T) {
	task, err := ParseTask(0, "/output\n```\nreason:string:描述详细原因\nweather:天气状态\nflag::是否通过\n```\nReport.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.FileName != "" || task.Output.SchemaFormat != "json" {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	for _, want := range []string{`"reason"`, `"weather"`, `"flag"`, `"type": "string"`, `"description": "天气状态"`} {
		if !strings.Contains(task.Output.Schema, want) {
			t.Fatalf("expected schema to contain %q:\n%s", want, task.Output.Schema)
		}
	}
}

func TestOutputCommandParsesYAMLSchemaFence(t *testing.T) {
	task, err := ParseTask(0, "/output answer\n```yaml\ntype: object\nproperties:\n  answer:\n    type: string\n```\nReport.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.FileName != "answer" || task.Output.SchemaFormat != "yaml" {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	if !strings.Contains(task.Output.Schema, "properties:") {
		t.Fatalf("unexpected schema: %q", task.Output.Schema)
	}
}

func TestOutputCommandRejectsTildeSchemaFence(t *testing.T) {
	_, err := ParseTask(0, "/output result\n~~~schema\nreason:string:why\n~~~\nExplain.\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "/output schema fence must use backticks") {
		t.Fatalf("expected tilde output schema fence error, got %v", err)
	}
}

func TestOutputCommandRejectsDuplicateOutput(t *testing.T) {
	_, err := ParseTask(0, "/output one\n```\na:string:first\n```\n/output two\n```\nb:string:second\n```\nReport.\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "can only appear once") {
		t.Fatalf("expected duplicate /output error, got %v", err)
	}
}

func TestMarkdownHeadingsProvideContextAndCommandsStartTasks(t *testing.T) {
	content := `# Release notes

ordinary intro

## Verify

Run tests.

Run vet.

## Notes

ignored note

## Review docs
/go
Review docs.

Use two paragraphs.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one command-started block, got %#v", blocks)
	}
	if !strings.Contains(blocks[0].Prefix, "# Release notes") || !strings.Contains(blocks[0].Prefix, "## Review docs") {
		t.Fatalf("expected markdown prefix preserved: %q", blocks[0].Prefix)
	}
	if strings.TrimSpace(blocks[0].Body) != "/go\nReview docs.\n\nUse two paragraphs." {
		t.Fatalf("unexpected body: %q", blocks[0].Body)
	}
	if blocks[0].Context != "# Release notes\n## Review docs" {
		t.Fatalf("unexpected context: %q", blocks[0].Context)
	}
}

func TestMarkdownWithoutTaskCommandsIsDocumentationOnly(t *testing.T) {
	content := `# Verify
Intro.

## Details

Still part of the task.

# Plain heading

Ignored.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 0 {
		t.Fatalf("expected documentation-only markdown to have no tasks, got %#v", blocks)
	}
}

func TestMarkdownTaskCommandStartsPlainTask(t *testing.T) {
	content := `# Verify
Runbook context.

/task
Run tests.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one /task block, got %#v", blocks)
	}
	if strings.TrimSpace(blocks[0].Body) != "/task\nRun tests." {
		t.Fatalf("unexpected block: %#v", blocks[0])
	}
	if !strings.Contains(blocks[0].Context, "Runbook context.") {
		t.Fatalf("expected section context, got %q", blocks[0].Context)
	}
}

func TestRootMarkdownCommandStartsTaskWithoutHeading(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 3\n审计当前项目。\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one task, got %#v", plan.Tasks)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != "For(n in [0 1 2]) -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
	if got := plan.Tasks[0].Prompt; got != "审计当前项目。\n" {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestMarkdownContextCommandIncludesReferencedSection(t *testing.T) {
	content := `# Shared Rules

Use the release checklist.

# Private Notes

/doc Do not expose this note.

# Work

Local context.

/task
/context #Shared Rules
Review the change.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one task block, got %#v", blocks)
	}
	if strings.Contains(blocks[0].Body, "/context") {
		t.Fatalf("expected /context header to be removed from task body: %q", blocks[0].Body)
	}
	for _, want := range []string{"# Work", "Local context.", "# Shared Rules", "Use the release checklist."} {
		if !strings.Contains(blocks[0].Context, want) {
			t.Fatalf("expected context to contain %q, got:\n%s", want, blocks[0].Context)
		}
	}
	if strings.Contains(blocks[0].Context, "Do not expose") || strings.Contains(blocks[0].Context, "Private Notes") {
		t.Fatalf("private section leaked into context:\n%s", blocks[0].Context)
	}
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || !strings.Contains(plan.Tasks[0].Prompt, "Use the release checklist.") || strings.Contains(plan.Tasks[0].Prompt, "/context") {
		t.Fatalf("expected compiled prompt to include explicit context without command line, got %#v", plan.Tasks)
	}
}

func TestMarkdownContextCommandRejectsMissingSection(t *testing.T) {
	content := `# Work

/task
/context #Missing
Review the change.
`
	_, err := CompileProgram("todo.md", content)
	if err == nil || !strings.Contains(err.Error(), "/context requires a known Markdown heading reference") {
		t.Fatalf("expected missing context command to reach validation, got %v", err)
	}
}

func TestMarkdownDocRemovesDefaultSectionContext(t *testing.T) {
	content := "# Sensitive\n\n/doc\n```\nDo not send this to the agent.\n```\n\n/task\nReview public files.\n"
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one task block, got %#v", blocks)
	}
	if strings.Contains(blocks[0].Context, "Do not send") {
		t.Fatalf("expected /doc content to be excluded from default context, got:\n%s", blocks[0].Context)
	}
}

func TestMarkdownTaskEndsAtSameOrHigherHeading(t *testing.T) {
	content := `# Review

/task
Intro.

## Details

Keep details.

# Next

/task
Next task.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected two heading-scoped tasks, got %#v", blocks)
	}
	if !strings.Contains(blocks[0].Body, "## Details") || !strings.Contains(blocks[0].Body, "Keep details.") || strings.Contains(blocks[0].Body, "# Next") {
		t.Fatalf("expected nested markdown heading preserved: %q", blocks[0].Body)
	}
}

func TestMarkdownTaskKeepsFencedHeadingsInPrompt(t *testing.T) {
	content := "# Work\n\n/task\nExplain this example:\n\n```md\n# Example heading\nbody\n```\n\nStill same task.\n\n# Next\n\n/task\nNext task.\n"
	blocks := ParseBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected fenced heading to stay inside first task, got %#v", blocks)
	}
	if !strings.Contains(blocks[0].Body, "# Example heading") || !strings.Contains(blocks[0].Body, "Still same task.") {
		t.Fatalf("expected fenced heading and following prompt preserved: %q", blocks[0].Body)
	}
	if strings.Contains(blocks[0].Body, "# Next") {
		t.Fatalf("expected real sibling heading to end first task: %q", blocks[0].Body)
	}
}

func TestMarkdownRootCommandAfterPromptStartsSiblingTask(t *testing.T) {
	content := `# Verify

/task
Run tests.

/for 2
Run again {{n}}.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected two sibling tasks, got %#v", blocks)
	}
	if strings.TrimSpace(blocks[0].Body) != "/task\nRun tests." {
		t.Fatalf("unexpected first task body: %q", blocks[0].Body)
	}
	if strings.TrimSpace(blocks[1].Body) != "/for 2\nRun again {{n}}." {
		t.Fatalf("unexpected second task body: %q", blocks[1].Body)
	}
}

func TestMarkdownChildHeadingTaskInheritsParentRootAndOwnSection(t *testing.T) {
	content := `# Review

/task
Review backend.

### Scope1

API and migrations.

/for 2
Fix tests {{n}}.

### Scope2

Docs.

/task
Fix docs.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 3 {
		t.Fatalf("expected three blocks, got %#v", blocks)
	}
	if blocks[0].HasParent {
		t.Fatalf("parent task should not have a parent: %#v", blocks[0])
	}
	if !blocks[1].HasParent || blocks[1].ParentIndex != 0 {
		t.Fatalf("expected scope1 task to be child of parent block 0, got %#v", blocks[1])
	}
	if !blocks[2].HasParent || blocks[2].ParentIndex != 0 {
		t.Fatalf("expected scope2 task to be child of parent block 0, got %#v", blocks[2])
	}

	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected parent task plus two child-heading tasks, got %#v", plan.Tasks)
	}

	scope1 := plan.Tasks[1].Prompt
	for _, want := range []string{"# Review", "Review backend.", "### Scope1", "API and migrations.", "Fix tests {{n}}."} {
		if !strings.Contains(scope1, want) {
			t.Fatalf("expected scope1 prompt to contain %q, got:\n%s", want, scope1)
		}
	}
	if strings.Contains(scope1, "Scope2") || strings.Contains(scope1, "Docs.") {
		t.Fatalf("scope1 prompt leaked sibling section:\n%s", scope1)
	}

	scope2 := plan.Tasks[2].Prompt
	for _, want := range []string{"# Review", "Review backend.", "### Scope2", "Docs.", "Fix docs."} {
		if !strings.Contains(scope2, want) {
			t.Fatalf("expected scope2 prompt to contain %q, got:\n%s", want, scope2)
		}
	}
	if strings.Contains(scope2, "Scope1") || strings.Contains(scope2, "API and migrations.") || strings.Contains(scope2, "Fix tests") {
		t.Fatalf("scope2 prompt leaked sibling section:\n%s", scope2)
	}
}

func TestMarkdownChildHeadingTaskInheritsParentHeaderLet(t *testing.T) {
	content := `# Review

/task
/let area backend
Review {{area}}.

### Scope

/task
Fix {{area}} tests.
`
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected parent and child task, got %#v", plan.Tasks)
	}
	if got := plan.Tasks[1].Vars["area"]; got != "backend" {
		t.Fatalf("expected child task to inherit parent header /let, got %#v", plan.Tasks[1].Vars)
	}
}

func TestMarkdownChildSectionLetShadowsParentHeaderLet(t *testing.T) {
	content := `# Review

/task
/let area backend
Review {{area}}.

### Scope

/let area api

/task
Fix {{area}} tests.
`
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected parent and child task, got %#v", plan.Tasks)
	}
	if got := plan.Tasks[1].Vars["area"]; got != "api" {
		t.Fatalf("expected child section /let to shadow parent header /let, got %#v", plan.Tasks[1].Vars)
	}
}

func TestMarkdownChildHeadingTaskSeesParentLazyLetName(t *testing.T) {
	content := `# Review

/task
/let area /bash printf backend
Review {{area}}.

### Scope

/task
Fix {{area}} tests.
`
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected parent and child task, got %#v", plan.Tasks)
	}
	if got := plan.Tasks[1].Vars["area"]; got != "{{area}}" {
		t.Fatalf("expected child task to see parent lazy /let placeholder, got %#v", plan.Tasks[1].Vars)
	}
}

func TestV2DefinitionBlockEndsAtReturnAndIsNotRunnableTask(t *testing.T) {
	content := strings.Join([]string{
		"# Runbook",
		"",
		"/def city",
		"Find the current city.",
		"",
		"/return {{agent.last_message}}",
		"",
		"## Weather",
		"",
		"/let city /call city",
		"Weather for {{city}}.",
		"",
	}, "\n")

	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 || plan.Definitions[0].Name != "city" {
		t.Fatalf("unexpected definitions: %#v", plan.Definitions)
	}
	if len(plan.Definitions[0].Blocks) != 1 {
		t.Fatalf("expected one final return task block in definition, got %#v", plan.Definitions[0].Blocks)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected only outer weather task, got %#v", plan.Tasks)
	}
	if strings.Contains(plan.Tasks[0].Prompt, "Find the current city") {
		t.Fatalf("definition body leaked into outer task prompt: %q", plan.Tasks[0].Prompt)
	}
}

func TestV2DefinitionHeadingIsPromptTextAndWarns(t *testing.T) {
	content := strings.Join([]string{
		"/def review",
		"## Internal heading",
		"Review docs.",
		"",
		"/return done",
		"",
		"/call review",
		"",
	}, "\n")

	plan, diagnostics := CompileProgramDiagnostics("todo.md", content)
	if len(plan.Definitions) != 1 || len(plan.Definitions[0].Blocks) != 1 {
		t.Fatalf("expected one definition block, got plan=%#v diagnostics=%#v", plan, diagnostics)
	}
	if !strings.Contains(plan.Definitions[0].Blocks[0].Body, "## Internal heading") {
		t.Fatalf("expected heading preserved as definition prompt text, got %q", plan.Definitions[0].Blocks[0].Body)
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != "warning" || !strings.Contains(diagnostics[0].Message, "headings inside /def are prompt text") {
		t.Fatalf("expected definition heading warning, got %#v", diagnostics)
	}
}

func TestV2DefinitionBlockCanContainMultipleTaskBlocks(t *testing.T) {
	content := strings.Join([]string{
		"/def review area",
		"/pool reviewer 2",
		"",
		"/go reviewer",
		"Review {{area}} implementation.",
		"",
		"/go reviewer",
		"Review {{area}} docs.",
		"",
		"/wait reviewer",
		"",
		"/return Review {{area}} done.",
		"",
		"/call review checkout",
		"",
	}, "\n")

	plan, err := CompileProgram("todo.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 1 || len(plan.Definitions[0].Blocks) != 4 {
		t.Fatalf("unexpected definition blocks: %#v", plan.Definitions)
	}
	if len(plan.Tasks) != 1 || strings.TrimSpace(plan.Tasks[0].Prompt) != "" {
		t.Fatalf("expected only /call task outside definition, got %#v", plan.Tasks)
	}
}

func TestAppendDoneUsesATMQuoteBlockWithMessages(t *testing.T) {
	body := marker.AppendDone("Review docs.", marker.DoneInfo{
		Start:    mustMarkerTime(t, "2026-05-18 10:00"),
		End:      mustMarkerTime(t, "2026-05-18 10:01"),
		Runs:     1,
		ID:       "review-docs-abc123",
		Source:   "sha256:abc123",
		Rendered: "sha256:def456",
		Messages: []ir.OutputMessage{{
			Tool: "codex",
			Role: "assistant",
			Text: "done\nnext line",
		}},
	})
	if !strings.Contains(body, "> [!ATM]\n> status: done") {
		t.Fatalf("missing ATM quote block:\n%s", body)
	}
	if !strings.Contains(body, "<!-- atm:report v=2 id=review-docs-abc123 source=sha256:abc123 rendered=sha256:def456 report=.atm/reports/review-docs-abc123.md status=done -->") || !strings.Contains(body, "<!-- /atm:report -->") {
		t.Fatalf("missing ATM report envelope:\n%s", body)
	}
	if !strings.Contains(body, "> rendered: sha256:def456") {
		t.Fatalf("missing rendered prompt hash:\n%s", body)
	}
	if !strings.Contains(body, "> report: .atm/reports/review-docs-abc123.md") {
		t.Fatalf("missing report path:\n%s", body)
	}
	if !strings.Contains(body, "> messages:\n> - assistant (codex):\n>   done\n>   next line") {
		t.Fatalf("missing message block:\n%s", body)
	}
	if id, ok := marker.ATMReportID(body); !ok || id != "review-docs-abc123" {
		t.Fatalf("expected report id, got %q ok=%v:\n%s", id, ok, body)
	}
	if !marker.IsDone(body) {
		t.Fatalf("expected done body:\n%s", body)
	}
	if report := marker.VisibleATMReport(body); !strings.Contains(report, "> [!ATM]\n> status: done") || strings.Contains(report, "<!--") {
		t.Fatalf("expected visible report without envelope, got:\n%s", report)
	}
}

func TestStripRunningReadsATMQuoteBlockAndRemovesOnlyGeneratedTail(t *testing.T) {
	body := "Prompt\n> quoted by user\n> [!ATM]\n> status: running\n> started: 2026-05-18 10:00\n> step: 2\n> step-runs: 3x\n> total-runs: 4x\n> id: prompt-abc123\n> source: sha256:abc123"
	clean, running, err := marker.StripRunning(body)
	if err != nil {
		t.Fatal(err)
	}
	if !running.Active || running.StepIndex != 1 || running.StepRuns != 3 || running.TotalRuns != 4 {
		t.Fatalf("unexpected running info: %#v", running)
	}
	if running.ID != "prompt-abc123" || running.Source != "sha256:abc123" {
		t.Fatalf("expected running report identity, got %#v", running)
	}
	if strings.Contains(clean, "[!ATM]") {
		t.Fatalf("expected ATM block removed:\n%s", clean)
	}
	if !strings.Contains(clean, "> quoted by user") {
		t.Fatalf("expected user quote preserved:\n%s", clean)
	}
}

func TestStripRunningReadsATMReportEnvelope(t *testing.T) {
	body := marker.AppendRunning("Prompt\n> quoted by user", marker.RunningInfo{
		Start:     mustMarkerTime(t, "2026-05-18 10:00"),
		StepIndex: 1,
		StepRuns:  3,
		TotalRuns: 4,
		ID:        "prompt-abc123",
		Source:    "sha256:abc123",
		Rendered:  "sha256:def456",
	})
	clean, running, err := marker.StripRunning(body)
	if err != nil {
		t.Fatal(err)
	}
	if running.ID != "prompt-abc123" || running.Source != "sha256:abc123" || running.Rendered != "sha256:def456" || running.StepIndex != 1 {
		t.Fatalf("unexpected running info: %#v", running)
	}
	if strings.Contains(clean, "[!ATM]") || strings.Contains(clean, "atm:report") {
		t.Fatalf("expected generated envelope removed:\n%s", clean)
	}
	if !strings.Contains(clean, "> quoted by user") {
		t.Fatalf("expected user quote preserved:\n%s", clean)
	}
}

func TestAppendFailedUsesTerminalATMReport(t *testing.T) {
	body := marker.AppendFailed("Run tests.", marker.FailedInfo{
		Start:    mustMarkerTime(t, "2026-05-18 10:00"),
		End:      mustMarkerTime(t, "2026-05-18 10:01"),
		Runs:     1,
		Error:    "runner failed\nwith details",
		ID:       "run-tests-abc123",
		Source:   "sha256:abc123",
		Rendered: "sha256:def456",
	})
	if !strings.Contains(body, "<!-- atm:report v=2 id=run-tests-abc123 source=sha256:abc123 rendered=sha256:def456 report=.atm/reports/run-tests-abc123.md status=failed -->") {
		t.Fatalf("missing failed envelope:\n%s", body)
	}
	if !strings.Contains(body, "> status: failed") || !strings.Contains(body, "> error: runner failed with details") {
		t.Fatalf("missing failed fields:\n%s", body)
	}
	if !marker.IsDone(body) {
		t.Fatalf("expected failed block to be terminal:\n%s", body)
	}
	clean, ok := marker.RemoveDone(body)
	if !ok || strings.Contains(clean, "[!ATM]") {
		t.Fatalf("expected failed block removable as terminal state, ok=%v:\n%s", ok, clean)
	}
}

func TestCompileProgramRejectsDuplicateATMReportIDs(t *testing.T) {
	content := strings.Join([]string{
		"/task",
		"one",
		"<!-- atm:report v=2 id=dup source=sha256:a status=done -->",
		"> [!ATM]",
		"> status: done",
		"> id: dup",
		"<!-- /atm:report -->",
		"",
		"/task",
		"two",
		"<!-- atm:report v=2 id=dup source=sha256:b status=done -->",
		"> [!ATM]",
		"> status: done",
		"> id: dup",
		"<!-- /atm:report -->",
		"",
	}, "\n")
	_, err := CompileProgram("todo.txt", content)
	if err == nil || !strings.Contains(err.Error(), "duplicate ATM report id") {
		t.Fatalf("expected duplicate report id error, got %v", err)
	}
}

func mustMarkerTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
