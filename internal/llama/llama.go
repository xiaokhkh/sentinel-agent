// Package llama manages the local llama.cpp server used by the llamacpp
// inference provider.
package llama

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultBaseURL = "http://127.0.0.1:8080/v1"
	binaryName     = "llama-server"
	brewHint       = "llama-server not found. Install it with: brew install llama.cpp"
)

var userHomeDir = os.UserHomeDir

// Home returns Sentinel's home directory, creating it if it does not exist.
func Home() string {
	home, err := userHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	dir := filepath.Join(home, ".sentinel")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func pidPath() string {
	return filepath.Join(Home(), "llama-server.pid")
}

func logPath() string {
	return filepath.Join(Home(), "llama-server.log")
}

func hostPort(baseURL string) (host, port string) {
	if strings.TrimSpace(baseURL) == "" {
		return "127.0.0.1", "8080"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "127.0.0.1", "8080"
	}
	host = u.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port = u.Port()
	if port == "" {
		port = "8080"
	}
	return host, port
}

// Reachable reports whether the OpenAI-compatible models endpoint is ready.
func Reachable(baseURL string, timeout time.Duration) bool {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(endpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// FindBinary returns the llama-server path or a Homebrew install hint.
func FindBinary() (string, error) {
	path, err := exec.LookPath(binaryName)
	if err != nil {
		return "", errors.New(brewHint)
	}
	return path, nil
}

// RunForeground runs llama-server attached to the current terminal.
func RunForeground(model, baseURL string) error {
	bin, err := FindBinary()
	if err != nil {
		return err
	}
	host, port := hostPort(baseURL)
	cmd := exec.Command(bin, "-hf", model, "--host", host, "--port", port, "-ngl", "999")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// EnsureServer starts a detached llama-server if the configured endpoint is not
// already reachable.
func EnsureServer(model, baseURL string, timeout time.Duration) error {
	if Reachable(baseURL, 2*time.Second) {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	if pid, err := readPID(); err == nil && processLive(pid) {
		return waitUntilReady(baseURL, timeout)
	}

	bin, err := FindBinary()
	if err != nil {
		return err
	}

	host, port := hostPort(baseURL)
	logFile, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open llama-server log: %w", err)
	}
	defer logFile.Close()

	fmt.Fprintln(os.Stderr, "starting local llama-server (first run downloads the model, this may take a while)...")
	cmd := exec.Command(bin, "-hf", model, "--host", host, "--port", port, "-ngl", "999")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}
	if err := writePID(cmd.Process.Pid); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_ = cmd.Process.Kill()
		return fmt.Errorf("write llama-server pidfile: %w", err)
	}
	return waitUntilReady(baseURL, timeout)
}

// Stop stops the background llama-server process recorded in the pidfile.
func Stop() error {
	pid, err := readPID()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if processLive(pid) {
		if err := signalStop(pid); err != nil {
			return err
		}
	}
	if err := os.Remove(pidPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func waitUntilReady(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if Reachable(baseURL, 2*time.Second) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("llama-server did not become ready after %s; see %s", timeout.Round(time.Second), logPath())
		}
		time.Sleep(time.Second)
	}
}

func writePID(pid int) error {
	return os.WriteFile(pidPath(), []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

func readPID() (int, error) {
	raw, err := os.ReadFile(pidPath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid llama-server pidfile %s", pidPath())
	}
	return pid, nil
}

func processLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func signalStop(pid int) error {
	groupErr := syscall.Kill(-pid, syscall.SIGTERM)
	procErr := syscall.Kill(pid, syscall.SIGTERM)
	if procErr != nil && !errors.Is(procErr, syscall.ESRCH) {
		return procErr
	}
	if groupErr != nil && !errors.Is(groupErr, syscall.ESRCH) {
		return groupErr
	}
	return nil
}
