package ir

func FlattenTaskFlow(task Task) []FlatOp {
	return FlattenFlow(task.Flow)
}

func FlattenFlow(node FlowNode) []FlatOp {
	var ops []FlatOp
	switch node.Kind {
	case FlowSeq:
	case FlowCd:
		ops = append(ops, FlatOp{Kind: FlatOpCd, Cd: node.Cd})
	case FlowBash:
		ops = append(ops, FlatOp{Kind: FlatOpBash, Bash: node.Bash})
	case FlowFor:
		ops = append(ops, FlatOp{Kind: FlatOpFor, For: node.For})
	case FlowIf:
		ops = append(ops, FlatOp{Kind: FlatOpIf, If: node.If})
	case FlowGo:
		ops = append(ops, FlatOp{Kind: FlatOpGo, Pool: node.Pool})
	case FlowWait:
		ops = append(ops, FlatOp{Kind: FlatOpWait, Pool: node.Pool})
	case FlowCall:
		ops = append(ops, FlatOp{Kind: FlatOpCall, Call: node.Call})
	case FlowWebhook:
		ops = append(ops, FlatOp{Kind: FlatOpWebhook, Webhook: node.Webhook})
	case FlowReturn:
		ops = append(ops, FlatOp{Kind: FlatOpReturn, Return: node.Return})
	case FlowExecute:
		ops = append(ops, FlatOp{Kind: FlatOpExecute, ExecuteOptions: node.ExecuteOptions})
	default:
		if node.Kind != "" {
			ops = append(ops, FlatOp{Kind: FlatOpKind(node.Kind)})
		}
	}
	for _, child := range node.Children {
		ops = append(ops, FlattenFlow(child)...)
	}
	for _, child := range node.ElseChildren {
		ops = append(ops, FlattenFlow(child)...)
	}
	return ops
}
