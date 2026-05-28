// Package memory stores Sentinel's structured, non-secret local memory.
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var homeDir = os.UserHomeDir

// Store is the version-1 structured memory shape persisted as JSON.
type Store struct {
	Kubernetes struct {
		Kubeconfig string `json:"kubeconfig,omitempty"`
		Context    string `json:"context,omitempty"`
		Namespace  string `json:"namespace,omitempty"`
	} `json:"kubernetes"`
	Runtime struct {
		HFEndpoint string `json:"hf_endpoint,omitempty"`
	} `json:"runtime"`
	Facts []string `json:"memory,omitempty"`
}

// Path returns the structured-memory config file path.
func Path() string {
	home, err := homeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".sentinel", "config.json")
}

// Load reads the structured-memory store. A missing file is an empty store.
func Load() (*Store, error) {
	raw, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{}, nil
		}
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the structured-memory store as pretty JSON with 0600 perms.
func (s *Store) Save() error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Get returns a known dotted key from the structured-memory store.
func (s *Store) Get(dottedKey string) (string, bool) {
	switch dottedKey {
	case "kubernetes.kubeconfig":
		return s.Kubernetes.Kubeconfig, true
	case "kubernetes.context":
		return s.Kubernetes.Context, true
	case "kubernetes.namespace":
		return s.Kubernetes.Namespace, true
	case "runtime.hf_endpoint":
		return s.Runtime.HFEndpoint, true
	default:
		return "", false
	}
}

// Set writes a known dotted key into the structured-memory store.
func (s *Store) Set(dottedKey, value string) error {
	switch dottedKey {
	case "kubernetes.kubeconfig":
		s.Kubernetes.Kubeconfig = value
	case "kubernetes.context":
		s.Kubernetes.Context = value
	case "kubernetes.namespace":
		s.Kubernetes.Namespace = value
	case "runtime.hf_endpoint":
		s.Runtime.HFEndpoint = value
	default:
		return fmt.Errorf("unknown memory key %q", dottedKey)
	}
	return nil
}

// AddFact appends a non-empty fact unless it is already remembered.
func (s *Store) AddFact(fact string) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return
	}
	for _, existing := range s.Facts {
		if existing == fact {
			return
		}
	}
	s.Facts = append(s.Facts, fact)
}
