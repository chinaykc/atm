package compiler

import "testing"

func TestParseFlagDeclsAndCoerceValues(t *testing.T) {
	content := "/flag string name user name\n/flag bool dry run default:true\n/flag []int shards shard list default:1,2\n\n/task\nHello {{name}}\n"
	flags, err := ParseFlagDecls("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 3 {
		t.Fatalf("expected 3 flags, got %#v", flags)
	}
	vars, err := CoerceFlagValues(flags, map[string][]string{"name": {"Ada"}, "shards": {"3", "4"}})
	if err != nil {
		t.Fatal(err)
	}
	if vars["name"] != "Ada" || vars["dry"] != true {
		t.Fatalf("unexpected vars: %#v", vars)
	}
	shards, ok := vars["shards"].([]int)
	if !ok || len(shards) != 2 || shards[0] != 3 || shards[1] != 4 {
		t.Fatalf("unexpected shards: %#v", vars["shards"])
	}
}

func TestCompileWebhookDeclarationAndCall(t *testing.T) {
	content := "/webhook new notify provider:generic url:env:HOOK_URL\n\n/webhook notify done {{name}}\n\n/task\nship it\n"
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Webhooks) != 1 || plan.Webhooks[0].Name != "notify" {
		t.Fatalf("unexpected webhooks: %#v", plan.Webhooks)
	}
	ops := FlattenTaskFlow(plan.Tasks[0])
	if len(ops) == 0 || ops[0].Kind != FlatOpWebhook || ops[0].Webhook.Name != "notify" {
		t.Fatalf("expected webhook op, got %#v", ops)
	}
}

func TestCompileWebhookUseMountsMCP(t *testing.T) {
	content := "/webhook new notify provider:generic url:env:HOOK_URL\n\n/task\n/webhook use notify\nDecide whether to notify.\n"
	plan, err := CompileProgram("todo.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 || len(plan.Tasks[0].Webhook.Use) != 1 || plan.Tasks[0].Webhook.Use[0] != "notify" {
		t.Fatalf("unexpected webhook config: %#v", plan.Tasks)
	}
}

func TestParseWebhookInlineCredentials(t *testing.T) {
	decls, err := ParseWebhookDecls("todo.md", "/webhook new alarm provider:dingtalk url:https://oapi.dingtalk.com/robot/send?access_token=x secret:SEC123 keyword:监控报警\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 1 || decls[0].URL == "" || decls[0].Secret != "SEC123" || decls[0].URLEnv != "" {
		t.Fatalf("unexpected inline webhook declaration: %#v", decls)
	}
}
