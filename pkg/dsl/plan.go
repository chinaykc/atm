package dsl

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RunPlanCLI(args []string, stdout, stderr io.Writer) error {
	var file string
	var jsonOut bool
	var htmlOut string
	var openHTML bool
	if len(args) > 0 && args[0] == "dry-run" {
		args = args[1:]
	}

	flags := flag.NewFlagSet("atm plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&file, "file", "todo.txt", "todo file path")
	flags.BoolVar(&jsonOut, "json", false, "print the IR plan as JSON")
	flags.StringVar(&htmlOut, "html", "", "write an HTML flowchart to this file")
	flags.BoolVar(&openHTML, "open", false, "open the HTML flowchart in the default browser")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm plan prints a dry-run execution plan without running tools or bash.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage of atm plan:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if jsonOut && (htmlOut != "" || openHTML) {
		return fmt.Errorf("-json cannot be combined with -html or -open")
	}
	return RunPlanWithOptions(file, stdout, PlanOptions{JSON: jsonOut, HTML: htmlOut, Open: openHTML})
}

type PlanOptions struct {
	JSON bool
	HTML string
	Open bool
}

func RunPlan(filePath string, stdout io.Writer) error {
	return RunPlanWithOptions(filePath, stdout, PlanOptions{})
}

func RunPlanWithOptions(filePath string, stdout io.Writer, opts PlanOptions) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read todo file %q: %w", filePath, err)
	}
	plan, err := CompileProgram(filePath, string(content))
	if err != nil {
		return err
	}
	if opts.JSON {
		return writePlanJSON(stdout, plan)
	}
	if opts.HTML != "" || opts.Open {
		path := opts.HTML
		if path == "" {
			tmp, err := os.CreateTemp("", "atm-plan-*.html")
			if err != nil {
				return fmt.Errorf("create temporary plan HTML: %w", err)
			}
			path = tmp.Name()
			if err := tmp.Close(); err != nil {
				return fmt.Errorf("close temporary plan HTML: %w", err)
			}
		}
		if err := writePlanHTMLFile(path, plan, string(content)); err != nil {
			return err
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		fmt.Fprintf(stdout, "atm plan HTML: %s\n", abs)
		if opts.Open {
			if err := openBrowser(abs); err != nil {
				return err
			}
		}
		return nil
	}

	fmt.Fprintf(stdout, "atm plan dry-run: %s\n", filePath)
	fmt.Fprintln(stdout, "commands will not be executed")
	for _, item := range plan.Imports {
		if item.Namespace != "" {
			fmt.Fprintf(stdout, "\nimport block %d: %s from %s\n", item.BlockIndex+1, item.Namespace, item.Path)
		} else {
			fmt.Fprintf(stdout, "\nimport block %d: %s\n", item.BlockIndex+1, item.Path)
		}
	}
	for _, group := range groupGlobalBindings(plan.Globals) {
		fmt.Fprintf(stdout, "\nglobal block %d: %d variable(s)\n", group.blockIndex+1, group.count)
	}
	for _, group := range groupPoolDecls(plan.Pools) {
		fmt.Fprintf(stdout, "\npool block %d: %d pool(s): %s\n", group.blockIndex+1, group.count, poolDeclGroupSummary(group.pools))
	}
	for _, db := range plan.DBs {
		fmt.Fprintf(stdout, "\ndb block %d: %s scope=%s persist=%s access=%s\n", db.BlockIndex+1, db.Name, db.Scope, db.Persist, db.Access)
		if strings.TrimSpace(db.Usage) != "" {
			fmt.Fprintf(stdout, "  usage: %s\n", promptPreview(db.Usage))
		}
	}
	for _, skill := range plan.Skills {
		fmt.Fprintf(stdout, "\nskill block %d: %s from %s\n", skill.BlockIndex+1, skill.Name, skill.Path)
	}
	for _, mcp := range plan.MCPs {
		fmt.Fprintf(stdout, "\nmcp block %d: %s\n", mcp.BlockIndex+1, mcp.Name)
	}
	for _, control := range plan.Controls {
		fmt.Fprintf(stdout, "\n%s\n", formatControlBlock(control))
	}
	if len(plan.Definitions) > 0 {
		fmt.Fprintf(stdout, "\ndefinitions: %d\n", len(plan.Definitions))
	}
	for i, task := range plan.Tasks {
		fmt.Fprintf(stdout, "\ntask %d:\n", i+1)
		fmt.Fprintf(stdout, "  block: %d\n", task.BlockIndex+1)
		fmt.Fprintf(stdout, "  flow: %s\n", FormatTaskFlow(task))
		if strings.TrimSpace(task.Prompt) == "" {
			fmt.Fprintln(stdout, "  prompt: <empty>")
		} else {
			fmt.Fprintf(stdout, "  prompt: %s\n", promptPreview(task.Prompt))
		}
		if task.Output != nil {
			fmt.Fprintf(stdout, "  output: %s\n", formatPlanOutput(task.Output))
		}
		if detail := formatDBTaskConfig(task.DB); detail != "" {
			fmt.Fprintf(stdout, "  db: %s\n", detail)
		}
		if detail := formatSkillTaskConfig(task.Skill); detail != "" {
			fmt.Fprintf(stdout, "  skill: %s\n", detail)
		}
		if detail := formatMCPTaskConfig(task.MCP); detail != "" {
			fmt.Fprintf(stdout, "  mcp: %s\n", detail)
		}
		if task.Cursor.Active {
			fmt.Fprintf(stdout, "  cursor: op=%d step-runs=%d total-runs=%d started=%s\n",
				task.Cursor.OpIndex, task.Cursor.RunIndex, task.Cursor.TotalRuns, task.Cursor.Start.Format(time.RFC3339))
		}
	}
	if len(plan.Tasks) == 0 {
		fmt.Fprintln(stdout, "\nno runnable tasks")
	}
	return nil
}

