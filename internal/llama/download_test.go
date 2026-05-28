package llama

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectGGUFFilePrefersConfiguredQuant(t *testing.T) {
	raw := []byte(`{
		"siblings": [
			{"rfilename":"model-Q8_0.gguf"},
			{"rfilename":"model-Q4_K_M.gguf"},
			{"rfilename":"notes.txt"}
		]
	}`)
	files, err := ggufFilenamesFromJSON(raw)
	if err != nil {
		t.Fatalf("ggufFilenamesFromJSON: %v", err)
	}
	got, err := selectGGUFFile(files, "q4_k_m")
	if err != nil {
		t.Fatalf("selectGGUFFile: %v", err)
	}
	if got != "model-Q4_K_M.gguf" {
		t.Fatalf("selected %q; want model-Q4_K_M.gguf", got)
	}
}

func TestResolveModelDownloadsViaHTTPAndSkipsExisting(t *testing.T) {
	withTempHome(t)
	t.Setenv("SENTINEL_QUANT", "Q4_K_M")

	const body = "fake gguf content"
	var downloadHits int
	server := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/org/model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"siblings":[{"rfilename":"model-Q4_K_M.gguf"},{"rfilename":"model-Q8_0.gguf"}]}`))
		case "/org/model/resolve/main/model-Q4_K_M.gguf":
			downloadHits++
			_, _ = w.Write([]byte(body))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	t.Setenv("HF_ENDPOINT", server.URL)

	path, err := ResolveModel("org/model")
	if err != nil {
		t.Fatalf("ResolveModel first: %v", err)
	}
	if filepath.Dir(path) != ModelsDir() {
		t.Fatalf("model dir = %q; want %q", filepath.Dir(path), ModelsDir())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded model: %v", err)
	}
	if string(raw) != body {
		t.Fatalf("downloaded body = %q; want %q", string(raw), body)
	}

	path2, err := ResolveModel("org/model")
	if err != nil {
		t.Fatalf("ResolveModel second: %v", err)
	}
	if path2 != path {
		t.Fatalf("second path = %q; want %q", path2, path)
	}
	if downloadHits != 1 {
		t.Fatalf("download hits = %d; want 1", downloadHits)
	}
}

func TestResolveModelExistingPath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "local.gguf")
	if err := os.WriteFile(file, []byte("local"), 0o600); err != nil {
		t.Fatalf("write local model: %v", err)
	}
	got, err := ResolveModel(file)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != file {
		t.Fatalf("ResolveModel = %q; want %q", got, file)
	}
}
