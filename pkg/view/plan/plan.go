package plan

import (
	"cmp"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/document"
	langformat "github.com/chinaykc/atm/pkg/lang/format"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	JSON    bool
	HTML    string
	Open    bool
	Preview bool
}

type scopeRef struct {
	SourcePath string
	Scope      []string
	Line       int
}

type definitionScopeRef struct {
	SourcePath string
	Scope      []string
	Line       int
}

func RunFile(filePath string, stdout io.Writer, opts Options) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read ATM file %q: %w", filePath, err)
	}
	plan, err := compiler.CompileProgram(filePath, string(content))
	if err != nil {
		return err
	}
	var preview []ProviderPreview
	if opts.Preview {
		preview = buildProviderPreview(filePath, plan)
	}
	document := buildPlanDocument(string(content))
	async := buildPlanAsyncSummary(plan)
	conditions := buildPlanConditionSummary(plan)
	loops := buildPlanLoopSummary(plan)
	if opts.JSON {
		return writePlanJSON(stdout, plan, preview, document, async, conditions, loops)
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
		fmt.Fprintf(stdout, "atm check plan HTML: %s\n", abs)
		if opts.Open {
			if err := openBrowser(abs); err != nil {
				return err
			}
		}
		return nil
	}

	fmt.Fprintf(stdout, "atm check plan dry-run: %s\n", filePath)
	if opts.Preview {
		fmt.Fprintln(stdout, "preview mode: lazy providers may be executed; no agent, report, or state is written")
	} else {
		fmt.Fprintln(stdout, "commands will not be executed")
	}
	writePlanDocumentText(stdout, document)
	for _, diagnostic := range plan.Diagnostics {
		fmt.Fprintf(stdout, "%s: %s\n", diagnostic.Severity, diagnostic.Message)
	}
	for _, item := range plan.Imports {
		if item.Namespace != "" {
			fmt.Fprintf(stdout, "\nimport block %d: %s from %s\n", item.BlockIndex+1, item.Namespace, item.Path)
		} else {
			fmt.Fprintf(stdout, "\nimport block %d: %s\n", item.BlockIndex+1, item.Path)
		}
	}
	for _, group := range groupGlobalBindings(plan.Globals) {
		fmt.Fprintf(stdout, "\nvariable block %d: %d variable(s)\n", group.blockIndex+1, group.count)
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
	writePlanLoopsText(stdout, loops)
	writePlanConditionsText(stdout, conditions)
	writePlanAsyncText(stdout, async)
	if opts.Preview {
		writeProviderPreviewText(stdout, preview)
	}
	if len(plan.Definitions) > 0 {
		fmt.Fprintf(stdout, "\ndefinitions: %d\n", len(plan.Definitions))
	}
	for i, task := range plan.Tasks {
		childBlocks := childTaskBlockNumbers(plan.Tasks, task.BlockIndex)
		fmt.Fprintf(stdout, "\ntask %d:\n", i+1)
		fmt.Fprintf(stdout, "  block: %d\n", task.BlockIndex+1)
		if task.Line > 0 {
			fmt.Fprintf(stdout, "  line: %d\n", task.Line)
		}
		if len(task.Scope) > 0 {
			fmt.Fprintf(stdout, "  scope: %s\n", formatPlanScope(task.Scope))
		}
		if context := buildTaskContextSummary(task.Context); !context.IsZero() {
			fmt.Fprintf(stdout, "  context: %d line(s), %d char(s): %s\n", context.Lines, context.Chars, context.Preview)
		}
		if task.HasParent {
			fmt.Fprintf(stdout, "  parent-block: %d\n", task.ParentIndex+1)
		}
		if len(childBlocks) > 0 {
			fmt.Fprintf(stdout, "  child-blocks: %s\n", formatBlockNumberList(childBlocks))
			fmt.Fprintln(stdout, "  execution: after child blocks complete")
		}
		if decision := buildTaskDecisionSummary(task, childBlocks); !decision.IsZero() {
			fmt.Fprintf(stdout, "  decision: %s - %s", decision.Action, decision.Reason)
			if len(decision.Dependencies) > 0 {
				fmt.Fprintf(stdout, "; dependencies: %s", strings.Join(decision.Dependencies, ", "))
			}
			if len(decision.Skips) > 0 {
				fmt.Fprintf(stdout, "; skips: %s", strings.Join(decision.Skips, ", "))
			}
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "  flow: %s\n", langformat.TaskFlow(task))
		if strings.TrimSpace(task.Prompt) == "" {
			fmt.Fprintln(stdout, "  prompt: <empty>")
		} else {
			fmt.Fprintf(stdout, "  prompt: %s\n", promptPreview(task.Prompt))
		}
		if task.Output != nil {
			fmt.Fprintf(stdout, "  output: %s\n", formatPlanOutput(task.Output))
		}
		if variables := buildTaskVariableRefs(plan, task); len(variables) > 0 {
			fmt.Fprintf(stdout, "  variables: %s\n", formatTaskVariableRefs(variables))
		}
		if runtime := buildTaskRuntimeSummary(task); !runtime.IsZero() {
			fmt.Fprintf(stdout, "  runtime: %s\n", formatTaskRuntimeSummary(runtime))
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
		if resources := buildTaskResourceView(plan, task); !resources.IsZero() {
			fmt.Fprintf(stdout, "  resources: %s\n", formatTaskResourceView(resources))
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

type Model struct {
	Source      string                `json:"source"`
	Document    *Document             `json:"document,omitempty"`
	Diagnostics []compiler.Diagnostic `json:"diagnostics,omitempty"`
	Preview     []ProviderPreview     `json:"preview,omitempty"`
	Globals     []Global              `json:"globals,omitempty"`
	Pools       []Pool                `json:"pools,omitempty"`
	DBs         []DB                  `json:"dbs,omitempty"`
	Skills      []Skill               `json:"skills,omitempty"`
	MCPs        []MCP                 `json:"mcps,omitempty"`
	Imports     []Import              `json:"imports,omitempty"`
	Controls    []Control             `json:"controls,omitempty"`
	Loops       []Loop                `json:"loops,omitempty"`
	Conditions  []Condition           `json:"conditions,omitempty"`
	Async       *Async                `json:"async,omitempty"`
	Definitions []Definition          `json:"definitions,omitempty"`
	Tasks       []Task                `json:"tasks"`
}

type Document struct {
	Title    string    `json:"title,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

func (d Document) IsZero() bool {
	return d.Title == "" && len(d.Sections) == 0
}

type Section struct {
	Line     int       `json:"line"`
	Level    int       `json:"level"`
	Title    string    `json:"title"`
	Path     []string  `json:"path,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

type Condition struct {
	Task          int    `json:"task"`
	Block         int    `json:"block"`
	Condition     string `json:"condition,omitempty"`
	ConditionKind string `json:"conditionKind,omitempty"`
	Then          string `json:"then"`
	Else          string `json:"else"`
}

type Loop struct {
	Task         int      `json:"task"`
	Block        int      `json:"block"`
	Var          string   `json:"var,omitempty"`
	Summary      string   `json:"summary"`
	Mode         string   `json:"mode"`
	Values       []string `json:"values,omitempty"`
	Count        int      `json:"count,omitempty"`
	Source       string   `json:"source,omitempty"`
	SourceKind   string   `json:"sourceKind,omitempty"`
	Until        string   `json:"until,omitempty"`
	UntilKind    string   `json:"untilKind,omitempty"`
	Resume       bool     `json:"resume,omitempty"`
	ResumeTarget string   `json:"resumeTarget,omitempty"`
	Fork         bool     `json:"fork,omitempty"`
	ForkTarget   string   `json:"forkTarget,omitempty"`
	Args         []string `json:"args,omitempty"`
}

type Async struct {
	Background []AsyncBackground `json:"background,omitempty"`
	Joins      []AsyncJoin       `json:"joins,omitempty"`
	Unjoined   []AsyncBackground `json:"unjoined,omitempty"`
}

func (a Async) IsZero() bool {
	return len(a.Background) == 0 && len(a.Joins) == 0 && len(a.Unjoined) == 0
}

type AsyncBackground struct {
	Task   int    `json:"task"`
	Block  int    `json:"block"`
	Pool   string `json:"pool,omitempty"`
	Fanout string `json:"fanout,omitempty"`
}

type AsyncJoin struct {
	FromTask  int    `json:"fromTask"`
	FromBlock int    `json:"fromBlock"`
	WaitTask  int    `json:"waitTask"`
	WaitBlock int    `json:"waitBlock"`
	Pool      string `json:"pool,omitempty"`
	Fanout    string `json:"fanout,omitempty"`
}

type ProviderPreview struct {
	Block    int    `json:"block"`
	Scope    string `json:"scope"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Executed bool   `json:"executed"`
	Value    string `json:"value,omitempty"`
	Error    string `json:"error,omitempty"`
	Note     string `json:"note,omitempty"`
}

type Global struct {
	Block int    `json:"block"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	Bash  string `json:"bash,omitempty"`
}

type Pool struct {
	Block  int    `json:"block"`
	Name   string `json:"name"`
	Max    int    `json:"max"`
	Buffer *int   `json:"buffer,omitempty"`
}

type DB struct {
	Block   int                    `json:"block"`
	Name    string                 `json:"name"`
	Scope   compiler.DBScope       `json:"scope"`
	Persist compiler.DBPersistence `json:"persist"`
	Access  compiler.DBAccess      `json:"access"`
	Usage   string                 `json:"usage,omitempty"`
}

type Skill struct {
	Block int    `json:"block"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

type MCP struct {
	Block  int    `json:"block"`
	Name   string `json:"name"`
	Config string `json:"config"`
}

type Import struct {
	Block     int    `json:"block"`
	Path      string `json:"path"`
	Namespace string `json:"namespace,omitempty"`
}

type Task struct {
	Index       int                       `json:"index"`
	Block       int                       `json:"block"`
	Line        int                       `json:"line,omitempty"`
	Scope       []string                  `json:"scope,omitempty"`
	Context     *Context                  `json:"context,omitempty"`
	Decision    *Decision                 `json:"decision,omitempty"`
	ParentBlock int                       `json:"parentBlock,omitempty"`
	ChildBlocks []int                     `json:"childBlocks,omitempty"`
	Prompt      string                    `json:"prompt"`
	Flow        *FlowNode                 `json:"flow,omitempty"`
	Output      *compiler.OutputSpec      `json:"output,omitempty"`
	Variables   []Variable                `json:"variables,omitempty"`
	Runtime     *Runtime                  `json:"runtime,omitempty"`
	DB          *compiler.DBTaskConfig    `json:"db,omitempty"`
	Skill       *compiler.SkillTaskConfig `json:"skill,omitempty"`
	MCP         *compiler.MCPTaskConfig   `json:"mcp,omitempty"`
	Resources   *Resources                `json:"resources,omitempty"`
	Cursor      *Cursor                   `json:"cursor,omitempty"`
}

type Decision struct {
	Action       string   `json:"action"`
	Reason       string   `json:"reason"`
	Dependencies []string `json:"dependencies,omitempty"`
	Skips        []string `json:"skips,omitempty"`
}

func (d Decision) IsZero() bool {
	return d.Action == "" && d.Reason == "" && len(d.Dependencies) == 0 && len(d.Skips) == 0
}

type Resources struct {
	DBs     []DBResource         `json:"dbs,omitempty"`
	Skills  []Resource           `json:"skills,omitempty"`
	MCPs    []Resource           `json:"mcps,omitempty"`
	DefMCPs []DefinitionResource `json:"defMCPs,omitempty"`
}

func (v Resources) IsZero() bool {
	return len(v.DBs) == 0 && len(v.Skills) == 0 && len(v.MCPs) == 0 && len(v.DefMCPs) == 0
}

type DBResource struct {
	Name    string                 `json:"name"`
	Block   int                    `json:"block,omitempty"`
	Scope   compiler.DBScope       `json:"scope"`
	Persist compiler.DBPersistence `json:"persist"`
	Access  compiler.DBAccess      `json:"access"`
}

type Resource struct {
	Name  string `json:"name"`
	Block int    `json:"block,omitempty"`
	Path  string `json:"path,omitempty"`
}

type DefinitionResource struct {
	Name   string   `json:"name"`
	Block  int      `json:"block,omitempty"`
	Params []string `json:"params,omitempty"`
}

type Variable struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Block  int    `json:"block,omitempty"`
	Value  string `json:"value,omitempty"`
}

type Runtime struct {
	Resume       bool       `json:"resume,omitempty"`
	ResumeTarget string     `json:"resumeTarget,omitempty"`
	Fork         bool       `json:"fork,omitempty"`
	ForkTarget   string     `json:"forkTarget,omitempty"`
	Args         []string   `json:"args,omitempty"`
	Workdirs     []Workdir  `json:"workdirs,omitempty"`
	Bash         []Bash     `json:"bash,omitempty"`
	LazyProvider []Provider `json:"lazyProviders,omitempty"`
}

func (r Runtime) IsZero() bool {
	return !r.Resume && r.ResumeTarget == "" && !r.Fork && r.ForkTarget == "" && len(r.Args) == 0 && len(r.Workdirs) == 0 && len(r.Bash) == 0 && len(r.LazyProvider) == 0
}

type Context struct {
	Lines   int    `json:"lines"`
	Chars   int    `json:"chars"`
	Preview string `json:"preview,omitempty"`
}

func (c Context) IsZero() bool {
	return c.Lines == 0 && c.Chars == 0 && c.Preview == ""
}

type Workdir struct {
	Path      string `json:"path"`
	MustExist bool   `json:"mustExist,omitempty"`
}

type Bash struct {
	Script string `json:"script"`
}

type Provider struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Call string `json:"call,omitempty"`
}

type Control struct {
	Block         int    `json:"block"`
	Kind          string `json:"kind"`
	HeaderOnly    bool   `json:"headerOnly,omitempty"`
	Condition     string `json:"condition,omitempty"`
	ConditionKind string `json:"conditionKind,omitempty"`
}

type Definition struct {
	Name   string   `json:"name"`
	Params []string `json:"params,omitempty"`
	Source string   `json:"source,omitempty"`
	Blocks int      `json:"blocks"`
}

type Operation struct {
	Kind          compiler.FlatOpKind `json:"kind"`
	Name          string              `json:"name,omitempty"`
	Var           string              `json:"var,omitempty"`
	Values        []string            `json:"values,omitempty"`
	MaxRuns       int                 `json:"maxRuns,omitempty"`
	Until         string              `json:"until,omitempty"`
	UntilKind     string              `json:"untilKind,omitempty"`
	Condition     string              `json:"condition,omitempty"`
	ConditionKind string              `json:"conditionKind,omitempty"`
	Resume        bool                `json:"resume,omitempty"`
	ResumeTarget  string              `json:"resumeTarget,omitempty"`
	Fork          bool                `json:"fork,omitempty"`
	ForkTarget    string              `json:"forkTarget,omitempty"`
	Args          []string            `json:"args,omitempty"`
	Workdir       string              `json:"workdir,omitempty"`
	MustExist     bool                `json:"mustExistWorkdir,omitempty"`
	Script        string              `json:"script,omitempty"`
	PromptRef     string              `json:"promptRef,omitempty"`
	Pool          string              `json:"pool,omitempty"`
}

type FlowNode struct {
	Kind         compiler.FlowKind `json:"kind"`
	Op           *Operation        `json:"op,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	Children     []FlowNode        `json:"children,omitempty"`
	ElseChildren []FlowNode        `json:"elseChildren,omitempty"`
}

type Cursor struct {
	OpIndex   int    `json:"opIndex"`
	RunIndex  int    `json:"runIndex"`
	TotalRuns int    `json:"totalRuns"`
	Started   string `json:"started,omitempty"`
}

func writePlanJSON(stdout io.Writer, plan compiler.Plan, preview []ProviderPreview, document Document, async Async, conditions []Condition, loops []Loop) error {
	out := Model{Source: plan.SourcePath, Diagnostics: plan.Diagnostics, Preview: preview, Loops: loops, Conditions: conditions}
	if !document.IsZero() {
		out.Document = &document
	}
	if !async.IsZero() {
		out.Async = &async
	}
	for _, binding := range plan.Globals {
		out.Globals = append(out.Globals, Global{
			Block: binding.BlockIndex + 1,
			Name:  binding.Name,
			Value: binding.Value,
			Bash:  binding.BashScript,
		})
	}
	for _, pool := range plan.Pools {
		item := Pool{Block: pool.BlockIndex + 1, Name: pool.Name, Max: pool.Max}
		if pool.Buffer >= 0 {
			buffer := pool.Buffer
			item.Buffer = &buffer
		}
		out.Pools = append(out.Pools, item)
	}
	for _, db := range plan.DBs {
		out.DBs = append(out.DBs, DB{
			Block:   db.BlockIndex + 1,
			Name:    db.Name,
			Scope:   db.Scope,
			Persist: db.Persist,
			Access:  db.Access,
			Usage:   db.Usage,
		})
	}
	for _, skill := range plan.Skills {
		out.Skills = append(out.Skills, Skill{Block: skill.BlockIndex + 1, Name: skill.Name, Path: skill.Path})
	}
	for _, mcp := range plan.MCPs {
		out.MCPs = append(out.MCPs, MCP{Block: mcp.BlockIndex + 1, Name: mcp.Name, Config: mcp.Config})
	}
	for _, item := range plan.Imports {
		out.Imports = append(out.Imports, Import{Block: item.BlockIndex + 1, Path: item.Path, Namespace: item.Namespace})
	}
	for _, control := range plan.Controls {
		item := Control{Block: control.BlockIndex + 1, Kind: control.Kind, HeaderOnly: control.HeaderOnly}
		if control.Condition.Kind != compiler.ConditionNone {
			item.Condition = control.Condition.Text
			item.ConditionKind = string(control.Condition.Kind)
		}
		out.Controls = append(out.Controls, item)
	}
	for _, def := range plan.Definitions {
		out.Definitions = append(out.Definitions, Definition{
			Name:   def.Name,
			Params: slices.Clone(def.Params),
			Source: def.SourcePath,
			Blocks: len(def.Blocks),
		})
	}
	for i, task := range plan.Tasks {
		childBlocks := childTaskBlockNumbers(plan.Tasks, task.BlockIndex)
		item := Task{
			Index:  i + 1,
			Block:  task.BlockIndex + 1,
			Line:   task.Line,
			Scope:  slices.Clone(task.Scope),
			Prompt: task.Prompt,
			Output: task.Output,
		}
		if task.HasParent {
			item.ParentBlock = task.ParentIndex + 1
		}
		if decision := buildTaskDecisionSummary(task, childBlocks); !decision.IsZero() {
			item.Decision = &decision
		}
		if context := buildTaskContextSummary(task.Context); !context.IsZero() {
			item.Context = &context
		}
		item.ChildBlocks = childBlocks
		item.Variables = buildTaskVariableRefs(plan, task)
		if runtime := buildTaskRuntimeSummary(task); !runtime.IsZero() {
			item.Runtime = &runtime
		}
		if !task.DB.IsZero() {
			db := ir.CloneDBTaskConfig(task.DB)
			item.DB = &db
		}
		if !task.Skill.IsZero() {
			skill := ir.CloneSkillTaskConfig(task.Skill)
			item.Skill = &skill
		}
		if !task.MCP.IsZero() {
			mcp := ir.CloneMCPTaskConfig(task.MCP)
			item.MCP = &mcp
		}
		if task.Flow.Kind != "" {
			flow := flowNodeToJSON(task.Flow)
			item.Flow = &flow
		}
		if resources := buildTaskResourceView(plan, task); !resources.IsZero() {
			item.Resources = &resources
		}
		if task.Cursor.Active {
			item.Cursor = &Cursor{
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

func buildProviderPreview(filePath string, plan compiler.Plan) []ProviderPreview {
	root := filepath.Dir(filePath)
	defs := make(map[string]compiler.Definition, len(plan.Definitions))
	for _, def := range plan.Definitions {
		defs[def.Name] = def
	}
	var out []ProviderPreview
	for _, binding := range plan.Globals {
		if strings.TrimSpace(binding.BashScript) == "" {
			continue
		}
		item := ProviderPreview{Block: binding.BlockIndex + 1, Scope: "global", Name: binding.Name, Kind: "bash", Executed: true}
		item.Value, item.Error = runPreviewBash(root, binding.BashScript)
		out = append(out, item)
	}
	for _, task := range plan.Tasks {
		collectTaskProviderPreview(root, defs, task.BlockIndex+1, task.Vars, task.Flow, &out)
	}
	return out
}

type planSectionNode struct {
	line     int
	level    int
	title    string
	path     []string
	children []*planSectionNode
}

func buildPlanDocument(content string) Document {
	headings := document.MarkdownHeadings(content)
	if len(headings) == 0 {
		return Document{}
	}
	var title string
	var roots []*planSectionNode
	var stack []*planSectionNode
	for _, heading := range headings {
		start := heading.Start
		if start < 0 {
			start = 0
		}
		if start > len(content) {
			start = len(content)
		}
		node := &planSectionNode{
			line:  strings.Count(content[:start], "\n") + 1,
			level: heading.Level,
			title: strings.TrimSpace(heading.Text),
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= node.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			node.path = append(slices.Clone(stack[len(stack)-1].path), document.MarkdownScopeSegment(heading))
			stack[len(stack)-1].children = append(stack[len(stack)-1].children, node)
		} else {
			node.path = []string{document.MarkdownScopeSegment(heading)}
			roots = append(roots, node)
		}
		if title == "" && node.level == 1 {
			title = node.title
		}
		stack = append(stack, node)
	}
	return Document{Title: title, Sections: planSectionNodesToJSON(roots)}
}

func planSectionNodesToJSON(nodes []*planSectionNode) []Section {
	out := make([]Section, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, Section{
			Line:     node.line,
			Level:    node.level,
			Title:    node.title,
			Path:     slices.Clone(node.path),
			Sections: planSectionNodesToJSON(node.children),
		})
	}
	return out
}

func writePlanDocumentText(stdout io.Writer, document Document) {
	if document.IsZero() {
		return
	}
	fmt.Fprintln(stdout)
	if document.Title != "" {
		fmt.Fprintf(stdout, "document: %s\n", document.Title)
	} else {
		fmt.Fprintln(stdout, "document: <untitled>")
	}
	if len(document.Sections) > 0 {
		fmt.Fprintln(stdout, "sections:")
		for _, section := range document.Sections {
			writePlanSectionText(stdout, section, 0)
		}
	}
}

func writePlanSectionText(stdout io.Writer, section Section, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(stdout, "%s- line %d: %s %s\n", indent, section.Line, strings.Repeat("#", section.Level), section.Title)
	for _, child := range section.Sections {
		writePlanSectionText(stdout, child, depth+1)
	}
}

func buildPlanLoopSummary(plan compiler.Plan) []Loop {
	var out []Loop
	for i, task := range plan.Tasks {
		for _, op := range ir.FlattenTaskFlow(task) {
			if op.Kind != compiler.FlatOpFor {
				continue
			}
			item := Loop{
				Task:    i + 1,
				Block:   task.BlockIndex + 1,
				Var:     op.For.VarName,
				Summary: formatForIR(op.For),
				Mode:    planLoopMode(op.For),
				Values:  slices.Clone(op.For.Values),
				Count:   planLoopCount(op.For),
				Source:  op.For.Source.Text,
				Until:   op.For.Condition.Text,
				Resume:  op.For.Options.Resume,
				Fork:    op.For.Options.Fork,
				Args:    slices.Clone(op.For.Options.Args),
			}
			item.ResumeTarget = op.For.Options.ResumeTarget
			item.ForkTarget = op.For.Options.ForkTarget
			if op.For.Source.Kind != compiler.ConditionNone {
				item.SourceKind = string(op.For.Source.Kind)
			}
			if op.For.Condition.Kind != compiler.ConditionNone {
				item.UntilKind = string(op.For.Condition.Kind)
			}
			out = append(out, item)
		}
	}
	return out
}

func planLoopMode(loop compiler.For) string {
	switch {
	case loop.Source.Kind != compiler.ConditionNone:
		return "dynamic"
	case len(loop.Values) > 0:
		return "static"
	case loop.MaxRuns > 0:
		return "count"
	case loop.Condition.Kind != compiler.ConditionNone:
		return "until"
	default:
		return "unknown"
	}
}

func planLoopCount(loop compiler.For) int {
	if len(loop.Values) > 0 {
		return len(loop.Values)
	}
	if loop.MaxRuns > 0 {
		return loop.MaxRuns
	}
	return 0
}

func writePlanLoopsText(stdout io.Writer, loops []Loop) {
	if len(loops) == 0 {
		return
	}
	fmt.Fprintln(stdout, "\nloops:")
	for _, item := range loops {
		fmt.Fprintf(stdout, "- task %d block %d: %s", item.Task, item.Block, item.Summary)
		var details []string
		details = append(details, "mode="+item.Mode)
		if item.Count > 0 {
			details = append(details, fmt.Sprintf("count=%d", item.Count))
		}
		if item.Source != "" {
			details = append(details, fmt.Sprintf("source=%s:%s", item.SourceKind, item.Source))
		}
		if item.Until != "" {
			details = append(details, fmt.Sprintf("until=%s:%s", item.UntilKind, item.Until))
		}
		fmt.Fprintf(stdout, " (%s)\n", strings.Join(details, ", "))
	}
}

func buildPlanConditionSummary(plan compiler.Plan) []Condition {
	var out []Condition
	for i, task := range plan.Tasks {
		collectPlanConditions(task.Flow, i+1, task.BlockIndex+1, &out)
	}
	return out
}

func collectPlanConditions(node compiler.FlowNode, taskNumber, blockNumber int, out *[]Condition) {
	if node.Kind == compiler.FlowIf {
		item := Condition{
			Task:          taskNumber,
			Block:         blockNumber,
			Condition:     node.If.Condition.Text,
			ConditionKind: string(node.If.Condition.Kind),
			Then:          "execute when true; skipped when false",
			Else:          "no-op when false",
		}
		if len(node.ElseChildren) > 0 {
			item.Else = "skipped when true; execute when false"
		}
		*out = append(*out, item)
	}
	for _, child := range node.Children {
		collectPlanConditions(child, taskNumber, blockNumber, out)
	}
	for _, child := range node.ElseChildren {
		collectPlanConditions(child, taskNumber, blockNumber, out)
	}
}

func writePlanConditionsText(stdout io.Writer, conditions []Condition) {
	if len(conditions) == 0 {
		return
	}
	fmt.Fprintln(stdout, "\nconditions:")
	for _, item := range conditions {
		condition := item.Condition
		if condition == "" {
			condition = "<none>"
		}
		kind := item.ConditionKind
		if kind == "" {
			kind = string(compiler.ConditionNone)
		}
		fmt.Fprintf(stdout, "- task %d block %d if %s(%s): then %s; else %s\n", item.Task, item.Block, kind, condition, item.Then, item.Else)
	}
}

func buildPlanAsyncSummary(plan compiler.Plan) Async {
	type pendingBranch struct {
		task   int
		block  int
		pool   string
		fanout string
	}
	var out Async
	var pending []pendingBranch
	refs := make([]taskRef, 0, len(plan.Tasks))
	for i, task := range plan.Tasks {
		refs = append(refs, taskRef{Number: i + 1, Task: task})
	}
	slices.SortFunc(refs, func(a, b taskRef) int {
		return cmp.Compare(a.Task.BlockIndex, b.Task.BlockIndex)
	})
	for _, ref := range refs {
		task := ref.Task
		for _, op := range ir.FlattenTaskFlow(task) {
			if op.Kind != compiler.FlatOpWait {
				continue
			}
			var rest []pendingBranch
			for _, branch := range pending {
				if op.Pool == "" || branch.pool == op.Pool {
					out.Joins = append(out.Joins, AsyncJoin{
						FromTask:  branch.task,
						FromBlock: branch.block + 1,
						WaitTask:  ref.Number,
						WaitBlock: task.BlockIndex + 1,
						Pool:      branch.pool,
						Fanout:    branch.fanout,
					})
				} else {
					rest = append(rest, branch)
				}
			}
			pending = rest
		}
		if pool, ok := taskGoPool(task); ok {
			item := pendingBranch{
				task:   ref.Number,
				block:  task.BlockIndex,
				pool:   pool,
				fanout: taskFanoutSummary(task),
			}
			pending = append(pending, item)
			out.Background = append(out.Background, AsyncBackground{
				Task:   item.task,
				Block:  item.block + 1,
				Pool:   item.pool,
				Fanout: item.fanout,
			})
		}
	}
	for _, branch := range pending {
		out.Unjoined = append(out.Unjoined, AsyncBackground{
			Task:   branch.task,
			Block:  branch.block + 1,
			Pool:   branch.pool,
			Fanout: branch.fanout,
		})
	}
	return out
}

func writePlanAsyncText(stdout io.Writer, async Async) {
	if async.IsZero() {
		return
	}
	fmt.Fprintln(stdout, "\nasync:")
	for _, item := range async.Background {
		fmt.Fprintf(stdout, "- background task %d block %d via %s", item.Task, item.Block, formatPlanPool(item.Pool))
		if item.Fanout != "" {
			fmt.Fprintf(stdout, ": %s", item.Fanout)
		}
		fmt.Fprintln(stdout)
	}
	for _, item := range async.Joins {
		fmt.Fprintf(stdout, "- wait task %d block %d joins task %d block %d via %s", item.WaitTask, item.WaitBlock, item.FromTask, item.FromBlock, formatPlanPool(item.Pool))
		if item.Fanout != "" {
			fmt.Fprintf(stdout, ": %s", item.Fanout)
		}
		fmt.Fprintln(stdout)
	}
	for _, item := range async.Unjoined {
		fmt.Fprintf(stdout, "- unjoined task %d block %d via %s", item.Task, item.Block, formatPlanPool(item.Pool))
		if item.Fanout != "" {
			fmt.Fprintf(stdout, ": %s", item.Fanout)
		}
		fmt.Fprintln(stdout)
	}
}

func formatPlanPool(pool string) string {
	if pool == "" {
		return "default"
	}
	return pool
}

func collectTaskProviderPreview(root string, defs map[string]compiler.Definition, block int, vars map[string]any, node compiler.FlowNode, out *[]ProviderPreview) {
	if node.Kind == compiler.FlowBash && node.Bash.Name != "" {
		item := ProviderPreview{Block: block, Scope: "task", Name: node.Bash.Name, Kind: "bash", Executed: true}
		item.Value, item.Error = runPreviewBash(root, node.Bash.Script)
		*out = append(*out, item)
	}
	if node.Kind == compiler.FlowCall && node.Call.Assign != "" {
		item := ProviderPreview{Block: block, Scope: "task", Name: node.Call.Assign, Kind: "call"}
		if value, ok, err := previewDefinitionCall(root, defs, vars, node.Call, nil); err != nil {
			item.Error = err.Error()
			item.Note = fmt.Sprintf("/call %s preview failed before running an agent", node.Call.Name)
		} else if ok {
			item.Executed = true
			item.Value = ir.StringValue(value)
		} else {
			item.Note = fmt.Sprintf("/call %s preview requires runtime execution and was not run", node.Call.Name)
		}
		*out = append(*out, item)
	}
	for _, child := range node.Children {
		collectTaskProviderPreview(root, defs, block, vars, child, out)
	}
	for _, child := range node.ElseChildren {
		collectTaskProviderPreview(root, defs, block, vars, child, out)
	}
}

func previewDefinitionCall(root string, defs map[string]compiler.Definition, vars map[string]any, call compiler.Call, stack []string) (any, bool, error) {
	def, ok := defs[call.Name]
	if !ok {
		return nil, false, fmt.Errorf("unknown definition %q", call.Name)
	}
	for _, active := range stack {
		if active == call.Name {
			return nil, false, fmt.Errorf("recursive definition preview for %q", call.Name)
		}
	}
	if len(call.Args) != len(def.Params) {
		return nil, false, fmt.Errorf("/call %s expects %d argument(s), got %d", call.Name, len(def.Params), len(call.Args))
	}
	localVars := ir.CloneVars(vars)
	for i, param := range def.Params {
		value, err := compiler.RenderTemplate(call.Args[i], localVars)
		if err != nil {
			return nil, false, fmt.Errorf("/call %s argument %d: %w", call.Name, i+1, err)
		}
		localVars[param] = value
	}
	stack = append(stack, call.Name)
	defRoot := root
	if def.SourcePath != "" {
		defRoot = filepath.Dir(def.SourcePath)
	}
	for i, block := range def.Blocks {
		body := block.Body
		if _, ok, err := compiler.ParseGlobalPoolBlock(body); err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		} else if ok {
			continue
		}
		if bindings, ok, err := compiler.ParseGlobalLetBlock(body); err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		} else if ok {
			for _, binding := range bindings {
				if strings.TrimSpace(binding.BashScript) != "" {
					return nil, false, nil
				}
				localVars[binding.Name] = binding.Value
			}
			continue
		}
		task, err := compiler.ParseTaskForFile(def.SourcePath, def.BlockIndex, body, localVars, compiler.CompileOptions{Root: defRoot})
		if err != nil {
			return nil, false, err
		}
		blockVars := ir.CloneVars(task.Vars)
		value, returned, previewable, err := previewFlowReturn(root, defs, blockVars, task.Flow, stack)
		if err != nil {
			return nil, false, fmt.Errorf("definition %s block %d: %w", call.Name, i+1, err)
		}
		if !previewable {
			return nil, false, nil
		}
		if returned {
			return value, true, nil
		}
		localVars = blockVars
	}
	return nil, false, nil
}

func previewFlowReturn(root string, defs map[string]compiler.Definition, vars map[string]any, node compiler.FlowNode, stack []string) (value any, returned bool, previewable bool, err error) {
	switch node.Kind {
	case compiler.FlowSeq:
		for _, child := range node.Children {
			value, returned, previewable, err = previewFlowReturn(root, defs, vars, child, stack)
			if err != nil || !previewable || returned {
				return value, returned, previewable, err
			}
		}
		return nil, false, true, nil
	case compiler.FlowCall:
		if node.Call.Assign == "" {
			_, _, err := previewDefinitionCall(root, defs, vars, node.Call, stack)
			if err != nil {
				return nil, false, true, err
			}
			return nil, false, true, nil
		}
		call := node.Call
		assign := call.Assign
		call.Assign = ""
		value, ok, err := previewDefinitionCall(root, defs, vars, call, stack)
		if err != nil || !ok {
			return nil, false, ok, err
		}
		vars[assign] = normalizePreviewValue(value)
		return nil, false, true, nil
	case compiler.FlowReturn:
		value, ok, err := previewReturnSpec(root, vars, node.Return)
		return value, ok, ok, err
	case compiler.FlowExecute:
		return nil, false, strings.TrimSpace(node.Prompt) == "", nil
	case compiler.FlowCd:
		return nil, false, true, nil
	default:
		return nil, false, false, nil
	}
}

func previewReturnSpec(root string, vars map[string]any, spec compiler.ReturnSpec) (any, bool, error) {
	returnVars := ir.CloneVars(vars)
	returnVars["agent"] = map[string]any{
		"message":       "",
		"last_message":  "",
		"messages":      "",
		"messages_json": "[]",
	}
	switch spec.Kind {
	case compiler.ReturnBash:
		script, err := compiler.RenderTemplate(spec.Script, returnVars)
		if err != nil {
			return nil, true, err
		}
		value, errText := runPreviewBash(root, script)
		if errText != "" {
			return value, true, fmt.Errorf("%s", errText)
		}
		return value, true, nil
	case compiler.ReturnStructured:
		return nil, false, nil
	default:
		value, err := compiler.RenderTemplate(spec.Text, returnVars)
		if err != nil {
			return nil, true, err
		}
		return normalizePreviewValue(value), true, nil
	}
}

func normalizePreviewValue(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return value
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return value
	}
	return parsed
}

func runPreviewBash(root, script string) (string, string) {
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = root
	out, err := cmd.Output()
	value := strings.TrimRight(string(out), "\r\n")
	if err != nil {
		return value, err.Error()
	}
	return value, ""
}

func writeProviderPreviewText(stdout io.Writer, preview []ProviderPreview) {
	if len(preview) == 0 {
		fmt.Fprintln(stdout, "\npreview providers: none")
		return
	}
	fmt.Fprintln(stdout, "\npreview providers:")
	for _, item := range preview {
		status := "not-executed"
		if item.Executed {
			status = "executed"
		}
		label := item.Name
		if label == "" {
			label = "<unnamed>"
		}
		fmt.Fprintf(stdout, "- block %d %s %s provider %q: %s", item.Block, item.Scope, item.Kind, label, status)
		if item.Value != "" {
			fmt.Fprintf(stdout, " value=%q", item.Value)
		}
		if item.Error != "" {
			fmt.Fprintf(stdout, " error=%q", item.Error)
		}
		if item.Note != "" {
			fmt.Fprintf(stdout, " note=%q", item.Note)
		}
		fmt.Fprintln(stdout)
	}
}

func childTaskBlockNumbers(tasks []compiler.Task, parentBlockIndex int) []int {
	var out []int
	for _, task := range tasks {
		if task.HasParent && task.ParentIndex == parentBlockIndex {
			out = append(out, task.BlockIndex+1)
		}
	}
	return out
}

func buildTaskDecisionSummary(task compiler.Task, childBlocks []int) Decision {
	decision := Decision{
		Action: "execute",
		Reason: "runnable task block with prompt or executable flow",
	}
	switch {
	case strings.TrimSpace(task.Prompt) != "" && taskHasWait(task):
		decision.Action = "wait-then-execute"
		decision.Reason = "/wait joins matching background work before running the prompt with wait result context"
	case strings.TrimSpace(task.Prompt) == "" && taskHasWait(task):
		decision.Action = "wait"
		decision.Reason = "/wait without prompt only joins matching background work; no agent prompt is sent"
	case taskHasGo(task):
		decision.Action = "dispatch-background"
		decision.Reason = "/go dispatches background work and continues; matching /wait controls join"
	case compiler.TaskHasFlowIf(task):
		decision.Action = "conditional-execute"
		decision.Reason = "/if selects one branch; non-selected branches are skipped or no-op"
	}
	if task.HasParent {
		decision.Dependencies = append(decision.Dependencies, fmt.Sprintf("runs before parent block %d", task.ParentIndex+1))
	}
	if len(childBlocks) > 0 {
		decision.Dependencies = append(decision.Dependencies, fmt.Sprintf("waits for child blocks %s", formatBlockNumberList(childBlocks)))
	}
	decision.Skips = collectTaskDecisionSkips(task.Flow)
	return decision
}

func taskHasGo(task compiler.Task) bool {
	_, ok := taskGoPool(task)
	return ok
}

func collectTaskDecisionSkips(node compiler.FlowNode) []string {
	var out []string
	collectTaskDecisionSkipsInto(node, &out)
	return out
}

func collectTaskDecisionSkipsInto(node compiler.FlowNode, out *[]string) {
	if node.Kind == compiler.FlowIf {
		condition := formatCondition(node.If.Condition)
		*out = append(*out, fmt.Sprintf("then branch skipped when %s is false", condition))
		if len(node.ElseChildren) > 0 {
			*out = append(*out, fmt.Sprintf("else branch skipped when %s is true", condition))
		} else {
			*out = append(*out, fmt.Sprintf("false branch is no-op when %s is false", condition))
		}
	}
	for _, child := range node.Children {
		collectTaskDecisionSkipsInto(child, out)
	}
	for _, child := range node.ElseChildren {
		collectTaskDecisionSkipsInto(child, out)
	}
}

func formatBlockNumberList(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ", ")
}

func flowNodeToJSON(node compiler.FlowNode) FlowNode {
	out := FlowNode{Kind: node.Kind, Prompt: node.Prompt}
	if op, ok := flowNodeOpJSON(node); ok {
		out.Op = &op
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, flowNodeToJSON(child))
	}
	for _, child := range node.ElseChildren {
		out.ElseChildren = append(out.ElseChildren, flowNodeToJSON(child))
	}
	return out
}

func flowNodeOpJSON(node compiler.FlowNode) (Operation, bool) {
	switch node.Kind {
	case compiler.FlowCd:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpCd, Cd: node.Cd}), true
	case compiler.FlowBash:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpBash, Bash: node.Bash}), true
	case compiler.FlowFor:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpFor, For: node.For}), true
	case compiler.FlowIf:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpIf, If: node.If}), true
	case compiler.FlowGo:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpGo, Pool: node.Pool}), true
	case compiler.FlowWait:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpWait, Pool: node.Pool}), true
	case compiler.FlowCall:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpCall, Call: node.Call}), true
	case compiler.FlowReturn:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpReturn, Return: node.Return}), true
	case compiler.FlowExecute:
		return planOpToJSON(compiler.FlatOp{Kind: compiler.FlatOpExecute, ExecuteOptions: node.ExecuteOptions}), true
	default:
		return Operation{}, false
	}
}

