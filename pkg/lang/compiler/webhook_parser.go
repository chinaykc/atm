package compiler

import (
	"fmt"
	"slices"
	"strings"
)

func ParseGlobalWebhookBlock(body string) ([]WebhookDecl, bool, error) {
	lines := SplitLines(body)
	var decls []WebhookDecl
	seen := map[string]struct{}{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if trimmed != "/webhook new" && !strings.HasPrefix(trimmed, "/webhook new ") {
			return nil, false, nil
		}
		decl, err := parseWebhookDeclLine(trimmed)
		if err != nil {
			return nil, true, err
		}
		if _, ok := seen[decl.Name]; ok {
			return nil, true, fmt.Errorf("duplicate webhook %q", decl.Name)
		}
		seen[decl.Name] = struct{}{}
		decls = append(decls, decl)
	}
	if len(decls) == 0 {
		return nil, false, nil
	}
	return decls, true, nil
}

func parseWebhookDeclLine(line string) (WebhookDecl, error) {
	fields, err := commandFields(line)
	if err != nil {
		return WebhookDecl{}, err
	}
	if len(fields) < 4 || fields[0] != "/webhook" || fields[1] != "new" {
		return WebhookDecl{}, fmt.Errorf("/webhook new requires name and url")
	}
	decl := WebhookDecl{Name: fields[2], Provider: "generic"}
	if !isVariableName(decl.Name) {
		return WebhookDecl{}, fmt.Errorf("invalid webhook name %q", decl.Name)
	}
	for _, field := range fields[3:] {
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			return WebhookDecl{}, fmt.Errorf("invalid webhook option %q", field)
		}
		switch key {
		case "provider":
			switch value {
			case "generic", "feishu", "dingtalk":
				decl.Provider = value
			default:
				return WebhookDecl{}, fmt.Errorf("unsupported webhook provider %q", value)
			}
		case "url":
			if env, ok := strings.CutPrefix(value, "env:"); ok {
				if strings.TrimSpace(env) == "" {
					return WebhookDecl{}, fmt.Errorf("webhook url env name cannot be empty")
				}
				decl.URLEnv = env
			} else if strings.TrimSpace(value) == "" {
				return WebhookDecl{}, fmt.Errorf("webhook url cannot be empty")
			} else {
				decl.URL = value
			}
		case "secret":
			if env, ok := strings.CutPrefix(value, "env:"); ok {
				if strings.TrimSpace(env) == "" {
					return WebhookDecl{}, fmt.Errorf("webhook secret env name cannot be empty")
				}
				decl.SecretEnv = env
			} else if strings.TrimSpace(value) == "" {
				return WebhookDecl{}, fmt.Errorf("webhook secret cannot be empty")
			} else {
				decl.Secret = value
			}
		case "keyword":
			if strings.TrimSpace(value) == "" {
				return WebhookDecl{}, fmt.Errorf("webhook keyword cannot be empty")
			}
			decl.Keywords = append(decl.Keywords, value)
		case "keywords":
			for _, keyword := range strings.Split(value, ",") {
				keyword = strings.TrimSpace(keyword)
				if keyword != "" {
					decl.Keywords = append(decl.Keywords, keyword)
				}
			}
		default:
			return WebhookDecl{}, fmt.Errorf("unknown webhook option %q", key)
		}
	}
	if decl.URLEnv == "" && decl.URL == "" {
		return WebhookDecl{}, fmt.Errorf("/webhook new %s requires url:<URL> or url:env:<VAR>", decl.Name)
	}
	decl.Keywords = slices.Compact(decl.Keywords)
	if len(decl.Keywords) > 10 {
		return WebhookDecl{}, fmt.Errorf("webhook %s supports at most 10 keywords", decl.Name)
	}
	return decl, nil
}

func parseWebhookCallLine(line string) (WebhookCall, bool, error) {
	if !strings.HasPrefix(strings.TrimSpace(line), "/webhook ") {
		return WebhookCall{}, false, nil
	}
	fields, err := commandFields(line)
	if err != nil {
		return WebhookCall{}, true, err
	}
	call, next, err := parseWebhookCallFields(fields, 0)
	if err != nil {
		return WebhookCall{}, true, err
	}
	if next != len(fields) {
		return WebhookCall{}, true, fmt.Errorf("unexpected command argument %q", fields[next])
	}
	return call, true, nil
}

