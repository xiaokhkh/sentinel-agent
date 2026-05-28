package policy

import "testing"

func TestGuardEvaluate(t *testing.T) {
	g := New()
	cases := []struct {
		command string
		want    Decision
	}{
		{"kubectl get pods -n default", Allow},
		{"kubectl describe pod web-0", Allow},
		{"ls -la /tmp", Allow},
		{"kubectl rollout restart deployment/nginx -n default", Confirm},
		{"kubectl delete pod web-0 -n default", Confirm},
		{"git push origin main --force", Confirm},
		{"systemctl restart nginx", Confirm},
		{"kubectl delete pods --all -n default", Block},
		{"kubectl delete namespace prod", Block},
		{"drop table users", Block},
		{"DROP DATABASE prod", Block},
		{"truncate table sessions", Block},
		{"delete from accounts", Block},
		{"rm -rf /", Block},
		{"cat ~/.ssh/id_rsa", Block},
		{"some-unknown-tool --weird", Confirm}, // safe default
	}

	for _, c := range cases {
		got := g.Evaluate(c.command).Decision
		if got != c.want {
			t.Errorf("Evaluate(%q) = %s, want %s", c.command, got, c.want)
		}
	}
}

func TestGuardDefaultsToConfirm(t *testing.T) {
	v := New().Evaluate("frobnicate the widget")
	if v.Decision != Confirm {
		t.Fatalf("unrecognized command should default to Confirm, got %s", v.Decision)
	}
	if v.Rule != "default" {
		t.Fatalf("expected default rule, got %q", v.Rule)
	}
}
