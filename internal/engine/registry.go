package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// defaultModel is the model tag assumed when none is configured. Point it at
// whatever LFM 2.5 build you have pulled (e.g. an Ollama tag or a GGUF name).
const defaultModel = "lfm2.5"

const defaultTimeout = 60 * time.Second

// ProviderConfig selects and configures an inference backend.
type ProviderConfig struct {
	Name    string
	BaseURL string
	Model   string
	APIKey  string
	Timeout time.Duration
}

// NewProvider constructs the backend named in cfg. The protocol layer is shared
// across the HTTP backends, so adding a new one is just another case here.
func NewProvider(cfg ProviderConfig) (Inferencer, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	useSchema := os.Getenv("SENTINEL_NO_SCHEMA") != "1"
	switch strings.ToLower(strings.TrimSpace(cfg.Name)) {
	case "", "mock":
		return NewMockProvider(), nil
	case "ollama":
		p := NewOllamaProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
		p.useSchema = useSchema
		return p, nil
	case "llamacpp", "llama.cpp", "llama":
		p := NewLlamaCppProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
		p.useSchema = useSchema
		return p, nil
	case "mlx":
		p := NewMLXProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
		p.useSchema = useSchema
		return p, nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: mock, ollama, llamacpp, mlx)", cfg.Name)
	}
}

// Chat sends a short OpenAI-compatible chat request through one of Sentinel's
// configured HTTP backends. Callers are responsible for keeping prompts local
// and small; this helper exists for local-only classifiers that need JSON
// output but not a full execution Plan.
func Chat(ctx context.Context, cfg ProviderConfig, system, user string) (string, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Name)) {
	case "ollama":
		p := NewOllamaProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
		return openAIChat(ctx, p.baseURL, p.apiKey, p.model, system, user, p.timeout, false)
	case "llamacpp", "llama.cpp", "llama":
		p := NewLlamaCppProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
		return openAIChat(ctx, p.baseURL, p.apiKey, p.model, system, user, p.timeout, false)
	case "mlx":
		p := NewMLXProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
		return openAIChat(ctx, p.baseURL, p.apiKey, p.model, system, user, p.timeout, false)
	case "", "mock":
		return "", fmt.Errorf("provider %q does not support raw chat", cfg.Name)
	default:
		return "", fmt.Errorf("unknown provider %q (supported: ollama, llamacpp, mlx)", cfg.Name)
	}
}
