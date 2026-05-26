package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

func parseForCommand(fields []string, start int, root string) (forAST, int, error) {
	if start >= len(fields) {
		return forAST{}, start, fmt.Errorf("/for requires an iterator")
	}

	step := forAST{}
	token := fields[start]
	next := start + 1
	if token == "until" || strings.HasPrefix(token, "until(") {
		condition, after, err := parseUntilCondition(fields, start)
		if err != nil {
			return forAST{}, start, err
		}
		if condition.Kind != ConditionExpr {
			return forAST{}, start, fmt.Errorf("/for until without a count requires a parenthesized expression")
		}
		step.VarName = "n"
		step.Condition = condition
		return step, after, nil
	}
	switch {
	case parsePositiveIntToken(token) > 0:
		n := parsePositiveIntToken(token)
		step.MaxRuns = n
		step.VarName = "n"
		step.Values = make([]string, n)
		for i := range step.Values {
			step.Values[i] = strconv.Itoa(i)
		}
	case isVariableName(token):
		if next >= len(fields) || !strings.HasPrefix(fields[next], "in") {
			return forAST{}, start, fmt.Errorf("unsupported /for iterator %q", token)
		}
		step.VarName = token
		inToken := fields[next]
		if inToken == "in" {
			sourceStart := next + 1
			if sourceStart >= len(fields) {
				return forAST{}, start, fmt.Errorf("/for in requires a bracketed list or parenthesized expression")
			}
			if strings.HasPrefix(fields[sourceStart], "[") {
				values, after, err := parseForList(fields, sourceStart)
				if err != nil {
					return forAST{}, start, err
				}
				step.Values = values
				step.MaxRuns = len(values)
				next = after
			} else if strings.HasPrefix(fields[sourceStart], "(") {
				after, err := setDynamicForSource(&step, fields, sourceStart)
				if err != nil {
					return forAST{}, start, err
				}
				next = after
			} else if !isCommandToken(fields[sourceStart]) {
				after, err := setBareDynamicForSource(&step, fields, sourceStart)
				if err != nil {
					return forAST{}, start, err
				}
				next = after
			} else {
				return forAST{}, start, fmt.Errorf("/for in requires a bracketed list or parenthesized expression")
			}
		} else if strings.HasPrefix(inToken, "in(") {
			synthetic := append([]string{strings.TrimPrefix(inToken, "in")}, fields[next+1:]...)
			after, err := setDynamicForSource(&step, synthetic, 0)
			if err != nil {
				return forAST{}, start, err
			}
			next = next + after
		} else {
			return forAST{}, start, fmt.Errorf("/for in requires a bracketed list or parenthesized expression")
		}
	default:
		return forAST{}, start, fmt.Errorf("unsupported /for iterator %q", token)
	}

	if next < len(fields) && (fields[next] == "until" || strings.HasPrefix(fields[next], "until(")) {
		condition, after, err := parseUntilCondition(fields, next)
		if err != nil {
			return forAST{}, start, err
		}
		step.Condition = condition
		next = after
	}
	return step, next, nil
}

func setBareDynamicForSource(step *forAST, fields []string, start int) (int, error) {
	parts, after, err := collectBareForSourceTokens(fields, start)
	if err != nil {
		return start, err
	}
	source := strings.TrimSpace(strings.Join(parts, " "))
	if source == "" {
		return start, fmt.Errorf("/for in requires an expression")
	}
	step.Source = Condition{Kind: ConditionExpr, Text: source}
	return after, nil
}

func setDynamicForSource(step *forAST, fields []string, start int) (int, error) {
	parts, after, err := collectParenthesizedTokens(fields, start, "/for in")
	if err != nil {
		return start, err
	}
	source := trimExprParens(strings.Join(parts, " "))
	if source == "" {
		return start, fmt.Errorf("/for in requires an expression")
	}
	kind := ConditionExpr
	if source == "/call" || strings.HasPrefix(source, "/call ") {
		if _, err := ParseCallExpression(source); err != nil {
			return start, err
		}
		kind = ConditionCall
	}
	step.Source = Condition{Kind: kind, Text: source}
	return after, nil
}

func collectBareForSourceTokens(fields []string, start int) ([]string, int, error) {
	var parts []string
	depth := 0
	var quote rune
	escaped := false
	for i := start; i < len(fields); i++ {
		token := fields[i]
		if depth == 0 && len(parts) > 0 && (isCommandToken(token) || token == "until" || strings.HasPrefix(token, "until(")) {
			return parts, i, nil
		}
		if depth == 0 && len(parts) == 0 && isCommandToken(token) {
			return nil, start, fmt.Errorf("/for in requires an expression")
		}
		parts = append(parts, token)
		for _, r := range token {
			if escaped {
				escaped = false
				continue
			}
			if quote != 0 {
				if r == '\\' {
					escaped = true
					continue
				}
				if r == quote {
					quote = 0
				}
				continue
			}
			switch r {
			case '\'', '"':
				quote = r
			case '(':
				depth++
			case ')':
				depth--
				if depth < 0 {
					return nil, start, fmt.Errorf("/for in expression has unmatched )")
				}
			}
		}
		if depth == 0 {
			return parts, i + 1, nil
		}
	}
	if quote != 0 {
		return nil, start, fmt.Errorf("/for in expression has unterminated quote")
	}
	if depth != 0 {
		return nil, start, fmt.Errorf("/for in expression missing closing )")
	}
	return parts, len(fields), nil
}