type planJSON struct {
	Source      string              `json:"source"`
	Globals     []globalBindingJSON `json:"globals,omitempty"`
	Pools       []poolDeclJSON      `json:"pools,omitempty"`
	DBs         []dbDeclJSON        `json:"dbs,omitempty"`
	Skills      []skillDeclJSON     `json:"skills,omitempty"`
	MCPs        []mcpDeclJSON       `json:"mcps,omitempty"`
	Imports     []importDeclJSON    `json:"imports,omitempty"`
	Controls    []controlJSON       `json:"controls,omitempty"`
	Definitions []definitionJSON    `json:"definitions,omitempty"`
	Tasks       []taskPlanJSON      `json:"tasks"`
}

type globalBindingJSON struct {
	Block int    `json:"block"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	Bash  string `json:"bash,omitempty"`
}

type poolDeclJSON struct {
	Block  int    `json:"block"`
	Name   string `json:"name"`
	Max    int    `json:"max"`
	Buffer *int   `json:"buffer,omitempty"`
}

type dbDeclJSON struct {
	Block   int           `json:"block"`
	Name    string        `json:"name"`
	Scope   DBScope       `json:"scope"`
	Persist DBPersistence `json:"persist"`
	Access  DBAccess      `json:"access"`
	Usage   string        `json:"usage,omitempty"`
}

type skillDeclJSON struct {
	Block int    `json:"block"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

type mcpDeclJSON struct {
	Block  int    `json:"block"`
	Name   string `json:"name"`
	Config string `json:"config"`
}

type importDeclJSON struct {
	Block     int    `json:"block"`
	Path      string `json:"path"`
	Namespace string `json:"namespace,omitempty"`
}

type taskPlanJSON struct {
	Index  int              `json:"index"`
	Block  int              `json:"block"`
	Prompt string           `json:"prompt"`
	Ops    []planOpJSON     `json:"ops"`
	Output *OutputSpec      `json:"output,omitempty"`
	DB     *DBTaskConfig    `json:"db,omitempty"`
	Skill  *SkillTaskConfig `json:"skill,omitempty"`
	MCP    *MCPTaskConfig   `json:"mcp,omitempty"`
	Cursor *cursorJSON      `json:"cursor,omitempty"`
}

type controlJSON struct {
	Block         int    `json:"block"`
	Kind          string `json:"kind"`
	HeaderOnly    bool   `json:"headerOnly,omitempty"`
	Condition     string `json:"condition,omitempty"`
	ConditionKind string `json:"conditionKind,omitempty"`
}

type definitionJSON struct {
	Name   string   `json:"name"`
	Params []string `json:"params,omitempty"`
	Source string   `json:"source,omitempty"`
	Blocks int      `json:"blocks"`
}

type planOpJSON struct {
	Kind      OpKind   `json:"kind"`
	Name      string   `json:"name,omitempty"`
	Var       string   `json:"var,omitempty"`
	Values    []string `json:"values,omitempty"`
	MaxRuns   int      `json:"maxRuns,omitempty"`
	Until     string   `json:"until,omitempty"`
	UntilKind string   `json:"untilKind,omitempty"`
	Resume    bool     `json:"resume,omitempty"`
	Args      []string `json:"args,omitempty"`
	Workdir   string   `json:"workdir,omitempty"`
	MustExist bool     `json:"mustExistWorkdir,omitempty"`
	Script    string   `json:"script,omitempty"`
	PromptRef string   `json:"promptRef,omitempty"`
	Pool      string   `json:"pool,omitempty"`
}

type cursorJSON struct {
	OpIndex   int    `json:"opIndex"`
	RunIndex  int    `json:"runIndex"`
	TotalRuns int    `json:"totalRuns"`
	Started   string `json:"started,omitempty"`
}

func writePlanJSON(stdout io.Writer, plan Plan) error {
	out := planJSON{Source: plan.SourcePath}
	for _, binding := range plan.Globals {
		out.Globals = append(out.Globals, globalBindingJSON{
			Block: binding.BlockIndex + 1,
			Name:  binding.Name,
			Value: binding.Value,
			Bash:  binding.BashScript,
		})
	}
	for _, pool := range plan.Pools {
		item := poolDeclJSON{Block: pool.BlockIndex + 1, Name: pool.Name, Max: pool.Max}
		if pool.Buffer >= 0 {
			buffer := pool.Buffer
			item.Buffer = &buffer
		}
		out.Pools = append(out.Pools, item)
	}
	for _, db := range plan.DBs {
		out.DBs = append(out.DBs, dbDeclJSON{
			Block:   db.BlockIndex + 1,
			Name:    db.Name,
			Scope:   db.Scope,
			Persist: db.Persist,
			Access:  db.Access,
			Usage:   db.Usage,
		})
	}
	for _, skill := range plan.Skills {
		out.Skills = append(out.Skills, skillDeclJSON{Block: skill.BlockIndex + 1, Name: skill.Name, Path: skill.Path})
	}
	for _, mcp := range plan.MCPs {
		out.MCPs = append(out.MCPs, mcpDeclJSON{Block: mcp.BlockIndex + 1, Name: mcp.Name, Config: mcp.Config})
	}
	for _, item := range plan.Imports {
		out.Imports = append(out.Imports, importDeclJSON{Block: item.BlockIndex + 1, Path: item.Path, Namespace: item.Namespace})
	}
	for _, control := range plan.Controls {
		item := controlJSON{Block: control.BlockIndex + 1, Kind: control.Kind, HeaderOnly: control.HeaderOnly}
		if control.Condition.Kind != ConditionNone {
			item.Condition = control.Condition.Text
			item.ConditionKind = string(control.Condition.Kind)
		}
		out.Controls = append(out.Controls, item)
	}
	for _, def := range plan.Definitions {
		out.Definitions = append(out.Definitions, definitionJSON{
			Name:   def.Name,
			Params: append([]string{}, def.Params...),
			Source: def.SourcePath,
			Blocks: len(def.Blocks),
		})
	}
	for i, task := range plan.Tasks {
		item := taskPlanJSON{Index: i + 1, Block: task.BlockIndex + 1, Prompt: task.Prompt, Output: task.Output}
		if !task.DB.IsZero() {
			db := cloneDBTaskConfig(task.DB)
			item.DB = &db
		}
		if !task.Skill.IsZero() {
			skill := cloneSkillTaskConfig(task.Skill)
			item.Skill = &skill
		}
		if !task.MCP.IsZero() {
			mcp := cloneMCPTaskConfig(task.MCP)
			item.MCP = &mcp
		}
		for _, op := range task.Ops {
			item.Ops = append(item.Ops, planOpToJSON(op))
		}
		if task.Cursor.Active {
			item.Cursor = &cursorJSON{
				OpIndex:   task.Cursor.OpIndex,
				RunIndex:  task.Cursor.RunIndex,
				TotalRuns: task.Cursor.TotalRuns,
				Started:   task.Cursor.Start.Format(time.RFC3339),
			}
		}
		out.Tasks = append(out.Tasks, item)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func planOpToJSON(op Op) planOpJSON {
	out := planOpJSON{Kind: op.Kind}
	switch op.Kind {
	case OpCd:
		out.Workdir = op.Cd.Path
		out.MustExist = op.Cd.MustExist
	case OpBash:
		out.Name = op.Bash.Name
		out.Script = op.Bash.Script
	case OpFor:
		out.Var = op.For.VarName
		out.Values = append([]string{}, op.For.Values...)
		out.MaxRuns = op.For.MaxRuns
		out.Until = op.For.Condition.Text
		if op.For.Condition.Kind != ConditionNone {
			out.UntilKind = string(op.For.Condition.Kind)
		}
		out.Resume = op.For.Options.Resume
		out.Args = append([]string{}, op.For.Options.Args...)
	case OpExecute:
		out.PromptRef = "prompt"
		out.Resume = op.ExecuteOptions.Resume
		out.Args = append([]string{}, op.ExecuteOptions.Args...)
	case OpGo, OpWait:
		out.Pool = op.Pool
	case OpCall:
		out.Name = op.Call.Name
		out.Args = append([]string{}, op.Call.Args...)
	case OpReturn:
		out.Name = string(op.Return.Kind)
	}
	return out
}

type globalBindingGroup struct {
	blockIndex int
	count      int
}

func groupGlobalBindings(bindings []GlobalBinding) []globalBindingGroup {
	var groups []globalBindingGroup
	for _, binding := range bindings {
		if len(groups) == 0 || groups[len(groups)-1].blockIndex != binding.BlockIndex {
			groups = append(groups, globalBindingGroup{blockIndex: binding.BlockIndex})
		}
		groups[len(groups)-1].count++
	}
	return groups
}

type poolDeclGroup struct {
	blockIndex int
	count      int
	pools      []PoolDecl
}

func groupPoolDecls(pools []PoolDecl) []poolDeclGroup {
	var groups []poolDeclGroup
	for _, pool := range pools {
		if len(groups) == 0 || groups[len(groups)-1].blockIndex != pool.BlockIndex {
			groups = append(groups, poolDeclGroup{blockIndex: pool.BlockIndex})
		}
		groups[len(groups)-1].count++
		groups[len(groups)-1].pools = append(groups[len(groups)-1].pools, pool)
	}
	return groups
}

func FormatTaskFlow(task Task) string {
	parts := make([]string, 0, len(task.Ops))
	for _, op := range task.Ops {
		parts = append(parts, formatPlanOp(op))
	}
	return strings.Join(parts, " -> ")
}

func formatPlanOp(op Op) string {
	switch op.Kind {
	case OpCd:
		if op.Cd.MustExist {
			return fmt.Sprintf("Cd(%s, must-exist)", op.Cd.Path)
		}
		return fmt.Sprintf("Cd(%s)", op.Cd.Path)
	case OpBash:
		if op.Bash.Name != "" {
			return fmt.Sprintf("Bash(%s)", op.Bash.Name)
		}
		return "Bash"
	case OpFor:
		return formatForIR(op.For)
	case OpGo:
		if op.Pool != "" {
			return fmt.Sprintf("Go(%s)", op.Pool)
		}
		return "Go"
	case OpWait:
		if op.Pool != "" {
			return fmt.Sprintf("Wait(%s)", op.Pool)
		}
		return "Wait"
	case OpCall:
		if op.Call.Assign != "" {
			return fmt.Sprintf("Call(%s -> %s)", op.Call.Name, op.Call.Assign)
		}
		return fmt.Sprintf("Call(%s)", op.Call.Name)
	case OpReturn:
		return "Return"
	case OpExecute:
		return appendRunOptionSummary("Execute", op.ExecuteOptions)
	default:
		return string(op.Kind)
	}
}

func formatForIR(step For) string {
	name := step.VarName
	if name == "" {
		name = "run"
	}
	if step.MaxRuns == 0 && step.Condition.Kind == ConditionCEL {
		return appendRunOptionSummary(fmt.Sprintf("For(%s until cel(%q))", name, step.Condition.Text), step.Options)
	}
	if step.Source.Kind == ConditionCEL {
		detail := fmt.Sprintf("For(%s in cel(%q))", name, step.Source.Text)
		if step.Condition.Text != "" {
			if step.Condition.Kind == ConditionCEL {
				detail += fmt.Sprintf(" until cel(%q)", step.Condition.Text)
			} else {
				detail += fmt.Sprintf(" until %q", step.Condition.Text)
			}
		}
		return appendRunOptionSummary(detail, step.Options)
	}
	if step.Source.Kind == ConditionCall {
		return appendRunOptionSummary(fmt.Sprintf("For(%s in call(%q))", name, step.Source.Text), step.Options)
	}
	detail := fmt.Sprintf("For(%s x %d)", name, step.MaxRuns)
	if len(step.Values) > 0 && len(step.Values) <= 4 {
		detail = fmt.Sprintf("For(%s in [%s])", name, strings.Join(step.Values, " "))
	}
	if step.Condition.Text != "" {
		if step.Condition.Kind == ConditionCEL {
			detail += fmt.Sprintf(" until cel(%q)", step.Condition.Text)
		} else {
			detail += fmt.Sprintf(" until %q", step.Condition.Text)
		}
	}
	return appendRunOptionSummary(detail, step.Options)
}

func promptPreview(prompt string) string {
	line := strings.TrimSpace(prompt)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	if len(line) > 80 {
		line = line[:77] + "..."
	}
	return line
}

func poolDeclGroupSummary(pools []PoolDecl) string {
	parts := make([]string, 0, len(pools))
	for _, pool := range pools {
		detail := fmt.Sprintf("%s(max=%d", pool.Name, pool.Max)
		if pool.Buffer >= 0 {
			detail += fmt.Sprintf(", buffer=%d", pool.Buffer)
		}
		parts = append(parts, detail+")")
	}
	return strings.Join(parts, ", ")
}

func formatPlanOutput(output *OutputSpec) string {
	name := output.FileName
	if name == "" {
		name = "<auto>"
	}
	if output.IsStructured() {
		format := output.SchemaFormat
		if format == "" {
			format = "json"
		}
		return fmt.Sprintf("%s (%s schema)", name, format)
	}
	return fmt.Sprintf("%s (text)", name)
}

func formatControlBlock(control ControlBlock) string {
	if control.Kind == "else" {
		return fmt.Sprintf("else block %d%s", control.BlockIndex+1, headerOnlySuffix(control.HeaderOnly))
	}
	return fmt.Sprintf("if block %d: %s%s", control.BlockIndex+1, formatCondition(control.Condition), headerOnlySuffix(control.HeaderOnly))
}

func headerOnlySuffix(headerOnly bool) string {
	if headerOnly {
		return " [header]"
	}
	return ""
}

func formatCondition(condition Condition) string {
	switch condition.Kind {
	case ConditionCEL:
		return fmt.Sprintf("cel(%q)", condition.Text)
	case ConditionCall:
		return fmt.Sprintf("call(%q)", condition.Text)
	case ConditionNatural:
		return fmt.Sprintf("%q", condition.Text)
	default:
		return "<none>"
	}
}

func appendRunOptionSummary(base string, options RunOptions) string {
	var parts []string
	if options.Resume {
		parts = append(parts, "resume")
	}
	if len(options.Args) > 0 {
		parts = append(parts, "args="+strings.Join(options.Args, " "))
	}
	if len(parts) == 0 {
		return base
	}
	return base + " [" + strings.Join(parts, ", ") + "]"
}

func formatDBTaskConfig(config DBTaskConfig) string {
	var parts []string
	for _, use := range config.Use {
		detail := "use " + strings.Join(use.Names, " ")
		if use.Access != "" {
			detail += " access=" + string(use.Access)
		}
		parts = append(parts, detail)
	}
	for _, rule := range config.Access {
		parts = append(parts, "access "+strings.Join(rule.Names, " ")+" "+string(rule.Access))
	}
	if config.IgnoreAll {
		parts = append(parts, "ignore all")
	} else if len(config.Ignore) > 0 {
		parts = append(parts, "ignore "+strings.Join(config.Ignore, " "))
	}
	return strings.Join(parts, "; ")
}

func formatSkillTaskConfig(config SkillTaskConfig) string {
	var parts []string
	if len(config.Use) > 0 {
		parts = append(parts, "use "+strings.Join(config.Use, " "))
	}
	if config.IgnoreAll {
		parts = append(parts, "ignore all")
	} else if len(config.Ignore) > 0 {
		parts = append(parts, "ignore "+strings.Join(config.Ignore, " "))
	}
	return strings.Join(parts, "; ")
}

func formatMCPTaskConfig(config MCPTaskConfig) string {
	var parts []string
	if len(config.Use) > 0 {
		parts = append(parts, "use "+strings.Join(config.Use, " "))
	}
	if len(config.DefUse) > 0 {
		parts = append(parts, "def use "+strings.Join(config.DefUse, " "))
	}
	if config.IgnoreAll {
		parts = append(parts, "ignore all")
	} else if len(config.Ignore) > 0 {
		parts = append(parts, "ignore "+strings.Join(config.Ignore, " "))
	}
	return strings.Join(parts, "; ")
}