func parseWebhookCallFields(fields []string, start int) (WebhookCall, int, error) {
	if start >= len(fields) || fields[start] != "/webhook" {
		return WebhookCall{}, start, fmt.Errorf("expected /webhook")
	}
	if start+1 >= len(fields) || isCommandToken(fields[start+1]) || fields[start+1] == "new" || fields[start+1] == "use" {
		return WebhookCall{}, start, fmt.Errorf("/webhook requires a webhook name")
	}
	name := fields[start+1]
	if !isVariableName(name) {
		return WebhookCall{}, start, fmt.Errorf("invalid webhook name %q", name)
	}
	args, next := collectCommandArgs(fields, start+2)
	return WebhookCall{Name: name, Message: strings.Join(args, " ")}, next, nil
}

func parseWebhookTaskLine(line string) (WebhookTaskConfig, bool, error) {
	fields, err := commandFields(line)
	if err != nil {
		return WebhookTaskConfig{}, false, err
	}
	if len(fields) == 0 || fields[0] != "/webhook" {
		return WebhookTaskConfig{}, false, nil
	}
	if len(fields) < 2 || fields[1] != "use" {
		return WebhookTaskConfig{}, false, nil
	}
	out, next, err := parseWebhookTaskFields(fields, 0)
	if err != nil {
		return WebhookTaskConfig{}, true, err
	}
	if next != len(fields) {
		return WebhookTaskConfig{}, true, fmt.Errorf("unexpected command argument %q", fields[next])
	}
	return out, true, nil
}

func parseWebhookTaskFields(fields []string, start int) (WebhookTaskConfig, int, error) {
	if start >= len(fields) || fields[start] != "/webhook" {
		return WebhookTaskConfig{}, start, fmt.Errorf("expected /webhook")
	}
	if start+1 >= len(fields) || fields[start+1] != "use" {
		return WebhookTaskConfig{}, start, fmt.Errorf("/webhook use requires at least one name")
	}
	args, next := collectCommandArgs(fields, start+2)
	if len(args) == 0 {
		return WebhookTaskConfig{}, start, fmt.Errorf("/webhook use requires at least one name")
	}
	var use []string
	for _, name := range args {
		if !isVariableName(name) {
			return WebhookTaskConfig{}, start, fmt.Errorf("invalid webhook name %q", name)
		}
		use = append(use, name)
	}
	return WebhookTaskConfig{Use: use}, next, nil
}

func parseWebhookCallAt(lines []string, index int, line string) (WebhookCall, int, bool, error) {
	call, ok, err := parseWebhookCallLine(line)
	if !ok || err != nil {
		return call, index + 1, ok, err
	}
	if index+1 >= len(lines) {
		return call, index + 1, true, nil
	}
	fence, ok := parseAnyFenceStart(lines[index+1])
	if !ok {
		return call, index + 1, true, nil
	}
	payload, next, err := collectTextFenceBlock(lines, index+2, fence)
	if err != nil {
		return call, next, true, err
	}
	lang := strings.ToLower(fence.lang)
	if lang == "" {
		lang = "json"
	}
	call.Payload = payload
	call.PayloadFormat = lang
	return call, next, true, nil
}

func ParseWebhookDecls(sourcePath, content string) ([]WebhookDecl, error) {
	seen := map[string]int{}
	var out []WebhookDecl
	for i, line := range SplitLines(content) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "/webhook new ") {
			continue
		}
		decl, err := parseWebhookDeclLine(trimmed)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if first, exists := seen[decl.Name]; exists {
			return nil, fmt.Errorf("duplicate webhook %q also appears on line %d", decl.Name, first+1)
		}
		decl.BlockIndex = -1
		decl.SourcePath = sourcePath
		decl.Line = i + 1
		seen[decl.Name] = i
		out = append(out, decl)
	}
	return out, nil
}
