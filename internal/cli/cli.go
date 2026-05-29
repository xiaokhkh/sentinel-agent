// Package cli wires Sentinel's subcommands together: the Intent Bridge that
// turns a task into a plan, the Policy Guard that screens it, and the executor
// that applies it.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/xiaokhkh/sentinel-agent/internal/config"
	"github.com/xiaokhkh/sentinel-agent/internal/engine"
	"github.com/xiaokhkh/sentinel-agent/internal/executor"
	"github.com/xiaokhkh/sentinel-agent/internal/llama"
	"github.com/xiaokhkh/sentinel-agent/internal/mcp"
	"github.com/xiaokhkh/sentinel-agent/internal/memory"
	"github.com/xiaokhkh/sentinel-agent/internal/permission"
	"github.com/xiaokhkh/sentinel-agent/internal/policy"
	"github.com/xiaokhkh/sentinel-agent/internal/redact"
	"github.com/xiaokhkh/sentinel-agent/internal/semantic"
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
	case "skill":
		return cmdSkill(args[1:])
	case "skills":
		return cmdSkills()
	case "context":
		return cmdContext()
	case "config":
		return cmdConfig(args[1:])
	case "remember":
		return cmdRemember(args[1:])
	case "memory":
		return cmdMemory()
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

	var plan *engine.Plan
	transientFacts := []string{}
	reader := bufio.NewReader(os.Stdin)
	for round := 0; round < 3; round++ {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Second)
		next, err := inf.Plan(ctx, task, rag)
		cancel()
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
		plan = next
		if plan.NeedsInput == nil {
			break
		}

		fmt.Println(plan.NeedsInput.Prompt)
		answer, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, "input error:", err)
			return 1
		}
		trimmed := strings.TrimSpace(answer)
		if trimmed == "" {
			fmt.Println("no input; aborting")
			return 2
		}

		if plan.NeedsInput.Key != "" {
			store, err := memory.Load()
			if err != nil {
				fmt.Fprintln(os.Stderr, "memory load error:", err)
				return 1
			}
			if err := store.Set(plan.NeedsInput.Key, trimmed); err != nil {
				fmt.Fprintln(os.Stderr, "memory set error:", err)
				return 2
			}
			if err := store.Save(); err != nil {
				fmt.Fprintln(os.Stderr, "memory save error:", err)
				return 1
			}
			fmt.Printf("saved to %s (%s)\n", memory.Path(), plan.NeedsInput.Key)
		} else {
			transientFacts = append(transientFacts, trimmed)
		}

		rag = engine.LoadLocalContext()
		rag.Facts = append(rag.Facts, transientFacts...)
	}
	if plan == nil || plan.NeedsInput != nil {
		fmt.Println("still missing info; aborting")
		return 2
	}

	fmt.Printf("\ngenerated plan (%d action(s)) [mode=%s]:\n\n", len(plan.Actions), pmode)
	guard := policy.New()
	sem := semanticForConfig(withProviderConfig(cfg, *provider, *baseURL, *model))
	evaluate := guard.Evaluate
	if sem != nil {
		evaluate = func(command string) policy.Verdict {
			return sem.ClassifyCommand(context.Background(), command, guard.Evaluate(command))
		}
	}
	exc := executor.New(pmode)
	results := exc.RunPlanWithEvaluator(plan, evaluate)

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

	cfg := config.Load()
	sem := semanticForConfig(cfg)
	v := policy.New().Evaluate(command)
	if sem != nil {
		v = sem.ClassifyCommand(context.Background(), command, v)
	}
	fmt.Printf("%s\n", command)
	fmt.Printf("  decision: %s  risk: %s  rule: %s\n", strings.ToUpper(string(v.Decision)), v.Risk, v.Rule)
	fmt.Printf("  reason:   %s\n", v.Reason)
	if v.Decision == policy.Block {
		return 1
	}
	return 0
}

const skillMaxOutputBytes = 8000
const skillSolveMaxOutputBytes = 4000