func planOpToJSON(op compiler.FlatOp) Operation {
	out := Operation{Kind: op.Kind}
	switch op.Kind {
	case compiler.FlatOpCd:
		out.Workdir = op.Cd.Path
		out.MustExist = op.Cd.MustExist
	case compiler.FlatOpBash:
		out.Name = op.Bash.Name
		out.Script = op.Bash.Script
	case compiler.FlatOpFor:
		out.Var = op.For.VarName
		out.Values = slices.Clone(op.For.Values)
		out.MaxRuns = op.For.MaxRuns
		out.Until = op.For.Condition.Text
		if op.For.Condition.Kind != compiler.ConditionNone {
			out.UntilKind = string(op.For.Condition.Kind)
		}
		out.Resume = op.For.Options.Resume
		out.ResumeTarget = op.For.Options.ResumeTarget
		out.Fork = op.For.Options.Fork
		out.ForkTarget = op.For.Options.ForkTarget
		out.Args = slices.Clone(op.For.Options.Args)
	case compiler.FlatOpIf:
		out.Condition = op.If.Condition.Text
		if op.If.Condition.Kind != compiler.ConditionNone {
			out.ConditionKind = string(op.If.Condition.Kind)
		}
	case compiler.FlatOpExecute:
		out.PromptRef = "prompt"
		out.Resume = op.ExecuteOptions.Resume
		out.ResumeTarget = op.ExecuteOptions.ResumeTarget
		out.Fork = op.ExecuteOptions.Fork
		out.ForkTarget = op.ExecuteOptions.ForkTarget
		out.Args = slices.Clone(op.ExecuteOptions.Args)
	case compiler.FlatOpGo, compiler.FlatOpWait:
		out.Pool = op.Pool
	case compiler.FlatOpCall:
		out.Name = op.Call.Name
		out.Args = slices.Clone(op.Call.Args)
	case compiler.FlatOpReturn:
		out.Name = string(op.Return.Kind)
	}
	return out
}

