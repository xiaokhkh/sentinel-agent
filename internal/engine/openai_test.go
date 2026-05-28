package engine

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestChatRequestResponseFormat(t *testing.T) {
	raw, err := json.Marshal(newChatRequest("model", "system", "user", true))
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "json_schema") {
		t.Fatalf("request body missing json_schema: %s", body)
	}
	if !strings.Contains(body, "minLength") {
		t.Fatalf("request body missing minLength: %s", body)
	}
}

func TestChatRequestResponseFormatDisabled(t *testing.T) {
	raw, err := json.Marshal(newChatRequest("model", "system", "user", false))
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	if strings.Contains(string(raw), "response_format") {
		t.Fatalf("request body included response_format when disabled: %s", string(raw))
	}
}

func TestNewProviderDisablesSchemaFromEnv(t *testing.T) {
	t.Setenv("SENTINEL_NO_SCHEMA", "1")
	inf, err := NewProvider(ProviderConfig{Name: "llamacpp"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	p, ok := inf.(*httpProvider)
	if !ok {
		t.Fatalf("provider type = %T; want *httpProvider", inf)
	}
	if p.useSchema {
		t.Fatal("useSchema = true; want false")
	}
}

func TestLlamaCppDefaultBaseURLUsesIPv4Loopback(t *testing.T) {
	p := NewLlamaCppProvider("", "", "", 0)
	u, err := url.Parse(p.baseURL)
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}
	if u.Hostname() != "127.0.0.1" {
		t.Fatalf("host = %q; want 127.0.0.1", u.Hostname())
	}
}
