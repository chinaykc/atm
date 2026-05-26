package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strconv"
	"strings"
)

func ParseGlobalPoolBlock(body string) ([]PoolDecl, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return nil, false, err
	}
	lines := SplitLines(body)
	var pools []PoolDecl
	seen := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "/pool" {
			return nil, true, fmt.Errorf("/pool requires name, max concurrency, and optional buffer size")
		}
		if !strings.HasPrefix(line, "/pool ") {
			return nil, false, nil
		}
		fields := strings.Fields(line)
		if len(fields) != 3 && len(fields) != 4 {
			return nil, true, fmt.Errorf("/pool requires name, max concurrency, and optional buffer size")
		}
		name := fields[1]
		if !isVariableName(name) {
			return nil, true, fmt.Errorf("invalid pool name %q", name)
		}
		max := parsePositiveIntToken(fields[2])
		if max <= 0 {
			return nil, true, fmt.Errorf("/pool %s max concurrency must be a positive integer", name)
		}
		buffer := -1
		if len(fields) == 4 {
			n, err := strconv.Atoi(fields[3])
			if err != nil || n < 0 {
				return nil, true, fmt.Errorf("/pool %s buffer size must be a non-negative integer", name)
			}
			buffer = n
		}
		pools = append(pools, PoolDecl{Name: name, Max: max, Buffer: buffer})
		seen = true
	}
	return pools, seen, nil
}