func cmdSkill(args []string) int {
	if len(args) < 1 {
		printSkillUsage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "context":
		return cmdSkillContext()
	case "plan":
		return cmdSkillPlan(args[1:])
	case "exec":
		return cmdSkillExec(args[1:])
	case "policy":
		return cmdSkillPolicy(args[1:])
	case "solve":
		return cmdSkillSolve(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown skill command %q\n\n", args[0])
		printSkillUsage(os.Stderr)
		return 2
	}
}

type skillSolveStep struct {
	Command  string `json:"command"`
	Decision string `json:"decision"`
	Rule     string `json:"rule"`
	Output   string `json:"output"`
}

type skillSolveAction struct {
	Command  string `json:"command"`
	Decision string `json:"decision"`
	Risk     string `json:"risk"`
	Reason   string `json:"reason"`
}

type skillSolveResult struct {
	Status         string            `json:"status"`
	Task           string            `json:"task"`
	Provider       string            `json:"provider"`
	Mode           string            `json:"mode"`
	Steps          []skillSolveStep  `json:"steps"`
	ProposedAction *skillSolveAction `json:"proposed_action,omitempty"`
	Conclusion     string            `json:"conclusion,omitempty"`
	Prompt         string            `json:"prompt,omitempty"`
	Key            string            `json:"key,omitempty"`
	Note           string            `json:"note"`
}

func cmdSkillSolve(args []string) int {
	fs := flag.NewFlagSet("skill solve", flag.ContinueOnError)
	cfg := config.Load()
	provider := fs.String("provider", cfg.Provider, "inference provider: mock|ollama|llamacpp|mlx")
	baseURL := fs.String("base-url", cfg.BaseURL, "OpenAI-compatible endpoint base URL")
	model := fs.String("model", cfg.Model, "model name/tag")
	mode := fs.String("mode", cfg.Mode, "execution mode: readonly|auto|full")
	maxSteps := fs.Int("max-steps", 5, "maximum read-only investigation steps")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	task := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if task == "" {
		fmt.Fprintln(os.Stderr, `usage: guard skill solve [flags] "<natural language task>"`)
		return 2
	}
	if *maxSteps < 1 {
		*maxSteps = 1
	}

	pmode, ok := permission.ParseMode(*mode)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown mode %q (readonly|auto|full)\n", *mode)
		return 2
	}

	result := skillSolveResult{
		Status:   "stuck",
		Task:     redact.Redact(task),
		Provider: redact.Redact(*provider),
		Mode:     redact.Redact(string(pmode)),
		Steps:    []skillSolveStep{},
		Note:     "evidence is desensitized; cloud planner analyzes and decides next step",
	}

	if isLlamaCppProvider(*provider) {
		if err := llama.EnsureServer(*model, *baseURL, 5*time.Minute); err != nil {
			writeJSON(result)
			return 1
		}
	}

	inf, err := engine.NewProvider(engine.ProviderConfig{
		Name:    *provider,
		BaseURL: *baseURL,
		Model:   *model,
		APIKey:  cfg.APIKey,
		Timeout: cfg.Timeout,
	})
	if err != nil {
		writeJSON(result)
		return 1
	}

	sem := semanticForConfig(withProviderConfig(cfg, *provider, *baseURL, *model))
	redactText := func(text string) string {
		return redactWithSemantic(sem, text)
	}
	result.Task = redactText(task)
	result.Provider = redactText(inf.Name())
	result.Mode = redactText(string(pmode))

	guard := policy.New()
	rag := engine.LoadLocalContext()
	observations := []string{}
	executed := map[string]bool{}

	for i := 0; i < *maxSteps; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Second)
		step, err := inf.PlanNextStep(ctx, task, rag, observations)
		cancel()
		if err != nil || step == nil {
			result.Status = "stuck"
			return writeJSON(result)
		}

		if step.NeedsInput != nil {
			result.Status = "needs_input"
			result.Prompt = redactText(step.NeedsInput.Prompt)
			result.Key = redactText(step.NeedsInput.Key)
			return writeJSON(result)
		}
		if step.Done {
			result.Status = "completed"
			result.Conclusion = redactText(step.Conclusion)
			return writeJSON(result)
		}

		command := strings.TrimSpace(step.Command)
		if command == "" {
			result.Status = "stuck"
			return writeJSON(result)
		}
		if executed[command] {
			result.Status = "completed"
			result.Conclusion = "model repeated an already executed read-only command; no new evidence requested"
			return writeJSON(result)
		}

		verdict := guard.Evaluate(command)
		if sem != nil {
			verdict = sem.ClassifyCommand(context.Background(), command, verdict)
		}
		if unsafeShellOperator(command) && verdict.Decision == policy.Allow {
			verdict = policy.Verdict{
				Decision: policy.Block,
				Rule:     "solve-single-command",
				Reason:   "compound shell syntax is not allowed in autonomous solve mode",
				Risk:     policy.RiskHigh,
			}
		}
		outcome := permission.Decide(verdict.Decision, pmode)
		if verdict.Decision == policy.Allow && outcome == permission.Run {
			out, _ := osexec.Command("sh", "-c", command).CombinedOutput()
			output := capUTF8(redactText(string(out)), skillSolveMaxOutputBytes)
			redactedCommand := redactText(command)
			result.Steps = append(result.Steps, skillSolveStep{
				Command:  redactedCommand,
				Decision: redactText(string(verdict.Decision)),
				Rule:     redactText(verdict.Rule),
				Output:   output,
			})
			executed[command] = true
			observations = append(observations, redactedCommand+" -> "+compactObservation(output))
			continue
		}

		result.ProposedAction = &skillSolveAction{
			Command:  redactText(command),
			Decision: redactText(string(verdict.Decision)),
			Risk:     redactText(string(verdict.Risk)),
			Reason:   redactText(verdict.Reason),
		}
		switch verdict.Decision {
		case policy.Confirm:
			result.Status = "needs_approval"
		case policy.Block:
			result.Status = "blocked"
		default:
			result.Status = "stuck"
		}
		return writeJSON(result)
	}

	result.Status = "stuck"
	return writeJSON(result)
}

