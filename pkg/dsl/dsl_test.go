package dsl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompileProgramUsesExplicitRootForPathIterators(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "server.go"), []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := CompileProgramWithOptions("todo.txt", "/for path\nreview {{path}}\n", CompileOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || len(plan.Tasks[0].Ops) != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	values := strings.Join(plan.Tasks[0].Ops[0].For.Values, ",")
	if !strings.Contains(values, "README.md") || !strings.Contains(values, "api/server.go") {
		t.Fatalf("unexpected path values: %q", values)
	}
}

func TestPlanPreservesForGoOrder(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 2 /go\nparallel {{N}}\n\n/go /for 2\nloop {{N}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != "For(N in [1 2]) -> Go -> Execute" {
		t.Fatalf("unexpected /for /go flow: %s", got)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "Go -> For(N in [1 2]) -> Execute" {
		t.Fatalf("unexpected /go /for flow: %s", got)
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

func TestForUntilCELAndUnboundedCELParse(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for 5 until (exists(\"result.json\") && len(read(\"result.json\")) > 0)\nretry {{N}}\n\n/for until(json(\"gate.json\").passed)\nfinish {{N}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", plan.Tasks)
	}
	first := plan.Tasks[0].Ops[0].For
	if first.MaxRuns != 5 || first.Condition.Kind != ConditionCEL || first.Condition.Text != "exists(\"result.json\") && len(read(\"result.json\")) > 0" {
		t.Fatalf("unexpected bounded CEL loop: %#v", first)
	}
	second := plan.Tasks[1].Ops[0].For
	if second.MaxRuns != 0 || second.VarName != "N" || second.Condition.Kind != ConditionCEL || second.Condition.Text != "json(\"gate.json\").passed" {
		t.Fatalf("unexpected unbounded CEL loop: %#v", second)
	}
	if got := FormatTaskFlow(plan.Tasks[1]); got != "For(N until cel(\"json(\\\"gate.json\\\").passed\")) -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
}

func TestForUntilWithoutCountRequiresCEL(t *testing.T) {
	_, err := CompileProgram("todo.txt", "/for until tests pass\nretry\n")
	if err == nil || !strings.Contains(err.Error(), "requires a parenthesized CEL condition") {
		t.Fatalf("expected unbounded natural-language until error, got %v", err)
	}
}

func TestDynamicForSourceParsesAsCEL(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for area in(jsonOutput(\"release-plan.json\").areas)\n/go reviewer\nReview {{area.name}}.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || len(plan.Tasks[0].Ops) < 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	loop := plan.Tasks[0].Ops[0].For
	if loop.VarName != "area" || loop.Source.Kind != ConditionCEL || loop.Source.Text != `jsonOutput("release-plan.json").areas` {
		t.Fatalf("unexpected dynamic loop: %#v", loop)
	}
	if got := FormatTaskFlow(plan.Tasks[0]); got != `For(area in cel("jsonOutput(\"release-plan.json\").areas")) -> Go(reviewer) -> Execute` {
		t.Fatalf("unexpected flow: %s", got)
	}
}

func TestDynamicForSourceParsesCall(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/for plan in(/call plan_shards)\n/go reviewer\n{{plan}}\n")
	if err != nil {
		t.Fatal(err)
	}
	loop := plan.Tasks[0].Ops[0].For
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
	arrayTask, err := ParseTask(0, "/return\n```\nplans:[]string:计划\n```\nPlan.\n", nil, CompileOptions{Root: "."})
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

func TestIfAndElseCommandsAreBlockLevelNoOpsInTaskIR(t *testing.T) {
	task, err := ParseTask(0, "/if (exists(\"gate.json\"))\nContinue.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(task.Prompt) != "Continue." {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
	ifBlock, ok, err := ParseIfBlock("/if (exists(\"gate.json\"))\nContinue.\n")
	if err != nil || !ok || ifBlock.Condition.Text != `exists("gate.json")` || ifBlock.Condition.Kind != ConditionCEL || ifBlock.HeaderOnly {
		t.Fatalf("unexpected if parse ok=%v block=%#v err=%v", ok, ifBlock, err)
	}
	compact, ok, err := ParseIfBlock("/if(exists(\"gate.json\"))\nContinue.\n")
	if err != nil || !ok || compact.Condition.Text != `exists("gate.json")` || compact.Condition.Kind != ConditionCEL {
		t.Fatalf("unexpected compact if parse ok=%v block=%#v err=%v", ok, compact, err)
	}
	natural, ok, err := ParseIfBlock("/if release gate is open\nContinue.\n")
	if err != nil || !ok || natural.Condition.Text != "release gate is open" || natural.Condition.Kind != ConditionNatural {
		t.Fatalf("unexpected natural if parse ok=%v block=%#v err=%v", ok, natural, err)
	}
	elseTask, err := ParseTask(0, "/else\nFallback.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(elseTask.Prompt) != "Fallback." {
		t.Fatalf("unexpected else prompt: %q", elseTask.Prompt)
	}
	_, err = ParseTask(0, "/args --yolo\n/if (true)\nContinue.\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "must be the first command") {
		t.Fatalf("expected /if first-command error, got %v", err)
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

func TestPoolDeclarationDefaultsToUnlimitedBuffer(t *testing.T) {
	pools, ok, err := ParseGlobalPoolBlock("/pool tester 5\n")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || len(pools) != 1 || pools[0].Buffer != -1 {
		t.Fatalf("expected unlimited-buffer pool declaration, got ok=%v pools=%#v", ok, pools)
	}
}

func TestDBDeclarationsAndTaskConfigParse(t *testing.T) {
	content := "/db new decisions scope:global persist:project access:write\nUse for release decisions.\n\n/db use notes access:append\n/db access decisions read\nReview.\n\n/db ignore\nNo DB.\n"
	plan, err := CompileProgram("todo.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.DBs) != 1 {
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
	content := "## /def whereami\n\nReturn city.\n\n/return Paris\n\n## /weather\n\nWeather for\n/call whereami\n"
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

func TestDefinitionCycleDetectionIncludesInlineCalls(t *testing.T) {
	content := "## /def a\n\n/call b\n\n## /def b\n\n/call a\n"
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
	if len(task.Ops) < 2 || task.Ops[0].Kind != OpCall || task.Ops[0].Call.Assign != "city" {
		t.Fatalf("expected assigned call op, got %#v", task.Ops)
	}
	defTask, err := ParseTask(0, "Find city.\n/return\nCity: {{agent.last_message}}\n", nil, CompileOptions{Root: "."})
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
	for _, op := range task.Ops {
		if op.Kind == OpBash {
			bashOps++
		}
	}
	if bashOps != 2 {
		t.Fatalf("expected two bash ops, got %#v", task.Ops)
	}
}

func TestLoopOptionsDoNotAlsoAttachToExecuteOp(t *testing.T) {
	task, err := ParseTask(0, "/args --yolo /for 2\nreview\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Ops) != 2 || task.Ops[0].Kind != OpFor || task.Ops[1].Kind != OpExecute {
		t.Fatalf("unexpected ops: %#v", task.Ops)
	}
	if got := strings.Join(task.Ops[0].For.Options.Args, " "); got != "--yolo" {
		t.Fatalf("expected loop args, got %q", got)
	}
	if len(task.Ops[1].ExecuteOptions.Args) != 0 {
		t.Fatalf("did not expect duplicate execute args: %#v", task.Ops[1].ExecuteOptions.Args)
	}
}

func TestParseBlocksSupportsWhitespaceBlankLinesAndWholeLineComments(t *testing.T) {
	content := strings.Join([]string{
		"# disabled task",
		"   # disabled with leading spaces",
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
	if len(blocks) != 2 {
		t.Fatalf("expected two blocks, got %#v", blocks)
	}
	if strings.TrimSpace(blocks[0].Body) != "first # inline hash stays visible" {
		t.Fatalf("unexpected first block: %q", blocks[0].Body)
	}
	if strings.TrimSpace(blocks[1].Body) != "second" {
		t.Fatalf("unexpected second block: %q", blocks[1].Body)
	}
}

func TestCommentsDoNotApplyInsideHeredoc(t *testing.T) {
	task, err := ParseTask(0, "/bash <<'SH'\n# shell comment\nprintf ok\nSH\nRun after bash.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Ops) == 0 || task.Ops[0].Kind != OpBash {
		t.Fatalf("expected bash op: %#v", task.Ops)
	}
	if !strings.Contains(task.Ops[0].Bash.Script, "# shell comment") {
		t.Fatalf("expected heredoc comment preserved: %q", task.Ops[0].Bash.Script)
	}
}

func TestMixedContentAndCommentSyntaxIsPromptText(t *testing.T) {
	blocks := ParseBlocks("visible <!-- not a todo comment -->\n\n<!-- comment --> visible text\n\nprompt --- still prompt\n\n--- not ignored\n")
	if len(blocks) != 4 {
		t.Fatalf("expected four prompt blocks, got %#v", blocks)
	}
	if strings.TrimSpace(blocks[0].Body) != "visible <!-- not a todo comment -->" {
		t.Fatalf("unexpected first block: %q", blocks[0].Body)
	}
	if strings.TrimSpace(blocks[1].Body) != "<!-- comment --> visible text" {
		t.Fatalf("unexpected second block: %q", blocks[1].Body)
	}
	if strings.TrimSpace(blocks[2].Body) != "prompt --- still prompt" {
		t.Fatalf("unexpected third block: %q", blocks[2].Body)
	}
	if strings.TrimSpace(blocks[3].Body) != "--- not ignored" {
		t.Fatalf("unexpected fourth block: %q", blocks[3].Body)
	}
}

func TestUnusedLetBindingsAreAllowed(t *testing.T) {
	plan, err := CompileProgram("todo.txt", "/let bypass true\n\nRun without using the variable.\n")
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

func TestRenderTemplateKeepsLegacyVariablesCompatible(t *testing.T) {
	got, err := RenderTemplate("Review {{path}} pass {{N}}; keep {{future}}.", map[string]string{
		"path": "README.md",
		"N":    "2",
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
	got, err := RenderTemplate("{{if .N}}Pass {{.N}}: {{end}}{{.path}} {{index .Vars \"path\"}} {{var \"name-with-dash\"}} {{has \"path\"}}", map[string]string{
		"N":              "3",
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
	if _, err := RenderTemplate("{{if .N}}missing end", map[string]string{"N": "1"}); err == nil {
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

func TestOutputCommandCanAppearAfterPrompt(t *testing.T) {
	task, err := ParseTask(0, "Summarize first.\n/output result\n```\nreason:string:why\n```\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.FileName != "result" {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	if strings.TrimSpace(task.Prompt) != "Summarize first." {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
}

func TestOutputCommandCanAppearBetweenFlowCommandsAndPrompt(t *testing.T) {
	task, err := ParseTask(0, "/for 2 /go\n/output result-{{N}}\n```\nreason:string:why\n```\nSummarize {{N}}.\n", nil, CompileOptions{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Output == nil || task.Output.FileName != "result-{{N}}" {
		t.Fatalf("unexpected output spec: %#v", task.Output)
	}
	if got := FormatTaskFlow(task); got != "For(N in [1 2]) -> Go -> Execute" {
		t.Fatalf("unexpected flow: %s", got)
	}
	if strings.TrimSpace(task.Prompt) != "Summarize {{N}}." {
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
	if len(blocks) != 2 {
		t.Fatalf("expected two blocks, got %#v", blocks)
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

func TestOutputCommandRejectsDuplicateOutput(t *testing.T) {
	_, err := ParseTask(0, "/output one\n```\na:string:first\n```\n/output two\n```\nb:string:second\n```\nReport.\n", nil, CompileOptions{Root: "."})
	if err == nil || !strings.Contains(err.Error(), "can only appear once") {
		t.Fatalf("expected duplicate /output error, got %v", err)
	}
}

func TestMarkdownSlashHeadingsSelectRunnableSections(t *testing.T) {
	content := `# Release notes

ordinary intro

## //verify

Run tests.

Run vet.

## Notes

ignored note

## /review docs
/go
Review docs.

Use two paragraphs.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 3 {
		t.Fatalf("expected three runnable blocks, got %#v", blocks)
	}
	if !strings.Contains(blocks[0].Prefix, "# Release notes") || !strings.Contains(blocks[0].Prefix, "## //verify") {
		t.Fatalf("expected markdown prefix preserved: %q", blocks[0].Prefix)
	}
	if strings.TrimSpace(blocks[0].Body) != "Run tests." {
		t.Fatalf("unexpected first body: %q", blocks[0].Body)
	}
	if strings.TrimSpace(blocks[1].Body) != "Run vet." {
		t.Fatalf("unexpected second body: %q", blocks[1].Body)
	}
	if !strings.Contains(blocks[2].Prefix, "## Notes") || !strings.Contains(blocks[2].Prefix, "## /review docs") {
		t.Fatalf("expected ordinary section before next task preserved: %q", blocks[2].Prefix)
	}
	if strings.TrimSpace(blocks[2].Body) != "/go\nReview docs.\n\nUse two paragraphs." {
		t.Fatalf("unexpected third body: %q", blocks[2].Body)
	}
}

func TestMarkdownSlashSectionEndsAtSameOrHigherHeading(t *testing.T) {
	content := `# /verify
Intro.

## Details

Still part of the task.

# Plain heading

Ignored.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one single-task slash section, got %#v", blocks)
	}
	if !strings.Contains(blocks[0].Body, "Intro.\n\n## Details\n\nStill part of the task.") {
		t.Fatalf("expected markdown section body preserved as one task: %q", blocks[0].Body)
	}
	if !strings.Contains(blocks[0].Sep, "# Plain heading") {
		t.Fatalf("expected following ordinary section preserved in separator: %q", blocks[0].Sep)
	}
}

func TestMarkdownDoubleSlashSectionUsesLegacyBlockSplitting(t *testing.T) {
	content := `# //verify
Run tests.

Run vet.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected two tasks in double-slash section, got %#v", blocks)
	}
	if strings.TrimSpace(blocks[0].Body) != "Run tests." || strings.TrimSpace(blocks[1].Body) != "Run vet." {
		t.Fatalf("unexpected blocks: %#v", blocks)
	}
}

func TestMarkdownSingleSlashSkipsCommentsButPreservesNestedHeadings(t *testing.T) {
	content := `# /review
#ignored comment
Intro.

## Details

<!-- hidden
comment
-->

Keep details.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one task, got %#v", blocks)
	}
	if strings.Contains(blocks[0].Body, "ignored comment") || strings.Contains(blocks[0].Body, "hidden") {
		t.Fatalf("expected comments removed from task body: %q", blocks[0].Body)
	}
	if !strings.Contains(blocks[0].Body, "## Details") || !strings.Contains(blocks[0].Body, "Keep details.") {
		t.Fatalf("expected nested markdown heading preserved: %q", blocks[0].Body)
	}
}

func TestMarkdownEmptySlashSectionIsNotRunnable(t *testing.T) {
	content := `# /disabled
# comment only

# //verify
Run tests.
`
	blocks := ParseBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected one runnable task, got %#v", blocks)
	}
	if strings.TrimSpace(blocks[0].Body) != "Run tests." {
		t.Fatalf("unexpected task body: %q", blocks[0].Body)
	}
	if !strings.Contains(blocks[0].Prefix, "# /disabled") || !strings.Contains(blocks[0].Prefix, "# //verify") {
		t.Fatalf("expected skipped slash section preserved in prefix: %q", blocks[0].Prefix)
	}
}

func TestAppendDoneUsesATMQuoteBlockWithMessages(t *testing.T) {
	body := AppendDone("Review docs.", DoneInfo{
		Start: mustMarkerTime(t, "2026-05-18 10:00"),
		End:   mustMarkerTime(t, "2026-05-18 10:01"),
		Runs:  1,
		Messages: []OutputMessage{{
			Tool: "codex",
			Role: "assistant",
			Text: "done\nnext line",
		}},
	})
	if !strings.Contains(body, "> [!ATM]\n> status: done") {
		t.Fatalf("missing ATM quote block:\n%s", body)
	}
	if !strings.Contains(body, "> messages:\n> - assistant (codex):\n>   done\n>   next line") {
		t.Fatalf("missing message block:\n%s", body)
	}
	if !IsDone(body) {
		t.Fatalf("expected done body:\n%s", body)
	}
}

func TestStripRunningReadsATMQuoteBlockAndRemovesOnlyGeneratedTail(t *testing.T) {
	body := "Prompt\n> quoted by user\n> [!ATM]\n> status: running\n> started: 2026-05-18 10:00\n> step: 2\n> step-runs: 3x\n> total-runs: 4x"
	clean, running, err := StripRunning(body)
	if err != nil {
		t.Fatal(err)
	}
	if !running.Active || running.StepIndex != 1 || running.StepRuns != 3 || running.TotalRuns != 4 {
		t.Fatalf("unexpected running info: %#v", running)
	}
	if strings.Contains(clean, "[!ATM]") {
		t.Fatalf("expected ATM block removed:\n%s", clean)
	}
	if !strings.Contains(clean, "> quoted by user") {
		t.Fatalf("expected user quote preserved:\n%s", clean)
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
