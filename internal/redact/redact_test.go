package redact

import (
	"strings"
	"testing"
)

func TestRedactRemovesSecrets(t *testing.T) {
	in := strings.Join([]string{
		"normal log line about pod nginx-7d in default",
		"DB_PASSWORD=sup3rs3cr3tvalue",
		"apiKey: abcd1234efgh5678",
		"contact admin@example.com for access",
		"Authorization: Bearer eyJhbGciOi.eyJzdWIIOi.SflKxwRJ12345",
		"postgres://app:hunter2@db.internal:5432/main",
		"token: AKIAIOSFODNN7EXAMPLE",
	}, "\n")

	out := Redact(in)

	mustGone := []string{
		"sup3rs3cr3tvalue", "abcd1234efgh5678", "admin@example.com",
		"hunter2", "AKIAIOSFODNN7EXAMPLE",
	}
	for _, s := range mustGone {
		if strings.Contains(out, s) {
			t.Errorf("secret %q was not redacted:\n%s", s, out)
		}
	}

	// benign content survives
	if !strings.Contains(out, "pod nginx-7d") {
		t.Errorf("benign text should be preserved:\n%s", out)
	}
}

func TestRedactPrivateKeyBlock(t *testing.T) {
	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"
	if got := Redact(in); strings.Contains(got, "MIIEowIBAAKCAQEA") {
		t.Errorf("private key body should be redacted: %s", got)
	}
}