func cmdSkillContext() int {
	rag := engine.LoadLocalContext()
	return writeJSON(map[string]any{
		"hostname":       rag.Hostname,
		"has_kubeconfig": rag.HasKubeConfig,
		"kube_context":   rag.KubeContext,
		"namespace":      rag.Namespace,
		"has_ssh_config": rag.HasSSHConfig,
		"memory":         append([]string{}, rag.Facts...),
		"note":           "non-secret summary only; file contents and credentials are never exposed",
	})
}

func cmdSkillPlan(args []string) int {
	fs := flag.NewFlagSet("skill plan", flag.ContinueOnError)
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
		fmt.Fprintln(os.Stderr, `usage: guard skill plan [flags] "<natural language task>"`)
		return 2
	}

	pmode, ok := permission.ParseMode(*mode)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown mode %q (readonly|auto|full)\n", *mode)
		return 2
	}

	if isLlamaCppProvider(*provider) {
		if err := llama.EnsureServer(*model, *baseURL, 5*time.Minute); err != nil {
			writeJSON(map[string]any{"status": "error", "error": err.Error()})
			return 1
		}
	}

	inf, err := engine.NewProvider(engine.ProviderConfig{
		Name:    *provider,
		BaseURL: *baseURL,
		Model:   *model,
		APIKey:  cfg.APIKey,
		Timeout: cfg.Timeout,
	})
	if err != nil {
		writeJSON(map[string]any{"status": "error", "error": err.Error()})
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout+5*time.Second)
	plan, err := inf.Plan(ctx, task, engine.LoadLocalContext())
	cancel()
	if errors.Is(err, engine.ErrIntentDowngrade) {
		writeJSON(map[string]any{
			"status": "no_plan",
			"error":  err.Error(),
			"note":   "per Sentinel policy, the raw task was not escalated off-device",
		})
		return 3
	}
	if err != nil {
		writeJSON(map[string]any{"status": "error", "error": err.Error()})
		return 1
	}
	if plan.NeedsInput != nil {
		return writeJSON(map[string]any{
			"status": "needs_input",
			"prompt": plan.NeedsInput.Prompt,
			"key":    plan.NeedsInput.Key,
			"note":   "answer via guard config set or include it and call guard skill plan again",
		})
	}

	type screened struct {
		Kind        string `json:"kind"`
		Command     string `json:"command"`
		Explanation string `json:"explanation"`
		Decision    string `json:"decision"`
		Risk        string `json:"risk"`
		Rule        string `json:"rule"`
		Outcome     string `json:"outcome_under_mode"`
	}
	out := struct {
		Status   string     `json:"status"`
		Task     string     `json:"task"`
		Provider string     `json:"provider"`
		Mode     string     `json:"mode"`
		Actions  []screened `json:"actions"`
		Note     string     `json:"note"`
	}{
		Status:   "planned",
		Task:     task,
		Provider: inf.Name(),
		Mode:     string(pmode),
		Note:     "run read-only actions with guard skill exec; mutating actions require approval",
	}

	guard := policy.New()
	sem := semanticForConfig(withProviderConfig(cfg, *provider, *baseURL, *model))
	for _, ac := range plan.Actions {
		v := guard.Evaluate(ac.Command)
		if sem != nil {
			v = sem.ClassifyCommand(context.Background(), ac.Command, v)
		}
		out.Actions = append(out.Actions, screened{
			Kind:        string(ac.Kind),
			Command:     redactWithSemantic(sem, ac.Command),
			Explanation: ac.Explanation,
			Decision:    string(v.Decision),
			Risk:        string(v.Risk),
			Rule:        v.Rule,
			Outcome:     string(permission.Decide(v.Decision, pmode)),
		})
	}
	return writeJSON(out)
}

