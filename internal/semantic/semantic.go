// Package semantic adds an optional local-LLM safety layer on top of the
// deterministic regex redactor and policy guard.
//
// Semantic safety must point at a local provider. It analyzes raw or
// secret-bearing data; sending these prompts to a cloud model defeats the
// purpose of this layer and weakens Sentinel's privacy boundary.
package semantic

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xiaokhkh/sentinel-agent/internal/config"
	"github.com/xiaokhkh/sentinel-agent/internal/engine"
	"github.com/xiaokhkh/sentinel-agent/internal/policy"
	"github.com/xiaokhkh/sentinel-agent/internal/redact"
)

const (
	maxModelInputBytes = 4000
	defaultTimeout     = 20 * time.Second
)

// ChatClient is the small model surface Classifier needs. Tests can inject a
// fake implementation so no model or network is required.
type ChatClient interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Classifier augments regex redaction and policy verdicts with an optional
// local model pass. Its zero value is safe and behaves like the regex-only
// baseline.
type Classifier struct {
	client  ChatClient
	timeout time.Duration
	enabled bool
}

type engineChatClient struct {
	cfg engine.ProviderConfig
}

func (c engineChatClient) Complete(ctx context.Context, system, user string) (string, error) {
	return engine.Chat(ctx, c.cfg, system, user)
}

// Enabled reports whether semantic augmentation should run.
func Enabled() bool {
	return os.Getenv("SENTINEL_SEMANTIC") == "1"
}

// New builds a classifier bound to the configured provider. The provider must
// be local-only because this layer may inspect raw secrets or PII.
func New(cfg config.Config) *Classifier {
	timeout := boundedTimeout(cfg.Timeout)
	return &Classifier{
		client: engineChatClient{cfg: engine.ProviderConfig{
			Name:    cfg.Provider,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			APIKey:  cfg.APIKey,
			Timeout: timeout,
		}},
		timeout: timeout,
		enabled: Enabled(),
	}
}

// NewWithClient builds an enabled classifier with an injected model client.
func NewWithClient(client ChatClient) *Classifier {
	return &Classifier{client: client, timeout: defaultTimeout, enabled: true}
}

// RedactText applies the regex redactor first, then optionally asks a local
// model to identify residual sensitive substrings in the redacted text.
func (c *Classifier) RedactText(ctx context.Context, text string) string {
	redacted := redact.Redact(text)
	if c == nil || !c.enabled || c.client == nil {
		return redacted
	}

	raw, err := c.complete(ctx, redactSystemPrompt, "Text:\n"+capBytes(redacted, maxModelInputBytes))
	if err != nil {
		return redacted
	}
	var parsed struct {
		Secrets []string `json:"secrets"`
	}
	if !decodeObject(raw, &parsed) {
		return redacted
	}
	for _, secret := range parsed.Secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" || strings.HasPrefix(secret, "[REDACTED") {
			continue
		}
		redacted = strings.ReplaceAll(redacted, secret, "[REDACTED:semantic]")
	}
	return redacted
}

// ClassifyCommand may upgrade a weak default regex policy verdict with a local
// semantic risk judgment. It never downgrades a regex verdict.
func (c *Classifier) ClassifyCommand(ctx context.Context, command string, base policy.Verdict) policy.Verdict {
	if c == nil || !c.enabled || c.client == nil || base.Decision == policy.Block || base.Rule != "default" {
		return base
	}

	raw, err := c.complete(ctx, policySystemPrompt, "Command:\n"+capBytes(command, maxModelInputBytes))
	if err != nil {
		return base
	}
	var parsed struct {
		Risk        string `json:"risk"`
		Destructive bool   `json:"destructive"`
		Reason      string `json:"reason"`
	}
	if !decodeObject(raw, &parsed) {
		return base
	}

	risk := normalizeRisk(parsed.Risk)
	switch {
	case parsed.Destructive || risk == policy.RiskHigh || risk == policy.RiskCritical:
		return upgraded(base, policy.Block, maxRisk(base.Risk, risk, policy.RiskHigh), parsed.Reason)
	case risk == policy.RiskMedium && lessStrict(base.Decision, policy.Confirm):
		return upgraded(base, policy.Confirm, maxRisk(base.Risk, risk), parsed.Reason)
	default:
		return base
	}
}

func (c *Classifier) complete(ctx context.Context, system, user string) (string, error) {
	timeout := boundedTimeout(c.timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.client.Complete(ctx, system, user)
}

func boundedTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 || timeout > defaultTimeout {
		return defaultTimeout
	}
	return timeout
}

func decodeObject(raw string, dst any) bool {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return false
	}
	return json.Unmarshal([]byte(raw[start:end+1]), dst) == nil
}

func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func normalizeRisk(r string) policy.Risk {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case string(policy.RiskLow):
		return policy.RiskLow
	case string(policy.RiskMedium):
		return policy.RiskMedium
	case string(policy.RiskHigh):
		return policy.RiskHigh
	case string(policy.RiskCritical):
		return policy.RiskCritical
	default:
		return ""
	}
}

func upgraded(base policy.Verdict, decision policy.Decision, risk policy.Risk, reason string) policy.Verdict {
	base.Decision = decision
	base.Rule = "semantic"
	base.Risk = risk
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "local semantic classifier flagged elevated command risk"
	}
	base.Reason = reason
	return base
}

func lessStrict(a, b policy.Decision) bool {
	return decisionRank(a) < decisionRank(b)
}

func decisionRank(d policy.Decision) int {
	switch d {
	case policy.Block:
		return 3
	case policy.Confirm:
		return 2
	case policy.Allow:
		return 1
	default:
		return 0
	}
}

func maxRisk(risks ...policy.Risk) policy.Risk {
	best := policy.RiskLow
	bestRank := 0
	for _, r := range risks {
		if rank := riskRank(r); rank > bestRank {
			best = r
			bestRank = rank
		}
	}
	return best
}

func riskRank(r policy.Risk) int {
	switch r {
	case policy.RiskCritical:
		return 4
	case policy.RiskHigh:
		return 3
	case policy.RiskMedium:
		return 2
	case policy.RiskLow:
		return 1
	default:
		return 0
	}
}

const redactSystemPrompt = `You are Sentinel's local-only semantic redaction layer.
Find residual secrets or PII in text that has already passed regex redaction.
Sensitive data includes API keys, tokens, passwords, connection strings, emails, private hostnames, internal IPs, and anything that should not leave this machine.
Do not report existing [REDACTED...] markers.
Output ONLY one JSON object: {"secrets":["<exact substring to mask>"]}`

const policySystemPrompt = `You are Sentinel's local-only semantic command safety layer.
Classify whether a shell or kubectl command is destructive or risky.
Output ONLY one JSON object: {"risk":"low|medium|high|critical","destructive":true|false,"reason":"<short reason>"}
Use destructive=true for commands that delete, overwrite, disable, expose, exfiltrate, or irreversibly mutate data or infrastructure.`
