package plan

import (
	"cmp"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/lang/document"
	langformat "github.com/chinaykc/atm/pkg/lang/format"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

func writePlanHTMLFile(path string, plan compiler.Plan, content string) error {
	if err := os.WriteFile(path, []byte(renderPlanHTML(plan, content)), 0o644); err != nil {
		return fmt.Errorf("write plan HTML %q: %w", path, err)
	}
	return nil
}

func renderPlanHTML(plan compiler.Plan, content string) string {
	data := buildPlanAppData(plan, content)
	encoded, err := json.Marshal(data)
	if err != nil {
		encoded = []byte(`{"error":"marshal plan data failed"}`)
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<link rel=\"icon\" href=\"data:,\">\n")
	b.WriteString("<title>ATM Plan - " + escape(plan.SourcePath) + "</title>\n")
	b.WriteString(planAppCSS())
	b.WriteString("</head>\n<body>\n")
	b.WriteString("<div id=\"app\"></div>\n")
	b.WriteString("<script id=\"plan-data\" type=\"application/json\">")
	b.Write(encoded)
	b.WriteString("</script>\n")
	b.WriteString(planAppJS())
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

type planHTMLView struct {
	plan           compiler.Plan
	blocks         []compiler.Block
	taskByBlock    map[int]taskRef
	controlByBlock map[int]compiler.ControlBlock
	resources      map[int][]resourceChip
	waitJoins      map[int][]asyncEdge
	implicitWaits  []asyncEdge
	taskCalls      map[int][]callRef
	defCalls       map[string][]string
	defCallers     map[string][]string
	vars           map[string]compiler.GlobalBinding
	varUsers       map[string][]string
	importDocs     []importDoc
	fanoutCount    int
	waitCount      int
}

type taskRef struct {
	Number int
	Task   compiler.Task
}

type resourceChip struct {
	Kind  string
	Label string
}

type asyncEdge struct {
	FromBlock int
	FromTask  int
	ToBlock   int
	Pool      string
	Fanout    string
	Implicit  bool
}

type callRef struct {
	Name   string
	Assign string
	Source string
}

type importDoc struct {
	Decl    compiler.ImportDecl
	Content string
	Error   string
}

type planAppData struct {
	Source      string            `json:"source"`
	Stats       map[string]int    `json:"stats"`
	Resources   appResources      `json:"resources"`
	Imports     []appImport       `json:"imports"`
	Definitions []appDefinition   `json:"definitions"`
	Tasks       []appTask         `json:"tasks"`
	Blocks      []appBlock        `json:"blocks"`
	Flow        []appFlowNode     `json:"flow"`
	Trace       map[string]string `json:"trace"`
}

type appResources struct {
	Variables []appVariable `json:"variables"`
	Pools     []appResource `json:"pools"`
	DBs       []appResource `json:"dbs"`
	Skills    []appResource `json:"skills"`
	MCPs      []appResource `json:"mcps"`
}

type appResource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Block  int    `json:"block"`
}

type appVariable struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Value  string   `json:"value"`
	Bash   string   `json:"bash,omitempty"`
	Block  int      `json:"block"`
	UsedBy []string `json:"usedBy,omitempty"`
}

type appImport struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Namespace string `json:"namespace,omitempty"`
	Block     int    `json:"block"`
	Content   string `json:"content,omitempty"`
	Error     string `json:"error,omitempty"`
}

type appDefinition struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Params   []string `json:"params,omitempty"`
	Source   string   `json:"source,omitempty"`
	Block    int      `json:"block"`
	Blocks   []string `json:"blocks"`
	Calls    []string `json:"calls,omitempty"`
	CalledBy []string `json:"calledBy,omitempty"`
}

type appTask struct {
	ID          string      `json:"id"`
	Number      int         `json:"number"`
	Block       int         `json:"block"`
	ParentBlock int         `json:"parentBlock,omitempty"`
	ChildBlocks []int       `json:"childBlocks,omitempty"`
	Title       string      `json:"title"`
	Prompt      string      `json:"prompt"`
	Ops         []appOp     `json:"ops"`
	Calls       []appCall   `json:"calls,omitempty"`
	Variables   []appVarRef `json:"variables,omitempty"`
	Output      *appOutput  `json:"output,omitempty"`
	Joins       []appJoin   `json:"joins,omitempty"`
}

type appOp struct {
	Kind   string   `json:"kind"`
	Label  string   `json:"label"`
	Detail string   `json:"detail,omitempty"`
	Name   string   `json:"name,omitempty"`
	Assign string   `json:"assign,omitempty"`
	Args   []string `json:"args,omitempty"`
	Script string   `json:"script,omitempty"`
	Pool   string   `json:"pool,omitempty"`
	Lanes  []string `json:"lanes,omitempty"`
}

type appCall struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Target string `json:"target,omitempty"`
	Source string `json:"source"`
}

type appVarRef struct {
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	Kind   string `json:"kind"`
}

type appOutput struct {
	Label  string `json:"label"`
	Schema string `json:"schema,omitempty"`
}

type appJoin struct {
	From   string `json:"from"`
	Pool   string `json:"pool"`
	Fanout string `json:"fanout"`
}

type appBlock struct {
	ID          string      `json:"id"`
	Number      int         `json:"number"`
	Prefix      string      `json:"prefix,omitempty"`
	Body        string      `json:"body"`
	Sep         string      `json:"sep,omitempty"`
	Kind        string      `json:"kind"`
	Task        string      `json:"task,omitempty"`
	ParentBlock int         `json:"parentBlock,omitempty"`
	ChildBlocks []int       `json:"childBlocks,omitempty"`
	Prompt      string      `json:"prompt,omitempty"`
	Control     string      `json:"control,omitempty"`
	Ops         []appOp     `json:"ops,omitempty"`
	Calls       []appCall   `json:"calls,omitempty"`
	Vars        []appVarRef `json:"vars,omitempty"`
	Links       []appLink   `json:"links,omitempty"`
	Children    []string    `json:"children,omitempty"`
}

