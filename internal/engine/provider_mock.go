package engine

import (
	"context"
	"fmt"
	"strings"
)

// mockProvider is a deterministic, model-free backend. It lets the full
// pipeline (RAG -> plan -> Policy Guard -> executor) run end-to-end with no
// model installed, which is what CI and first-run demos use. It recognizes a
// handful of K8s intents and downgrades on anything else.
type mockProvider struct{}

// NewMockProvider returns the offline demo backend.
func NewMockProvider() Inferencer { return &mockProvider{} }

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Plan(_ context.Context, task string, _ *LocalContext) (*Plan, error) {
	t := strings.ToLower(task)
	ns := "default"
	name := extractName(t)

	mk := func(kind ActionKind, cmd, why string) Action {
		return Action{Kind: kind, Command: cmd, Explanation: why}
	}

	var actions []Action
	switch {
	case containsAny(t, "log", "日志"):
		target := name
		if target == "" {
			target = "your-service"
		}
		actions = []Action{
			mk(ActionKubectl, fmt.Sprintf("kubectl get pods -n %s", ns), "list pods to confirm the target exists"),
			mk(ActionKubectl, fmt.Sprintf("kubectl logs -n %s -l app=%s --tail=200", ns, target), "fetch recent logs for the service"),
		}
	case containsAny(t, "restart", "重启", "rollout"):
		target := name
		if target == "" {
			target = "your-deployment"
		}
		actions = []Action{
			mk(ActionKubectl, fmt.Sprintf("kubectl get deploy -n %s", ns), "confirm the deployment before restarting"),
			mk(ActionKubectl, fmt.Sprintf("kubectl rollout restart deployment/%s -n %s", target, ns), "trigger a rolling restart"),
		}
	case containsAny(t, "diagnose", "诊断", "not ready", "未就绪", "crash", "pending", "unhealthy", "健康", "排查"):
		actions = []Action{
			mk(ActionKubectl, fmt.Sprintf("kubectl get pods -n %s --field-selector=status.phase!=Running", ns), "find pods that are not Running"),
			mk(ActionKubectl, fmt.Sprintf("kubectl describe pods -n %s", ns), "inspect events and conditions for failing pods"),
		}
	case containsAny(t, "list", "查看", "show", "get", "pods", "pod"):
		actions = []Action{
			mk(ActionKubectl, fmt.Sprintf("kubectl get pods -n %s -o wide", ns), "list pods with node placement"),
		}
	default:
		return nil, ErrIntentDowngrade
	}

	return &Plan{Task: task, Actions: actions, Source: m.Name()}, nil
}

func (m *mockProvider) PlanNextStep(ctx context.Context, task string, rag *LocalContext, observations []string) (*Step, error) {
	plan, err := m.Plan(ctx, task, rag)
	if err != nil {
		return nil, err
	}
	if len(observations) >= len(plan.Actions) {
		return &Step{Done: true, Conclusion: "mock investigation complete"}, nil
	}
	return &Step{Command: plan.Actions[len(observations)].Command}, nil
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractName makes a best-effort guess at a resource name: the first ASCII
// token that is not an ops keyword. Heuristic only — the real model does this
// far better.
func extractName(task string) string {
	stop := map[string]bool{
		"kubectl": true, "pod": true, "pods": true, "the": true, "log": true,
		"logs": true, "get": true, "restart": true, "deployment": true,
		"deploy": true, "service": true, "svc": true, "namespace": true,
		"diagnose": true, "show": true, "list": true, "run": true, "guard": true,
	}
	for _, f := range strings.Fields(task) {
		w := strings.Trim(strings.ToLower(f), `"'.,:;`)
		if len(w) < 3 || stop[w] {
			continue
		}
		if isASCIIName(w) {
			return w
		}
	}
	return ""
}

func isASCIIName(s string) bool {
	for _, r := range s {
		if !(r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
