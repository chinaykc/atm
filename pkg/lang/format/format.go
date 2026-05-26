package format

import (
	"fmt"
	"strings"

	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/marker"
)

func BlockBody(body string) string {
	return marker.FormatBlockBody(body)
}

func TaskFlow(task ir.Task) string {
	if strings.TrimSpace(task.Prompt) != "" {
		if parts := formatWaitAgentFlow(task.Flow); len(parts) > 0 {
			return strings.Join(parts, " -> ")
		}
	}
	return strings.Join(formatFlowNode(task.Flow), " -> ")
}

func formatWaitAgentFlow(node ir.FlowNode) []string {
	if node.Kind != ir.FlowSeq {
		return nil
	}
	var parts []string
	for i := 0; i < len(node.Children); i++ {
		child := node.Children[i]
		if child.Kind == ir.FlowWait && i+1 < len(node.Children) && node.Children[i+1].Kind == ir.FlowExecute {
			if child.Pool != "" {
				parts = append(parts, fmt.Sprintf("WaitAgent(%s)", child.Pool))
			} else {
				parts = append(parts, "WaitAgent")
			}
			i++
			continue
		}
		parts = append(parts, formatFlowNode(child)...)
	}
	return parts
}

func formatFlowNode(node ir.FlowNode) []string {
	switch node.Kind {
	case "", ir.FlowSeq:
		var parts []string
		for _, child := range node.Children {
			parts = append(parts, formatFlowNode(child)...)
		}
		return parts
	case ir.FlowCd, ir.FlowBash, ir.FlowFor, ir.FlowIf, ir.FlowGo, ir.FlowWait, ir.FlowCall, ir.FlowReturn, ir.FlowExecute:
		op, ok := flowNodeFlatOp(node)
		if !ok {
			return nil
		}
		label := formatFlatOp(op)
		if node.Kind == ir.FlowIf && len(node.ElseChildren) > 0 {
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

func formatFlowNodes(nodes []ir.FlowNode) []string {
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

func flowNodeFlatOp(node ir.FlowNode) (ir.FlatOp, bool) {
	switch node.Kind {
	case ir.FlowCd:
		return ir.FlatOp{Kind: ir.FlatOpCd, Cd: node.Cd}, true
	case ir.FlowBash:
		return ir.FlatOp{Kind: ir.FlatOpBash, Bash: node.Bash}, true
	case ir.FlowFor:
		return ir.FlatOp{Kind: ir.FlatOpFor, For: node.For}, true
	case ir.FlowIf:
		return ir.FlatOp{Kind: ir.FlatOpIf, If: node.If}, true
	case ir.FlowGo:
		return ir.FlatOp{Kind: ir.FlatOpGo, Pool: node.Pool}, true
	case ir.FlowWait:
		return ir.FlatOp{Kind: ir.FlatOpWait, Pool: node.Pool}, true
	case ir.FlowCall:
		return ir.FlatOp{Kind: ir.FlatOpCall, Call: node.Call}, true
	case ir.FlowReturn:
		return ir.FlatOp{Kind: ir.FlatOpReturn, Return: node.Return}, true
	case ir.FlowExecute:
		return ir.FlatOp{Kind: ir.FlatOpExecute, ExecuteOptions: node.ExecuteOptions}, true
	default:
		return ir.FlatOp{}, false
	}
}

func formatFlatOp(op ir.FlatOp) string {
	switch op.Kind {
	case ir.FlatOpCd:
		if op.Cd.MustExist {
			return fmt.Sprintf("Cd(%s, must-exist)", op.Cd.Path)
		}
		return fmt.Sprintf("Cd(%s)", op.Cd.Path)
	case ir.FlatOpBash:
		if op.Bash.Name != "" {
			return fmt.Sprintf("LazyBash(%s)", op.Bash.Name)
		}
		return "Bash"
	case ir.FlatOpFor:
		return formatForFlow(op.For)
	case ir.FlatOpIf:
		return formatIfFlow(op.If)
	case ir.FlatOpGo:
		if op.Pool != "" {
			return fmt.Sprintf("Go(%s)", op.Pool)
		}
		return "Go"
	case ir.FlatOpWait:
		if op.Pool != "" {
			return fmt.Sprintf("Wait(%s)", op.Pool)
		}
		return "Wait"
	case ir.FlatOpCall:
		if op.Call.Assign != "" {
			return fmt.Sprintf("LazyCall(%s -> %s)", op.Call.Name, op.Call.Assign)
		}
		return fmt.Sprintf("Call(%s)", op.Call.Name)
	case ir.FlatOpReturn:
		return "Return"
	case ir.FlatOpExecute:
		return appendRunOptionFlow("Execute", op.ExecuteOptions)
	default:
		return string(op.Kind)
	}
}

func formatIfFlow(branch ir.If) string {
	switch branch.Condition.Kind {
	case ir.ConditionExpr:
		return fmt.Sprintf("If(expr:%s)", branch.Condition.Text)
	case ir.ConditionNatural:
		return fmt.Sprintf("If(%s)", branch.Condition.Text)
	default:
		return "If"
	}
}

func formatForFlow(step ir.For) string {
	name := step.VarName
	if name == "" {
		name = "run"
	}
	if step.Source.Kind == ir.ConditionExpr {
		detail := fmt.Sprintf("For(%s in expr(%q))", name, step.Source.Text)
		if step.Condition.Text != "" {
			detail += formatForUntilFlow(step.Condition)
		}
		return appendRunOptionFlow(detail, step.Options)
	}
	if step.Source.Kind == ir.ConditionCall {
		detail := fmt.Sprintf("For(%s in call(%q))", name, step.Source.Text)
		if step.Condition.Text != "" {
			detail += formatForUntilFlow(step.Condition)
		}
		return appendRunOptionFlow(detail, step.Options)
	}
	if step.MaxRuns == 0 && step.Condition.Kind == ir.ConditionExpr {
		return appendRunOptionFlow(fmt.Sprintf("For(%s until expr(%q))", name, step.Condition.Text), step.Options)
	}
	detail := fmt.Sprintf("For(%s x %d)", name, step.MaxRuns)
	if len(step.Values) > 0 && len(step.Values) <= 4 {
		detail = fmt.Sprintf("For(%s in [%s])", name, strings.Join(step.Values, " "))
	}
	if step.Condition.Text != "" {
		detail += formatForUntilFlow(step.Condition)
	}
	return appendRunOptionFlow(detail, step.Options)
}

func formatForUntilFlow(condition ir.Condition) string {
	if condition.Kind == ir.ConditionExpr {
		return fmt.Sprintf(" until expr(%q)", condition.Text)
	}
	return fmt.Sprintf(" until %q", condition.Text)
}

func appendRunOptionFlow(base string, options ir.RunOptions) string {
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
