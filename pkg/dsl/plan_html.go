package dsl

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func writePlanHTMLFile(path string, plan Plan, content string) error {
	if err := os.WriteFile(path, []byte(renderPlanHTML(plan, content)), 0o644); err != nil {
		return fmt.Errorf("write plan HTML %q: %w", path, err)
	}
	return nil
}

func renderPlanHTML(plan Plan, content string) string {
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
	plan           Plan
	blocks         []Block
	taskByBlock    map[int]taskRef
	controlByBlock map[int]ControlBlock
	resources      map[int][]resourceChip
	waitJoins      map[int][]asyncEdge
	implicitWaits  []asyncEdge
	taskCalls      map[int][]callRef
	defCalls       map[string][]string
	defCallers     map[string][]string
	vars           map[string]GlobalBinding
	varUsers       map[string][]string
	importDocs     []importDoc
	fanoutCount    int
	waitCount      int
}

type taskRef struct {
	Number int
	Task   Task
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
	Decl    ImportDecl
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
	ID        string      `json:"id"`
	Number    int         `json:"number"`
	Block     int         `json:"block"`
	Title     string      `json:"title"`
	Prompt    string      `json:"prompt"`
	Ops       []appOp     `json:"ops"`
	Calls     []appCall   `json:"calls,omitempty"`
	Variables []appVarRef `json:"variables,omitempty"`
	Output    *appOutput  `json:"output,omitempty"`
	Joins     []appJoin   `json:"joins,omitempty"`
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
	ID       string      `json:"id"`
	Number   int         `json:"number"`
	Prefix   string      `json:"prefix,omitempty"`
	Body     string      `json:"body"`
	Sep      string      `json:"sep,omitempty"`
	Kind     string      `json:"kind"`
	Task     string      `json:"task,omitempty"`
	Prompt   string      `json:"prompt,omitempty"`
	Control  string      `json:"control,omitempty"`
	Ops      []appOp     `json:"ops,omitempty"`
	Calls    []appCall   `json:"calls,omitempty"`
	Vars     []appVarRef `json:"vars,omitempty"`
	Links    []appLink   `json:"links,omitempty"`
	Children []string    `json:"children,omitempty"`
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

func buildPlanAppData(plan Plan, content string) planAppData {
	view := buildPlanHTMLView(plan, content)
	blockLinks := make(map[int][]appLink)
	data := planAppData{
		Source: plan.SourcePath,
		Stats: map[string]int{
			"tasks":       len(plan.Tasks),
			"branches":    len(plan.Controls),
			"fanouts":     view.fanoutCount,
			"joins":       view.waitCount,
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
	defs := append([]Definition{}, plan.Definitions...)
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	for _, def := range defs {
		id := "def-" + anchorID(def.Name)
		data.Definitions = append(data.Definitions, appDefinition{
			ID:       id,
			Name:     def.Name,
			Params:   append([]string{}, def.Params...),
			Source:   def.SourcePath,
			Block:    def.BlockIndex + 1,
			Blocks:   append([]string{}, def.Blocks...),
			Calls:    view.defCalls[def.Name],
			CalledBy: view.defCallers[def.Name],
		})
		data.Trace[id] = "Definition " + def.Name
	}
	for _, ref := range sortedTasks(view.taskByBlock) {
		task := ref.Task
		id := fmt.Sprintf("task-%d", ref.Number)
		taskData := appTask{
			ID:        id,
			Number:    ref.Number,
			Block:     task.BlockIndex + 1,
			Title:     taskTitle(task),
			Prompt:    strings.TrimSpace(task.Prompt),
			Ops:       appOps(task),
			Calls:     appCalls(view.taskCalls[task.BlockIndex]),
			Variables: appVarRefs(task, view),
			Joins:     appJoins(view.waitJoins[task.BlockIndex]),
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
		data.Flow = append(data.Flow, appFlowNode{ID: "implicit-wait", Kind: "wait", Label: "Implicit final wait", Joins: appJoins(view.implicitWaits)})
	}
	data.Flow = append(data.Flow, appFlowNode{ID: "end", Kind: "end", Label: "End", Detail: "Plan mode does not execute tools or bash."})
	return data
}

func appOps(task Task) []appOp {
	var out []appOp
	for _, op := range task.Ops {
		item := appOp{Kind: string(op.Kind), Label: displayPlanOp(op), Detail: formatPlanOp(op)}
		switch op.Kind {
		case OpBash:
			item.Name = op.Bash.Name
			item.Script = strings.TrimSpace(op.Bash.Script)
		case OpCall:
			item.Name = op.Call.Name
			item.Assign = op.Call.Assign
			item.Args = append([]string{}, op.Call.Args...)
		case OpGo:
			item.Pool = op.Pool
			item.Lanes = fanoutLabels(task)
		case OpWait:
			item.Pool = op.Pool
		}
		out = append(out, item)
	}
	return out
}

func fanoutLabels(task Task) []string {
	for _, op := range task.Ops {
		if op.Kind != OpFor {
			continue
		}
		if len(op.For.Values) > 0 {
			return append([]string{}, op.For.Values...)
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

func appVarRefs(task Task, view planHTMLView) []appVarRef {
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
		if strings.TrimSpace(view.blocks[i].Body) == "" || IsDone(view.blocks[i].Body) {
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
	var thenNodes []appFlowNode
	if group.ifBlock.HeaderOnly {
		thenNodes = appFlowRange(view, group.thenStart, group.thenEnd)
	} else if thenNode, ok := appBlockFlowNode(view, group.ifIndex); ok {
		thenNodes = []appFlowNode{thenNode}
	}
	var elseNodes []appFlowNode
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
		return appFlowNode{ID: fmt.Sprintf("flow-task-%d", ref.Number), Kind: kind, Label: fmt.Sprintf("Task %d", ref.Number), Detail: taskTitle(task), Target: fmt.Sprintf("task-%d", ref.Number), Lanes: fanoutLabels(task), Joins: appJoins(view.waitJoins[block])}, true
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

func buildPlanHTMLView(plan Plan, content string) planHTMLView {
	blocks := ParseBlocks(content)
	view := planHTMLView{
		plan:           plan,
		blocks:         blocks,
		taskByBlock:    make(map[int]taskRef),
		controlByBlock: make(map[int]ControlBlock),
		resources:      make(map[int][]resourceChip),
		waitJoins:      make(map[int][]asyncEdge),
		taskCalls:      make(map[int][]callRef),
		defCalls:       make(map[string][]string),
		defCallers:     make(map[string][]string),
		vars:           make(map[string]GlobalBinding),
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
		view.defCalls[def.Name] = uniqueStrings(definitionCalls(def))
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

func readImportDoc(root string, decl ImportDecl) importDoc {
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
		for _, op := range task.Ops {
			if op.Kind != OpWait {
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
		v.waitCount++
	}
}

func sortedTasks(tasks map[int]taskRef) []taskRef {
	out := make([]taskRef, 0, len(tasks))
	for _, ref := range tasks {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Task.BlockIndex < out[j].Task.BlockIndex
	})
	return out
}

func taskGoPool(task Task) (string, bool) {
	for _, op := range task.Ops {
		if op.Kind == OpGo {
			return op.Pool, true
		}
	}
	return "", false
}

func taskHasWait(task Task) bool {
	for _, op := range task.Ops {
		if op.Kind == OpWait {
			return true
		}
	}
	return false
}

func taskFanoutSummary(task Task) string {
	for _, op := range task.Ops {
		if op.Kind == OpFor {
			return formatForIR(op.For)
		}
	}
	return "single background branch"
}

func taskCallRefs(task Task) []callRef {
	var calls []callRef
	for _, op := range task.Ops {
		if op.Kind == OpCall {
			source := "command"
			if op.Call.Assign != "" {
				source = "let"
			}
			calls = append(calls, callRef{Name: op.Call.Name, Assign: op.Call.Assign, Source: source})
		}
		if op.Kind == OpFor && op.For.Source.Kind == ConditionCall {
			if call, err := ParseCallExpression(op.For.Source.Text); err == nil {
				calls = append(calls, callRef{Name: call.Name, Source: "fanout"})
			}
		}
	}
	return calls
}

func taskVariableRefs(task Task) []string {
	var values []string
	for _, match := range legacyTemplateVar.FindAllStringSubmatch(task.Prompt, -1) {
		if len(match) > 1 {
			values = append(values, match[1])
		}
	}
	if task.Output != nil {
		for _, match := range legacyTemplateVar.FindAllStringSubmatch(task.Output.FileName+" "+task.Output.Schema, -1) {
			if len(match) > 1 {
				values = append(values, match[1])
			}
		}
	}
	return uniqueStrings(values)
}

type htmlConditionalGroup struct {
	ifIndex   int
	ifBlock   IfBlock
	thenStart int
	thenEnd   int
	hasElse   bool
	elseIndex int
	elseBlock ElseBlock
	elseStart int
	elseEnd   int
	end       int
}

func htmlParseConditionalGroup(blocks []Block, index int) (htmlConditionalGroup, bool) {
	ifBlock, ok, err := ParseIfBlock(blocks[index].Body)
	if err != nil || !ok {
		return htmlConditionalGroup{}, false
	}
	group := htmlConditionalGroup{ifIndex: index, ifBlock: ifBlock}
	if !ifBlock.HeaderOnly {
		group.thenStart = index
		group.thenEnd = index + 1
		group.end = index + 1
		if index+1 < len(blocks) && !IsDone(blocks[index+1].Body) {
			if elseBlock, ok, err := ParseElseBlock(blocks[index+1].Body); err == nil && ok {
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
		if elseBlock, ok, err := ParseElseBlock(blocks[group.thenEnd].Body); err == nil && ok {
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

func htmlNodeEnd(blocks []Block, index int) int {
	if index >= len(blocks) {
		return index
	}
	if _, ok, err := ParseElseBlock(blocks[index].Body); err == nil && ok {
		return index
	}
	if _, ok := htmlParseConditionalGroup(blocks, index); ok {
		group, _ := htmlParseConditionalGroup(blocks, index)
		return group.end
	}
	return index + 1
}

func writeGraphStart(b *strings.Builder, view planHTMLView) {
	b.WriteString("<div class=\"map-node meta\"><div class=\"node-title\">" + i18n("Start", "开始") + "</div><div class=\"node-detail\">" + escape(view.plan.SourcePath) + "</div></div>\n")
	if len(view.plan.Imports) > 0 || len(view.plan.Globals) > 0 || len(view.plan.Pools) > 0 || len(view.plan.DBs) > 0 || len(view.plan.Skills) > 0 || len(view.plan.MCPs) > 0 {
		b.WriteString("<div class=\"map-node resources\"><div class=\"node-title\">" + i18n("Setup", "准备") + "</div><div class=\"chip-row\">")
		writeSmallChip(b, "Imports", len(view.plan.Imports))
		writeSmallChip(b, "Vars", len(view.plan.Globals))
		writeSmallChip(b, "Pools", len(view.plan.Pools))
		writeSmallChip(b, "DB", len(view.plan.DBs))
		writeSmallChip(b, "Skills", len(view.plan.Skills))
		writeSmallChip(b, "MCP", len(view.plan.MCPs))
		b.WriteString("</div></div>\n")
	}
}

func writeSmallChip(b *strings.Builder, label string, value int) {
	if value == 0 {
		return
	}
	fmt.Fprintf(b, "<span class=\"mini-chip\">%s %d</span>", escape(label), value)
}

func writeGraphRange(b *strings.Builder, view planHTMLView, start, end int) {
	for i := start; i < end && i < len(view.blocks); {
		if strings.TrimSpace(view.blocks[i].Body) == "" || IsDone(view.blocks[i].Body) {
			i++
			continue
		}
		if group, ok := htmlParseConditionalGroup(view.blocks, i); ok {
			writeBranchGroup(b, view, group)
			if group.end <= i {
				i++
			} else {
				i = group.end
			}
			continue
		}
		if _, ok, err := ParseElseBlock(view.blocks[i].Body); err == nil && ok {
			writeGraphBlock(b, view, i)
			i++
			continue
		}
		writeGraphBlock(b, view, i)
		i++
	}
}

func writeBranchGroup(b *strings.Builder, view planHTMLView, group htmlConditionalGroup) {
	b.WriteString("<div class=\"branch-group\" id=\"map-block-" + fmt.Sprint(group.ifIndex+1) + "\">")
	b.WriteString("<a class=\"condition-node\" href=\"#block-" + fmt.Sprint(group.ifIndex+1) + "\" title=\"Jump to condition block\"><span class=\"node-kicker\">if</span><span class=\"node-title\">" + escape(displayCondition(group.ifBlock.Condition)) + "</span></a>")
	b.WriteString("<div class=\"branch-columns\">")
	b.WriteString("<div class=\"branch-col\"><div class=\"branch-label true\">" + i18n("true branch", "true 分支") + "</div>")
	if group.ifBlock.HeaderOnly {
		writeGraphRange(b, view, group.thenStart, group.thenEnd)
	} else {
		writeGraphBlock(b, view, group.ifIndex)
	}
	if group.thenStart == group.thenEnd {
		b.WriteString("<div class=\"empty-branch\">" + i18n("No runnable task", "没有可运行任务") + "</div>")
	}
	b.WriteString("</div>")
	b.WriteString("<div class=\"branch-col\"><div class=\"branch-label false\">" + i18n("false branch", "false 分支") + "</div>")
	if group.hasElse {
		if group.elseBlock.HeaderOnly {
			writeGraphRange(b, view, group.elseStart, group.elseEnd)
		} else {
			writeGraphBlock(b, view, group.elseIndex)
		}
	} else {
		b.WriteString("<div class=\"empty-branch\">" + i18n("Continue", "继续") + "</div>")
	}
	b.WriteString("</div></div>")
	b.WriteString("<div class=\"join-bar\">" + i18n("branches join", "分支汇合") + "</div>")
	b.WriteString("</div>\n")
}

func writeGraphBlock(b *strings.Builder, view planHTMLView, block int) {
	if ref, ok := view.taskByBlock[block]; ok {
		task := ref.Task
		class := "map-node task"
		if _, ok := taskGoPool(task); ok {
			class += " fanout"
		}
		if taskHasWait(task) {
			class += " wait"
		}
		b.WriteString("<a class=\"" + class + "\" href=\"#block-" + fmt.Sprint(block+1) + "\">")
		b.WriteString("<div class=\"node-kicker\">" + i18n("Task", "任务") + " " + fmt.Sprint(ref.Number) + " · " + i18n("block", "块") + " " + fmt.Sprint(block+1) + "</div>")
		b.WriteString("<div class=\"node-title\">" + escape(taskTitle(task)) + "</div>")
		b.WriteString("<div class=\"node-detail\">" + escape(FormatTaskFlow(task)) + "</div>")
		if _, ok := taskGoPool(task); ok {
			writeFanoutLanes(b, task)
		}
		if joins := view.waitJoins[block]; len(joins) > 0 {
			b.WriteString("<div class=\"join-list\">" + i18n("joins", "汇合") + " " + escape(joinEdgeSummary(joins)) + "</div>")
		}
		b.WriteString("</a>\n")
		return
	}
	if chips := view.resources[block]; len(chips) > 0 {
		b.WriteString("<a class=\"map-node resources\" href=\"#block-" + fmt.Sprint(block+1) + "\"><div class=\"node-kicker\">" + i18n("Resource block", "资源块") + " " + fmt.Sprint(block+1) + "</div><div class=\"chip-row\">")
		for _, chip := range chips {
			b.WriteString("<span class=\"mini-chip " + escape(chip.Kind) + "\">" + escape(chip.Kind) + " · " + escape(chip.Label) + "</span>")
		}
		b.WriteString("</div></a>\n")
		return
	}
	if control, ok := view.controlByBlock[block]; ok {
		b.WriteString("<a class=\"map-node control\" href=\"#block-" + fmt.Sprint(block+1) + "\"><div class=\"node-title\">" + escape(displayControlBlock(control)) + "</div></a>\n")
	}
}

func writeImplicitWaits(b *strings.Builder, view planHTMLView) {
	if len(view.implicitWaits) == 0 {
		return
	}
	b.WriteString("<div class=\"map-node wait implicit\"><div class=\"node-title\">" + i18n("Implicit final wait", "最终隐式等待") + "</div><div class=\"node-detail\">" + escape(joinEdgeSummary(view.implicitWaits)) + "</div></div>\n")
}

func joinEdgeSummary(edges []asyncEdge) string {
	parts := make([]string, 0, len(edges))
	for _, edge := range edges {
		pool := "default"
		if edge.Pool != "" {
			pool = edge.Pool
		}
		detail := fmt.Sprintf("task %d via %s", edge.FromTask, pool)
		if edge.Fanout != "" {
			detail += " (" + edge.Fanout + ")"
		}
		parts = append(parts, detail)
	}
	return joinSummary(parts)
}

func taskTitle(task Task) string {
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return FormatTaskFlow(task)
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

func displayControlBlock(control ControlBlock) string {
	if control.Kind == "else" {
		return fmt.Sprintf("else block %d%s", control.BlockIndex+1, headerOnlySuffix(control.HeaderOnly))
	}
	return fmt.Sprintf("if block %d: %s%s", control.BlockIndex+1, displayCondition(control.Condition), headerOnlySuffix(control.HeaderOnly))
}

func displayCondition(condition Condition) string {
	switch condition.Kind {
	case ConditionCEL:
		return "cel(" + condition.Text + ")"
	case ConditionCall:
		return "call(" + condition.Text + ")"
	case ConditionNatural:
		return condition.Text
	default:
		return "<none>"
	}
}

func writeResourcesPanel(b *strings.Builder, view planHTMLView) {
	b.WriteString("<section class=\"panel\"><h2>" + i18n("Structured Resources", "结构化资源") + "</h2><div class=\"resource-grid dense\">")
	for _, item := range view.plan.Globals {
		b.WriteString("<article class=\"resource-card\" id=\"var-" + escape(anchorID(item.Name)) + "\"><strong>" + i18n("Variable", "变量") + " · " + escape(item.Name) + "</strong>")
		if item.BashScript != "" {
			b.WriteString("<span>/bash</span><pre class=\"inline-code\">" + escape(strings.TrimSpace(item.BashScript)) + "</pre>")
		} else {
			b.WriteString("<span>value</span><p>" + escape(item.Value) + "</p>")
		}
		if users := view.varUsers[item.Name]; len(users) > 0 {
			b.WriteString("<p class=\"trace-line\">" + i18n("used by", "被使用") + ": " + escape(strings.Join(users, ", ")) + "</p>")
		}
		b.WriteString("</article>")
	}
	writeResourceGroup(b, "Pools", "工作池", poolSummary(view.plan.Pools), len(view.plan.Pools))
	writeResourceGroup(b, "Databases", "数据库", dbDeclSummary(view.plan.DBs), len(view.plan.DBs))
	writeResourceGroup(b, "Skills", "技能", skillDeclSummary(view.plan.Skills), len(view.plan.Skills))
	writeResourceGroup(b, "MCPs", "MCP", mcpDeclSummary(view.plan.MCPs), len(view.plan.MCPs))
	b.WriteString("</div></section>\n")
}

func writeResourceGroup(b *strings.Builder, en, zh, summary string, count int) {
	if count == 0 {
		return
	}
	b.WriteString("<article class=\"resource-card\"><strong>" + i18n(en, zh) + "</strong><span>" + fmt.Sprint(count) + "</span><p>" + escape(summary) + "</p></article>")
}

func writeImportPanel(b *strings.Builder, view planHTMLView) {
	if len(view.importDocs) == 0 {
		return
	}
	b.WriteString("<section class=\"panel\"><h2>" + i18n("Imports", "导入内容") + "</h2><div class=\"import-list\">")
	for i, doc := range view.importDocs {
		label := doc.Decl.Path
		if doc.Decl.Namespace != "" {
			label = doc.Decl.Namespace + " from " + doc.Decl.Path
		}
		b.WriteString("<details class=\"import-card\" id=\"import-" + fmt.Sprint(i+1) + "\" open><summary><span>" + escape(label) + "</span><a href=\"#block-" + fmt.Sprint(doc.Decl.BlockIndex+1) + "\">" + i18n("declaration", "声明") + "</a></summary>")
		if doc.Error != "" {
			b.WriteString("<p class=\"meta-line error\">" + escape(doc.Error) + "</p>")
		} else {
			writeMarkdownDoc(b, doc.Content)
		}
		b.WriteString("</details>")
	}
	b.WriteString("</div></section>\n")
}

func writeDefinitionPanel(b *strings.Builder, view planHTMLView) {
	if len(view.plan.Definitions) == 0 {
		return
	}
	b.WriteString("<section class=\"panel\"><h2>" + i18n("Definition Calls", "定义调用") + "</h2><div class=\"definition-grid\">")
	defs := append([]Definition{}, view.plan.Definitions...)
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	for _, def := range defs {
		b.WriteString("<article class=\"definition-card\" id=\"def-" + escape(anchorID(def.Name)) + "\">")
		b.WriteString("<div class=\"card-head\"><h3>/def " + escape(def.Name) + "</h3><span class=\"badge\">" + fmt.Sprint(len(def.Blocks)) + " blocks</span></div>")
		if len(def.Params) > 0 {
			b.WriteString("<div class=\"chip-row\">")
			for _, param := range def.Params {
				b.WriteString("<span class=\"mini-chip\">param · " + escape(param) + "</span>")
			}
			b.WriteString("</div>")
		}
		if def.SourcePath != "" {
			b.WriteString("<p class=\"meta-line\">" + i18n("source", "来源") + ": " + escape(def.SourcePath) + "</p>")
		}
		if calls := view.defCalls[def.Name]; len(calls) > 0 {
			b.WriteString("<p class=\"call-line\">" + i18n("calls", "调用") + ": ")
			for _, call := range calls {
				b.WriteString("<a class=\"trace-link\" data-trace=\"def-" + escape(anchorID(call)) + "\" title=\"Jump to definition\" href=\"#def-" + escape(anchorID(call)) + "\">" + escape(call) + "</a> ")
			}
			b.WriteString("</p>")
		}
		if callers := view.defCallers[def.Name]; len(callers) > 0 {
			b.WriteString("<p class=\"call-line\">" + i18n("called by", "被调用") + ": ")
			writeTraceLabels(b, callers)
			b.WriteString("</p>")
		}
		if len(def.Blocks) > 0 {
			b.WriteString("<details open><summary>" + i18n("Definition body", "定义内容") + "</summary>")
			for i, body := range def.Blocks {
				b.WriteString("<pre class=\"raw-block compact\"># block " + fmt.Sprint(i+1) + "\n" + escape(strings.TrimRight(body, "\r\n")) + "</pre>")
			}
			b.WriteString("</details>")
		}
		b.WriteString("</article>")
	}
	b.WriteString("</div></section>\n")
}

func writeTraceLabels(b *strings.Builder, labels []string) {
	for _, label := range labels {
		switch {
		case strings.HasPrefix(label, "task "):
			id := strings.TrimSpace(strings.TrimPrefix(label, "task "))
			b.WriteString("<a class=\"trace-link\" data-trace=\"task-" + escape(anchorID(id)) + "\" title=\"Jump to task\" href=\"#task-" + escape(id) + "\">" + escape(label) + "</a> ")
		case strings.HasPrefix(label, "def "):
			name := strings.TrimSpace(strings.TrimPrefix(label, "def "))
			b.WriteString("<a class=\"trace-link\" data-trace=\"def-" + escape(anchorID(name)) + "\" title=\"Jump to definition\" href=\"#def-" + escape(anchorID(name)) + "\">" + escape(label) + "</a> ")
		default:
			b.WriteString("<span>" + escape(label) + "</span> ")
		}
	}
}

func writeTaskDetailsPanel(b *strings.Builder, view planHTMLView) {
	b.WriteString("<section class=\"panel\"><h2>" + i18n("Task Details", "任务详情") + "</h2><div class=\"tasks\">")
	if len(view.plan.Tasks) == 0 {
		b.WriteString("<div class=\"empty\">" + i18n("No runnable tasks.", "没有可运行任务。") + "</div>")
	}
	for _, ref := range sortedTasks(view.taskByBlock) {
		task := ref.Task
		b.WriteString("<article class=\"task\" id=\"task-" + fmt.Sprint(ref.Number) + "\">")
		b.WriteString("<div class=\"card-head\"><h3>" + i18n("Task ", "任务 ") + fmt.Sprint(ref.Number) + "</h3><a class=\"badge\" href=\"#block-" + fmt.Sprint(task.BlockIndex+1) + "\">" + i18n("source block ", "源块 ") + fmt.Sprint(task.BlockIndex+1) + "</a></div>")
		writeTaskOps(b, task)
		writeVariableRefs(b, task, view)
		writeCallRefs(b, view.taskCalls[task.BlockIndex])
		if joins := view.waitJoins[task.BlockIndex]; len(joins) > 0 {
			b.WriteString("<div class=\"meta-line join-note\">" + i18n("wait joins", "等待汇合") + ": " + escape(joinEdgeSummary(joins)) + "</div>")
		}
		if prompt := strings.TrimSpace(task.Prompt); prompt != "" {
			b.WriteString("<details open><summary>" + i18n("Prompt", "提示词") + "</summary><div class=\"prompt\">" + escape(prompt) + "</div></details>")
		}
		if task.Output != nil {
			b.WriteString("<details open><summary>" + i18n("Output", "输出") + ": " + escape(formatPlanOutput(task.Output)) + "</summary>")
			if strings.TrimSpace(task.Output.Schema) != "" {
				b.WriteString("<pre class=\"code schema\">" + escape(prettySchema(task.Output.Schema)) + "</pre>")
			}
			b.WriteString("</details>")
		}
		if task.Cursor.Active {
			b.WriteString("<div class=\"meta-line\">" + i18n("cursor", "游标") + ": " + escape(fmt.Sprintf("op=%d step-runs=%d total-runs=%d started=%s", task.Cursor.OpIndex, task.Cursor.RunIndex, task.Cursor.TotalRuns, task.Cursor.Start.Format("2006-01-02T15:04:05Z07:00"))) + "</div>")
		}
		b.WriteString("</article>")
	}
	b.WriteString("</div></section>\n")
}

func writeTaskOps(b *strings.Builder, task Task) {
	b.WriteString("<ol class=\"op-list\">")
	for _, op := range task.Ops {
		label := displayPlanOp(op)
		class := "op-item " + string(op.Kind)
		b.WriteString("<li class=\"" + escape(class) + "\"><span class=\"op-kind\">" + escape(string(op.Kind)) + "</span><span class=\"op-label\">" + escape(label) + "</span>")
		if op.Kind == OpGo {
			writeFanoutLanes(b, task)
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ol>")
	if detail := formatDBTaskConfig(task.DB); detail != "" {
		b.WriteString("<div class=\"chip-row\"><span class=\"op-chip db\">DB · " + escape(detail) + "</span></div>")
	}
	if detail := formatSkillTaskConfig(task.Skill); detail != "" {
		b.WriteString("<div class=\"chip-row\"><span class=\"op-chip skill\">skill · " + escape(detail) + "</span></div>")
	}
	if detail := formatMCPTaskConfig(task.MCP); detail != "" {
		b.WriteString("<div class=\"chip-row\"><span class=\"op-chip mcp\">MCP · " + escape(detail) + "</span></div>")
	}
}

func displayPlanOp(op Op) string {
	switch op.Kind {
	case OpFor:
		return displayForIR(op.For)
	case OpGo:
		if op.Pool != "" {
			return "dispatch to pool " + op.Pool
		}
		return "dispatch in background"
	case OpWait:
		if op.Pool != "" {
			return "join pool " + op.Pool
		}
		return "join all previous background work"
	case OpCall:
		if op.Call.Assign != "" {
			return op.Call.Name + " -> " + op.Call.Assign
		}
		return op.Call.Name
	case OpExecute:
		return appendRunOptionSummary("execute prompt", op.ExecuteOptions)
	default:
		return formatPlanOp(op)
	}
}

func displayForIR(step For) string {
	name := step.VarName
	if name == "" {
		name = "run"
	}
	if step.Source.Kind == ConditionCall {
		return fmt.Sprintf("for %s in %s", name, step.Source.Text)
	}
	if step.Source.Kind == ConditionCEL {
		return fmt.Sprintf("for %s in cel(%s)", name, step.Source.Text)
	}
	if len(step.Values) > 0 {
		return fmt.Sprintf("for %s in [%s]", name, strings.Join(step.Values, ", "))
	}
	if step.MaxRuns > 0 {
		return fmt.Sprintf("for %s x %d", name, step.MaxRuns)
	}
	if step.Condition.Kind == ConditionCEL {
		return fmt.Sprintf("for %s until cel(%s)", name, step.Condition.Text)
	}
	return formatForIR(step)
}

func writeFanoutLanes(b *strings.Builder, task Task) {
	for _, op := range task.Ops {
		if op.Kind != OpFor {
			continue
		}
		labels := op.For.Values
		if len(labels) == 0 && op.For.MaxRuns > 0 && op.For.MaxRuns <= 12 {
			for i := 1; i <= op.For.MaxRuns; i++ {
				labels = append(labels, fmt.Sprintf("%d", i))
			}
		}
		if len(labels) == 0 {
			b.WriteString("<div class=\"fanout-lanes\"><span>" + escape(displayForIR(op.For)) + "</span><span class=\"lane dynamic\">dynamic</span></div>")
			return
		}
		b.WriteString("<div class=\"fanout-lanes\">")
		for _, label := range labels {
			b.WriteString("<span class=\"lane\">" + escape(label) + "</span>")
		}
		b.WriteString("</div>")
		return
	}
}

func writeTaskChips(b *strings.Builder, task Task) {
	b.WriteString("<div class=\"chip-row\">")
	for _, op := range task.Ops {
		switch op.Kind {
		case OpFor:
			b.WriteString("<span class=\"op-chip for\">" + escape(formatForIR(op.For)) + "</span>")
		case OpGo:
			label := "Go"
			if op.Pool != "" {
				label += "(" + op.Pool + ")"
			}
			b.WriteString("<span class=\"op-chip go\">" + escape(label) + "</span>")
		case OpWait:
			label := "Wait"
			if op.Pool != "" {
				label += "(" + op.Pool + ")"
			}
			b.WriteString("<span class=\"op-chip wait\">" + escape(label) + "</span>")
		case OpCall:
			label := "Call(" + op.Call.Name + ")"
			if op.Call.Assign != "" {
				label += " -> " + op.Call.Assign
			}
			b.WriteString("<span class=\"op-chip call\">" + escape(label) + "</span>")
		case OpBash:
			b.WriteString("<span class=\"op-chip bash\">" + escape(formatPlanOp(op)) + "</span>")
		case OpExecute:
			b.WriteString("<span class=\"op-chip execute\">" + escape(formatPlanOp(op)) + "</span>")
		default:
			b.WriteString("<span class=\"op-chip\">" + escape(formatPlanOp(op)) + "</span>")
		}
	}
	if detail := formatDBTaskConfig(task.DB); detail != "" {
		b.WriteString("<span class=\"op-chip db\">DB · " + escape(detail) + "</span>")
	}
	if detail := formatSkillTaskConfig(task.Skill); detail != "" {
		b.WriteString("<span class=\"op-chip skill\">skill · " + escape(detail) + "</span>")
	}
	if detail := formatMCPTaskConfig(task.MCP); detail != "" {
		b.WriteString("<span class=\"op-chip mcp\">MCP · " + escape(detail) + "</span>")
	}
	b.WriteString("</div>")
}

func writeVariableRefs(b *strings.Builder, task Task, view planHTMLView) {
	vars := taskVariableRefs(task)
	if len(vars) == 0 {
		return
	}
	b.WriteString("<div class=\"trace-row\"><span>" + i18n("variables", "变量") + "</span>")
	for _, name := range vars {
		if _, ok := view.vars[name]; ok {
			b.WriteString("<a class=\"trace-pill\" data-trace=\"var-" + escape(anchorID(name)) + "\" title=\"Jump to variable definition\" href=\"#var-" + escape(anchorID(name)) + "\">{{" + escape(name) + "}}</a>")
		} else if kind, ok := taskLocalVarKind(task, name); ok {
			b.WriteString("<a class=\"trace-pill local\" data-trace=\"block-" + fmt.Sprint(task.BlockIndex+1) + "\" title=\"Local " + escape(kind) + " variable in this block\" href=\"#block-" + fmt.Sprint(task.BlockIndex+1) + "\">{{" + escape(name) + "}}</a>")
		} else {
			b.WriteString("<span class=\"trace-pill unresolved\" title=\"No global definition found\">{{" + escape(name) + "}}</span>")
		}
	}
	b.WriteString("</div>")
}

func taskLocalVarKind(task Task, name string) (string, bool) {
	for _, op := range task.Ops {
		switch op.Kind {
		case OpCall:
			if op.Call.Assign == name {
				return "call result", true
			}
		case OpBash:
			if op.Bash.Name == name {
				return "bash capture", true
			}
		case OpFor:
			if op.For.VarName == name || (op.For.VarName == "" && name == "N") {
				return "loop", true
			}
		}
	}
	return "", false
}

func writeCallRefs(b *strings.Builder, calls []callRef) {
	if len(calls) == 0 {
		return
	}
	b.WriteString("<div class=\"call-line\">" + i18n("definition calls", "定义调用") + ": ")
	for _, call := range calls {
		label := call.Name
		if call.Assign != "" {
			label += " -> " + call.Assign
		}
		b.WriteString("<a class=\"trace-link\" data-trace=\"def-" + escape(anchorID(call.Name)) + "\" title=\"Jump to definition body\" href=\"#def-" + escape(anchorID(call.Name)) + "\">" + escape(label) + "</a><span class=\"call-source\">" + escape(call.Source) + "</span> ")
	}
	b.WriteString("</div>")
}

func writeDocumentPanel(b *strings.Builder, view planHTMLView) {
	b.WriteString("<section class=\"panel\"><h2>" + i18n("Document View", "文档视图") + "</h2><div class=\"document\">")
	for i, block := range view.blocks {
		writeMarkdownDoc(b, block.Prefix)
		b.WriteString("<details class=\"doc-block\" id=\"block-" + fmt.Sprint(i+1) + "\" open>")
		b.WriteString("<summary class=\"doc-block-head\"><span>" + i18n("Block", "块") + " " + fmt.Sprint(i+1) + "</span>")
		if ref, ok := view.taskByBlock[i]; ok {
			b.WriteString("<a href=\"#task-" + fmt.Sprint(ref.Number) + "\">" + i18n("Task", "任务") + " " + fmt.Sprint(ref.Number) + "</a>")
		} else if _, ok := view.controlByBlock[i]; ok {
			b.WriteString("<span>" + i18n("control", "控制") + "</span>")
		} else if len(view.resources[i]) > 0 {
			b.WriteString("<span>" + i18n("resource", "资源") + "</span>")
		}
		b.WriteString("</summary>")
		writeBlockBody(b, view, i, block.Body)
		b.WriteString("</details>")
		writeMarkdownDoc(b, block.Sep)
	}
	b.WriteString("</div></section>\n")
}

func writeBlockBody(b *strings.Builder, view planHTMLView, index int, body string) {
	if control, ok := view.controlByBlock[index]; ok {
		b.WriteString("<div class=\"control-banner\">" + escape(displayControlBlock(control)) + "</div>")
	}
	if chips := view.resources[index]; len(chips) > 0 {
		b.WriteString("<div class=\"chip-row\">")
		for _, chip := range chips {
			b.WriteString("<span class=\"op-chip " + escape(chip.Kind) + "\">" + escape(chip.Kind) + " · " + escape(chip.Label) + "</span>")
		}
		b.WriteString("</div>")
	}
	if ref, ok := view.taskByBlock[index]; ok {
		writeTaskOps(b, ref.Task)
		writeVariableRefs(b, ref.Task, view)
		writeCallRefs(b, view.taskCalls[index])
	}
	b.WriteString("<pre class=\"raw-block\">" + escape(strings.TrimRight(body, "\r\n")) + "</pre>")
}

func writeMarkdownDoc(b *strings.Builder, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	lines := SplitLines(text)
	inFence := false
	var fence strings.Builder
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		b.WriteString("<p class=\"doc-p\">" + escape(strings.Join(para, " ")) + "</p>")
		para = nil
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				b.WriteString("<pre class=\"code\">" + escape(fence.String()) + "</pre>")
				fence.Reset()
				inFence = false
			} else {
				flushPara()
				inFence = true
			}
			continue
		}
		if inFence {
			fence.WriteString(line)
			fence.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			flushPara()
			continue
		}
		if level, title, ok := parseMarkdownHeading(line); ok {
			flushPara()
			tag := "h3"
			if level <= 1 {
				tag = "h2"
			} else if level == 2 {
				tag = "h3"
			} else {
				tag = "h4"
			}
			b.WriteString("<" + tag + " class=\"doc-heading\">" + escape(title) + "</" + tag + ">")
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flushPara()
			b.WriteString("<div class=\"doc-bullet\">" + escape(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))) + "</div>")
			continue
		}
		para = append(para, trimmed)
	}
	flushPara()
	if inFence {
		b.WriteString("<pre class=\"code\">" + escape(fence.String()) + "</pre>")
	}
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
	sort.Strings(out)
	return out
}

func writeStat(b *strings.Builder, value int, label, labelZH string) {
	fmt.Fprintf(b, "<div class=\"stat\"><strong>%d</strong><span>%s</span></div>\n", value, i18n(label, labelZH))
}

func importSummary(imports []ImportDecl) string {
	parts := make([]string, 0, len(imports))
	for _, item := range imports {
		if item.Namespace != "" {
			parts = append(parts, item.Namespace+" from "+item.Path)
		} else {
			parts = append(parts, item.Path)
		}
	}
	return joinSummary(parts)
}

func globalSummary(bindings []GlobalBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		parts = append(parts, binding.Name)
	}
	return joinSummary(parts)
}

func poolSummary(pools []PoolDecl) string {
	parts := make([]string, 0, len(pools))
	for _, pool := range pools {
		detail := fmt.Sprintf("%s max %d", pool.Name, pool.Max)
		if pool.Buffer >= 0 {
			detail += fmt.Sprintf(" buffer %d", pool.Buffer)
		}
		parts = append(parts, detail)
	}
	return joinSummary(parts)
}

func dbDeclSummary(dbs []DBDecl) string {
	parts := make([]string, 0, len(dbs))
	for _, db := range dbs {
		parts = append(parts, fmt.Sprintf("%s %s/%s %s", db.Name, db.Scope, db.Persist, db.Access))
	}
	return joinSummary(parts)
}

func skillDeclSummary(skills []SkillDecl) string {
	parts := make([]string, 0, len(skills))
	for _, skill := range skills {
		parts = append(parts, skill.Name+" from "+skill.Path)
	}
	return joinSummary(parts)
}

func mcpDeclSummary(mcps []MCPDecl) string {
	parts := make([]string, 0, len(mcps))
	for _, mcp := range mcps {
		parts = append(parts, mcp.Name)
	}
	return joinSummary(parts)
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

func i18n(en, zh string) string {
	if zh == "" {
		zh = en
	}
	return `<span data-en="` + escape(en) + `" data-zh="` + escape(zh) + `">` + escape(en) + `</span>`
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
          branch.nodes.length ? branch.nodes.map(renderFlowNode) : el("div", { class: "muted", text: t("No runnable task", "没有可运行任务") })
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

func planHTMLCSS() string {
	return `<style>
:root {
  color-scheme: light;
  --ink: #172033;
  --muted: #64748b;
  --line: #cbd5e1;
  --paper: #f6f8fb;
  --card: #ffffff;
  --blue: #1d4ed8;
  --teal: #0f766e;
  --green: #15803d;
  --amber: #b45309;
  --red: #b91c1c;
  --violet: #6d28d9;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: var(--ink);
  background: #f6f8fb;
  --map-col: minmax(360px, 34vw);
}
header {
  padding: 24px 28px 18px;
  border-bottom: 1px solid #dbe3ef;
  background: #fff;
}
h1 { margin: 0 0 6px; font-size: 28px; letter-spacing: 0; }
.subtitle { color: var(--muted); font-size: 14px; }
main { width: 100%; margin: 0; padding: 18px 20px 38px; }
.stats {
  display: grid;
  grid-template-columns: repeat(8, minmax(105px, 1fr));
  gap: 10px;
  margin-bottom: 18px;
}
.stat {
  background: var(--card);
  border: 1px solid #dbe3ef;
  border-radius: 8px;
  padding: 12px 13px;
}
.stat strong { display: block; font-size: 22px; line-height: 1.1; }
.stat span { display: block; margin-top: 4px; color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: .06em; }
.layout {
  display: grid;
  grid-template-columns: var(--map-col) 8px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
}
.splitter {
  position: sticky;
  top: 14px;
  height: calc(100vh - 28px);
  border-radius: 999px;
  background: linear-gradient(180deg, #dbe3ef, #b6c5d8, #dbe3ef);
  cursor: col-resize;
}
.splitter:hover, body.resizing .splitter { background: #64748b; }
.layout-rail { display: none; }
.panel {
  background: var(--card);
  border: 1px solid #dbe3ef;
  border-radius: 8px;
  box-shadow: 0 10px 28px rgba(15, 23, 42, .06);
  overflow: hidden;
}
.panel h2 { margin: 0; padding: 14px 16px; font-size: 15px; border-bottom: 1px solid #e5eaf2; }
.map-panel { position: sticky; top: 14px; max-height: calc(100vh - 28px); overflow: auto; }
.map { padding: 16px; }
.map-node, .condition-node {
  display: block;
  position: relative;
  color: inherit;
  text-decoration: none;
  border: 1px solid #d8e2ef;
  background: #fff;
  border-left: 5px solid var(--blue);
  border-radius: 8px;
  padding: 11px 12px;
  margin: 0 0 16px;
}
.map-node:not(:last-child)::after {
  content: "";
  position: absolute;
  left: 20px;
  bottom: -17px;
  width: 2px;
  height: 16px;
  background: var(--line);
}
.map-node.meta, .map-node.resources { border-left-color: var(--teal); }
.map-node.fanout { border-left-color: var(--violet); }
.map-node.wait { border-left-color: var(--amber); }
.map-node.end { border-left-color: var(--red); }
.node-kicker { display: block; color: var(--muted); font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em; margin-bottom: 4px; }
.node-title { display: block; font-weight: 750; font-size: 13px; line-height: 1.35; overflow-wrap: anywhere; }
.node-detail { margin-top: 5px; color: var(--muted); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 11px; line-height: 1.45; overflow-wrap: anywhere; }
.branch-group {
  position: relative;
  border: 1px solid #dbe3ef;
  border-radius: 8px;
  padding: 10px;
  margin: 0 0 16px;
  background: #f8fafc;
}
.condition-node {
  margin-bottom: 10px;
  border-left-color: var(--green);
}
.branch-columns { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.branch-col {
  min-width: 0;
  border: 1px dashed #cbd5e1;
  border-radius: 8px;
  padding: 9px;
  background: #fff;
}
.branch-label {
  margin-bottom: 8px;
  color: var(--muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: .05em;
}
.branch-label.true { color: var(--green); }
.branch-label.false { color: var(--red); }
.join-bar {
  margin: 10px auto 0;
  width: max-content;
  max-width: 100%;
  border-radius: 999px;
  padding: 4px 10px;
  background: #e2e8f0;
  color: #475569;
  font-size: 11px;
  font-weight: 700;
}
.empty-branch { color: var(--muted); font-size: 12px; padding: 8px; border-radius: 6px; background: #f8fafc; }
.join-list, .join-note { margin-top: 7px; color: #92400e; font-size: 12px; }
.content-stack { display: grid; gap: 14px; min-width: 0; }
.resource-grid, .definition-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 12px; padding: 16px; }
.resource-grid.dense { grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
.resource-card, .definition-card, .task, .doc-block, .import-card {
  border: 1px solid #dbe3ef;
  border-radius: 8px;
  background: #fff;
  padding: 14px;
}
.import-list { display: grid; gap: 10px; padding: 16px; }
.import-card summary {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: baseline;
}
.import-card summary a { color: var(--blue); text-decoration: none; font-size: 12px; }
.resource-card strong { display: block; font-size: 13px; }
.resource-card > span { float: right; margin-top: -19px; color: var(--blue); font-weight: 800; }
.resource-card p { clear: both; margin: 8px 0 0; color: var(--muted); font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
.tasks { display: grid; gap: 12px; padding: 16px; }
.card-head, .doc-block-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 9px; }
.card-head h3 { margin: 0; font-size: 16px; }
.badge {
  display: inline-flex;
  align-items: center;
  min-height: 23px;
  border-radius: 999px;
  padding: 3px 9px;
  background: #e0f2fe;
  color: #075985;
  font-size: 12px;
  font-weight: 750;
  text-decoration: none;
  white-space: nowrap;
}
.flow-text { display: none; }
.chip-row { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 9px; }
.mini-chip, .op-chip {
  display: inline-flex;
  align-items: center;
  min-height: 22px;
  border-radius: 999px;
  padding: 3px 8px;
  background: #eef2f7;
  color: #334155;
  font-size: 11px;
  font-weight: 700;
  overflow-wrap: anywhere;
}
.op-chip.for { background: #ede9fe; color: #5b21b6; }
.op-chip.go { background: #f3e8ff; color: #6b21a8; }
.op-chip.wait { background: #fef3c7; color: #92400e; }
.op-chip.call { background: #dcfce7; color: #166534; }
.op-chip.bash { background: #e0f2fe; color: #075985; }
.op-chip.db { background: #ccfbf1; color: #115e59; }
.op-chip.mcp, .op-chip.skill { background: #f1f5f9; color: #334155; }
.op-list {
  display: grid;
  gap: 7px;
  margin: 8px 0 0;
  padding: 0;
  list-style: none;
}
.op-item {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  border: 1px solid #e2e8f0;
  border-left: 4px solid #94a3b8;
  border-radius: 8px;
  padding: 8px 10px;
  background: #fbfdff;
}
.op-item.for { border-left-color: var(--violet); }
.op-item.go { border-left-color: #7c3aed; background: #faf5ff; }
.op-item.wait { border-left-color: var(--amber); background: #fffbeb; }
.op-item.call { border-left-color: var(--green); background: #f0fdf4; }
.op-item.execute { border-left-color: var(--blue); }
.op-kind {
  color: var(--muted);
  font-size: 11px;
  font-weight: 900;
  text-transform: uppercase;
  letter-spacing: .06em;
}
.op-label {
  min-width: 0;
  color: #1f2937;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}
.fanout-lanes {
  grid-column: 2;
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 7px;
  padding-top: 7px;
  border-top: 1px dashed #d8b4fe;
  overflow-x: auto;
}
.fanout-lanes::before {
  content: "dispatch";
  flex: 0 0 auto;
  color: #6b21a8;
  font-size: 11px;
  font-weight: 800;
}
.lane {
  flex: 0 0 auto;
  position: relative;
  min-width: 70px;
  border-radius: 999px;
  padding: 5px 9px;
  background: #ede9fe;
  color: #5b21b6;
  font-size: 11px;
  font-weight: 800;
  text-align: center;
}
.lane:not(:last-child)::after {
  content: "";
  position: absolute;
  right: -8px;
  top: 50%;
  width: 8px;
  height: 1px;
  background: #c4b5fd;
}
.lane.dynamic { background: #e0f2fe; color: #075985; }
.trace-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 9px;
  color: var(--muted);
  font-size: 12px;
}
.trace-row > span:first-child { font-weight: 800; }
.trace-pill, .trace-link {
  display: inline-flex;
  align-items: center;
  border-radius: 999px;
  padding: 2px 7px;
  background: #eff6ff;
  color: var(--blue);
  font-size: 11px;
  font-weight: 800;
  text-decoration: none;
}
.trace-pill.unresolved { background: #f1f5f9; color: var(--muted); }
.trace-pill.local { background: #ecfdf5; color: #166534; }
.trace-line { font-size: 12px; color: var(--muted); }
.inline-code {
  clear: both;
  margin: 8px 0 0;
  padding: 8px;
  border-radius: 7px;
  background: #f1f5f9;
  color: #334155;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
}
.prompt, .raw-block, .code {
  white-space: pre-wrap;
  overflow-x: auto;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
}
.prompt { margin-top: 8px; color: #334155; }
.raw-block, .code {
  margin: 10px 0 0;
  padding: 11px;
  border-radius: 7px;
  background: #0f172a;
  color: #e2e8f0;
}
.raw-block.compact { max-height: 260px; }
.schema { background: #10251f; color: #d1fae5; }
details { margin-top: 10px; }
summary { cursor: pointer; color: #334155; font-size: 13px; font-weight: 750; }
.meta-line, .call-line { margin-top: 9px; color: var(--muted); font-size: 12px; line-height: 1.5; }
.call-line a { color: var(--blue); text-decoration: none; font-weight: 750; margin-right: 4px; }
.call-source { color: var(--muted); font-size: 11px; margin-right: 8px; }
.document { padding: 16px; }
.doc-heading { margin: 18px 0 8px; letter-spacing: 0; }
.doc-p { margin: 8px 0; color: #334155; line-height: 1.6; }
.doc-bullet { position: relative; margin: 5px 0; padding-left: 16px; color: #334155; }
.doc-bullet::before { content: ""; position: absolute; left: 2px; top: .72em; width: 5px; height: 5px; border-radius: 50%; background: var(--blue); }
.doc-block { margin: 12px 0; }
.doc-block-head {
  margin: -4px -4px 10px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: .04em;
}
.doc-block-head a { color: var(--blue); text-decoration: none; }
.control-banner {
  border-radius: 7px;
  padding: 8px 10px;
  background: #ecfdf5;
  color: #166534;
  font-size: 12px;
  font-weight: 750;
}
.empty { padding: 20px; color: var(--muted); }
.highlight { outline: 3px solid rgba(37, 99, 235, .28); outline-offset: 2px; }
.trace-hover { box-shadow: 0 0 0 3px rgba(15, 118, 110, .22); }
.tooltip {
  position: fixed;
  z-index: 30;
  max-width: 360px;
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  padding: 8px 10px;
  background: #0f172a;
  color: #e2e8f0;
  font-size: 12px;
  line-height: 1.45;
  pointer-events: none;
  box-shadow: 0 12px 26px rgba(15, 23, 42, .18);
}
@media (max-width: 980px) {
  main { padding: 16px 12px 34px; }
  header { padding: 22px 18px 16px; }
  .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .layout { grid-template-columns: 1fr; }
  .splitter { display: none; }
  .map-panel { position: static; max-height: none; }
}
@media (max-width: 620px) {
  .branch-columns { grid-template-columns: 1fr; }
  .card-head, .doc-block-head { align-items: flex-start; flex-direction: column; }
  .op-item { grid-template-columns: 1fr; }
  .fanout-lanes { grid-column: 1; }
}
</style>
`
}

func planHTMLJS() string {
	return `<script>
(() => {
  const languages = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || "en"];
  const useZH = languages.some((lang) => String(lang).toLowerCase().startsWith("zh"));
  const key = useZH ? "zh" : "en";
  document.documentElement.lang = useZH ? "zh-CN" : "en";
  document.querySelectorAll("[data-en][data-zh]").forEach((el) => {
    el.textContent = el.dataset[key] || el.dataset.en || "";
  });
  document.querySelectorAll('a[href^="#"]').forEach((link) => {
    link.addEventListener("click", () => {
      const id = link.getAttribute("href").slice(1);
      const target = document.getElementById(id);
      if (!target) return;
      if (target.tagName === "DETAILS") target.open = true;
      target.closest("details")?.setAttribute("open", "");
      document.querySelectorAll(".highlight").forEach((el) => el.classList.remove("highlight"));
      setTimeout(() => target.classList.add("highlight"), 80);
    });
  });
  const saved = localStorage.getItem("atm-plan-map-col");
  if (saved) document.body.style.setProperty("--map-col", saved);
  const splitter = document.querySelector(".splitter");
  if (splitter) {
    splitter.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      splitter.setPointerCapture(event.pointerId);
      document.body.classList.add("resizing");
      const move = (moveEvent) => {
        const width = Math.max(280, Math.min(window.innerWidth - 420, moveEvent.clientX - 20));
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
  const tooltip = document.createElement("div");
  tooltip.className = "tooltip";
  tooltip.hidden = true;
  document.body.appendChild(tooltip);
  const showTip = (event, text) => {
    if (!text) return;
    tooltip.textContent = text;
    tooltip.hidden = false;
    const x = Math.min(window.innerWidth - 380, event.clientX + 14);
    const y = Math.min(window.innerHeight - 90, event.clientY + 14);
    tooltip.style.left = Math.max(8, x) + "px";
    tooltip.style.top = Math.max(8, y) + "px";
  };
  document.querySelectorAll("[data-trace]").forEach((el) => {
    const target = document.getElementById(el.dataset.trace);
    el.addEventListener("mouseenter", (event) => {
      target?.classList.add("trace-hover");
      showTip(event, el.getAttribute("title") || el.textContent);
    });
    el.addEventListener("mousemove", (event) => showTip(event, el.getAttribute("title") || el.textContent));
    el.addEventListener("mouseleave", () => {
      target?.classList.remove("trace-hover");
      tooltip.hidden = true;
    });
  });
})();
</script>
`
}
