package compiler

import (
	"fmt"
	"slices"
)

type CompileOptions struct {
	Root                string
	Context             string
	NamedContexts       map[string]string
	ContextRefsResolved bool
}

func AttachElseTask(task Task, elseTask Task) (Task, error) {
	elseFlow := elseTask.Flow
	setExecutePrompt(&elseFlow, elseTask.Prompt)
	if !attachElseChildren(&task.Flow, elseFlow.Children) {
		return Task{}, fmt.Errorf("/else has no matching /if in previous task")
	}
	for name, value := range elseTask.Vars {
		if _, ok := task.Vars[name]; !ok {
			task.Vars[name] = value
		}
	}
	return task, nil
}

func EmptyTask(index int, globals map[string]any) Task {
	return Task{
		BlockIndex: index,
		Flow:       FlowNode{Kind: FlowSeq},
		Vars:       CloneVars(globals),
	}
}

func TaskHasFlowIf(task Task) bool {
	return flowHasKind(task.Flow, FlowIf)
}

func setExecutePrompt(node *FlowNode, prompt string) {
	if node.Kind == FlowExecute {
		node.Prompt = prompt
	}
	for i := range node.Children {
		setExecutePrompt(&node.Children[i], prompt)
	}
	for i := range node.ElseChildren {
		setExecutePrompt(&node.ElseChildren[i], prompt)
	}
}

func attachElseChildren(node *FlowNode, elseChildren []FlowNode) bool {
	if node.Kind == FlowIf && len(node.ElseChildren) == 0 {
		node.ElseChildren = elseChildren
		return true
	}
	for i := range node.Children {
		if attachElseChildren(&node.Children[i], elseChildren) {
			return true
		}
	}
	for i := range node.ElseChildren {
		if attachElseChildren(&node.ElseChildren[i], elseChildren) {
			return true
		}
	}
	return false
}

func flowHasKind(node FlowNode, kind FlowKind) bool {
	if node.Kind == kind {
		return true
	}
	for _, child := range node.Children {
		if flowHasKind(child, kind) {
			return true
		}
	}
	for _, child := range node.ElseChildren {
		if flowHasKind(child, kind) {
			return true
		}
	}
	return false
}

func lowerTaskASTToIR(index int, t taskAST) Task {
	flow := lowerTaskASTToFlow(t)

	return Task{
		BlockIndex:  index,
		Name:        t.name,
		Context:     t.context,
		ContextRefs: slices.Clone(t.contextRefs),
		Prompt:      t.prompt,
		Flow:        flow,
		Vars:        CloneVars(t.vars),
		Output:      cloneOutputSpec(t.output),
		Return:      cloneReturnSpec(t.returnSpec),
		DB:          cloneDBTaskConfig(t.db),
		Skill:       cloneSkillTaskConfig(t.skill),
		MCP:         cloneMCPTaskConfig(t.mcp),
		Webhook:     cloneWebhookTaskConfig(t.webhook),
		Cursor:      cursorFromRunningInfo(t.running),
	}
}

func lowerTaskASTToFlow(t taskAST) FlowNode {
	nodes := make([]FlowNode, 0, len(t.flow)+1)
	hasFor := false
	for _, op := range t.flow {
		switch op.kind {
		case astOpCd:
			nodes = append(nodes, FlowNode{Kind: FlowCd, Cd: op.CdCommand})
		case astOpBash:
			nodes = append(nodes, FlowNode{Kind: FlowBash, Bash: op.BashCommand})
		case astOpFor:
			hasFor = true
			nodes = append(nodes, FlowNode{Kind: FlowFor, For: forFromAST(op.step)})
		case astOpIf:
			nodes = append(nodes, FlowNode{Kind: FlowIf, If: If{Condition: op.Condition}})
		case astOpElse:
			nodes = append(nodes, FlowNode{Kind: FlowKind(astOpElse)})
		case astOpGo:
			nodes = append(nodes, FlowNode{Kind: FlowGo, Pool: op.Pool})
		case astOpWait:
			nodes = append(nodes, FlowNode{Kind: FlowWait, Pool: op.Pool})
		case astOpCall:
			nodes = append(nodes, FlowNode{Kind: FlowCall, Call: op.Call})
		case astOpWebhook:
			nodes = append(nodes, FlowNode{Kind: FlowWebhook, Webhook: op.Webhook})
		case astOpReturn:
			nodes = append(nodes, FlowNode{Kind: FlowReturn, Return: op.Return})
		}
	}

	executeOptions := RunOptions{}
	if len(t.steps) > 0 && !hasFor {
		executeOptions = t.steps[0].Options
	}
	nodes = append(nodes, FlowNode{Kind: FlowExecute, ExecuteOptions: executeOptions})
	if t.returnSpec != nil {
		nodes = append(nodes, FlowNode{Kind: FlowReturn, Return: *t.returnSpec})
	}
	return FlowNode{Kind: FlowSeq, Children: nestFlowNodes(nodes)}
}

