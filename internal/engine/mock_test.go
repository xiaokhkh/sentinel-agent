package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMockProviderPlans(t *testing.T) {
	m := NewMockProvider()
	ctx := context.Background()

	cases := []struct {
		task      string
		wantSub   string // substring expected in at least one action command
		minAction int
	}{
		{"查看 payment 服务最近的日志", "logs", 1},
		{"restart nginx deployment", "rollout restart", 1},
		{"诊断 default 命名空间里未就绪的 pod", "field-selector", 1},
		{"list pods", "get pods", 1},
	}

	for _, c := range cases {
		plan, err := m.Plan(ctx, c.task, nil)
		if err != nil {
			t.Errorf("Plan(%q) unexpected error: %v", c.task, err)
			continue
		}
		if len(plan.Actions) < c.minAction {
			t.Errorf("Plan(%q) returned %d actions, want >= %d", c.task, len(plan.Actions), c.minAction)
		}
		found := false
		for _, a := range plan.Actions {
			if strings.Contains(a.Command, c.wantSub) {
				found = true
			}
		}
		if !found {
			t.Errorf("Plan(%q) actions did not contain %q: %+v", c.task, c.wantSub, plan.Actions)
		}
	}
}

func TestMockProviderDowngrades(t *testing.T) {
	_, err := NewMockProvider().Plan(context.Background(), "qwerty asdf zxcv", nil)
	if !errors.Is(err, ErrIntentDowngrade) {
		t.Fatalf("expected ErrIntentDowngrade, got %v", err)
	}
}

func TestParsePlanDowngradeOnGarbage(t *testing.T) {
	if _, err := parsePlan("no json here", "test", "t"); !errors.Is(err, ErrIntentDowngrade) {
		t.Fatalf("expected downgrade on non-JSON, got %v", err)
	}
	if _, err := parsePlan(`{"actions":[]}`, "test", "t"); !errors.Is(err, ErrIntentDowngrade) {
		t.Fatalf("expected downgrade on empty actions, got %v", err)
	}
	if _, err := parsePlan(`{"actions":[{"kind":"shell","command":" "}]} `, "test", "t"); !errors.Is(err, ErrIntentDowngrade) {
		t.Fatalf("expected downgrade on empty command, got %v", err)
	}
	plan, err := parsePlan(`prefix {"actions":[{"kind":"shell","command":"ls"}]} suffix`, "test", "t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Command != "ls" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestParsePlanNeedsInput(t *testing.T) {
	plan, err := parsePlan(`{"needs_input":{"prompt":"Which kubeconfig should I use?","key":"kubernetes.kubeconfig"}}`, "test", "check pods")
	if err != nil {
		t.Fatalf("parsePlan needs_input: %v", err)
	}
	if plan.NeedsInput == nil {
		t.Fatal("NeedsInput is nil")
	}
	if plan.NeedsInput.Prompt != "Which kubeconfig should I use?" {
		t.Fatalf("prompt = %q", plan.NeedsInput.Prompt)
	}
	if plan.NeedsInput.Key != "kubernetes.kubeconfig" {
		t.Fatalf("key = %q", plan.NeedsInput.Key)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("actions = %#v; want none", plan.Actions)
	}
}
