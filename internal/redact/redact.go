// Package redact desensitizes command output before it may leave the machine
// (e.g. when an executed step's result is returned to a cloud orchestrator over
// MCP). It is deliberately conservative — it over-redacts rather than risk
// leaking a secret. This package is the linchpin of Sentinel's privacy model in
// the cloud-planner / on-device-executor flow: the strength of the guarantee
// equals the quality of this redaction.
package redact

import "regexp"

type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	// PEM private key blocks (multi-line).
	{regexp.MustCompile(`(?s)-----BEGIN[^-]*PRIVATE KEY-----.*?-----END[^-]*PRIVATE KEY-----`), "[REDACTED:private-key]"},
	// JWTs.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\b`), "[REDACTED:jwt]"},
	// AWS access key IDs.
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), "[REDACTED:aws-key]"},
	// kubeconfig / YAML sensitive fields (keep the key, drop the value).
	{regexp.MustCompile(`(?i)(client-certificate-data|client-key-data|certificate-authority-data|token|password)(\s*:\s*)\S+`), "$1$2[REDACTED]"},
	// credentials embedded in connection URLs: scheme://user:pass@host
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:@/\s]+:)[^@/\s]+@`), "${1}[REDACTED]@"},
	// Bearer tokens.
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`), "Bearer [REDACTED]"},
	// generic secret-ish assignments: FOO_TOKEN=..., apiKey: ...
	{regexp.MustCompile(`(?i)\b(\w*(?:secret|token|password|passwd|pwd|api[_-]?key|access[_-]?key)\w*)\s*[:=]\s*\S+`), "$1=[REDACTED]"},
	// email addresses.
	{regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`), "[REDACTED:email]"},
	// long base64 blobs (certs, encoded secrets). Last, to avoid clobbering the above.
	{regexp.MustCompile(`\b[A-Za-z0-9+/]{60,}={0,2}\b`), "[REDACTED:base64]"},
}

// Redact returns s with sensitive substrings replaced by placeholders.
func Redact(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
