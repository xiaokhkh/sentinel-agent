package engine

import (
	"fmt"
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
	switch strings.ToLower(strings.TrimSpace(cfg.Name)) {
	case "", "mock":
		return NewMockProvider(), nil
	case "ollama":
		return NewOllamaProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout), nil
	case "llamacpp", "llama.cpp", "llama":
		return NewLlamaCppProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout), nil
	case "mlx":
		return NewMLXProvider(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: mock, ollama, llamacpp, mlx)", cfg.Name)
	}
}
