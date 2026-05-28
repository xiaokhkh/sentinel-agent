package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaokhkh/sentinel-agent/internal/memory"
)

func TestLoadLocalContextUsesStructuredMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store := &memory.Store{}
	store.Kubernetes.Kubeconfig = "/tmp/custom-kubeconfig"
	store.Kubernetes.Context = "prod"
	store.Kubernetes.Namespace = "payments"
	store.AddFact("payment pods are in payments")
	if err := store.Save(); err != nil {
		t.Fatalf("Save memory: %v", err)
	}

	rag := LoadLocalContext()
	if !rag.HasKubeConfig {
		t.Fatal("HasKubeConfig=false; want true from memory")
	}
	if rag.KubeConfigPath != "/tmp/custom-kubeconfig" {
		t.Fatalf("KubeConfigPath = %q", rag.KubeConfigPath)
	}
	if rag.KubeContext != "prod" {
		t.Fatalf("KubeContext = %q", rag.KubeContext)
	}
	if rag.Namespace != "payments" {
		t.Fatalf("Namespace = %q", rag.Namespace)
	}
	if len(rag.Facts) != 1 || rag.Facts[0] != "payment pods are in payments" {
		t.Fatalf("Facts = %#v", rag.Facts)
	}
	summary := rag.Summary()
	for _, want := range []string{"namespace=payments", "memory=payment pods are in payments"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Summary() = %q; want substring %q", summary, want)
		}
	}
}

func TestLoadLocalContextFallsBackToDetectedKubeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	kubePath := filepath.Join(home, ".kube", "config")
	if err := os.MkdirAll(filepath.Dir(kubePath), 0o700); err != nil {
		t.Fatalf("mkdir kube dir: %v", err)
	}
	if err := os.WriteFile(kubePath, []byte("current-context: dev\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	rag := LoadLocalContext()
	if !rag.HasKubeConfig {
		t.Fatal("HasKubeConfig=false; want true from auto-detect")
	}
	if rag.KubeConfigPath != kubePath {
		t.Fatalf("KubeConfigPath = %q; want %q", rag.KubeConfigPath, kubePath)
	}
	if rag.KubeContext != "dev" {
		t.Fatalf("KubeContext = %q; want dev", rag.KubeContext)
	}
	if rag.Namespace != "default" {
		t.Fatalf("Namespace = %q; want default", rag.Namespace)
	}
}