func cmdSkillExec(args []string) int {
	fs := flag.NewFlagSet("skill exec", flag.ContinueOnError)
	cfg := config.Load()
	mode := fs.String("mode", cfg.Mode, "execution mode: readonly|auto|full")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	command := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if command == "" {
		fmt.Fprintln(os.Stderr, `usage: guard skill exec [--mode readonly|auto|full] "<command>"`)
		return 2
	}

	pmode, ok := permission.ParseMode(*mode)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown mode %q (readonly|auto|full)\n", *mode)
		return 2
	}

	sem := semanticForConfig(cfg)
	v := policy.New().Evaluate(command)
	if sem != nil {
		v = sem.ClassifyCommand(context.Background(), command, v)
	}
	res := map[string]any{
		"command":  redactWithSemantic(sem, command),
		"decision": v.Decision,
		"risk":     v.Risk,
		"rule":     v.Rule,
		"mode":     pmode,
	}

	switch permission.Decide(v.Decision, pmode) {
	case permission.Refuse:
		res["status"] = "refused"
		res["reason"] = "blocked by policy: " + v.Reason
		writeJSON(res)
		return 1
	case permission.Ask:
		res["status"] = "approval_required"
		res["reason"] = "mutating command not auto-executed; ask the user or run it locally with explicit approval"
		writeJSON(res)
		return 2
	case permission.Run:
		out, err := osexec.Command("sh", "-c", command).CombinedOutput()
		text := string(out)
		if len(text) > skillMaxOutputBytes {
			text = text[:skillMaxOutputBytes] + "\n...[truncated]"
		}
		res["status"] = "executed"
		res["output"] = redactWithSemantic(sem, text)
		if err != nil {
			res["error"] = err.Error()
			writeJSON(res)
			return 1
		}
		return writeJSON(res)
	default:
		res["status"] = "error"
		res["reason"] = "unknown permission outcome"
		writeJSON(res)
		return 1
	}
}

func cmdSkillPolicy(args []string) int {
	command := strings.TrimSpace(strings.Join(args, " "))
	if command == "" {
		fmt.Fprintln(os.Stderr, `usage: guard skill policy "<command>"`)
		return 2
	}
	cfg := config.Load()
	sem := semanticForConfig(cfg)
	v := policy.New().Evaluate(command)
	if sem != nil {
		v = sem.ClassifyCommand(context.Background(), command, v)
	}
	return writeJSON(map[string]any{
		"command":  redactWithSemantic(sem, command),
		"decision": v.Decision,
		"risk":     v.Risk,
		"rule":     v.Rule,
		"reason":   v.Reason,
	})
}

func semanticForConfig(cfg config.Config) *semantic.Classifier {
	if !semantic.Enabled() {
		return nil
	}
	return semantic.New(cfg)
}

