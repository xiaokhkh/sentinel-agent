// Package cli wires Sentinel's subcommands together: the Intent Bridge that
// turns a task into a plan, the Policy Guard that screens it, and the executor
// that applies it.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/xiaokhkh/sentinel-agent/internal/config"
	"github.com/xiaokhkh/sentinel-agent/internal/engine"
	"github.com/xiaokhkh/sentinel-agent/internal/executor"
	"github.com/xiaokhkh/sentinel-agent/internal/llama"
	"github.com/xiaokhkh/sentinel-agent/internal/mcp"
	"github.com/xiaokhkh/sentinel-agent/internal/permission"
	"github.com/xiaokhkh/sentinel-agent/internal/policy"
	"github.com/xiaokhkh/sentinel-agent/internal/skills"

	// Register capability packs for `guard skills`.
	_ "github.com/xiaokhkh/sentinel-agent/internal/skills/k8s"
)

// Version is the CLI version.
const Version = "0.1.0"

// Run dispatches a subcommand and returns the process exit code.
func Run(args []string) int {
	if len(args) < 1 {
		printUsage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "run":
		return cmdRun(args[1:])
	case "policy":
		return cmdPolicy(args[1:])
	case "skills":
		return cmdSkills()
	case "context":
		return cmdContext()
	case "serve":
		return cmdServe(args[1:])
	case "stop":
		return cmdStop()
	case "model":
		return cmdModel(args[1:])
	case "mcp":
		return cmdMCP()
	case "version", "--version", "-v":
		fmt.Printf("sentinel (guard) %s\n", Version)
		return 0
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfg := config.Load()
	provider := fs.String("provider", cfg.Provider, "inference provider: mock|ollama|llamacpp|mlx")
	baseURL := fs.String("base-url", cfg.BaseURL, "OpenAI-compatible endpoint base URL")
	model := fs.String("model", cfg.Model, "model name/tag")
	mode := fs.String("mode", cfg.Mode, "execution mode: readonly|auto|full")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	task := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if task == "" {
		fmt.Fprintln(os.Stderr, `usage: guard run [flags] "<natural language task>"`)
		return 2
	}

	pmode, ok := permission.ParseMode(*mode)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown mode %q (readonly|auto|full)\n", *mode)
		return 2
	}

	if isLlamaCppProvider(*provider) {
		if err := llama.EnsureServer(*model, *baseURL, 5*time.Minute); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
	}

	rag := engine.LoadLocalContext()
	inf, err := engine.NewProvider(engine.ProviderConfig{
		Name:    *provider,
		BaseURL: *baseURL,
		Model:   *model,
		APIKey:  cfg.APIKey,
		Timeout: cfg.Timeout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("provider: %s   task: %s\n", inf.Name(), task)
	if rag.KubeContext != "" {
		fmt.Printf("local context: kube current-context=%s\n", rag.KubeContext)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Second)
	defer cancel()

	plan, err := inf.Plan(ctx, task, rag)
	if errors.Is(err, engine.ErrIntentDowngrade) {
		fmt.Println()
		fmt.Println("本地模型无法可靠处理该意图。")
		fmt.Println("  根据 Sentinel 隐私策略，已停止 —— 不会将你的上下文发往任何云端模型。")
		fmt.Println("  可尝试：细化指令、切换更强的本地模型，或扩展对应技能包。")
		return 3
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "inference error:", err)
		if *provider != "mock" {
			fmt.Fprintln(os.Stderr, "hint: 本地推理服务未就绪？可先用 --provider mock 体验流程，或启动 Ollama/llama.cpp 后重试。")
		}
		return 1
	}

	fmt.Printf("\ngenerated plan (%d action(s)) [mode=%s]:\n\n", len(plan.Actions), pmode)
	guard := policy.New()
	exc := executor.New(pmode)
	results := exc.RunPlan(plan, guard)

	var ran, blocked, skipped int
	for _, r := range results {
		switch {
		case r.Verdict.Decision == policy.Block:
			blocked++
		case r.Ran:
			ran++
		default:
			skipped++
		}
	}
	fmt.Printf("\nsummary: %d ran, %d blocked, %d skipped\n", ran, blocked, skipped)
	return 0
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfg := config.Load()
	baseURL := fs.String("base-url", cfg.BaseURL, "OpenAI-compatible endpoint base URL")
	model := fs.String("model", cfg.Model, "model name/tag")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := llama.RunForeground(*model, *baseURL); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func cmdStop() int {
	if err := llama.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("local llama-server stopped")
	return 0
}

func cmdModel(args []string) int {
	if len(args) > 0 && args[0] == "pull" {
		return cmdModelPull(args[1:])
	}
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, `usage: guard model [pull]`)
		return 2
	}

	cfg := config.Load()
	fmt.Println("model status:")
	fmt.Printf("  configured model: %s\n", cfg.Model)
	if path, err := llama.FindBinary(); err != nil {
		fmt.Printf("  llama-server:     %s\n", err)
	} else {
		fmt.Printf("  llama-server:     %s\n", path)
	}
	fmt.Printf("  endpoint:         reachable=%v\n", llama.Reachable(cfg.BaseURL, 2*time.Second))
	fmt.Printf("  sentinel home:    %s\n", llama.Home())
	return 0
}

