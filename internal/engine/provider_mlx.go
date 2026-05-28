package engine

import "time"

// NewMLXProvider targets mlx_lm.server on Apple Silicon. It offers the best
// throughput on M-series Macs but is macOS-only, so it suits local development
// and performance benchmarking rather than cross-platform deployment.
func NewMLXProvider(baseURL, apiKey, model string, timeout time.Duration) Inferencer {
	if baseURL == "" {
		baseURL = "http://localhost:8080/v1"
	}
	if model == "" {
		model = defaultModel
	}
	return &httpProvider{name: "mlx", baseURL: baseURL, apiKey: apiKey, model: model, timeout: timeout}
}
