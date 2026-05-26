package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strings"
)

func ParseGlobalImportBlock(body string) ([]ImportDecl, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return nil, false, err
	}
	lines := SplitLines(body)
	var imports []ImportDecl
	seen := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "/import" {
			return nil, true, fmt.Errorf("/import requires a path")
		}
		if !strings.HasPrefix(line, "/import ") {
			return nil, false, nil
		}
		fields := strings.Fields(line)
		switch len(fields) {
		case 2:
			imports = append(imports, ImportDecl{Path: fields[1]})
		case 4:
			if fields[2] != "from" {
				return nil, true, fmt.Errorf("/import namespace form is /import name from path")
			}
			if !isVariableName(fields[1]) {
				return nil, true, fmt.Errorf("invalid import namespace %q", fields[1])
			}
			imports = append(imports, ImportDecl{Namespace: fields[1], Path: fields[3]})
		default:
			return nil, true, fmt.Errorf("/import requires a path or namespace from path")
		}
		seen = true
	}
	return imports, seen, nil
}
