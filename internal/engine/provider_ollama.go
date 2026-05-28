package engine

import "time"

// NewOllamaProvider targets a local Ollama daemon. Ollama is the recommended
// default backend: cross-platform, built-in model management, and an
// OpenAI-compatible API at /v1.
func NewOllamaProvider(baseURL, apiKey, model string, timeout time.Duration) Inferencer {
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	if model == "" {
		model = defaultModel
	}
	return &httpProvider{name: "ollama", baseURL: baseURL, apiKey: apiKey, model: model, timeout: timeout}
}