func collectParenthesizedTokens(fields []string, start int, label string) ([]string, int, error) {
	var parts []string
	depth := 0
	seen := false
	var quote rune
	escaped := false
	for i := start; i < len(fields); i++ {
		if isCommandToken(fields[i]) && !seen {
			return nil, start, fmt.Errorf("%s requires an expression", label)
		}
		token := fields[i]
		parts = append(parts, token)
		for _, r := range token {
			if escaped {
				escaped = false
				continue
			}
			if quote != 0 {
				if r == '\\' {
					escaped = true
					continue
				}
				if r == quote {
					quote = 0
				}
				continue
			}
			switch r {
			case '\'', '"':
				quote = r
			case '(':
				depth++
				seen = true
			case ')':
				depth--
				if depth == 0 && seen {
					return parts, i + 1, nil
				}
				if depth < 0 {
					return nil, start, fmt.Errorf("%s expression has unmatched )", label)
				}
			}
		}
	}
	if !seen {
		return nil, start, fmt.Errorf("%s expression must start with (", label)
	}
	return nil, start, fmt.Errorf("%s expression missing closing )", label)
}

func parseUntilCondition(fields []string, start int) (Condition, int, error) {
	if start >= len(fields) {
		return Condition{}, start, fmt.Errorf("/for until requires a condition")
	}
	token := fields[start]
	if token != "until" && !strings.HasPrefix(token, "until(") {
		return Condition{}, start, fmt.Errorf("/for until requires a condition")
	}
	var parts []string
	next := start + 1
	if token == "until" {
		if next >= len(fields) || isCommandToken(fields[next]) {
			return Condition{}, start, fmt.Errorf("/for until requires a condition")
		}
		if strings.HasPrefix(fields[next], "(") {
			var err error
			parts, next, err = collectExprConditionTokens(fields, next)
			if err != nil {
				return Condition{}, start, err
			}
			return Condition{Kind: ConditionExpr, Text: trimExprParens(strings.Join(parts, " "))}, next, nil
		}
		conditionStart := next
		for next < len(fields) && !isCommandToken(fields[next]) {
			next++
		}
		if conditionStart == next {
			return Condition{}, start, fmt.Errorf("/for until requires a condition")
		}
		conditionParts := unquoteCommandValues(fields[conditionStart:next])
		return Condition{Kind: ConditionNatural, Text: strings.Join(conditionParts, " ")}, next, nil
	}

	first := strings.TrimPrefix(token, "until")
	if first == "" || !strings.HasPrefix(first, "(") {
		return Condition{}, start, fmt.Errorf("/for until requires a condition")
	}
	var err error
	parts, next, err = collectExprConditionTokens(append([]string{first}, fields[next:]...), 0)
	if err != nil {
		return Condition{}, start, err
	}
	return Condition{Kind: ConditionExpr, Text: trimExprParens(strings.Join(parts, " "))}, start + next, nil
}

func collectExprConditionTokens(fields []string, start int) ([]string, int, error) {
	var parts []string
	depth := 0
	seen := false
	var quote rune
	escaped := false
	for i := start; i < len(fields); i++ {
		if isCommandToken(fields[i]) && !seen {
			return nil, start, fmt.Errorf("/for until requires a condition")
		}
		token := fields[i]
		parts = append(parts, token)
		for _, r := range token {
			if escaped {
				escaped = false
				continue
			}
			if quote != 0 {
				if r == '\\' {
					escaped = true
					continue
				}
				if r == quote {
					quote = 0
				}
				continue
			}
			switch r {
			case '\'', '"':
				quote = r
			case '(':
				depth++
				seen = true
			case ')':
				depth--
				if depth == 0 && seen {
					return parts, i + 1, nil
				}
				if depth < 0 {
					return nil, start, fmt.Errorf("/for until expression has unmatched )")
				}
			}
		}
	}
	if !seen {
		return nil, start, fmt.Errorf("/for until expression must start with (")
	}
	return nil, start, fmt.Errorf("/for until expression missing closing )")
}

func trimExprParens(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func parseForList(fields []string, start int) ([]string, int, error) {
	if start >= len(fields) || !strings.HasPrefix(fields[start], "[") {
		return nil, start, fmt.Errorf("/for in requires a bracketed list")
	}
	var values []string
	for i := start; i < len(fields); i++ {
		token := fields[i]
		first := i == start
		last := strings.HasSuffix(token, "]")
		token = strings.TrimPrefix(token, "[")
		token = strings.TrimSuffix(token, "]")
		if token != "" {
			values = append(values, unquoteCommandValue(token))
		}
		if last {
			if first && strings.HasPrefix(fields[i], "[]") {
				return nil, i + 1, fmt.Errorf("/for in list cannot be empty")
			}
			if len(values) == 0 {
				return nil, i + 1, fmt.Errorf("/for in list cannot be empty")
			}
			return values, i + 1, nil
		}
	}
	return nil, start, fmt.Errorf("/for in list missing closing ]")
}
