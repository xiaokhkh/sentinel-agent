package engine

import "time"

// NewLlamaCppProvider targets llama.cpp's llama-server. It is the pick when you
// want a single static binary with no background daemon and full control over
// the GGUF model file — a smaller, more auditable footprint for an offline
// security tool.
func NewLlamaCppProvider(baseURL, apiKey, model string, timeout time.Duration) *httpProvider {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080/v1"
	}
	if model == "" {
		model = defaultModel
	}
	return &httpProvider{name: "llamacpp", baseURL: baseURL, apiKey: apiKey, model: model, timeout: timeout, useSchema: true}
}
