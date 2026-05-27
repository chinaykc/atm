package engine

import (
	"context"
	"encoding/json"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSignWebhookRequestFeishuAddsBodyFields(t *testing.T) {
	body, rawURL, err := signWebhookRequest("feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/abc", "secret", []byte(`{"msg_type":"text","content":{"text":"hi"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if rawURL != "https://open.feishu.cn/open-apis/bot/v2/hook/abc" {
		t.Fatalf("unexpected URL: %s", rawURL)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["timestamp"] == "" || payload["sign"] == "" {
		t.Fatalf("missing feishu signature fields: %#v", payload)
	}
	if payload["msg_type"] != "text" {
		t.Fatalf("payload was not preserved: %#v", payload)
	}
}

func TestSignWebhookRequestDingTalkAddsQueryFields(t *testing.T) {
	body, rawURL, err := signWebhookRequest("dingtalk", "https://oapi.dingtalk.com/robot/send?access_token=abc", "secret", []byte(`{"msgtype":"text"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"msgtype":"text"}` {
		t.Fatalf("unexpected body: %s", body)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("access_token") != "abc" || parsed.Query().Get("timestamp") == "" || parsed.Query().Get("sign") == "" {
		t.Fatalf("missing dingtalk query fields: %s", rawURL)
	}
}

func TestValidateDingTalkKeywords(t *testing.T) {
	decl := compiler.WebhookDecl{Name: "alert", Provider: "dingtalk", Keywords: []string{"监控报警"}}
	if err := validateWebhookKeywords(decl, []byte(`{"msgtype":"text","text":{"content":"监控报警: cpu high"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := validateWebhookKeywords(decl, []byte(`{"msgtype":"text","text":{"content":"cpu high"}}`)); err == nil {
		t.Fatal("expected missing keyword error")
	}
}

func TestDeliverWebhookAcceptsInlineURLAndSecret(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	decl := compiler.WebhookDecl{
		Name:     "alarm",
		Provider: "dingtalk",
		URL:      server.URL + "?access_token=x",
		Secret:   "SEC123",
		Keywords: []string{"监控报警"},
	}
	if err := deliverWebhook(context.Background(), decl, []byte(`{"msgtype":"text","text":{"content":"监控报警"}}`)); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("timestamp") == "" || gotQuery.Get("sign") == "" {
		t.Fatalf("inline credentials did not sign request: %#v", gotQuery)
	}
}

func TestDecodeWebhookToolArgsRejectsUnknownFieldsAndNonObjectPayload(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{
			name: "unknown field",
			args: `{"msg":"hi"}`,
			want: `unknown field "msg"`,
		},
		{
			name: "string payload",
			args: `{"payload":"hi"}`,
			want: "payload must be a JSON object",
		},
		{
			name: "array payload",
			args: `{"payload":["hi"]}`,
			want: "payload must be a JSON object",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeWebhookToolArgs(json.RawMessage(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
	args, err := decodeWebhookToolArgs(json.RawMessage(`{"message":"hi","payload":{"message":"override"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if args.Message != "hi" || string(args.Payload) != `{"message":"override"}` {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestWebhookMCPConfigRejectsUnknownFields(t *testing.T) {
	_, err := parseWebhookMCPConfig([]byte(`{"webhooks":[],"webhookz":[]}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "webhookz"`) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}
