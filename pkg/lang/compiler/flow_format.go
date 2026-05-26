package compiler

import langformat "github.com/chinaykc/atm/pkg/lang/format"

func FormatTaskFlow(task Task) string {
	return langformat.TaskFlow(task)
}