type globalBindingGroup struct {
	blockIndex int
	count      int
}

func groupGlobalBindings(bindings []compiler.GlobalBinding) []globalBindingGroup {
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
	pools      []compiler.PoolDecl
}

func groupPoolDecls(pools []compiler.PoolDecl) []poolDeclGroup {
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

func formatFlowNode(node compiler.FlowNode) []string {
	switch node.Kind {
	case "", compiler.FlowSeq:
		var parts []string
		for _, child := range node.Children {
			parts = append(parts, formatFlowNode(child)...)
		}
		return parts
	case compiler.FlowCd, compiler.FlowBash, compiler.FlowFor, compiler.FlowIf, compiler.FlowGo, compiler.FlowWait, compiler.FlowCall, compiler.FlowReturn, compiler.FlowExecute:
		op, ok := flowNodeFlatOp(node)
		if !ok {
			return nil
		}
		label := formatPlanOp(op)
		if node.Kind == compiler.FlowIf && len(node.ElseChildren) > 0 {
			thenText := strings.Join(formatFlowNodes(node.Children), " -> ")
			elseText := strings.Join(formatFlowNodes(node.ElseChildren), " -> ")
			return []string{fmt.Sprintf("%s {then: %s; else: %s}", label, emptyFlowLabel(thenText), emptyFlowLabel(elseText))}
		}
		parts := []string{label}
		parts = append(parts, formatFlowNodes(node.Children)...)
		return parts
	default:
		return []string{string(node.Kind)}
	}
}

func formatFlowNodes(nodes []compiler.FlowNode) []string {
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, formatFlowNode(node)...)
	}
	return parts
}

