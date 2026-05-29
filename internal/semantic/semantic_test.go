package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xiaokhkh/sentinel-agent/internal/policy"
)

type fakeClient struct {
	raw  string
	err  error
	user string
}

func (f *fakeClient) Complete(_ context.Context, _, user string) (string, error) {
	f.user = user
	if f.err != nil {
		return "", f.err
	}
	return f.raw, nil
}

func TestEnabled(t *testing.T) {
	t.Setenv("SENTINEL_SEMANTIC", "")
	if Enabled() {
		t.Fatal("Enabled() = true, want false")
	}

	t.Setenv("SENTINEL_SEMANTIC", "1")
	if !Enabled() {
		t.Fatal("Enabled() = false, want true")
	}
}

func TestRedactTextAppliesRegexFirstAndSemanticMask(t *testing.T) {
	fake := &fakeClient{raw: `prefix {"secrets":["db.internal.local"]} suffix`}
	c := NewWithClient(fake)

	got := c.RedactText(context.Background(), "aws=AKIA1234567890ABCDEF host=db.internal.local")

	if strings.Contains(got, "AKIA1234567890ABCDEF") {
		t.Fatalf("regex secret was not masked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:aws-key]") {
		t.Fatalf("regex redaction marker missing: %q", got)
	}
	if strings.Contains(got, "db.internal.local") {
		t.Fatalf("semantic secret was not masked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:semantic]") {
		t.Fatalf("semantic redaction marker missing: %q", got)
	}
	if strings.Contains(fake.user, "AKIA1234567890ABCDEF") {
		t.Fatalf("model saw unredacted regex secret: %q", fake.user)
	}
}

func TestClassifyCommandUpgradesDefaultVerdict(t *testing.T) {
	tests := []struct {
		name string
		base policy.Verdict
		raw  string
		want policy.Decision
	}{
		{
			name: "allow to block when destructive",
			base: policy.Verdict{Decision: policy.Allow, Rule: "default", Risk: policy.RiskLow},
			raw:  `{"risk":"low","destructive":true,"reason":"removes files"}`,
			want: policy.Block,
		},
		{
			name: "confirm to block on high risk",
			base: policy.Verdict{Decision: policy.Confirm, Rule: "default", Risk: policy.RiskMedium},
			raw:  `{"risk":"high","destructive":false,"reason":"dangerous mutation"}`,
			want: policy.Block,
		},
		{
			name: "allow to confirm on medium risk",
			base: policy.Verdict{Decision: policy.Allow, Rule: "default", Risk: policy.RiskLow},
			raw:  `{"risk":"medium","destructive":false,"reason":"needs review"}`,
			want: policy.Confirm,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewWithClient(&fakeClient{raw: tt.raw})
			got := c.ClassifyCommand(context.Background(), "custom command", tt.base)
			if got.Decision != tt.want {
				t.Fatalf("Decision = %s, want %s", got.Decision, tt.want)
			}
			if got.Rule != "semantic" {
				t.Fatalf("Rule = %q, want semantic", got.Rule)
			}
		})
	}
}

func TestClassifyCommandNeverDowngradesRegexBlock(t *testing.T) {
	base := policy.Verdict{
		Decision: policy.Block,
		Rule:     "specific-regex-rule",
		Risk:     policy.RiskCritical,
		Reason:   "regex block",
	}
	c := NewWithClient(&fakeClient{raw: `{"risk":"low","destructive":false,"reason":"safe"}`})

	got := c.ClassifyCommand(context.Background(), "rm -rf /", base)

	if got != base {
		t.Fatalf("ClassifyCommand downgraded block: got %+v want %+v", got, base)
	}
}

func TestFailurePathsReturnBaseline(t *testing.T) {
	c := NewWithClient(&fakeClient{err: errors.New("model failed")})

	text := "email=alice@example.com"
	gotText := c.RedactText(context.Background(), text)
	wantText := "email=[REDACTED:email]"
	if gotText != wantText {
		t.Fatalf("RedactText failure = %q, want regex-only %q", gotText, wantText)
	}

	base := policy.Verdict{Decision: policy.Confirm, Rule: "default", Risk: policy.RiskMedium, Reason: "baseline"}
	gotVerdict := c.ClassifyCommand(context.Background(), "custom command", base)
	if gotVerdict != base {
		t.Fatalf("ClassifyCommand failure = %+v, want %+v", gotVerdict, base)
	}
}
