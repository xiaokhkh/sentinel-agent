package memory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	withTempHome(t)

	s := &Store{}
	s.Kubernetes.Kubeconfig = "/tmp/kubeconfig"
	s.Kubernetes.Context = "prod"
	s.Kubernetes.Namespace = "payments"
	s.Runtime.HFEndpoint = "https://hf.example"
	s.AddFact("payment service is in payments namespace")

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %v; want 0600", got)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Kubernetes.Kubeconfig != s.Kubernetes.Kubeconfig {
		t.Fatalf("kubeconfig = %q; want %q", loaded.Kubernetes.Kubeconfig, s.Kubernetes.Kubeconfig)
	}
	if loaded.Kubernetes.Context != s.Kubernetes.Context {
		t.Fatalf("context = %q; want %q", loaded.Kubernetes.Context, s.Kubernetes.Context)
	}
	if loaded.Kubernetes.Namespace != s.Kubernetes.Namespace {
		t.Fatalf("namespace = %q; want %q", loaded.Kubernetes.Namespace, s.Kubernetes.Namespace)
	}
	if loaded.Runtime.HFEndpoint != s.Runtime.HFEndpoint {
		t.Fatalf("hf_endpoint = %q; want %q", loaded.Runtime.HFEndpoint, s.Runtime.HFEndpoint)
	}
	if len(loaded.Facts) != 1 || loaded.Facts[0] != s.Facts[0] {
		t.Fatalf("facts = %#v; want %#v", loaded.Facts, s.Facts)
	}
}

func TestLoadMissingFileReturnsEmptyStore(t *testing.T) {
	withTempHome(t)

	s, err := Load()
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if s == nil {
		t.Fatal("Load returned nil store")
	}
	if _, err := os.Stat(Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("memory file should not be created by Load, stat err=%v", err)
	}
}

func TestSetGetKnownKeysAndUnknownError(t *testing.T) {
	s := &Store{}
	cases := map[string]string{
		"kubernetes.kubeconfig": "/tmp/kubeconfig",
		"kubernetes.context":    "prod",
		"kubernetes.namespace":  "payments",
		"runtime.hf_endpoint":   "https://hf.example",
	}

	for key, want := range cases {
		if err := s.Set(key, want); err != nil {
			t.Fatalf("Set(%q): %v", key, err)
		}
		got, ok := s.Get(key)
		if !ok {
			t.Fatalf("Get(%q) ok=false", key)
		}
		if got != want {
			t.Fatalf("Get(%q) = %q; want %q", key, got, want)
		}
	}

	if err := s.Set("kubernetes.token", "secret"); err == nil {
		t.Fatal("Set unknown key returned nil error")
	}
	if _, ok := s.Get("kubernetes.token"); ok {
		t.Fatal("Get unknown key ok=true")
	}
}

func TestAddFactDeduplicates(t *testing.T) {
	s := &Store{}
	s.AddFact("pods live in payments namespace")
	s.AddFact("pods live in payments namespace")
	s.AddFact("  pods live in payments namespace  ")
	s.AddFact("")

	if len(s.Facts) != 1 {
		t.Fatalf("facts = %#v; want one deduplicated fact", s.Facts)
	}
}

func withTempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := homeDir
	homeDir = func() (string, error) {
		return dir, nil
	}
	t.Cleanup(func() {
		homeDir = old
	})
	if got := Path(); got != filepath.Join(dir, ".sentinel", "config.json") {
		t.Fatalf("Path() = %q; want under temp home", got)
	}
}
