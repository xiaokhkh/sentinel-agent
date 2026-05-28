package engine

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaokhkh/sentinel-agent/internal/memory"
)

// LocalContext is the local-RAG background the engine reads to ground its
// reasoning. It only records the presence of config files and non-secret
// identifiers (e.g. the current kube context name) — never file contents,
// keys, or tokens.
type LocalContext struct {
	Hostname       string
	KubeConfigPath string
	KubeContext    string
	Namespace      string
	Facts          []string
	HasKubeConfig  bool
	SSHConfigPath  string
	HasSSHConfig   bool
}

// LoadLocalContext inspects the user's home directory for well-known ops config
// files. All reads stay on this machine.
func LoadLocalContext() *LocalContext {
	lc := &LocalContext{}
	lc.Hostname, _ = os.Hostname()
	lc.Namespace = "default"

	store, _ := memory.Load()
	if store != nil {
		if store.Kubernetes.Kubeconfig != "" {
			lc.HasKubeConfig = true
			lc.KubeConfigPath = store.Kubernetes.Kubeconfig
		}
		if store.Kubernetes.Context != "" {
			lc.KubeContext = store.Kubernetes.Context
		}
		if store.Kubernetes.Namespace != "" {
			lc.Namespace = store.Kubernetes.Namespace
		}
		lc.Facts = append(lc.Facts, store.Facts...)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return lc
	}

	if !lc.HasKubeConfig {
		kube := filepath.Join(home, ".kube", "config")
		if fi, err := os.Stat(kube); err == nil && !fi.IsDir() {
			lc.HasKubeConfig = true
			lc.KubeConfigPath = kube
		}
	}
	if lc.HasKubeConfig && lc.KubeContext == "" {
		lc.KubeContext = currentKubeContext(lc.KubeConfigPath)
	}

	ssh := filepath.Join(home, ".ssh", "config")
	if fi, err := os.Stat(ssh); err == nil && !fi.IsDir() {
		lc.HasSSHConfig = true
		lc.SSHConfigPath = ssh
	}
	return lc
}

// Summary returns a one-line, secret-free description safe to inject into a
// model prompt.
func (lc *LocalContext) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "host=%s; ", lc.Hostname)
	if lc.HasKubeConfig {
		fmt.Fprintf(&b, "kubeconfig present (current-context=%s, namespace=%s); ", lc.KubeContext, lc.Namespace)
	} else {
		fmt.Fprintf(&b, "no kubeconfig (namespace=%s); ", lc.Namespace)
	}
	if lc.HasSSHConfig {
		b.WriteString("ssh config present; ")
	}
	if len(lc.Facts) > 0 {
		fmt.Fprintf(&b, "memory=%s; ", strings.Join(lc.Facts, " | "))
	}
	return strings.TrimSpace(b.String())
}

// currentKubeContext extracts only the current-context name. It deliberately
// avoids a YAML dependency and never parses credential blocks.
func currentKubeContext(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "current-context:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "current-context:"))
		}
	}
	return ""
}
