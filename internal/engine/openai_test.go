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

func TestParseStep(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Step
	}{
		{
			name: "command",
			raw:  `prefix {"command":" kubectl get pods -n default ","done":false} suffix`,
			want: Step{Command: "kubectl get pods -n default"},
		},
		{
			name: "done",
			raw:  `{"done":true,"conclusion":" pods are healthy "}`,
			want: Step{Done: true, Conclusion: "pods are healthy"},
		},
		{
			name: "needs input",
			raw:  `{"needs_input":{"prompt":" kubeconfig path? ","key":" kubernetes.kubeconfig "}}`,
			want: Step{NeedsInput: &Clarification{Prompt: "kubeconfig path?", Key: "kubernetes.kubeconfig"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStep(tt.raw)
			if err != nil {
				t.Fatalf("parseStep: %v", err)
			}
			if got.Command != tt.want.Command || got.Done != tt.want.Done || got.Conclusion != tt.want.Conclusion {
				t.Fatalf("step = %+v; want %+v", got, tt.want)
			}
			switch {
			case tt.want.NeedsInput == nil && got.NeedsInput != nil:
				t.Fatalf("needs_input = %+v; want nil", got.NeedsInput)
			case tt.want.NeedsInput != nil && got.NeedsInput == nil:
				t.Fatalf("needs_input = nil; want %+v", tt.want.NeedsInput)
			case tt.want.NeedsInput != nil:
				if got.NeedsInput.Prompt != tt.want.NeedsInput.Prompt || got.NeedsInput.Key != tt.want.NeedsInput.Key {
					t.Fatalf("needs_input = %+v; want %+v", got.NeedsInput, tt.want.NeedsInput)
				}
			}
		})
	}
}

func TestParseStepRejectsUnusableJSON(t *testing.T) {
	tests := []string{
		`no json`,
		`{"done":false}`,
		`{"command":"   "}`,
		`{"done":true,"conclusion":"   "}`,
		`{"needs_input":{"prompt":"   ","key":"kubernetes.kubeconfig"}}`,
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseStep(raw); err == nil {
				t.Fatal("parseStep returned nil error")
			}
		})
	}
}