type appLink struct {
	Label  string `json:"label"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type appFlowNode struct {
	ID       string        `json:"id"`
	Kind     string        `json:"kind"`
	Label    string        `json:"label"`
	Detail   string        `json:"detail,omitempty"`
	Target   string        `json:"target,omitempty"`
	Lanes    []string      `json:"lanes,omitempty"`
	Joins    []appJoin     `json:"joins,omitempty"`
	Branches []appFlowPath `json:"branches,omitempty"`
}

type appFlowPath struct {
	Label string        `json:"label"`
	Nodes []appFlowNode `json:"nodes"`
}

func buildPlanAppData(plan compiler.Plan, content string) planAppData {
	view := buildPlanHTMLView(plan, content)
	blockLinks := make(map[int][]appLink)
	data := planAppData{
		Source: plan.SourcePath,
		Stats: map[string]int{
			"tasks":       len(plan.Tasks),
			"branches":    len(plan.Controls),
			"fanouts":     view.fanoutCount,
			"joins":       view.waitCount,
			"unjoined":    len(view.implicitWaits),
			"definitions": len(plan.Definitions),
			"pools":       len(plan.Pools),
			"databases":   len(plan.DBs),
			"variables":   len(plan.Globals),
		},
		Trace: make(map[string]string),
	}
	for _, item := range plan.Globals {
		id := "var-" + anchorID(item.Name)
		data.Resources.Variables = append(data.Resources.Variables, appVariable{
			ID:     id,
			Name:   item.Name,
			Value:  item.Value,
			Bash:   strings.TrimSpace(item.BashScript),
			Block:  item.BlockIndex + 1,
			UsedBy: view.varUsers[item.Name],
		})
		data.Trace[id] = "Variable " + item.Name
		blockLinks[item.BlockIndex] = append(blockLinks[item.BlockIndex], appLink{Label: "/var " + item.Name, Target: id, Kind: "var"})
	}
	for _, item := range plan.Pools {
		detail := fmt.Sprintf("max %d", item.Max)
		if item.Buffer >= 0 {
			detail += fmt.Sprintf(", buffer %d", item.Buffer)
		}
		id := "pool-" + anchorID(item.Name)
		data.Resources.Pools = append(data.Resources.Pools, appResource{ID: id, Name: item.Name, Detail: detail, Block: item.BlockIndex + 1})
		data.Trace[id] = "Pool " + item.Name
		blockLinks[item.BlockIndex] = append(blockLinks[item.BlockIndex], appLink{Label: "/pool " + item.Name, Target: id, Kind: "pool"})
	}
	for _, item := range plan.DBs {
		id := "db-" + anchorID(item.Name)
		data.Resources.DBs = append(data.Resources.DBs, appResource{ID: id, Name: item.Name, Detail: fmt.Sprintf("%s/%s %s", item.Scope, item.Persist, item.Access), Block: item.BlockIndex + 1})
		data.Trace[id] = "Database " + item.Name
		blockLinks[item.BlockIndex] = append(blockLinks[item.BlockIndex], appLink{Label: "/db " + item.Name, Target: id, Kind: "db"})
	}
	for _, item := range plan.Skills {
		id := "skill-" + anchorID(item.Name)
		data.Resources.Skills = append(data.Resources.Skills, appResource{ID: id, Name: item.Name, Detail: item.Path, Block: item.BlockIndex + 1})
		data.Trace[id] = "Skill " + item.Name
		blockLinks[item.BlockIndex] = append(blockLinks[item.BlockIndex], appLink{Label: "/skill " + item.Name, Target: id, Kind: "skill"})
	}
	for _, item := range plan.MCPs {
		id := "mcp-" + anchorID(item.Name)
		data.Resources.MCPs = append(data.Resources.MCPs, appResource{ID: id, Name: item.Name, Detail: item.Config, Block: item.BlockIndex + 1})
		data.Trace[id] = "MCP " + item.Name
		blockLinks[item.BlockIndex] = append(blockLinks[item.BlockIndex], appLink{Label: "/mcp " + item.Name, Target: id, Kind: "mcp"})
	}
	for i, doc := range view.importDocs {
		label := doc.Decl.Path
		if doc.Decl.Namespace != "" {
			label = doc.Decl.Namespace + " from " + doc.Decl.Path
		}
		id := fmt.Sprintf("import-%d", i+1)
		data.Imports = append(data.Imports, appImport{ID: id, Label: label, Path: doc.Decl.Path, Namespace: doc.Decl.Namespace, Block: doc.Decl.BlockIndex + 1, Content: doc.Content, Error: doc.Error})
		data.Trace[id] = "Import " + label
		blockLinks[doc.Decl.BlockIndex] = append(blockLinks[doc.Decl.BlockIndex], appLink{Label: "/import " + label, Target: id, Kind: "import"})
	}
	defs := slices.Clone(plan.Definitions)
	slices.SortFunc(defs, func(a, b compiler.Definition) int { return cmp.Compare(a.Name, b.Name) })
	for _, def := range defs {
		id := "def-" + anchorID(def.Name)
		data.Definitions = append(data.Definitions, appDefinition{
			ID:       id,
			Name:     def.Name,
			Params:   slices.Clone(def.Params),
			Source:   def.SourcePath,
			Block:    def.BlockIndex + 1,
			Blocks:   definitionBlockBodies(def),
			Calls:    view.defCalls[def.Name],
			CalledBy: view.defCallers[def.Name],
		})
		data.Trace[id] = "Definition " + def.Name
	}
	for _, ref := range sortedTasks(view.taskByBlock) {
		task := ref.Task
		id := fmt.Sprintf("task-%d", ref.Number)
		taskData := appTask{
			ID:          id,
			Number:      ref.Number,
			Block:       task.BlockIndex + 1,
			ParentBlock: parentBlockNumber(task),
			ChildBlocks: childTaskBlockNumbers(plan.Tasks, task.BlockIndex),
			Title:       taskTitle(task),
			Prompt:      strings.TrimSpace(task.Prompt),
			Ops:         appOps(task),
			Calls:       appCalls(view.taskCalls[task.BlockIndex]),
			Variables:   appVarRefs(task, view),
			Joins:       appJoins(view.waitJoins[task.BlockIndex]),
		}
		if task.Output != nil {
			taskData.Output = &appOutput{Label: formatPlanOutput(task.Output), Schema: prettySchema(task.Output.Schema)}
		}
		data.Tasks = append(data.Tasks, taskData)
		data.Trace[id] = fmt.Sprintf("Task %d", ref.Number)
	}
	for i, block := range view.blocks {
		item := appBlock{ID: fmt.Sprintf("block-%d", i+1), Number: i + 1, Prefix: block.Prefix, Body: block.Body, Sep: block.Sep, Kind: "block"}
		data.Trace[item.ID] = fmt.Sprintf("Block %d", i+1)
		if ref, ok := view.taskByBlock[i]; ok {
			item.Kind = "task"
			item.Task = fmt.Sprintf("task-%d", ref.Number)
			item.ParentBlock = parentBlockNumber(ref.Task)
			item.ChildBlocks = childTaskBlockNumbers(plan.Tasks, ref.Task.BlockIndex)
			item.Prompt = strings.TrimSpace(ref.Task.Prompt)
			item.Ops = appOps(ref.Task)
			item.Calls = appCalls(view.taskCalls[i])
			item.Vars = appVarRefs(ref.Task, view)
		}
		if control, ok := view.controlByBlock[i]; ok {
			item.Kind = "control"
			item.Control = displayControlBlock(control)
		}
		if len(view.resources[i]) > 0 && item.Kind == "block" {
			item.Kind = "resource"
		}
		item.Links = append(item.Links, blockLinks[i]...)
		data.Blocks = append(data.Blocks, item)
	}
	data.Flow = appFlowRange(view, 0, len(view.blocks))
	data.Flow = append([]appFlowNode{{ID: "start", Kind: "start", Label: "Start", Detail: plan.SourcePath}}, data.Flow...)
	if len(view.implicitWaits) > 0 {
		data.Flow = append(data.Flow, appFlowNode{ID: "unjoined-background", Kind: "wait", Label: "Unjoined background work", Detail: "No implicit /wait will be inserted.", Joins: appJoins(view.implicitWaits)})
	}
	data.Flow = append(data.Flow, appFlowNode{ID: "end", Kind: "end", Label: "End", Detail: "Plan mode does not execute tools or bash."})
	return data
}

func appOps(task compiler.Task) []appOp {
	var out []appOp
	for _, op := range ir.FlattenTaskFlow(task) {
		item := appOp{Kind: string(op.Kind), Label: displayPlanOp(op), Detail: formatPlanOp(op)}
		switch op.Kind {
		case compiler.FlatOpBash:
			item.Name = op.Bash.Name
			item.Script = strings.TrimSpace(op.Bash.Script)
		case compiler.FlatOpCall:
			item.Name = op.Call.Name
			item.Assign = op.Call.Assign
			item.Args = slices.Clone(op.Call.Args)
		case compiler.FlatOpGo:
			item.Pool = op.Pool
			item.Lanes = fanoutLabels(task)
		case compiler.FlatOpWait:
			item.Pool = op.Pool
		}
		out = append(out, item)
	}
	return out
}

func definitionBlockBodies(def compiler.Definition) []string {
	bodies := make([]string, 0, len(def.Blocks))
	for _, block := range def.Blocks {
		bodies = append(bodies, block.Body)
	}
	return bodies
}

func parentBlockNumber(task compiler.Task) int {
	if task.HasParent {
		return task.ParentIndex + 1
	}
	return 0
}

func fanoutLabels(task compiler.Task) []string {
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind != compiler.FlatOpFor {
			continue
		}
		if len(op.For.Values) > 0 {
			return slices.Clone(op.For.Values)
		}
		if op.For.MaxRuns > 0 && op.For.MaxRuns <= 20 {
			var values []string
			for i := 1; i <= op.For.MaxRuns; i++ {
				values = append(values, fmt.Sprintf("%d", i))
			}
			return values
		}
		return []string{"dynamic"}
	}
	return nil
}

func appCalls(calls []callRef) []appCall {
	var out []appCall
	for _, call := range calls {
		label := call.Name
		if call.Assign != "" {
			label += " -> " + call.Assign
		}
		out = append(out, appCall{Name: call.Name, Label: label, Target: "def-" + anchorID(call.Name), Source: call.Source})
	}
	return out
}

func appVarRefs(task compiler.Task, view planHTMLView) []appVarRef {
	var refs []appVarRef
	for _, name := range taskVariableRefs(task) {
		if _, ok := view.vars[name]; ok {
			refs = append(refs, appVarRef{Name: name, Target: "var-" + anchorID(name), Kind: "global"})
		} else if kind, ok := taskLocalVarKind(task, name); ok {
			refs = append(refs, appVarRef{Name: name, Target: fmt.Sprintf("block-%d", task.BlockIndex+1), Kind: kind})
		} else {
			refs = append(refs, appVarRef{Name: name, Kind: "unresolved"})
		}
	}
	return refs
}

func appJoins(edges []asyncEdge) []appJoin {
	var joins []appJoin
	for _, edge := range edges {
		pool := edge.Pool
		if pool == "" {
			pool = "default"
		}
		joins = append(joins, appJoin{From: fmt.Sprintf("task-%d", edge.FromTask), Pool: pool, Fanout: edge.Fanout})
	}
	return joins
}

func appFlowRange(view planHTMLView, start, end int) []appFlowNode {
	var nodes []appFlowNode
	for i := start; i < end && i < len(view.blocks); {
		if strings.TrimSpace(view.blocks[i].Body) == "" || marker.IsDone(view.blocks[i].Body) {
			i++
			continue
		}
		if group, ok := htmlParseConditionalGroup(view.blocks, i); ok {
			nodes = append(nodes, appBranchNode(view, group))
			if group.end <= i {
				i++
			} else {
				i = group.end
			}
			continue
		}
		if node, ok := appBlockFlowNode(view, i); ok {
			nodes = append(nodes, node)
		}
		i++
	}
	return nodes
}

func appBranchNode(view planHTMLView, group htmlConditionalGroup) appFlowNode {
	node := appFlowNode{ID: fmt.Sprintf("flow-branch-%d", group.ifIndex+1), Kind: "branch", Label: displayCondition(group.ifBlock.Condition), Target: fmt.Sprintf("block-%d", group.ifIndex+1)}
	thenNodes := []appFlowNode{}
	if group.ifBlock.HeaderOnly {
		thenNodes = appFlowRange(view, group.thenStart, group.thenEnd)
	} else if thenNode, ok := appBlockFlowNode(view, group.ifIndex); ok {
		thenNodes = []appFlowNode{thenNode}
	}
	elseNodes := []appFlowNode{}
	if group.hasElse {
		if group.elseBlock.HeaderOnly {
			elseNodes = appFlowRange(view, group.elseStart, group.elseEnd)
		} else if elseNode, ok := appBlockFlowNode(view, group.elseIndex); ok {
			elseNodes = []appFlowNode{elseNode}
		}
	}
	node.Branches = []appFlowPath{{Label: "true", Nodes: thenNodes}, {Label: "false", Nodes: elseNodes}}
	return node
}

func appBlockFlowNode(view planHTMLView, block int) (appFlowNode, bool) {
	if ref, ok := view.taskByBlock[block]; ok {
		task := ref.Task
		kind := "task"
		if _, ok := taskGoPool(task); ok {
			kind = "fanout"
		}
		if taskHasWait(task) {
			kind = "wait"
		}
		label := fmt.Sprintf("Task %d", ref.Number)
		detail := taskTitle(task)
		if task.HasParent {
			detail += fmt.Sprintf(" · child of block %d", task.ParentIndex+1)
		} else if children := childTaskBlockNumbers(view.plan.Tasks, task.BlockIndex); len(children) > 0 {
			detail += " · waits for child blocks " + formatBlockNumberList(children)
		}
		return appFlowNode{ID: fmt.Sprintf("flow-task-%d", ref.Number), Kind: kind, Label: label, Detail: detail, Target: fmt.Sprintf("task-%d", ref.Number), Lanes: fanoutLabels(task), Joins: appJoins(view.waitJoins[block])}, true
	}
	if chips := view.resources[block]; len(chips) > 0 {
		var labels []string
		for _, chip := range chips {
			labels = append(labels, chip.Kind+" "+chip.Label)
		}
		return appFlowNode{ID: fmt.Sprintf("flow-resource-%d", block+1), Kind: "resource", Label: fmt.Sprintf("Resource block %d", block+1), Detail: joinSummary(labels), Target: fmt.Sprintf("block-%d", block+1)}, true
	}
	if control, ok := view.controlByBlock[block]; ok {
		return appFlowNode{ID: fmt.Sprintf("flow-control-%d", block+1), Kind: "control", Label: displayControlBlock(control), Target: fmt.Sprintf("block-%d", block+1)}, true
	}
	return appFlowNode{}, false
}

func buildPlanHTMLView(plan compiler.Plan, content string) planHTMLView {
	blocks := document.ParseBlocks(content)
	view := planHTMLView{
		plan:           plan,
		blocks:         blocks,
		taskByBlock:    make(map[int]taskRef),
		controlByBlock: make(map[int]compiler.ControlBlock),
		resources:      make(map[int][]resourceChip),
		waitJoins:      make(map[int][]asyncEdge),
		taskCalls:      make(map[int][]callRef),
		defCalls:       make(map[string][]string),
		defCallers:     make(map[string][]string),
		vars:           make(map[string]compiler.GlobalBinding),
		varUsers:       make(map[string][]string),
	}
	for i, task := range plan.Tasks {
		view.taskByBlock[task.BlockIndex] = taskRef{Number: i + 1, Task: task}
		view.taskCalls[task.BlockIndex] = taskCallRefs(task)
		for _, call := range view.taskCalls[task.BlockIndex] {
			view.defCallers[call.Name] = append(view.defCallers[call.Name], fmt.Sprintf("task %d", i+1))
		}
		for _, name := range taskVariableRefs(task) {
			view.varUsers[name] = append(view.varUsers[name], fmt.Sprintf("task %d", i+1))
		}
	}
	for _, control := range plan.Controls {
		view.controlByBlock[control.BlockIndex] = control
	}
	root := "."
	if plan.SourcePath != "" {
		root = filepath.Dir(plan.SourcePath)
	}
	for _, item := range plan.Imports {
		label := item.Path
		if item.Namespace != "" {
			label = item.Namespace + " from " + item.Path
		}
		view.addResource(item.BlockIndex, "import", label)
		view.importDocs = append(view.importDocs, readImportDoc(root, item))
	}
	for _, item := range plan.Globals {
		label := item.Name
		if item.BashScript != "" {
			label += " /bash"
		}
		view.addResource(item.BlockIndex, "var", label)
		view.vars[item.Name] = item
	}
	for _, item := range plan.Pools {
		label := fmt.Sprintf("%s max=%d", item.Name, item.Max)
		if item.Buffer >= 0 {
			label += fmt.Sprintf(" buffer=%d", item.Buffer)
		}
		view.addResource(item.BlockIndex, "pool", label)
	}
	for _, item := range plan.DBs {
		view.addResource(item.BlockIndex, "db", fmt.Sprintf("%s %s/%s %s", item.Name, item.Scope, item.Persist, item.Access))
	}
	for _, item := range plan.Skills {
		view.addResource(item.BlockIndex, "skill", item.Name+" from "+item.Path)
	}
	for _, item := range plan.MCPs {
		view.addResource(item.BlockIndex, "mcp", item.Name)
	}
	for _, def := range plan.Definitions {
		view.defCalls[def.Name] = uniqueStrings(compiler.DefinitionCalls(def))
		for _, call := range view.defCalls[def.Name] {
			view.defCallers[call] = append(view.defCallers[call], "def "+def.Name)
		}
	}
	for name := range view.defCallers {
		view.defCallers[name] = uniqueStrings(view.defCallers[name])
	}
	for name := range view.varUsers {
		view.varUsers[name] = uniqueStrings(view.varUsers[name])
	}
	view.buildAsyncEdges()
	return view
}

func readImportDoc(root string, decl compiler.ImportDecl) importDoc {
	path := decl.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return importDoc{Decl: decl, Error: err.Error()}
	}
	return importDoc{Decl: decl, Content: string(data)}
}

func (v *planHTMLView) addResource(block int, kind, label string) {
	v.resources[block] = append(v.resources[block], resourceChip{Kind: kind, Label: label})
}

func (v *planHTMLView) buildAsyncEdges() {
	type pendingBranch struct {
		block  int
		task   int
		pool   string
		fanout string
	}
	var pending []pendingBranch
	for _, ref := range sortedTasks(v.taskByBlock) {
		task := ref.Task
		for _, op := range ir.FlattenTaskFlow(task) {
			if op.Kind != compiler.FlatOpWait {
				continue
			}
			v.waitCount++
			var rest []pendingBranch
			for _, branch := range pending {
				if op.Pool == "" || branch.pool == op.Pool {
					v.waitJoins[task.BlockIndex] = append(v.waitJoins[task.BlockIndex], asyncEdge{
						FromBlock: branch.block,
						FromTask:  branch.task,
						ToBlock:   task.BlockIndex,
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
			v.fanoutCount++
			pending = append(pending, pendingBranch{
				block:  task.BlockIndex,
				task:   ref.Number,
				pool:   pool,
				fanout: taskFanoutSummary(task),
			})
		}
	}
	for _, branch := range pending {
		v.implicitWaits = append(v.implicitWaits, asyncEdge{
			FromBlock: branch.block,
			FromTask:  branch.task,
			ToBlock:   -1,
			Pool:      branch.pool,
			Fanout:    branch.fanout,
			Implicit:  true,
		})
	}
	if len(v.implicitWaits) > 0 {
		// These are intentionally not counted as joins: v2 does not insert an implicit final /wait.
	}
}

func sortedTasks(tasks map[int]taskRef) []taskRef {
	out := make([]taskRef, 0, len(tasks))
	for _, ref := range tasks {
		out = append(out, ref)
	}
	slices.SortFunc(out, func(a, b taskRef) int {
		return cmp.Compare(a.Task.BlockIndex, b.Task.BlockIndex)
	})
	return out
}

func taskGoPool(task compiler.Task) (string, bool) {
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind == compiler.FlatOpGo {
			return op.Pool, true
		}
	}
	return "", false
}

func taskHasWait(task compiler.Task) bool {
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind == compiler.FlatOpWait {
			return true
		}
	}
	return false
}

func taskFanoutSummary(task compiler.Task) string {
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind == compiler.FlatOpFor {
			return formatForIR(op.For)
		}
	}
	return "single background branch"
}

func taskCallRefs(task compiler.Task) []callRef {
	var calls []callRef
	for _, op := range ir.FlattenTaskFlow(task) {
		if op.Kind == compiler.FlatOpCall {
			source := "command"
			if op.Call.Assign != "" {
				source = "let"
			}
			calls = append(calls, callRef{Name: op.Call.Name, Assign: op.Call.Assign, Source: source})
		}
		if op.Kind == compiler.FlatOpFor && op.For.Source.Kind == compiler.ConditionCall {
			if call, err := compiler.ParseCallExpression(op.For.Source.Text); err == nil {
				calls = append(calls, callRef{Name: call.Name, Source: "fanout"})
			}
		}
	}
	return calls
}

func taskVariableRefs(task compiler.Task) []string {
	var values []string
	values = append(values, compiler.TemplateVarNames(task.Prompt)...)
	if task.Output != nil {
		values = append(values, compiler.TemplateVarNames(task.Output.FileName+" "+task.Output.Schema)...)
	}
	return uniqueStrings(values)
}

type htmlConditionalGroup struct {
	ifIndex   int
	ifBlock   compiler.IfBlock
	thenStart int
	thenEnd   int
	hasElse   bool
	elseIndex int
	elseBlock compiler.ElseBlock
	elseStart int
	elseEnd   int
	end       int
}

func htmlParseConditionalGroup(blocks []compiler.Block, index int) (htmlConditionalGroup, bool) {
	ifBlock, ok, err := compiler.ParseIfBlock(blocks[index].Body)
	if err != nil || !ok {
		return htmlConditionalGroup{}, false
	}
	group := htmlConditionalGroup{ifIndex: index, ifBlock: ifBlock}
	if !ifBlock.HeaderOnly {
		group.thenStart = index
		group.thenEnd = index + 1
		group.end = index + 1
		if index+1 < len(blocks) && !marker.IsDone(blocks[index+1].Body) {
			if elseBlock, ok, err := compiler.ParseElseBlock(blocks[index+1].Body); err == nil && ok {
				group.hasElse = true
				group.elseIndex = index + 1
				group.elseBlock = elseBlock
				if elseBlock.HeaderOnly {
					group.elseStart = index + 2
					group.elseEnd = htmlNodeEnd(blocks, index+2)
					group.end = group.elseEnd
				} else {
					group.elseStart = index + 1
					group.elseEnd = index + 2
					group.end = index + 2
				}
			}
		}
		return group, true
	}
	group.thenStart = index + 1
	group.thenEnd = htmlNodeEnd(blocks, index+1)
	if group.thenEnd < len(blocks) {
		if elseBlock, ok, err := compiler.ParseElseBlock(blocks[group.thenEnd].Body); err == nil && ok {
			group.hasElse = true
			group.elseIndex = group.thenEnd
			group.elseBlock = elseBlock
			if elseBlock.HeaderOnly {
				group.elseStart = group.thenEnd + 1
				group.elseEnd = htmlNodeEnd(blocks, group.thenEnd+1)
				group.end = group.elseEnd
			} else {
				group.elseStart = group.thenEnd
				group.elseEnd = group.thenEnd + 1
				group.end = group.thenEnd + 1
			}
			return group, true
		}
	}
	group.end = group.thenEnd
	return group, true
}

func htmlNodeEnd(blocks []compiler.Block, index int) int {
	if index >= len(blocks) {
		return index
	}
	if _, ok, err := compiler.ParseElseBlock(blocks[index].Body); err == nil && ok {
		return index
	}
	if _, ok := htmlParseConditionalGroup(blocks, index); ok {
		group, _ := htmlParseConditionalGroup(blocks, index)
		return group.end
	}
	return index + 1
}

func taskTitle(task compiler.Task) string {
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return langformat.TaskFlow(task)
	}
	if idx := strings.IndexAny(prompt, "\r\n"); idx >= 0 {
		prompt = prompt[:idx]
	}
	return truncateRunes(prompt, 86)
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func displayControlBlock(control compiler.ControlBlock) string {
	if control.Kind == "else" {
		return fmt.Sprintf("else block %d%s", control.BlockIndex+1, headerOnlySuffix(control.HeaderOnly))
	}
	return fmt.Sprintf("if block %d: %s%s", control.BlockIndex+1, displayCondition(control.Condition), headerOnlySuffix(control.HeaderOnly))
}

func displayCondition(condition compiler.Condition) string {
	switch condition.Kind {
	case compiler.ConditionExpr:
		return "expr(" + condition.Text + ")"
	case compiler.ConditionCall:
		return "call(" + condition.Text + ")"
	case compiler.ConditionNatural:
		return condition.Text
	default:
		return "<none>"
	}
}

func displayPlanOp(op compiler.FlatOp) string {
	switch op.Kind {
	case compiler.FlatOpFor:
		return displayForIR(op.For)
	case compiler.FlatOpGo:
		if op.Pool != "" {
			return "dispatch to pool " + op.Pool
		}
		return "dispatch in background"
	case compiler.FlatOpWait:
		if op.Pool != "" {
			return "join pool " + op.Pool
		}
		return "join all previous background work"
	case compiler.FlatOpCall:
		if op.Call.Assign != "" {
			return op.Call.Name + " -> " + op.Call.Assign
		}
		return op.Call.Name
	case compiler.FlatOpExecute:
		return appendRunOptionSummary("execute prompt", op.ExecuteOptions)
	default:
		return formatPlanOp(op)
	}
}

func displayForIR(step compiler.For) string {
	name := step.VarName
	if name == "" {
		name = "run"
	}
	if step.Source.Kind == compiler.ConditionCall {
		return fmt.Sprintf("for %s in %s", name, step.Source.Text)
	}
	if step.Source.Kind == compiler.ConditionExpr {
		return fmt.Sprintf("for %s in expr(%s)", name, step.Source.Text)
	}
	if len(step.Values) > 0 {
		return fmt.Sprintf("for %s in [%s]", name, strings.Join(step.Values, ", "))
	}
	if step.MaxRuns > 0 {
		return fmt.Sprintf("for %s x %d", name, step.MaxRuns)
	}
	if step.Condition.Kind == compiler.ConditionExpr {
		return fmt.Sprintf("for %s until expr(%s)", name, step.Condition.Text)
	}
	return formatForIR(step)
}

func taskLocalVarKind(task compiler.Task, name string) (string, bool) {
	for _, op := range ir.FlattenTaskFlow(task) {
		switch op.Kind {
		case compiler.FlatOpCall:
			if op.Call.Assign == name {
				return "call result", true
			}
		case compiler.FlatOpBash:
			if op.Bash.Name == name {
				return "bash capture", true
			}
		case compiler.FlatOpFor:
			if op.For.VarName == name || (op.For.VarName == "" && name == "n") {
				return "loop", true
			}
		}
	}
	return "", false
}

func prettySchema(schema string) string {
	var value any
	if err := json.Unmarshal([]byte(schema), &value); err != nil {
		return schema
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return schema
	}
	return string(out)
}

func anchorID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func joinSummary(parts []string) string {
	if len(parts) > 4 {
		return strings.Join(parts[:4], ", ") + fmt.Sprintf(", +%d more", len(parts)-4)
	}
	return strings.Join(parts, ", ")
}

func openBrowser(path string) error {
	target := "file://" + path
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func escape(s string) string {
	return html.EscapeString(s)
}

func planAppCSS() string {
	return `<style>
:root {
  color-scheme: light;
  --bg: #f5f7fb;
  --panel: #ffffff;
  --ink: #162033;
  --muted: #66748a;
  --line: #d8e1ee;
  --blue: #155eef;
  --green: #067647;
  --amber: #b54708;
  --red: #b42318;
  --violet: #7f56d9;
  --map-col: 37vw;
}
* { box-sizing: border-box; }
html { scroll-behavior: smooth; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--ink);
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  letter-spacing: 0;
}
button, input { font: inherit; }
.app-header {
  position: sticky;
  top: 0;
  z-index: 20;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 16px;
  align-items: center;
  padding: 16px 22px;
  border-bottom: 1px solid var(--line);
  background: rgba(255,255,255,.94);
  backdrop-filter: blur(12px);
}
.app-title h1 { margin: 0 0 3px; font-size: 24px; }
.app-subtitle { color: var(--muted); font-size: 13px; overflow-wrap: anywhere; }
.toolbar { display: flex; align-items: center; gap: 8px; }
.search {
  width: min(34vw, 420px);
  min-width: 220px;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 8px 10px;
  background: #fff;
  color: var(--ink);
}
.tool-btn {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 8px 10px;
  background: #fff;
  color: #344054;
  font-weight: 700;
  cursor: pointer;
}
.tool-btn:hover { border-color: #a7b7ce; background: #f8fafc; }
.stats {
  display: grid;
  grid-template-columns: repeat(8, minmax(90px, 1fr));
  gap: 10px;
  padding: 14px 20px 0;
}
.stat {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 10px 12px;
  background: var(--panel);
}
.stat strong { display: block; font-size: 21px; line-height: 1.1; }
.stat span { display: block; margin-top: 3px; color: var(--muted); font-size: 12px; }
.workspace {
  display: grid;
  grid-template-columns: minmax(300px, var(--map-col)) 8px minmax(0, 1fr);
  gap: 10px;
  padding: 16px 20px 36px;
  align-items: start;
}
.splitter {
  position: sticky;
  top: 88px;
  height: calc(100vh - 104px);
  border-radius: 999px;
  background: #c8d3e3;
  cursor: col-resize;
}
.splitter:hover, body.resizing .splitter { background: #66748a; }
.panel {
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--panel);
  box-shadow: 0 10px 24px rgba(16, 24, 40, .05);
  overflow: hidden;
}
.panel-title {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  margin: 0;
  padding: 12px 14px;
  border-bottom: 1px solid #e8edf5;
  font-size: 15px;
}
.map-panel {
  position: sticky;
  top: 88px;
  max-height: calc(100vh - 104px);
  overflow: auto;
}
.map-body { padding: 14px; }
.flow-node {
  position: relative;
  display: block;
  width: 100%;
  margin: 0 0 12px;
  border: 1px solid #d9e3f0;
  border-left: 5px solid var(--blue);
  border-radius: 8px;
  padding: 10px 11px;
  background: #fff;
  color: inherit;
  text-align: left;
  text-decoration: none;
  cursor: pointer;
}
.flow-node::after {
  content: "";
  position: absolute;
  left: 18px;
  bottom: -13px;
  width: 2px;
  height: 12px;
  background: #cbd5e1;
}
.flow-node:last-child::after, .branch-box .flow-node::after { display: none; }
.flow-node.resource { border-left-color: #0e9384; }
.flow-node.branch { border-left-color: var(--green); }
.flow-node.fanout { border-left-color: var(--violet); background: #fcfaff; }
.flow-node.wait { border-left-color: var(--amber); background: #fffcf5; }
.flow-node.end { border-left-color: var(--red); }
.flow-node:hover { border-color: #9db1cb; }
.setup-box {
  margin: 0 0 12px;
  border: 1px solid #d9e3f0;
  border-radius: 8px;
  background: #f8fafc;
  overflow: hidden;
}
.setup-box > summary {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
  padding: 10px 11px;
  cursor: pointer;
}
.setup-title { font-weight: 900; }
.setup-list {
  display: grid;
  gap: 7px;
  padding: 0 10px 10px;
}
.setup-item {
  display: block;
  width: 100%;
  border: 1px solid #d9e3f0;
  border-left: 4px solid #0e9384;
  border-radius: 8px;
  padding: 8px 9px;
  background: #fff;
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.setup-item:hover { border-color: #9db1cb; }
.dispatch-node { padding-bottom: 12px; }
.dispatch-row {
  display: grid;
  grid-template-columns: minmax(92px, .42fr) minmax(0, 1fr);
  gap: 10px;
  align-items: stretch;
  margin-top: 10px;
}
.dispatch-core, .join-core {
  display: grid;
  place-items: center;
  min-height: 44px;
  border-radius: 8px;
  border: 1px solid #d6bbfb;
  background: #f4ebff;
  color: #6941c6;
  font-size: 11px;
  font-weight: 900;
  text-transform: uppercase;
}
.dispatch-lanes {
  position: relative;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(78px, 1fr));
  gap: 6px;
  align-content: center;
}
.dispatch-lanes::before {
  content: "";
  position: absolute;
  left: -10px;
  top: 50%;
  width: 10px;
  height: 2px;
  background: #b692f6;
}
.join-stack {
  display: grid;
  gap: 6px;
  margin-top: 9px;
}
.join-edge {
  display: grid;
  grid-template-columns: minmax(80px, .35fr) minmax(0, 1fr);
  gap: 8px;
  align-items: center;
  border-radius: 8px;
  border: 1px solid #fedf89;
  background: #fffcf5;
  padding: 7px 8px;
  color: #93370d;
  font-size: 12px;
}
.join-edge strong { font-size: 11px; text-transform: uppercase; }
.node-kind { color: var(--muted); font-size: 11px; font-weight: 900; text-transform: uppercase; }
.node-label { margin-top: 3px; font-weight: 800; line-height: 1.35; overflow-wrap: anywhere; }
.node-detail { margin-top: 4px; color: var(--muted); font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
.branch-box {
  margin: 0 0 12px;
  border: 1px solid #d9e3f0;
  border-radius: 8px;
  padding: 10px;
  background: #f8fafc;
}
.branch-head { margin-bottom: 10px; color: var(--green); font-weight: 900; overflow-wrap: anywhere; }
.branch-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; }
.branch-col {
  min-width: 0;
  border: 1px dashed #cbd5e1;
  border-radius: 8px;
  padding: 8px;
  background: #fff;
}
.branch-label { margin-bottom: 7px; color: var(--muted); font-size: 11px; font-weight: 900; text-transform: uppercase; }
.branch-join {
  margin: 10px auto 0;
  width: max-content;
  max-width: 100%;
  border-radius: 999px;
  padding: 4px 10px;
  background: #ecfdf3;
  color: #067647;
  font-size: 11px;
  font-weight: 900;
}
.lanes { display: flex; gap: 6px; margin-top: 8px; padding-top: 8px; border-top: 1px dashed #d6bbfb; overflow-x: auto; }
.lane {
  flex: 0 0 auto;
  min-width: 68px;
  border-radius: 999px;
  padding: 4px 8px;
  background: #f4ebff;
  color: #6941c6;
  font-size: 11px;
  font-weight: 900;
  text-align: center;
}
.content { display: grid; gap: 14px; min-width: 0; }
.content-nav {
  position: sticky;
  top: 88px;
  z-index: 12;
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(255,255,255,.94);
  backdrop-filter: blur(10px);
}
.nav-chip {
  flex: 0 0 auto;
  border: 1px solid #d9e3f0;
  border-radius: 999px;
  padding: 6px 10px;
  background: #fff;
  color: #344054;
  font-size: 12px;
  font-weight: 900;
  cursor: pointer;
}
.nav-chip:hover { border-color: #9db1cb; background: #f8fafc; }
.panel-anchor { scroll-margin-top: 148px; }
.selection-panel {
  position: sticky;
  top: 142px;
  z-index: 11;
  border: 1px solid #b2ccff;
  border-radius: 8px;
  background: #f8fbff;
  padding: 10px 12px;
  box-shadow: 0 10px 22px rgba(21, 94, 239, .08);
}
.selection-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: flex-start;
}
.selection-title { font-weight: 900; overflow-wrap: anywhere; }
.selection-kind {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 3px 8px;
  background: #d1e9ff;
  color: #175cd3;
  font-size: 11px;
  font-weight: 900;
  text-transform: uppercase;
}
.selection-detail { margin-top: 5px; color: #475467; font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
.selection-links { margin-top: 8px; display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.selection-empty { color: var(--muted); font-size: 12px; }
.section-body { padding: 14px; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 10px; }
.relation-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; }
.relation-card {
  border: 1px solid #d9e3f0;
  border-radius: 8px;
  background: #fff;
  padding: 11px;
  min-width: 0;
}
.relation-card h3 { margin: 0 0 8px; font-size: 13px; }
.card {
  border: 1px solid #d9e3f0;
  border-radius: 8px;
  background: #fff;
  padding: 12px;
  min-width: 0;
}
.card-head { display: flex; justify-content: space-between; gap: 10px; align-items: flex-start; margin-bottom: 8px; }
.card-title { margin: 0; font-size: 15px; line-height: 1.35; overflow-wrap: anywhere; }
.badge {
  display: inline-flex;
  border-radius: 999px;
  padding: 3px 8px;
  background: #eff8ff;
  color: #175cd3;
  font-size: 11px;
  font-weight: 900;
  white-space: nowrap;
}
.muted { color: var(--muted); font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
.op-list { display: grid; gap: 7px; margin: 9px 0 0; padding: 0; list-style: none; }
.op {
  display: grid;
  grid-template-columns: 82px minmax(0, 1fr);
  gap: 10px;
  border: 1px solid #e1e8f2;
  border-left: 4px solid #98a2b3;
  border-radius: 8px;
  padding: 8px 10px;
  background: #fbfdff;
}
.op.for { border-left-color: var(--violet); }
.op.go { border-left-color: #7f56d9; background: #fcfaff; }
.op.wait { border-left-color: var(--amber); background: #fffcf5; }
.op.call { border-left-color: var(--green); background: #f6fef9; }
.op.execute { border-left-color: var(--blue); }
.op-kind { color: var(--muted); font-size: 11px; font-weight: 900; text-transform: uppercase; }
.op-label { min-width: 0; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
.op > .lanes { grid-column: 1 / -1; }
.trace-row { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 9px; }
.trace-pill, .trace-link {
  display: inline-flex;
  align-items: center;
  border-radius: 999px;
  padding: 3px 8px;
  background: #eff8ff;
  color: #175cd3;
  font-size: 11px;
  font-weight: 900;
  text-decoration: none;
  cursor: pointer;
}
.trace-pill.local { background: #ecfdf3; color: #067647; }
.trace-pill.unresolved { background: #f2f4f7; color: #667085; }
.inline-ref {
  display: inline-flex;
  align-items: center;
  border-radius: 6px;
  padding: 0 4px;
  background: #eff8ff;
  color: #175cd3;
  font-weight: 800;
  text-decoration: none;
}
.prompt-rich {
  margin: 9px 0 0;
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  padding: 10px;
  background: #f8fafc;
  color: #344054;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 13px;
  line-height: 1.6;
}
details.card { padding: 0; }
details.card > summary {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: baseline;
  padding: 12px;
  cursor: pointer;
}
details.card > .details-body { padding: 0 12px 12px; }
.code, .prompt, .raw {
  margin: 9px 0 0;
  border-radius: 8px;
  padding: 10px;
  background: #101828;
  color: #e4e7ec;
  white-space: pre-wrap;
  overflow-x: auto;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
}
.prompt { background: #f8fafc; color: #344054; border: 1px solid #e4e7ec; }
.schema { background: #072b23; color: #d1fadf; }
.source-view {
  display: grid;
  gap: 5px;
  margin-top: 9px;
}
.source-line {
  display: grid;
  grid-template-columns: 34px 78px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  border: 1px solid #e1e8f2;
  border-radius: 8px;
  padding: 7px 8px;
  background: #fbfdff;
}
.source-line .line-no {
  color: #98a2b3;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
}
.source-line .line-kind {
  color: var(--muted);
  font-size: 11px;
  font-weight: 900;
  text-transform: uppercase;
}
.source-line .line-text {
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  font-size: 13px;
  line-height: 1.45;
}
.source-line.command { border-left: 4px solid var(--blue); background: #f8fbff; }
.source-line.prompt-line { border-left: 4px solid #0e9384; }
.source-line.prompt-block {
  border-left: 4px solid #0e9384;
  background: #fbfffd;
}
.source-line.bash-block {
  border-left: 4px solid #667085;
  background: #f8fafc;
}
.source-line.control-line { border-left: 4px solid var(--green); background: #f6fef9; }
.source-line.command .line-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}
.prompt-block-body {
  display: block;
  white-space: pre-wrap;
  line-height: 1.65;
}
.tok-name { color: #175cd3; font-weight: 900; }
.tok-call { color: #067647; font-weight: 900; }
.tok-value { color: #7f56d9; }
.tok-assign { color: #66748a; font-weight: 900; }
.bash-script {
  display: block;
  margin: 0;
  color: #101828;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
}
.doc h1, .doc h2, .doc h3 { margin: 14px 0 8px; letter-spacing: 0; }
.doc p { margin: 7px 0; color: #344054; line-height: 1.6; }
.doc .bullet { position: relative; padding-left: 16px; margin: 4px 0; color: #344054; }
.doc .bullet::before { content: ""; position: absolute; left: 2px; top: .72em; width: 5px; height: 5px; border-radius: 50%; background: var(--blue); }
.highlight { outline: 3px solid rgba(21, 94, 239, .28); outline-offset: 2px; }
.active-target { box-shadow: 0 0 0 3px rgba(21, 94, 239, .18); }
.trace-hover { box-shadow: 0 0 0 3px rgba(6, 118, 71, .22); }
.hidden { display: none !important; }
.tooltip {
  position: fixed;
  z-index: 40;
  max-width: 360px;
  border: 1px solid #344054;
  border-radius: 8px;
  padding: 8px 10px;
  background: #101828;
  color: #f2f4f7;
  font-size: 12px;
  line-height: 1.45;
  pointer-events: none;
  box-shadow: 0 14px 28px rgba(16,24,40,.2);
}
@media (max-width: 980px) {
  .app-header { position: static; grid-template-columns: 1fr; gap: 12px; padding: 16px 18px; }
  .toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 8px; }
  .search { width: 100%; min-width: 0; }
  .stats { grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; padding: 12px; }
  .stat { min-width: 0; padding: 8px 9px; }
  .stat strong { font-size: 18px; }
  .stat span { font-size: 11px; }
  .workspace { grid-template-columns: 1fr; padding: 12px; }
  .splitter { display: none; }
  .map-panel { position: static; max-height: min(620px, 68vh); overflow: auto; }
  .content-nav { position: static; }
  .selection-panel { position: static; }
  .dispatch-row, .join-edge { grid-template-columns: 1fr; }
  .op { grid-template-columns: 1fr; }
  .source-line { grid-template-columns: 30px 1fr; }
  .source-line .line-text { grid-column: 1 / -1; }
}
@media (max-width: 360px) {
  .app-header { padding: 14px 14px; }
  .toolbar { grid-template-columns: 1fr auto auto; }
  .tool-btn { padding: 8px 9px; }
  .stats { gap: 6px; }
  .stat { padding: 7px 8px; }
  .stat strong { font-size: 17px; }
  .grid, .relation-grid { grid-template-columns: 1fr; }
}
</style>
`
}

func planAppJS() string {
	return `<script>
(() => {
  const data = JSON.parse(document.getElementById("plan-data").textContent);
  data.resources ||= {};
  for (const key of ["variables", "pools", "dbs", "skills", "mcps"]) data.resources[key] ||= [];
	  for (const key of ["imports", "definitions", "tasks", "blocks", "flow"]) data[key] ||= [];
	  data.trace ||= {};
	  const varIDs = new Map(data.resources.variables.map((v) => [v.name, v.id]));
	  const defIDs = new Map(data.definitions.map((d) => [d.name, d.id]));
	  const app = document.getElementById("app");
	  let selectedID = "";
  const labels = {
    tasks: ["Tasks", "任务"], branches: ["Branches", "分支"], fanouts: ["Fanouts", "扇出"], joins: ["Joins", "汇合"],
    definitions: ["Definitions", "定义"], pools: ["Pools", "工作池"], databases: ["Databases", "数据库"], variables: ["Variables", "变量"]
  };
  const langZH = (navigator.languages || [navigator.language || "en"]).some((x) => String(x).toLowerCase().startsWith("zh"));
  document.documentElement.lang = langZH ? "zh-CN" : "en";
  const t = (en, zh) => langZH ? (zh || en) : en;
  const el = (tag, attrs, ...children) => {
    const node = document.createElement(tag);
    for (const [key, value] of Object.entries(attrs || {})) {
      if (value == null || value === false) continue;
      if (key === "class") node.className = value;
      else if (key === "text") node.textContent = value;
      else if (key === "html") node.innerHTML = value;
      else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
      else node.setAttribute(key, value === true ? "" : value);
    }
    for (const child of children.flat()) {
      if (child == null) continue;
      node.append(child.nodeType ? child : document.createTextNode(String(child)));
    }
    return node;
  };
  const esc = (s) => String(s || "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const target = (id) => {
    const node = document.getElementById(id);
    if (!node) return;
    node.closest("details")?.setAttribute("open", "");
    if (node.tagName === "DETAILS") node.open = true;
	    node.scrollIntoView({ block: "center", behavior: "smooth" });
	    document.querySelectorAll(".highlight").forEach((x) => x.classList.remove("highlight"));
	    document.querySelectorAll(".active-target").forEach((x) => x.classList.remove("active-target"));
	    node.classList.add("active-target");
	    document.querySelectorAll("[data-target=\"" + CSS.escape(id) + "\"]").forEach((x) => x.classList.add("active-target"));
	    selectedID = id;
	    updateInspector(id);
	    setTimeout(() => node.classList.add("highlight"), 120);
	  };
  const link = (text, id, cls = "trace-link") => el("a", { class: cls, href: "#" + id, "data-target": id, title: data.trace[id] || text, onclick: (e) => { e.preventDefault(); target(id); } }, text);
  const anchor = (s) => String(s || "").replace(/[^A-Za-z0-9]+/g, "-").replace(/^-|-$/g, "");
	  const traceLabelLink = (label) => {
	    if (label.startsWith("task ")) return link(label, "task-" + label.replace("task ", ""));
	    if (label.startsWith("def ")) return link(label, "def-" + anchor(label.replace("def ", "")));
	    return el("span", { class: "trace-pill", text: label });
	  };
	  function inlineRefs(text) {
	    const out = [];
	    const re = /{{\s*([A-Za-z_][A-Za-z0-9_-]*)\s*}}/g;
	    let last = 0;
	    for (const match of String(text || "").matchAll(re)) {
	      if (match.index > last) out.push(String(text).slice(last, match.index));
	      const id = varIDs.get(match[1]);
	      out.push(id ? link(match[0], id, "inline-ref") : el("span", { class: "trace-pill unresolved", text: match[0] }));
	      last = match.index + match[0].length;
	    }
	    if (last < String(text || "").length) out.push(String(text || "").slice(last));
	    return out;
	  }
	  const richPrompt = (text) => el("div", { class: "prompt-rich" }, inlineRefs(text));
	  const section = (id, title, body) => {
	    if (body == null) { body = title; title = id; id = ""; }
	    return el("section", { class: "panel panel-anchor", id }, el("h2", { class: "panel-title" }, title), el("div", { class: "section-body" }, body));
	  };
	  const code = (text, cls = "code") => el("pre", { class: cls, text: text || "" });
	  function renderInspector() {
	    return el("aside", { class: "selection-panel", id: "selection-inspector" }, inspectorBody(selectedID));
	  }
	  function updateInspector(id) {
	    const box = document.getElementById("selection-inspector");
	    if (box) box.replaceChildren(inspectorBody(id));
	  }
	  function inspectorBody(id) {
	    const ctx = inspectContext(id);
	    if (!ctx) return el("div", { class: "selection-empty", text: t("Select any variable, definition, task, import, or block to inspect its relationships.", "选择任意变量、定义、任务、导入或块，查看它的关系。") });
	    return el("div", {},
	      el("div", { class: "selection-head" },
	        el("div", { class: "selection-title", text: ctx.title }),
	        el("span", { class: "selection-kind", text: ctx.kind })
	      ),
	      ctx.detail ? el("div", { class: "selection-detail", text: ctx.detail }) : null,
	      ctx.links.length ? el("div", { class: "selection-links" }, ctx.links) : el("div", { class: "selection-detail", text: t("No linked objects.", "没有关联对象。") })
	    );
	  }
	  function inspectContext(id) {
	    let task = data.tasks.find((x) => x.id === id);
	    if (task) return {
	      kind: "task",
	      title: "Task " + task.number,
	      detail: task.title,
	      links: [
	        link("block " + task.block, "block-" + task.block, "trace-pill"),
	        ...(task.variables || []).map((v) => v.target ? link("{{" + v.name + "}}", v.target, "trace-pill") : el("span", { class: "trace-pill unresolved", text: "{{" + v.name + "}}" })),
	        ...(task.calls || []).map((c) => link("/def " + c.name, c.target, "trace-pill")),
	        ...(task.joins || []).map((j) => link(t("joins ", "汇合 ") + j.from, j.from, "trace-pill"))
	      ]
	    };
	    let def = data.definitions.find((x) => x.id === id);
	    if (def) return {
	      kind: "definition",
	      title: "/def " + def.name,
	      detail: [def.source, def.blocks.length + " blocks"].filter(Boolean).join(" · "),
	      links: [
	        link("block " + def.block, "block-" + def.block, "trace-pill"),
	        ...(def.calls || []).map((c) => link("calls " + c, "def-" + anchor(c), "trace-pill")),
	        ...(def.calledBy || []).map(traceLabelLink)
	      ]
	    };
	    let variable = data.resources.variables.find((x) => x.id === id);
	    if (variable) return {
	      kind: "variable",
	      title: "{{" + variable.name + "}}",
	      detail: variable.bash || variable.value || "",
	      links: [link("block " + variable.block, "block-" + variable.block, "trace-pill"), ...(variable.usedBy || []).map((x) => link(x, "task-" + x.replace("task ", ""), "trace-pill"))]
	    };
	    let imp = data.imports.find((x) => x.id === id);
	    if (imp) return {
	      kind: "import",
	      title: imp.label,
	      detail: imp.error || imp.path,
	      links: [link(t("declaration", "声明"), "block-" + imp.block, "trace-pill")]
	    };
	    for (const group of [data.resources.pools, data.resources.dbs, data.resources.skills, data.resources.mcps]) {
	      const resource = group.find((x) => x.id === id);
	      if (resource) return {
	        kind: "resource",
	        title: resource.name,
	        detail: resource.detail,
	        links: [link("block " + resource.block, "block-" + resource.block, "trace-pill")]
	      };
	    }
	    let block = data.blocks.find((x) => x.id === id);
	    if (block) return {
	      kind: block.kind,
	      title: "Block " + block.number,
	      detail: block.control || block.body.split(/\r?\n/).find((line) => line.trim()) || "",
	      links: [
	        block.task ? link(block.task, block.task, "trace-pill") : null,
	        ...(block.links || []).map((x) => link(x.label, x.target, "trace-pill")),
	        ...(block.vars || []).map((v) => v.target ? link("{{" + v.name + "}}", v.target, "trace-pill") : null),
	        ...(block.calls || []).map((c) => link("/def " + c.name, c.target, "trace-pill"))
	      ].filter(Boolean)
	    };
	    return null;
	  }
	  function renderMarkdown(text) {
    const box = el("div", { class: "doc" });
    let inFence = false, fence = [];
    const flushFence = () => { if (fence.length) box.append(code(fence.join("\n"))); fence = []; };
    for (const raw of String(text || "").split(/\r?\n/)) {
      const line = raw.trim();
      if (line.startsWith("\x60\x60\x60")) { if (inFence) { flushFence(); inFence = false; } else inFence = true; continue; }
      if (inFence) { fence.push(raw); continue; }
      if (!line) continue;
      const h = raw.match(/^(#{1,4})\s+(.*)$/);
      if (h) { box.append(el(h[1].length <= 1 ? "h1" : h[1].length === 2 ? "h2" : "h3", { text: h[2] })); continue; }
	      if (line.startsWith("- ")) { box.append(el("div", { class: "bullet" }, inlineRefs(line.slice(2)))); continue; }
	      box.append(el("p", {}, inlineRefs(raw)));
    }
    if (inFence) flushFence();
    return box;
  }
  function renderHeader() {
    return el("header", { class: "app-header" },
      el("div", { class: "app-title" }, el("h1", { text: "ATM Plan" }), el("div", { class: "app-subtitle" }, t("Execution model for ", "执行模型："), el("strong", { text: data.source }))),
      el("div", { class: "toolbar" },
        el("input", { class: "search", placeholder: t("Filter tasks, vars, defs...", "过滤任务、变量、定义..."), oninput: (e) => filter(e.target.value) }),
        el("button", { class: "tool-btn", onclick: () => setDetails(true) }, t("Expand", "展开")),
        el("button", { class: "tool-btn", onclick: () => setDetails(false) }, t("Collapse", "收起"))
      )
    );
  }
  function renderStats() {
    return el("div", { class: "stats" }, Object.entries(labels).map(([key, names]) => el("div", { class: "stat" }, el("strong", { text: data.stats[key] || 0 }), el("span", { text: t(names[0], names[1]) }))));
  }
	  function renderFlowNode(node) {
	    const lanes = node.lanes || [];
	    const joins = node.joins || [];
	    if (node.kind === "branch") {
	      return el("div", { class: "branch-box searchable", "data-search": node.label },
	        el("div", { class: "branch-head" }, t("IF ", "如果 "), node.label),
	        el("div", { class: "branch-grid" }, (node.branches || []).map((branch) => el("div", { class: "branch-col" },
	          el("div", { class: "branch-label", text: branch.label }),
	          (branch.nodes || []).length ? branch.nodes.map(renderFlowNode) : el("div", { class: "muted", text: t("No runnable task", "没有可运行任务") })
	        ))),
	        el("div", { class: "branch-join", text: t("branches join", "分支汇合") })
	      );
	    }
    if (node.kind === "fanout") {
      return el("button", { class: "flow-node fanout dispatch-node searchable", "data-search": [node.label, node.detail, lanes.join(" ")].join(" "), onclick: () => node.target && target(node.target) },
        el("div", { class: "node-kind", text: t("fanout dispatch", "扇出分发") }),
        el("div", { class: "node-label", text: node.label }),
        node.detail ? el("div", { class: "node-detail", text: node.detail }) : null,
        el("div", { class: "dispatch-row" },
          el("div", { class: "dispatch-core", text: "/go" }),
          el("div", { class: "dispatch-lanes" }, lanes.length ? lanes.map((x) => el("span", { class: "lane", text: x })) : el("span", { class: "lane", text: "dynamic" }))
        )
      );
    }
    if (node.kind === "wait") {
      return el("button", { class: "flow-node wait searchable", "data-search": [node.label, node.detail, joins.map((j) => j.from + " " + j.pool).join(" ")].join(" "), onclick: () => node.target && target(node.target) },
        el("div", { class: "node-kind", text: t("wait join", "等待汇合") }),
        el("div", { class: "node-label", text: node.label }),
        node.detail ? el("div", { class: "node-detail", text: node.detail }) : null,
        joins.length ? el("div", { class: "join-stack" }, joins.map((j) => el("div", { class: "join-edge" },
          el("strong", { text: j.from }),
          el("span", { text: t("via ", "通过 ") + j.pool + (j.fanout ? " · " + j.fanout : "") })
        ))) : null
      );
    }
    return el("button", { class: "flow-node " + node.kind + " searchable", "data-search": [node.label, node.detail].join(" "), onclick: () => node.target && target(node.target) },
      el("div", { class: "node-kind", text: node.kind }),
      el("div", { class: "node-label", text: node.label }),
      node.detail ? el("div", { class: "node-detail", text: node.detail }) : null,
      lanes.length ? el("div", { class: "lanes" }, lanes.map((x) => el("span", { class: "lane", text: x }))) : null,
      joins.length ? el("div", { class: "node-detail", text: t("joins ", "汇合 ") + joins.map((j) => j.from + " via " + j.pool).join(", ") }) : null
    );
  }
	  function renderMap() {
	    return el("aside", { class: "panel map-panel" }, el("h2", { class: "panel-title" }, t("Execution Map", "执行图")), el("div", { class: "map-body" }, renderFlow(data.flow)));
	  }
	  function renderFlow(nodes) {
	    const rendered = [];
	    for (let i = 0; i < nodes.length; i++) {
	      if (nodes[i].kind === "resource") {
	        const group = [];
	        while (i < nodes.length && nodes[i].kind === "resource") {
	          group.push(nodes[i]);
	          i++;
	        }
	        i--;
	        rendered.push(renderSetupGroup(group));
	        continue;
	      }
	      rendered.push(renderFlowNode(nodes[i]));
	    }
	    return rendered;
	  }
	  function renderSetupGroup(nodes) {
	    return el("details", { class: "setup-box searchable", open: true, "data-search": nodes.map((n) => [n.label, n.detail].join(" ")).join(" ") },
	      el("summary", {}, el("span", { class: "setup-title", text: t("Setup", "准备") }), el("span", { class: "badge", text: nodes.length + " blocks" })),
	      el("div", { class: "setup-list" }, nodes.map((node) => el("button", { class: "setup-item", onclick: () => node.target && target(node.target) },
	        el("div", { class: "node-kind", text: node.kind }),
	        el("div", { class: "node-label", text: node.label }),
	        node.detail ? el("div", { class: "node-detail", text: node.detail }) : null
	      )))
	    );
	  }
  function renderResourceCard(item, kind) {
    const body = [el("div", { class: "muted", text: item.value || item.detail || "" })];
    if (item.bash) body.push(code(item.bash, "prompt"));
    if (item.usedBy && item.usedBy.length) body.push(el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Used by", "被使用") }), item.usedBy.map((x) => {
      const n = x.replace("task ", "");
      return link(x, "task-" + n);
    })));
    return el("article", { class: "card searchable", id: item.id, "data-search": [kind, item.name, item.value, item.detail].join(" ") },
      el("div", { class: "card-head" }, el("h3", { class: "card-title", text: kind + " · " + item.name }), el("span", { class: "badge", text: item.bash ? "/bash" : (item.block ? "block " + item.block : "") })),
      body
    );
  }
  function renderResources() {
    const cards = [];
    data.resources.variables.forEach((v) => cards.push(renderResourceCard(v, t("Variable", "变量"))));
    data.resources.pools.forEach((v) => cards.push(renderResourceCard(v, t("Pool", "工作池"))));
    data.resources.dbs.forEach((v) => cards.push(renderResourceCard(v, "DB")));
    data.resources.skills.forEach((v) => cards.push(renderResourceCard(v, t("Skill", "技能"))));
    data.resources.mcps.forEach((v) => cards.push(renderResourceCard(v, "MCP")));
	    return section("section-resources", t("Resources", "资源"), el("div", { class: "grid" }, cards));
  }
  function renderImports() {
    if (!data.imports.length) return null;
	    return section("section-imports", t("Imports", "导入内容"), el("div", { class: "grid" }, data.imports.map((item) => el("details", { class: "card searchable", id: item.id, open: true, "data-search": item.label + " " + item.content },
      el("summary", {}, el("strong", { text: item.label }), link(t("declaration", "声明"), "block-" + item.block)),
      el("div", { class: "details-body" }, item.error ? el("div", { class: "muted", text: item.error }) : renderMarkdown(item.content))
    ))));
  }
  function renderDefinitions() {
    if (!data.definitions.length) return null;
	    return section("section-definitions", t("Definitions", "定义与调用"), el("div", { class: "grid" }, data.definitions.map((d) => el("details", { class: "card searchable", id: d.id, open: true, "data-search": d.name + " " + d.blocks.join(" ") },
      el("summary", {}, el("strong", { text: "/def " + d.name }), el("span", { class: "badge", text: d.blocks.length + " blocks" })),
      el("div", { class: "details-body" },
        d.params && d.params.length ? el("div", { class: "trace-row" }, d.params.map((p) => el("span", { class: "trace-pill local", text: "param " + p }))) : null,
        d.source ? el("div", { class: "muted", text: d.source }) : null,
        d.calls && d.calls.length ? el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Calls", "调用") }), d.calls.map((c) => link(c, "def-" + anchor(c)))) : null,
        d.calledBy && d.calledBy.length ? el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Called by", "被调用") }), d.calledBy.map(traceLabelLink)) : null,
        d.blocks.map((body, i) => code("# block " + (i + 1) + "\n" + body, "raw"))
      )
    ))));
  }
  function renderRelations() {
    const variableLinks = data.resources.variables.filter((v) => v.usedBy && v.usedBy.length).slice(0, 12);
    const calledDefs = data.definitions.filter((d) => (d.calls && d.calls.length) || (d.calledBy && d.calledBy.length)).slice(0, 12);
	    return section("section-trace", t("Trace Index", "关系索引"), el("div", { class: "relation-grid" },
      el("article", { class: "relation-card searchable", "data-search": data.definitions.map((d) => d.name).join(" ") },
        el("h3", { text: t("Definitions", "定义调用") }),
        calledDefs.length ? calledDefs.map((d) => el("div", { class: "trace-row" },
          link("/def " + d.name, d.id),
          (d.calledBy || []).slice(0, 4).map(traceLabelLink),
          (d.calls || []).slice(0, 4).map((c) => link("calls " + c, "def-" + anchor(c), "trace-pill"))
        )) : el("div", { class: "muted", text: t("No definition calls", "没有定义调用") })
      ),
      el("article", { class: "relation-card searchable", "data-search": data.resources.variables.map((v) => v.name).join(" ") },
        el("h3", { text: t("Variables", "变量追踪") }),
        variableLinks.length ? variableLinks.map((v) => el("div", { class: "trace-row" },
          link("{{" + v.name + "}}", v.id, "trace-pill"),
          (v.usedBy || []).slice(0, 5).map((x) => link(x, "task-" + x.replace("task ", "")))
        )) : el("div", { class: "muted", text: t("No variable references", "没有变量引用") })
      ),
      el("article", { class: "relation-card searchable", "data-search": data.imports.map((x) => x.label).join(" ") },
        el("h3", { text: t("Imports", "导入追踪") }),
        data.imports.length ? data.imports.map((item) => el("div", { class: "trace-row" },
          link(item.label, item.id, "trace-pill import"),
          link(t("declaration", "声明"), "block-" + item.block)
        )) : el("div", { class: "muted", text: t("No imports", "没有导入") })
      )
    ));
  }
  function renderOps(ops) {
    return el("ol", { class: "op-list" }, ops.map((op) => el("li", { class: "op " + op.kind },
      el("span", { class: "op-kind", text: op.kind }),
      el("span", { class: "op-label", text: op.label }),
      op.lanes && op.lanes.length ? el("div", { class: "lanes" }, op.lanes.map((x) => el("span", { class: "lane", text: x }))) : null
    )));
  }
  function renderTraceRow(task) {
    const bits = [];
    if (task.parentBlock) bits.push(el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Parent", "父任务") }), link("block " + task.parentBlock, "block-" + task.parentBlock, "trace-pill")));
    if (task.childBlocks && task.childBlocks.length) bits.push(el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Child blocks", "子任务块") }), task.childBlocks.map((b) => link("block " + b, "block-" + b, "trace-pill"))));
    if (task.variables && task.variables.length) bits.push(el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Variables", "变量") }), task.variables.map((v) => v.target ? link("{{" + v.name + "}}", v.target, "trace-pill " + (v.kind === "global" ? "" : "local")) : el("span", { class: "trace-pill unresolved", text: "{{" + v.name + "}}" }))));
    if (task.calls && task.calls.length) bits.push(el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Calls", "调用") }), task.calls.map((c) => link(c.label, c.target))));
    return bits;
  }
  function renderTasks() {
	    return section("section-tasks", t("Tasks", "任务"), el("div", { class: "grid" }, data.tasks.map((task) => el("article", { class: "card searchable", id: task.id, "data-search": [task.title, task.prompt, task.ops.map((o) => o.label).join(" ")].join(" ") },
      el("div", { class: "card-head" }, el("h3", { class: "card-title", text: "Task " + task.number + " · " + task.title }), link("block " + task.block, "block-" + task.block, "badge")),
      renderOps(task.ops),
      renderTraceRow(task),
      task.joins && task.joins.length ? el("div", { class: "muted" }, t("Wait joins: ", "等待汇合："), task.joins.map((j) => j.from + " via " + j.pool).join(", ")) : null,
	      task.prompt ? el("details", { open: true }, el("summary", { text: t("Prompt", "提示词") }), richPrompt(task.prompt)) : null,
      task.output ? el("details", { open: true }, el("summary", { text: t("Output: ", "输出：") + task.output.label }), task.output.schema ? code(task.output.schema, "code schema") : null) : null
    ))));
  }
  function renderStructuredTaskSource(block) {
    const items = [];
    for (const op of (block.ops || [])) {
      if (op.kind === "execute") continue;
      items.push(renderStructuredOp(op));
    }
    if (block.prompt) items.push(renderStructuredPrompt(block.prompt));
    if (!items.length) return el("div", { class: "muted", text: t("No structured task content.", "没有结构化任务内容。") });
    return el("div", { class: "source-view" }, items);
  }
  function renderStructuredOp(op) {
    if (op.kind === "bash") {
      const label = op.name ? "/LET" : "/BASH";
      const head = op.name ? [el("span", { class: "tok-name", text: op.name }), " ", el("span", { class: "tok-value", text: "/bash" })] : [el("span", { class: "tok-value", text: "script" })];
      return el("div", { class: "source-line bash-block" },
        el("span", { class: "line-no", text: "--" }),
        el("span", { class: "line-kind", text: label }),
        el("span", { class: "line-text" }, head, op.script ? el("pre", { class: "bash-script", text: op.script }) : null)
      );
    }
    if (op.kind === "call") {
      const id = defIDs.get(op.name);
      return el("div", { class: "source-line command" },
        el("span", { class: "line-no", text: "--" }),
        el("span", { class: "line-kind", text: op.assign ? "/LET" : "/CALL" }),
        el("span", { class: "line-text" },
          op.assign ? [el("span", { class: "tok-name", text: op.assign }), " "] : null,
          id ? link("/call " + op.name, id, "inline-ref tok-call") : el("span", { class: "tok-call", text: "/call " + op.name }),
          op.args && op.args.length ? [" ", el("span", { class: "tok-value" }, inlineRefs(op.args.join(" ")))] : null
        )
      );
    }
    return el("div", { class: "source-line " + (["for", "go", "wait"].includes(op.kind) ? "control-line" : "command") },
      el("span", { class: "line-no", text: "--" }),
      el("span", { class: "line-kind", text: "/" + op.kind.toUpperCase() }),
      el("span", { class: "line-text" }, inlineRefs(op.detail || op.label || ""))
    );
  }
  function renderStructuredPrompt(prompt) {
    return el("div", { class: "source-line prompt-block" },
      el("span", { class: "line-no", text: "--" }),
      el("span", { class: "line-kind", text: t("prompt", "提示") }),
      el("span", { class: "line-text prompt-block-body" }, inlineRefs(prompt))
    );
  }
  function renderDocument() {
	    return section("section-document", t("Document", "文档与块"), el("div", {}, data.blocks.map((b) => el("div", {},
      b.prefix ? renderMarkdown(b.prefix) : null,
      el("details", { class: "card searchable", id: b.id, open: true, "data-search": [b.kind, b.body].join(" ") },
        el("summary", {}, el("strong", { text: "Block " + b.number }), b.task ? link(b.task, b.task) : el("span", { class: "badge", text: b.kind })),
        el("div", { class: "details-body" },
          b.control ? el("div", { class: "badge", text: b.control }) : null,
          b.links && b.links.length ? el("div", { class: "trace-row" }, el("span", { class: "muted", text: t("Declares", "声明") }), b.links.map((x) => link(x.label, x.target, "trace-pill " + x.kind))) : null,
          b.ops && b.ops.length ? renderOps(b.ops) : null,
          b.vars && b.vars.length ? el("div", { class: "trace-row" }, b.vars.map((v) => v.target ? link("{{" + v.name + "}}", v.target, "trace-pill " + (v.kind === "global" ? "" : "local")) : el("span", { class: "trace-pill unresolved", text: "{{" + v.name + "}}" }))) : null,
          b.calls && b.calls.length ? el("div", { class: "trace-row" }, b.calls.map((c) => link(c.label, c.target))) : null,
          b.task ? renderStructuredTaskSource(b) : null,
          el("details", {}, el("summary", { text: t("Raw source", "原始源码") }), code(b.body, "raw"))
        )
      ),
      b.sep ? renderMarkdown(b.sep) : null
    ))));
  }
	  function renderContent() {
	    const panels = [renderRelations(), renderResources(), renderImports(), renderDefinitions(), renderTasks(), renderDocument()].filter(Boolean);
	    return el("main", {},
	      renderStats(),
	      el("div", { class: "workspace" },
	        renderMap(),
	        el("div", { class: "splitter", title: t("Drag to resize columns", "拖动调整列宽") }),
	        el("div", { class: "content" }, renderContentNav(), renderInspector(), panels)
	      )
	    );
	  }
	  function renderContentNav() {
	    const items = [
	      ["section-trace", t("Trace", "关系")],
	      ["section-resources", t("Resources", "资源")],
	      data.imports.length ? ["section-imports", t("Imports", "导入")] : null,
	      data.definitions.length ? ["section-definitions", t("Definitions", "定义")] : null,
	      ["section-tasks", t("Tasks", "任务")],
	      ["section-document", t("Document", "文档")]
	    ].filter(Boolean);
	    return el("nav", { class: "content-nav" }, items.map(([id, label]) => el("button", { class: "nav-chip", onclick: () => target(id) }, label)));
	  }
  function filter(q) {
    const needle = q.trim().toLowerCase();
    document.querySelectorAll(".searchable").forEach((node) => {
      node.classList.toggle("hidden", needle && !String(node.dataset.search || "").toLowerCase().includes(needle));
    });
  }
  function setDetails(open) { document.querySelectorAll("details").forEach((d) => d.open = open); }
  function wireResize() {
    const splitter = document.querySelector(".splitter");
    const saved = localStorage.getItem("atm-plan-map-col");
    if (saved) document.body.style.setProperty("--map-col", saved);
    if (!splitter) return;
    splitter.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      splitter.setPointerCapture(event.pointerId);
      document.body.classList.add("resizing");
      const move = (e) => {
        const width = Math.max(300, Math.min(window.innerWidth - 460, e.clientX - 20));
        const value = width + "px";
        document.body.style.setProperty("--map-col", value);
        localStorage.setItem("atm-plan-map-col", value);
      };
      const up = () => {
        document.body.classList.remove("resizing");
        splitter.removeEventListener("pointermove", move);
        splitter.removeEventListener("pointerup", up);
        splitter.removeEventListener("pointercancel", up);
      };
      splitter.addEventListener("pointermove", move);
      splitter.addEventListener("pointerup", up);
      splitter.addEventListener("pointercancel", up);
    });
  }
  function wireTrace() {
    const tip = el("div", { class: "tooltip", hidden: true });
    document.body.append(tip);
    document.querySelectorAll("[data-target]").forEach((a) => {
      const id = a.dataset.target;
	      a.addEventListener("mouseenter", (e) => {
	        document.getElementById(id)?.classList.add("trace-hover");
	        document.querySelectorAll("[data-target=\"" + CSS.escape(id) + "\"]").forEach((x) => x.classList.add("trace-hover"));
	        tip.textContent = a.title || a.textContent;
	        tip.hidden = false;
	        tip.style.left = Math.min(window.innerWidth - 380, e.clientX + 12) + "px";
	        tip.style.top = Math.min(window.innerHeight - 80, e.clientY + 12) + "px";
	      });
	      a.addEventListener("mouseleave", () => {
	        document.getElementById(id)?.classList.remove("trace-hover");
	        document.querySelectorAll("[data-target=\"" + CSS.escape(id) + "\"]").forEach((x) => x.classList.remove("trace-hover"));
	        tip.hidden = true;
	      });
    });
  }
  app.append(renderHeader(), renderContent());
  wireResize();
  wireTrace();
})();
</script>
`
}