func withProviderConfig(cfg config.Config, provider, baseURL, model string) config.Config {
	cfg.Provider = provider
	cfg.BaseURL = baseURL
	cfg.Model = model
	return cfg
}

func redactWithSemantic(c *semantic.Classifier, text string) string {
	if c == nil {
		return redact.Redact(text)
	}
	return c.RedactText(context.Background(), text)
}

func capUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s + "\n...[truncated]"
}

func compactObservation(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return capUTF8(s, 800)
}

func unsafeShellOperator(command string) bool {
	return strings.Contains(command, "\n") ||
		strings.Contains(command, "\r") ||
		strings.Contains(command, "&&") ||
		strings.Contains(command, "||") ||
		strings.Contains(command, ";") ||
		strings.Contains(command, "|") ||
		strings.Contains(command, ">") ||
		strings.Contains(command, "<") ||
		strings.Contains(command, "`") ||
		strings.Contains(command, "$(")
}

func writeJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, "json encode error:", err)
		return 1
	}
	return 0
}

func printSkillUsage(w io.Writer) {
	fmt.Fprint(w, `usage:
  guard skill context                         print non-secret local context as JSON
  guard skill plan [flags] "<task>"           plan locally and return screened JSON actions
  guard skill solve [flags] "<task>"          run bounded read-only investigation and return redacted JSON evidence
  guard skill exec [flags] "<command>"        run one allowed command and return redacted JSON output
  guard skill policy "<command>"              classify one command as JSON

`)
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
	fmt.Printf("  namespace:    %s\n", rag.Namespace)
	fmt.Printf("  ssh config:   present=%v path=%s\n", rag.HasSSHConfig, rag.SSHConfigPath)
	if len(rag.Facts) > 0 {
		fmt.Println("  memory:")
		for _, fact := range rag.Facts {
			fmt.Printf("    - %s\n", fact)
		}
	}
	return 0
}

func cmdConfig(args []string) int {
	store, err := memory.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "memory load error:", err)
		return 1
	}
	switch {
	case len(args) == 0:
		raw, err := json.MarshalIndent(store, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "memory encode error:", err)
			return 1
		}
		fmt.Printf("path: %s\n%s\n", memory.Path(), raw)
		return 0
	case len(args) == 2 && args[0] == "get":
		value, ok := store.Get(args[1])
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown memory key %q\n", args[1])
			return 2
		}
		fmt.Println(value)
		return 0
	case len(args) >= 3 && args[0] == "set":
		key := args[1]
		value := strings.Join(args[2:], " ")
		if err := store.Set(key, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if err := store.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "memory save error:", err)
			return 1
		}
		fmt.Printf("saved to %s (%s)\n", memory.Path(), key)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: guard config [get <key>|set <key> <value>]")
		return 2
	}
}

func cmdRemember(args []string) int {
	fact := strings.TrimSpace(strings.Join(args, " "))
	if fact == "" {
		fmt.Fprintln(os.Stderr, `usage: guard remember "<fact>"`)
		return 2
	}
	store, err := memory.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "memory load error:", err)
		return 1
	}
	store.AddFact(fact)
	if err := store.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "memory save error:", err)
		return 1
	}
	fmt.Printf("remembered in %s\n", memory.Path())
	return 0
}

func cmdMemory() int {
	store, err := memory.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "memory load error:", err)
		return 1
	}
	for _, fact := range store.Facts {
		fmt.Println(fact)
	}
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
  guard skill <context|plan|solve|exec|policy>  JSON interface for Sentinel Skill agents
  guard skills                                  list available capability packs
  guard context                                 show local RAG context (no secrets)
  guard config                                  show structured memory config
  guard config get <key>                        print a structured memory value
  guard config set <key> <value>                save a structured memory value
  guard remember "<fact>"                       add a local knowledge fact
  guard memory                                  list local knowledge facts
  guard serve                                   run llama-server in the foreground
  guard stop                                    stop the background llama-server
  guard model                                   show local model/bootstrap status
  guard model pull                              warm the local llama.cpp model cache
  guard mcp                                     run the optional MCP stdio adapter
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
