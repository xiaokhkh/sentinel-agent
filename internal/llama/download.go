package llama

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xiaokhkh/sentinel-agent/internal/memory"
)

const (
	defaultHFEndpoint = "https://huggingface.co"
	defaultQuant      = "Q4_K_M"
)

// ModelsDir returns Sentinel's local model cache, creating it if needed.
func ModelsDir() string {
	dir := filepath.Join(Home(), "models")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// ResolveModel returns a local GGUF file path for either an existing path or a
// Hugging Face repo id. Repo downloads go through Go's net/http, so standard
// proxy environment variables apply.
func ResolveModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("model is empty")
	}
	if info, err := os.Stat(model); err == nil && !info.IsDir() {
		return model, nil
	}
	if !looksLikeHFRepo(model) {
		return "", fmt.Errorf("model %q is not an existing file path or Hugging Face repo id", model)
	}
	return resolveHFModel(model, quantFromEnv(), hfEndpoint())
}

func looksLikeHFRepo(model string) bool {
	return strings.Contains(model, "/") && !strings.ContainsAny(model, " \t\r\n")
}

func quantFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("SENTINEL_QUANT")); v != "" {
		return v
	}
	return defaultQuant
}

func hfEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("HF_ENDPOINT")); v != "" {
		return strings.TrimRight(v, "/")
	}
	store, err := memory.Load()
	if err == nil && strings.TrimSpace(store.Runtime.HFEndpoint) != "" {
		return strings.TrimRight(store.Runtime.HFEndpoint, "/")
	}
	return defaultHFEndpoint
}

func resolveHFModel(repo, quant, base string) (string, error) {
	files, err := fetchGGUFFilenames(repo, base)
	if err != nil {
		return "", err
	}
	filename, err := selectGGUFFile(files, quant)
	if err != nil {
		return "", err
	}
	target := filepath.Join(ModelsDir(), filepath.Base(filename))
	if info, err := os.Stat(target); err == nil && info.Size() > 0 {
		return target, nil
	}
	if err := downloadHFFile(repo, filename, base, target); err != nil {
		return "", err
	}
	return target, nil
}

func fetchGGUFFilenames(repo, base string) ([]string, error) {
	endpoint := strings.TrimRight(base, "/") + "/api/models/" + escapeURLPath(repo)
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch model metadata from %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read model metadata from %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model metadata endpoint %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return ggufFilenamesFromJSON(raw)
}

func ggufFilenamesFromJSON(raw []byte) ([]string, error) {
	var payload struct {
		Siblings []struct {
			RFilename string `json:"rfilename"`
		} `json:"siblings"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode model metadata: %w", err)
	}
	var files []string
	for _, sibling := range payload.Siblings {
		name := strings.TrimSpace(sibling.RFilename)
		if strings.HasSuffix(strings.ToLower(name), ".gguf") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("model metadata has no .gguf files")
	}
	return files, nil
}

func selectGGUFFile(files []string, quant string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no .gguf files available")
	}
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	needle := strings.ToLower(strings.TrimSpace(quant))
	for _, file := range sorted {
		if needle != "" && strings.Contains(strings.ToLower(file), needle) {
			return file, nil
		}
	}
	return sorted[0], nil
}

func downloadHFFile(repo, filename, base, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	endpoint := strings.TrimRight(base, "/") + "/" + escapeURLPath(repo) + "/resolve/main/" + escapeURLPath(filename)
	resp, err := http.Get(endpoint)
	if err != nil {
		return fmt.Errorf("download model from %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download endpoint %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	part := target + ".part"
	file, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open partial model file: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(part)
	}()

	buf := make([]byte, 256*1024)
	var written int64
	lastReport := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return fmt.Errorf("write partial model file: %w", err)
			}
			written += int64(n)
			if time.Since(lastReport) >= time.Second {
				reportDownloadProgress(filepath.Base(filename), written, resp.ContentLength)
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read model download: %w", readErr)
		}
	}
	reportDownloadProgress(filepath.Base(filename), written, resp.ContentLength)
	fmt.Fprintln(os.Stderr)

	if err := file.Close(); err != nil {
		return fmt.Errorf("close partial model file: %w", err)
	}
	if err := os.Rename(part, target); err != nil {
		return fmt.Errorf("install model file: %w", err)
	}
	return nil
}

func reportDownloadProgress(name string, written, total int64) {
	if total > 0 {
		percent := float64(written) * 100 / float64(total)
		fmt.Fprintf(os.Stderr, "\rdownloading %s: %.1f/%.1f MB (%.0f%%)", name, bytesToMB(written), bytesToMB(total), percent)
		return
	}
	fmt.Fprintf(os.Stderr, "\rdownloading %s: %.1f MB", name, bytesToMB(written))
}

func bytesToMB(n int64) float64 {
	return float64(n) / 1024 / 1024
}

func escapeURLPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
