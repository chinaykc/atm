package compiler

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	gotemplate "text/template"

	"github.com/chinaykc/atm/pkg/lang/ir"
)

var legacyTemplateVar = regexp.MustCompile(`{{[ \t]*([A-Za-z_][A-Za-z0-9_-]*)[ \t]*}}`)

func TemplateVarNames(input string) []string {
	matches := legacyTemplateVar.FindAllStringSubmatch(input, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			out = append(out, match[1])
		}
	}
	return out
}

var legacyTemplateField = regexp.MustCompile(`{{[ \t]*([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)[ \t]*}}`)

func parsePositiveIntToken(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func isVariableName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func CloneVars(in map[string]any) map[string]any {
	return ir.CloneVars(in)
}

func CloneStringMap(in map[string]string) map[string]any {
	return ir.CloneStringMap(in)
}

func MergeRunOptions(base, override RunOptions) RunOptions {
	return ir.MergeRunOptions(base, override)
}

func prependPromptPrefixes(prefixes []string, prompt string) string {
	if len(prefixes) == 0 {
		return prompt
	}
	var b strings.Builder
	for _, prefix := range prefixes {
		b.WriteString(prefix)
		if !strings.HasSuffix(prefix, "\n") && !strings.HasSuffix(prefix, "\r") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(prompt)
	return b.String()
}

func RenderTemplate(input string, vars any) (string, error) {
	normalized := normalizeVars(vars)
	rewritten := rewriteLegacyTemplateVars(input)
	tpl, err := gotemplate.New("atm").
		Option("missingkey=zero").
		Funcs(templateFuncs(normalized)).
		Parse(rewritten)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, templateData(normalized)); err != nil {
		return "", err
	}
	return out.String(), nil
}

func normalizeVars(vars any) map[string]any {
	switch v := vars.(type) {
	case nil:
		return nil
	case map[string]any:
		return v
	case map[string]string:
		return CloneStringMap(v)
	default:
		return nil
	}
}

func rewriteLegacyTemplateVars(input string) string {
	input = legacyTemplateField.ReplaceAllStringFunc(input, func(match string) string {
		parts := legacyTemplateField.FindStringSubmatch(match)
		if len(parts) != 3 || isGoTemplateKeyword(parts[1]) {
			return match
		}
		return `{{index .` + parts[1] + ` "` + parts[2] + `"}}`
	})
	return legacyTemplateVar.ReplaceAllStringFunc(input, func(match string) string {
		parts := legacyTemplateVar.FindStringSubmatch(match)
		if len(parts) != 2 || isGoTemplateKeyword(parts[1]) {
			return match
		}
		return `{{var "` + parts[1] + `"}}`
	})
}

func templateFuncs(vars map[string]any) gotemplate.FuncMap {
	return gotemplate.FuncMap{
		"var": func(name string) string {
			if value, ok := vars[name]; ok {
				return StringValue(value)
			}
			return "{{" + name + "}}"
		},
		"has": func(name string) bool {
			_, ok := vars[name]
			return ok
		},
	}
}

func templateData(vars map[string]any) map[string]any {
	data := make(map[string]any, len(vars)+1)
	data["Vars"] = vars
	for name, value := range vars {
		if isTemplateIdentifier(name) {
			data[name] = value
		}
	}
	return data
}

func StringValue(value any) string {
	return ir.StringValue(value)
}

func isTemplateIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isGoTemplateKeyword(word string) bool {
	switch word {
	case "if", "else", "end", "range", "with", "define", "template", "block", "break", "continue", "nil", "true", "false":
		return true
	default:
		return false
	}
}
