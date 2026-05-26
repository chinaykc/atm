package engine

import (
	"fmt"
	"io"

	"github.com/fatih/color"
)

func writeATMEvent(w io.Writer, kind, format string, args ...any) {
	toolLabel := color.New(color.FgHiCyan, color.Bold).SprintFunc()
	kindLabel := atmKindColor(kind).SprintFunc()
	fmt.Fprintf(w, "%s %s %s\n", toolLabel("[atm]"), kindLabel(kind), fmt.Sprintf(format, args...))
}

func atmKindColor(kind string) *color.Color {
	switch kind {
	case "run":
		return color.New(color.FgHiMagenta, color.Bold)
	case "done":
		return color.New(color.FgHiGreen, color.Bold)
	case "output", "result":
		return color.New(color.FgHiYellow, color.Bold)
	default:
		return color.New(color.FgHiBlue, color.Bold)
	}
}