func emptyFlowLabel(text string) string {
	if text == "" {
		return "noop"
	}
	return text
}

func flowNodeFlatOp(node compiler.FlowNode) (compiler.FlatOp, bool) {
	switch node.Kind {
	case compiler.FlowCd:
		return compiler.FlatOp{Kind: compiler.FlatOpCd, Cd: node.Cd}, true
	case compiler.FlowBash:
		return compiler.FlatOp{Kind: compiler.FlatOpBash, Bash: node.Bash}, true
	case compiler.FlowFor:
		return compiler.FlatOp{Kind: compiler.FlatOpFor, For: node.For}, true
	case compiler.FlowIf:
		return compiler.FlatOp{Kind: compiler.FlatOpIf, If: node.If}, true
	case compiler.FlowGo:
		return compiler.FlatOp{Kind: compiler.FlatOpGo, Pool: node.Pool}, true
	case compiler.FlowWait:
		return compiler.FlatOp{Kind: compiler.FlatOpWait, Pool: node.Pool}, true
	case compiler.FlowCall:
		return compiler.FlatOp{Kind: compiler.FlatOpCall, Call: node.Call}, true
	case compiler.FlowReturn:
		return compiler.FlatOp{Kind: compiler.FlatOpReturn, Return: node.Return}, true
	case compiler.FlowExecute:
		return compiler.FlatOp{Kind: compiler.FlatOpExecute, ExecuteOptions: node.ExecuteOptions}, true
	default:
		return compiler.FlatOp{}, false
	}
}

