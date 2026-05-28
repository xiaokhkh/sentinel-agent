package mcp

import (
	"strings"
	"testing"

	"github.com/xiaokhkh/sentinel-agent/internal/config"
)

func TestServerInitializeAndToolCall(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"policy_check","arguments":{"command":"kubectl delete pods --all"}}}`,
	}, "\n") + "\n"

	var out strings.Builder
	if err := NewServer(strings.NewReader(input), &out, config.Default()).Serve(); err != nil {
		t.Fatalf("serve: %v", err)
	}

	got := out.String()
	// The tool result is a JSON-escaped string inside a content block, so inner
	// quotes appear escaped in the raw transport bytes — match bare tokens.
	for _, want := range []string{
		`"serverInfo"`,   // initialize responded
		`"run_task"`,     // tools/list advertised tools
		`block`,          // policy_check blocked the bulk delete
		`k8s-delete-all`, // ...via this rule
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q\n--- output ---\n%s", want, got)
		}
	}

	// The initialized notification carries no id and must not get a response.
	if strings.Count(got, `"jsonrpc":"2.0"`) != 3 {
		t.Errorf("expected exactly 3 responses (notification gets none), output:\n%s", got)
	}
}
