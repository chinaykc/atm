package engine

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chinaykc/atm/pkg/lang/compiler"
	"gopkg.in/yaml.v3"
)

func (x *taskExecution) sendWebhook(ctx context.Context, current execContext, call compiler.WebhookCall) error {
	decl, ok := x.engine.webhooks[call.Name]
	if !ok {
		return fmt.Errorf("webhook %q is not declared", call.Name)
	}
	message, err := x.renderTemplate(ctx, &current, call.Message, "/webhook")
	if err != nil {
		return err
	}
	body, err := x.webhookPayload(ctx, current, decl, call, message)
	if err != nil {
		return err
	}
	return deliverWebhook(ctx, decl, body)
}

func deliverWebhook(ctx context.Context, decl compiler.WebhookDecl, body []byte) error {
	url := strings.TrimSpace(decl.URL)
	if url == "" && decl.URLEnv != "" {
		url = strings.TrimSpace(os.Getenv(decl.URLEnv))
	}
	if url == "" {
		if decl.URLEnv != "" {
			return fmt.Errorf("webhook %s url env %s is not set", decl.Name, decl.URLEnv)
		}
		return fmt.Errorf("webhook %s url is empty", decl.Name)
	}
	if err := validateWebhookKeywords(decl, body); err != nil {
		return err
	}
	secret := decl.Secret
	if secret == "" && decl.SecretEnv != "" {
		secret = os.Getenv(decl.SecretEnv)
		if strings.TrimSpace(secret) == "" {
			return fmt.Errorf("webhook %s secret env %s is not set", decl.Name, decl.SecretEnv)
		}
	}
	var err error
	if secret != "" {
		body, url, err = signWebhookRequest(decl.Provider, url, secret, body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" && decl.Provider == "generic" {
		req.Header.Set("X-ATM-Webhook-Secret", secret)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned HTTP %d", decl.Name, resp.StatusCode)
	}
	return nil
}

func validateWebhookKeywords(decl compiler.WebhookDecl, body []byte) error {
	if decl.Provider != "dingtalk" || len(decl.Keywords) == 0 {
		return nil
	}
	text := string(body)
	for _, keyword := range decl.Keywords {
		if strings.Contains(text, keyword) {
			return nil
		}
	}
	return fmt.Errorf("webhook %s payload must contain at least one DingTalk keyword: %s", decl.Name, strings.Join(decl.Keywords, ", "))
}

func defaultWebhookPayload(decl compiler.WebhookDecl, message string) ([]byte, error) {
	var payload any
	switch decl.Provider {
	case "feishu":
		payload = map[string]any{"msg_type": "text", "content": map[string]any{"text": message}}
	case "dingtalk":
		payload = map[string]any{"msgtype": "text", "text": map[string]any{"content": message}}
	default:
		payload = map[string]any{"message": message}
	}
	return json.Marshal(payload)
}

func signWebhookRequest(provider, rawURL, secret string, body []byte) ([]byte, string, error) {
	switch provider {
	case "feishu":
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		payload := map[string]any{}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				return nil, "", err
			}
		}
		payload["timestamp"] = timestamp
		payload["sign"] = webhookHMACBase64(timestamp+"\n"+secret, "", secret)
		signed, err := json.Marshal(payload)
		return signed, rawURL, err
	case "dingtalk":
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return nil, "", err
		}
		values := parsed.Query()
		values.Set("timestamp", timestamp)
		values.Set("sign", webhookHMACBase64(timestamp+"\n"+secret, secret, secret))
		parsed.RawQuery = values.Encode()
		return body, parsed.String(), nil
	default:
		return body, rawURL, nil
	}
}

func webhookHMACBase64(message, key, fallbackKey string) string {
	if key == "" {
		key = message
		message = ""
	}
	if key == "" {
		key = fallbackKey
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (x *taskExecution) webhookPayload(ctx context.Context, current execContext, decl compiler.WebhookDecl, call compiler.WebhookCall, message string) ([]byte, error) {
	if strings.TrimSpace(call.Payload) != "" {
		rendered, err := x.renderTemplate(ctx, &current, call.Payload, "/webhook payload")
		if err != nil {
			return nil, err
		}
		var value any
		switch strings.ToLower(call.PayloadFormat) {
		case "yaml", "yml":
			if err := yaml.Unmarshal([]byte(rendered), &value); err != nil {
				return nil, fmt.Errorf("parse webhook YAML payload: %w", err)
			}
		default:
			if err := json.Unmarshal([]byte(rendered), &value); err != nil {
				return nil, fmt.Errorf("parse webhook JSON payload: %w", err)
			}
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	return defaultWebhookPayload(decl, message)
}
