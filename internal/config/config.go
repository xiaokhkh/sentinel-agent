// Package config resolves Sentinel's runtime settings. For the MVP it layers
// environment variables over built-in defaults; a file-based loader is a
// roadmap item and is intentionally kept out so the binary has no parser
// dependency.
package config

import (
	"os"
	"time"
)

// Config holds resolved settings. Flags on individual commands take final
// precedence over these values.
type Config struct {
	Provider string
	BaseURL  string
	Model    string
	APIKey   string
	Timeout  time.Duration
}

// Default returns the baseline configuration. Ollama is the recommended
// production backend; the CLI falls back to the `mock` provider on demand.
func Default() Config {
	return Config{
		Provider: "ollama",
		Model:    "lfm2.5",
		Timeout:  60 * time.Second,
	}
}

// Load applies SENTINEL_* environment variables over the defaults.
func Load() Config {
	c := Default()
	if v := os.Getenv("SENTINEL_PROVIDER"); v != "" {
		c.Provider = v
	}
	if v := os.Getenv("SENTINEL_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("SENTINEL_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("SENTINEL_API_KEY"); v != "" {
		c.APIKey = v
	}
	return c
}
