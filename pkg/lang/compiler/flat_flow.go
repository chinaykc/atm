package compiler

import "github.com/chinaykc/atm/pkg/lang/ir"

func FlattenTaskFlow(task Task) []FlatOp {
	return ir.FlattenTaskFlow(task)
}