func cmdModelPull(args []string) int {
	fs := flag.NewFlagSet("model pull", flag.ContinueOnError)
	cfg := config.Load()
	baseURL := fs.String("base-url", cfg.BaseURL, "OpenAI-compatible endpoint base URL")
	model := fs.String("model", cfg.Model, "model name/tag")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := llama.EnsureServer(*model, *baseURL, 5*time.Minute); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("model cache warm; local llama-server is running")
	return 0
}

func isLlamaCppProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "llamacpp", "llama.cpp", "llama":
		return true
	default:
		return false
	}
}

func cmdPolicy(args []string) int {
	if len(args) < 1 || args[0] != "check" {
		fmt.Fprintln(os.Stderr, `usage: guard policy check "<command>"`)
		return 2
	}
	command := strings.TrimSpace(strings.Join(args[1:], " "))
	if command == "" {
		fmt.Fprintln(os.Stderr, `usage: guard policy check "<command>"`)
		return 2
	}

	v := policy.New().Evaluate(command)
	fmt.Printf("%s\n", command)
	fmt.Printf("  decision: %s  risk: %s  rule: %s\n", strings.ToUpper(string(v.Decision)), v.Risk, v.Rule)
	fmt.Printf("  reason:   %s\n", v.Reason)
	if v.Decision == policy.Block {
		return 1
	}
	return 0
}

func cmdSkills() int {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "SKILL\tSTATUS\tDESCRIPTION")
	for _, s := range skills.All() {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Status, s.Description)
	}
	w.Flush()
	return 0
}

func cmdContext() int {
	rag := engine.LoadLocalContext()
	fmt.Println("local context (never leaves this machine):")
	fmt.Printf("  hostname:     %s\n", rag.Hostname)
	fmt.Printf("  kubeconfig:   present=%v path=%s\n", rag.HasKubeConfig, rag.KubeConfigPath)
	if rag.HasKubeConfig {
		fmt.Printf("  kube context: %s\n", rag.KubeContext)
	}
	fmt.Printf("  ssh config:   present=%v path=%s\n", rag.HasSSHConfig, rag.SSHConfigPath)
	return 0
}

func cmdMCP() int {
	if err := mcp.NewServer(os.Stdin, os.Stdout, config.Load()).Serve(); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		return 1
	}
	return 0
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `Sentinel (guard) — on-device secure execution layer

usage:
  guard run [flags] "<natural language task>"   plan (and optionally run) an ops task
  guard policy check "<command>"                test a command against the Policy Guard
  guard skills                                  list available capability packs
  guard context                                 show local RAG context (no secrets)
  guard serve                                   run llama-server in the foreground
  guard stop                                    stop the background llama-server
  guard model                                   show local model/bootstrap status
  guard model pull                              warm the local llama.cpp model cache
  guard mcp                                     run as an MCP server (stdio) for cloud LLM clients
  guard version                                 print version

run flags:
  --provider   mock|ollama|llamacpp|mlx   inference backend (default: llamacpp)
  --base-url   <url>                      OpenAI-compatible endpoint base URL
  --model      <tag>                      model name/tag
  --mode       readonly|auto|full         autonomy level (default: readonly)
                                            readonly=run reads, ask on writes;
                                            auto=run reads+writes; full=run everything (dangerous)

examples:
  guard run --provider mock "诊断 default 命名空间里未就绪的 pod"
  guard model pull
  guard run --mode readonly "show logs for the payment service"
  guard policy check "kubectl delete pods --all"
`)
}
