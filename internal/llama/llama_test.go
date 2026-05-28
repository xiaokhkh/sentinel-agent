package llama

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReachable(t *testing.T) {
	server := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q; want /models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if !Reachable(server.URL, time.Second) {
		t.Fatal("Reachable returned false for healthy /models endpoint")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listen unavailable: %v", err)
	}
	closedURL := "http://" + ln.Addr().String()
	_ = ln.Close()
	if Reachable(closedURL, 50*time.Millisecond) {
		t.Fatal("Reachable returned true for a closed endpoint")
	}
}

func TestHostPort(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		wantHost string
		wantPort string
	}{
		{name: "empty", wantHost: "127.0.0.1", wantPort: "8080"},
		{name: "localhost v1", baseURL: "http://localhost:8080/v1", wantHost: "localhost", wantPort: "8080"},
		{name: "loopback", baseURL: "http://127.0.0.1:9000", wantHost: "127.0.0.1", wantPort: "9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := hostPort(tt.baseURL)
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("hostPort(%q) = (%q, %q); want (%q, %q)", tt.baseURL, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestStopKillsProcessAndRemovesPIDFile(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found")
	}
	withTempHome(t)

	cmd := exec.Command(sleep, "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	if err := writePID(cmd.Process.Pid); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if err := Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(pidPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pidfile still exists or stat failed unexpectedly: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("sleep exited cleanly; want it to be terminated")
	}
}

func TestFindBinary(t *testing.T) {
	path, err := exec.LookPath(binaryName)
	if err == nil {
		got, findErr := FindBinary()
		if findErr != nil {
			t.Fatalf("FindBinary returned error with %s present at %s: %v", binaryName, path, findErr)
		}
		if got == "" {
			t.Fatal("FindBinary returned empty path")
		}
		return
	}

	_, findErr := FindBinary()
	if findErr == nil {
		t.Fatal("FindBinary returned nil error with llama-server absent")
	}
	if !strings.Contains(findErr.Error(), "brew install") {
		t.Fatalf("FindBinary error = %q; want brew install hint", findErr.Error())
	}
}

func withTempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := userHomeDir
	userHomeDir = func() (string, error) {
		return dir, nil
	}
	t.Cleanup(func() {
		userHomeDir = old
	})
}

func newLocalTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listen unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = ln
	server.Start()
	t.Cleanup(server.Close)
	return server
}