func formatPlanOp(op compiler.FlatOp) string {
	switch op.Kind {
	case compiler.FlatOpCd:
		if op.Cd.MustExist {
			return fmt.Sprintf("Cd(%s, must-exist)", op.Cd.Path)
		}
		return fmt.Sprintf("Cd(%s)", op.Cd.Path)
	case compiler.FlatOpBash:
		if op.Bash.Name != "" {
			return fmt.Sprintf("LazyBash(%s)", op.Bash.Name)
		}
		return "Bash"
	case compiler.FlatOpFor:
		return formatForIR(op.For)
	case compiler.FlatOpIf:
		return formatIfIR(op.If)
	case compiler.FlatOpGo:
		if op.Pool != "" {
			return fmt.Sprintf("Go(%s)", op.Pool)
		}
		return "Go"
	case compiler.FlatOpWait:
		if op.Pool != "" {
			return fmt.Sprintf("Wait(%s)", op.Pool)
		}
		return "Wait"
	case compiler.FlatOpCall:
		if op.Call.Assign != "" {
			return fmt.Sprintf("LazyCall(%s -> %s)", op.Call.Name, op.Call.Assign)
		}
		return fmt.Sprintf("compiler.Call(%s)", op.Call.Name)
	case compiler.FlatOpReturn:
		return "Return"
	case compiler.FlatOpExecute:
		return appendRunOptionSummary("Execute", op.ExecuteOptions)
	default:
		return string(op.Kind)
	}
}