func nestFlowNodes(flat []FlowNode) []FlowNode {
	nodes := make([]FlowNode, 0, len(flat))
	for i := 0; i < len(flat); i++ {
		node := flat[i]
		switch node.Kind {
		case FlowFor, FlowGo:
			node.Children = nestFlowNodes(flat[i+1:])
			nodes = append(nodes, node)
			return nodes
		case FlowIf:
			elseIndex := -1
			for j := i + 1; j < len(flat); j++ {
				if flat[j].Kind == FlowKind(astOpElse) {
					elseIndex = j
					break
				}
			}
			if elseIndex >= 0 {
				tail := trailingExecuteNodes(flat[elseIndex+1:])
				thenFlat := slices.Clone(flat[i+1 : elseIndex])
				thenFlat = append(thenFlat, tail...)
				elseFlat := slices.Clone(flat[elseIndex+1 : len(flat)-len(tail)])
				elseFlat = append(elseFlat, tail...)
				node.Children = nestFlowNodes(thenFlat)
				node.ElseChildren = nestFlowNodes(elseFlat)
			} else {
				node.Children = nestFlowNodes(flat[i+1:])
			}
			nodes = append(nodes, node)
			return nodes
		case FlowKind(astOpElse):
			return nodes
		case FlowReturn:
			nodes = append(nodes, node)
			return nodes
		default:
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func trailingExecuteNodes(flat []FlowNode) []FlowNode {
	start := len(flat)
	for start > 0 {
		switch flat[start-1].Kind {
		case FlowExecute, FlowReturn:
			start--
		default:
			return flat[start:]
		}
	}
	return flat[start:]
}

func cloneDBTaskConfig(config DBTaskConfig) DBTaskConfig {
	out := DBTaskConfig{IgnoreAll: config.IgnoreAll}
	out.Ignore = slices.Clone(config.Ignore)
	for _, use := range config.Use {
		out.Use = append(out.Use, DBUse{Names: slices.Clone(use.Names), Access: use.Access})
	}
	for _, rule := range config.Access {
		out.Access = append(out.Access, DBAccessRule{Names: slices.Clone(rule.Names), Access: rule.Access})
	}
	return out
}

func cloneSkillTaskConfig(config SkillTaskConfig) SkillTaskConfig {
	return SkillTaskConfig{
		IgnoreAll: config.IgnoreAll,
		Use:       slices.Clone(config.Use),
		Ignore:    slices.Clone(config.Ignore),
	}
}

func cloneMCPTaskConfig(config MCPTaskConfig) MCPTaskConfig {
	return MCPTaskConfig{
		IgnoreAll: config.IgnoreAll,
		Use:       slices.Clone(config.Use),
		Ignore:    slices.Clone(config.Ignore),
		DefUse:    slices.Clone(config.DefUse),
	}
}

func cloneWebhookTaskConfig(config WebhookTaskConfig) WebhookTaskConfig {
	return WebhookTaskConfig{Use: slices.Clone(config.Use)}
}

func cloneOutputSpec(spec *OutputSpec) *OutputSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	return &out
}

func cloneReturnSpec(spec *ReturnSpec) *ReturnSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	out.Output = cloneOutputSpec(spec.Output)
	return &out
}

func forFromAST(step forAST) For {
	return For{
		Options:   step.Options,
		Condition: step.Condition,
		MaxRuns:   step.MaxRuns,
		VarName:   step.VarName,
		Values:    slices.Clone(step.Values),
		Source:    step.Source,
	}
}

func cursorFromRunningInfo(info RunningInfo) ExecutionCursor {
	return ExecutionCursor{
		Active:    info.Active,
		Start:     info.Start,
		OpIndex:   info.StepIndex,
		RunIndex:  info.StepRuns,
		TotalRuns: info.TotalRuns,
	}
}
