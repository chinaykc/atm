package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"strings"
)

func ParseIfBlock(body string) (IfBlock, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return IfBlock{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok || !isIfCommandLine(line) {
		return IfBlock{}, false, nil
	}
	if line == "/if" {
		lines := SplitLines(body)
		for i, candidate := range lines {
			if strings.TrimSpace(candidate) == "" {
				continue
			}
			if strings.TrimSpace(candidate) != "/if" {
				break
			}
			if i+1 >= len(lines) {
				break
			}
			fence, ok := parseAnyFenceStart(lines[i+1])
			if !ok {
				break
			}
			condition, next, err := collectTextFenceBlock(lines, i+2, fence)
			if err != nil {
				return IfBlock{}, true, err
			}
			if strings.TrimSpace(condition) == "" {
				return IfBlock{}, true, fmt.Errorf("/if requires a condition")
			}
			rest := strings.TrimSpace(strings.Join(lines[next:], ""))
			return IfBlock{
				Condition:  Condition{Kind: ConditionNatural, Text: condition},
				HeaderOnly: rest == "",
				Body:       rest,
			}, true, nil
		}
	}
	fields, err := commandFields(line)
	if err != nil {
		return IfBlock{}, true, err
	}
	condition, next, err := parseIfCommandFields(fields, 0)
	if err != nil {
		return IfBlock{}, true, err
	}
	if next != len(fields) {
		return IfBlock{}, false, nil
	}
	rest = strings.TrimSpace(rest)
	return IfBlock{Condition: condition, HeaderOnly: rest == "", Body: rest}, true, nil
}

func ParseElseBlock(body string) (ElseBlock, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return ElseBlock{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok || line != "/else" {
		return ElseBlock{}, false, nil
	}
	rest = strings.TrimSpace(rest)
	return ElseBlock{HeaderOnly: rest == "", Body: rest}, true, nil
}

func isIfCommandLine(line string) bool {
	return line == "/if" || strings.HasPrefix(line, "/if ") || strings.HasPrefix(line, "/if(")
}

func isIfCommandToken(token string) bool {
	return token == "/if" || strings.HasPrefix(token, "/if(")
}

func parseIfCommandFields(fields []string, start int) (Condition, int, error) {
	if start >= len(fields) {
		return Condition{}, start, fmt.Errorf("/if requires a condition")
	}
	token := fields[start]
	if token == "/if" {
		next := start + 1
		if next >= len(fields) || isCommandToken(fields[next]) {
			return Condition{}, start, fmt.Errorf("/if requires a condition")
		}
		if strings.HasPrefix(fields[next], "(") {
			parts, after, err := collectExprConditionTokens(fields, next)
			if err != nil {
				return Condition{}, start, err
			}
			return Condition{Kind: ConditionExpr, Text: trimExprParens(strings.Join(parts, " "))}, after, nil
		}
		conditionStart := next
		for next < len(fields) && !isCommandToken(fields[next]) {
			next++
		}
		return Condition{Kind: ConditionNatural, Text: strings.Join(unquoteCommandValues(fields[conditionStart:next]), " ")}, next, nil
	}
	if strings.HasPrefix(token, "/if(") {
		first := strings.TrimPrefix(token, "/if")
		parts, after, err := collectExprConditionTokens(append([]string{first}, fields[start+1:]...), 0)
		if err != nil {
			return Condition{}, start, err
		}
		return Condition{Kind: ConditionExpr, Text: trimExprParens(strings.Join(parts, " "))}, start + after, nil
	}
	return Condition{}, start, fmt.Errorf("expected /if")
}