func formatIfIR(branch compiler.If) string {
	switch branch.Condition.Kind {
	case compiler.ConditionExpr:
		return fmt.Sprintf("compiler.If(expr:%s)", branch.Condition.Text)
	case compiler.ConditionNatural:
		return fmt.Sprintf("compiler.If(%s)", branch.Condition.Text)
	default:
		return "compiler.If"
	}
}

func formatForIR(step compiler.For) string {
	name := step.VarName
	if name == "" {
		name = "run"
	}
	if step.Source.Kind == compiler.ConditionExpr {
		detail := fmt.Sprintf("For(%s in expr(%q))", name, step.Source.Text)
		if step.Condition.Text != "" {
			detail += formatForUntilSuffix(step.Condition)
		}
		return appendRunOptionSummary(detail, step.Options)
	}
	if step.Source.Kind == compiler.ConditionCall {
		detail := fmt.Sprintf("For(%s in call(%q))", name, step.Source.Text)
		if step.Condition.Text != "" {
			detail += formatForUntilSuffix(step.Condition)
		}
		return appendRunOptionSummary(detail, step.Options)
	}
	if step.MaxRuns == 0 && step.Condition.Kind == compiler.ConditionExpr {
		return appendRunOptionSummary(fmt.Sprintf("For(%s until expr(%q))", name, step.Condition.Text), step.Options)
	}
	detail := fmt.Sprintf("For(%s x %d)", name, step.MaxRuns)
	if len(step.Values) > 0 && len(step.Values) <= 4 {
		detail = fmt.Sprintf("For(%s in [%s])", name, strings.Join(step.Values, " "))
	}
	if step.Condition.Text != "" {
		detail += formatForUntilSuffix(step.Condition)
	}
	return appendRunOptionSummary(detail, step.Options)
}

func formatForUntilSuffix(condition compiler.Condition) string {
	if condition.Kind == compiler.ConditionExpr {
		return fmt.Sprintf(" until expr(%q)", condition.Text)
	}
	return fmt.Sprintf(" until %q", condition.Text)
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

func formatPlanScope(scope []string) string {
	return strings.Join(scope, " > ")
}

func poolDeclGroupSummary(pools []compiler.PoolDecl) string {
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

func formatPlanOutput(output *compiler.OutputSpec) string {
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

func formatControlBlock(control compiler.ControlBlock) string {
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

func formatCondition(condition compiler.Condition) string {
	switch condition.Kind {
	case compiler.ConditionExpr:
		return fmt.Sprintf("expr(%q)", condition.Text)
	case compiler.ConditionCall:
		return fmt.Sprintf("call(%q)", condition.Text)
	case compiler.ConditionNatural:
		return fmt.Sprintf("%q", condition.Text)
	default:
		return "<none>"
	}
}

func appendRunOptionSummary(base string, options compiler.RunOptions) string {
	var parts []string
	if options.Resume {
		if options.ResumeTarget != "" {
			parts = append(parts, "resume="+options.ResumeTarget)
		} else {
			parts = append(parts, "resume")
		}
	}
	if options.Fork {
		if options.ForkTarget != "" {
			parts = append(parts, "fork="+options.ForkTarget)
		} else {
			parts = append(parts, "fork")
		}
	}
	if len(options.Args) > 0 {
		parts = append(parts, "args="+strings.Join(options.Args, " "))
	}
	if len(parts) == 0 {
		return base
	}
	return base + " [" + strings.Join(parts, ", ") + "]"
}

func formatDBTaskConfig(config compiler.DBTaskConfig) string {
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

func formatSkillTaskConfig(config compiler.SkillTaskConfig) string {
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

func formatMCPTaskConfig(config compiler.MCPTaskConfig) string {
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

func buildTaskContextSummary(context string) Context {
	context = strings.TrimSpace(context)
	if context == "" {
		return Context{}
	}
	return Context{
		Lines:   countPlanLines(context),
		Chars:   len([]rune(context)),
		Preview: promptPreview(context),
	}
}

func countPlanLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func buildTaskRuntimeSummary(task compiler.Task) Runtime {
	var out Runtime
	for _, op := range ir.FlattenTaskFlow(task) {
		switch op.Kind {
		case compiler.FlatOpExecute:
			if op.ExecuteOptions.Resume {
				out.Resume = true
				if op.ExecuteOptions.ResumeTarget != "" {
					out.ResumeTarget = op.ExecuteOptions.ResumeTarget
				}
			}
			if op.ExecuteOptions.Fork {
				out.Fork = true
				if op.ExecuteOptions.ForkTarget != "" {
					out.ForkTarget = op.ExecuteOptions.ForkTarget
				}
			}
			out.Args = append(out.Args, op.ExecuteOptions.Args...)
		case compiler.FlatOpFor:
			if op.For.Options.Resume {
				out.Resume = true
				if op.For.Options.ResumeTarget != "" {
					out.ResumeTarget = op.For.Options.ResumeTarget
				}
			}
			if op.For.Options.Fork {
				out.Fork = true
				if op.For.Options.ForkTarget != "" {
					out.ForkTarget = op.For.Options.ForkTarget
				}
			}
			out.Args = append(out.Args, op.For.Options.Args...)
		case compiler.FlatOpCd:
			out.Workdirs = append(out.Workdirs, Workdir{Path: op.Cd.Path, MustExist: op.Cd.MustExist})
		case compiler.FlatOpBash:
			if op.Bash.Name == "" {
				out.Bash = append(out.Bash, Bash{Script: op.Bash.Script})
			} else {
				out.LazyProvider = append(out.LazyProvider, Provider{Name: op.Bash.Name, Kind: "bash"})
			}
		case compiler.FlatOpCall:
			if op.Call.Assign != "" {
				out.LazyProvider = append(out.LazyProvider, Provider{Name: op.Call.Assign, Kind: "call", Call: op.Call.Name})
			}
		}
	}
	out.Args = uniqueStringsPreserveOrder(out.Args)
	return out
}

func uniqueStringsPreserveOrder(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func formatTaskRuntimeSummary(runtime Runtime) string {
	var parts []string
	if runtime.Resume {
		if runtime.ResumeTarget != "" {
			parts = append(parts, "resume="+runtime.ResumeTarget)
		} else {
			parts = append(parts, "resume")
		}
	}
	if runtime.Fork {
		if runtime.ForkTarget != "" {
			parts = append(parts, "fork="+runtime.ForkTarget)
		} else {
			parts = append(parts, "fork")
		}
	}
	if len(runtime.Args) > 0 {
		parts = append(parts, "args="+strings.Join(runtime.Args, " "))
	}
	if len(runtime.Workdirs) > 0 {
		var workdirs []string
		for _, item := range runtime.Workdirs {
			label := item.Path
			if item.MustExist {
				label += " (must-exist)"
			}
			workdirs = append(workdirs, label)
		}
		parts = append(parts, "cd="+strings.Join(workdirs, ", "))
	}
	if len(runtime.Bash) > 0 {
		parts = append(parts, fmt.Sprintf("bash=%d", len(runtime.Bash)))
	}
	if len(runtime.LazyProvider) > 0 {
		var providers []string
		for _, provider := range runtime.LazyProvider {
			label := provider.Name + ":" + provider.Kind
			if provider.Call != "" {
				label += "(" + provider.Call + ")"
			}
			providers = append(providers, label)
		}
		parts = append(parts, "lazy="+strings.Join(providers, ", "))
	}
	return strings.Join(parts, "; ")
}

func buildTaskVariableRefs(plan compiler.Plan, task compiler.Task) []Variable {
	names := taskVariableRefs(task)
	if len(names) == 0 {
		return nil
	}
	ref := scopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	lazy := taskLazyVariableSources(task)
	loops := taskLoopVariableSources(task)
	out := make([]Variable, 0, len(names))
	for _, name := range names {
		item := Variable{Name: name, Source: "unresolved"}
		if source, ok := lazy[name]; ok {
			item.Source = source
			item.Block = task.BlockIndex + 1
		} else if _, ok := loops[name]; ok {
			item.Source = "loop"
			item.Block = task.BlockIndex + 1
		} else if binding, ok := resolveVisibleGlobalBinding(plan.Globals, name, ref); ok {
			item.Source = "global-let"
			if binding.BashScript != "" {
				item.Source = "global-lazy-bash"
			} else {
				item.Value = binding.Value
			}
			item.Block = binding.BlockIndex + 1
			if value, ok := task.Vars[name]; ok && !globalBindingValueMatches(binding, value) {
				item.Source = "task-let"
				item.Block = task.BlockIndex + 1
				item.Value = ir.StringValue(value)
			}
		} else if value, ok := task.Vars[name]; ok {
			item.Source = "task-let"
			item.Block = task.BlockIndex + 1
			item.Value = ir.StringValue(value)
		}
		out = append(out, item)
	}
	return out
}

func taskLazyVariableSources(task compiler.Task) map[string]string {
	out := map[string]string{}
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind == compiler.FlatOpBash && op.Bash.Name != "" {
			out[op.Bash.Name] = "task-lazy-bash"
		}
		if op.Kind == compiler.FlatOpCall && op.Call.Assign != "" {
			out[op.Call.Assign] = "task-lazy-call"
		}
	}
	return out
}

func taskLoopVariableSources(task compiler.Task) map[string]struct{} {
	out := map[string]struct{}{}
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind == compiler.FlatOpFor && op.For.VarName != "" {
			out[op.For.VarName] = struct{}{}
		}
	}
	return out
}

func resolveVisibleGlobalBinding(bindings []compiler.GlobalBinding, name string, ref scopeRef) (compiler.GlobalBinding, bool) {
	var best compiler.GlobalBinding
	found := false
	bestDepth := -1
	bestLine := -1
	for _, binding := range bindings {
		if binding.Name != name || !globalBindingVisibleAt(binding, ref) {
			continue
		}
		depth := len(binding.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && binding.Line > bestLine) {
			best = binding
			found = true
			bestDepth = depth
			bestLine = binding.Line
		}
	}
	return best, found
}

func globalBindingVisibleAt(binding compiler.GlobalBinding, ref scopeRef) bool {
	if binding.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(binding.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	return declarationVisibleAt(binding.Line, binding.Scope, ref.Line, ref.Scope)
}

func resolveVisibleDefinition(defs []compiler.Definition, name string, ref definitionScopeRef) (compiler.Definition, bool) {
	var best compiler.Definition
	found := false
	bestDepth := -1
	bestLine := -1
	for _, def := range defs {
		if def.Name != name || !definitionVisibleAt(def, ref) {
			continue
		}
		depth := len(def.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && def.Line > bestLine) {
			best = def
			found = true
			bestDepth = depth
			bestLine = def.Line
		}
	}
	return best, found
}

func definitionVisibleAt(def compiler.Definition, ref definitionScopeRef) bool {
	sourcePath := def.VisibleSourcePath
	scope := def.VisibleScope
	line := def.VisibleLine
	if def.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(def.SourcePath) == filepath.Clean(ref.SourcePath) {
		sourcePath = def.SourcePath
		scope = def.Scope
		line = def.Line
	}
	if sourcePath != "" && ref.SourcePath != "" && filepath.Clean(sourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	return declarationVisibleAt(line, scope, ref.Line, ref.Scope)
}

func dbVisibleAt(db compiler.DBDecl, ref definitionScopeRef) bool {
	if db.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(db.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	return declarationVisibleAt(db.Line, db.ScopePath, ref.Line, ref.Scope)
}

func resolveVisibleSkill(skills []compiler.SkillDecl, name string, ref definitionScopeRef) (compiler.SkillDecl, bool) {
	var best compiler.SkillDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, skill := range skills {
		if skill.Name != name || !skillVisibleAt(skill, ref) {
			continue
		}
		depth := len(skill.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && skill.Line > bestLine) {
			best = skill
			found = true
			bestDepth = depth
			bestLine = skill.Line
		}
	}
	return best, found
}

func skillVisibleAt(skill compiler.SkillDecl, ref definitionScopeRef) bool {
	if skill.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(skill.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	return declarationVisibleAt(skill.Line, skill.Scope, ref.Line, ref.Scope)
}

func resolveVisibleMCP(mcps []compiler.MCPDecl, name string, ref definitionScopeRef) (compiler.MCPDecl, bool) {
	var best compiler.MCPDecl
	found := false
	bestDepth := -1
	bestLine := -1
	for _, mcp := range mcps {
		if mcp.Name != name || !mcpVisibleAt(mcp, ref) {
			continue
		}
		depth := len(mcp.Scope)
		if !found || depth > bestDepth || (depth == bestDepth && mcp.Line > bestLine) {
			best = mcp
			found = true
			bestDepth = depth
			bestLine = mcp.Line
		}
	}
	return best, found
}

func mcpVisibleAt(mcp compiler.MCPDecl, ref definitionScopeRef) bool {
	if mcp.SourcePath != "" && ref.SourcePath != "" && filepath.Clean(mcp.SourcePath) != filepath.Clean(ref.SourcePath) {
		return true
	}
	return declarationVisibleAt(mcp.Line, mcp.Scope, ref.Line, ref.Scope)
}

func declarationVisibleAt(line int, scope []string, refLine int, refScope []string) bool {
	if refLine > 0 && line > 0 && line >= refLine {
		return false
	}
	if len(scope) > len(refScope) {
		return false
	}
	for i := range scope {
		if scope[i] != refScope[i] {
			return false
		}
	}
	return true
}

func globalBindingValueMatches(binding compiler.GlobalBinding, value any) bool {
	if binding.BashScript != "" {
		return ir.StringValue(value) == "{{"+binding.Name+"}}"
	}
	return ir.StringValue(value) == binding.Value
}

func formatTaskVariableRefs(vars []Variable) string {
	parts := make([]string, 0, len(vars))
	for _, item := range vars {
		detail := item.Name + "(" + item.Source
		if item.Block > 0 {
			detail += fmt.Sprintf(", block %d", item.Block)
		}
		if item.Value != "" {
			detail += ", value=" + strconv.Quote(item.Value)
		}
		detail += ")"
		parts = append(parts, detail)
	}
	return strings.Join(parts, "; ")
}

func buildTaskResourceView(plan compiler.Plan, task compiler.Task) Resources {
	ref := definitionScopeRef{SourcePath: task.SourcePath, Scope: task.Scope, Line: task.Line}
	return Resources{
		DBs:     buildTaskDBResourceView(plan.DBs, task.DB, ref),
		Skills:  buildTaskSkillResourceView(plan.Skills, task.Skill, ref),
		MCPs:    buildTaskMCPResourceView(plan.MCPs, task.MCP, ref),
		DefMCPs: buildTaskDefMCPResourceView(plan.Definitions, task.MCP, ref),
	}
}

func buildTaskDBResourceView(dbs []compiler.DBDecl, config compiler.DBTaskConfig, ref definitionScopeRef) []DBResource {
	if config.IgnoreAll {
		return nil
	}
	decls := make(map[string]compiler.DBDecl)
	for _, db := range dbs {
		if dbVisibleAt(db, ref) {
			decls[db.Name] = db
		}
	}
	mounted := make(map[string]DBResource)
	for name, decl := range decls {
		if decl.Scope == compiler.DBScopeGlobal {
			mounted[name] = DBResource{
				Name:    name,
				Block:   decl.BlockIndex + 1,
				Scope:   decl.Scope,
				Persist: decl.Persist,
				Access:  decl.Access,
			}
		}
	}
	for _, use := range config.Use {
		for _, name := range use.Names {
			decl, ok := decls[name]
			if !ok {
				continue
			}
			access := decl.Access
			if use.Access != "" {
				access = use.Access
			}
			mounted[name] = DBResource{
				Name:    name,
				Block:   decl.BlockIndex + 1,
				Scope:   decl.Scope,
				Persist: decl.Persist,
				Access:  access,
			}
		}
	}
	for _, rule := range config.Access {
		for _, name := range expandPlanDBResourceNames(rule.Names, mounted) {
			item, ok := mounted[name]
			if !ok {
				continue
			}
			item.Access = rule.Access
			mounted[name] = item
		}
	}
	for _, name := range config.Ignore {
		delete(mounted, name)
	}
	names := slices.Sorted(maps.Keys(mounted))
	out := make([]DBResource, 0, len(names))
	for _, name := range names {
		out = append(out, mounted[name])
	}
	return out
}

func expandPlanDBResourceNames(names []string, mounted map[string]DBResource) []string {
	for _, name := range names {
		if name == "*" {
			return slices.Sorted(maps.Keys(mounted))
		}
	}
	return slices.Clone(names)
}

func buildTaskSkillResourceView(skills []compiler.SkillDecl, config compiler.SkillTaskConfig, ref definitionScopeRef) []Resource {
	if config.IgnoreAll {
		return nil
	}
	ignored := planStringSet(config.Ignore)
	var out []Resource
	seen := map[string]struct{}{}
	for _, item := range config.Use {
		if _, skip := ignored[item]; skip {
			continue
		}
		var resource Resource
		if decl, ok := resolveVisibleSkill(skills, item, ref); ok {
			resource = Resource{Name: decl.Name, Block: decl.BlockIndex + 1, Path: decl.Path}
		} else {
			resource = Resource{Name: filepath.Base(filepath.Clean(item)), Path: item}
		}
		if _, exists := seen[resource.Name]; exists {
			continue
		}
		seen[resource.Name] = struct{}{}
		out = append(out, resource)
	}
	return out
}

func buildTaskMCPResourceView(mcps []compiler.MCPDecl, config compiler.MCPTaskConfig, ref definitionScopeRef) []Resource {
	if config.IgnoreAll {
		return nil
	}
	ignored := planStringSet(config.Ignore)
	var out []Resource
	seen := map[string]struct{}{}
	for _, name := range config.Use {
		if _, skip := ignored[name]; skip {
			continue
		}
		decl, ok := resolveVisibleMCP(mcps, name, ref)
		if !ok {
			continue
		}
		if _, exists := seen[decl.Name]; exists {
			continue
		}
		seen[decl.Name] = struct{}{}
		out = append(out, Resource{Name: decl.Name, Block: decl.BlockIndex + 1})
	}
	return out
}

func buildTaskDefMCPResourceView(defs []compiler.Definition, config compiler.MCPTaskConfig, ref definitionScopeRef) []DefinitionResource {
	if config.IgnoreAll {
		return nil
	}
	var out []DefinitionResource
	seen := map[string]struct{}{}
	for _, name := range config.DefUse {
		def, ok := resolveVisibleDefinition(defs, name, ref)
		if !ok {
			continue
		}
		if _, exists := seen[def.Name]; exists {
			continue
		}
		seen[def.Name] = struct{}{}
		out = append(out, DefinitionResource{Name: def.Name, Block: def.BlockIndex + 1, Params: slices.Clone(def.Params)})
	}
	return out
}

func planStringSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func formatTaskResourceView(resources Resources) string {
	var groups []string
	if len(resources.DBs) > 0 {
		var parts []string
		for _, db := range resources.DBs {
			parts = append(parts, fmt.Sprintf("%s(%s,%s/%s)", db.Name, db.Access, db.Scope, db.Persist))
		}
		groups = append(groups, "db="+strings.Join(parts, ", "))
	}
	if len(resources.Skills) > 0 {
		var parts []string
		for _, skill := range resources.Skills {
			parts = append(parts, skill.Name)
		}
		groups = append(groups, "skill="+strings.Join(parts, ", "))
	}
	if len(resources.MCPs) > 0 {
		var parts []string
		for _, mcp := range resources.MCPs {
			parts = append(parts, mcp.Name)
		}
		groups = append(groups, "mcp="+strings.Join(parts, ", "))
	}
	if len(resources.DefMCPs) > 0 {
		var parts []string
		for _, def := range resources.DefMCPs {
			parts = append(parts, def.Name)
		}
		groups = append(groups, "def-mcp="+strings.Join(parts, ", "))
	}
	return strings.Join(groups, "; ")
}
