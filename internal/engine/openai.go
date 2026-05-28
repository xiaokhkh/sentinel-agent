package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpProvider is the shared implementation for every backend that exposes an
// OpenAI-compatible /chat/completions endpoint (Ollama, llama.cpp's
// llama-server, mlx_lm.server, ...). Concrete providers differ only by their
// default base URL and model tag.
type httpProvider struct {
	name      string
	baseURL   string
	apiKey    string
	model     string
	timeout   time.Duration
	useSchema bool
}

func (p *httpProvider) Name() string { return p.name }

func (p *httpProvider) Plan(ctx context.Context, task string, rag *LocalContext) (*Plan, error) {
	raw, err := openAIChat(ctx, p.baseURL, p.apiKey, p.model, planSystemPrompt, buildUserPrompt(task, rag), p.timeout, p.useSchema)
	if err != nil {
		return nil, err
	}
	return parsePlan(raw, p.name, task)
}

const planSystemPrompt = `You are Sentinel, an on-device DevOps copilot that runs fully offline.
Convert the user's natural-language task into a minimal sequence of concrete shell or kubectl commands.

Rules:
- Output ONLY a single JSON object. No prose, no markdown code fences.
- Schema: {"actions":[{"kind":"kubectl"|"shell","command":"<one command>","explanation":"<short why>"}]}
- The user message includes a "Local context" line (the agent's memory). READ IT before deciding:
  * If it says "kubeconfig present", you HAVE cluster access. Produce the kubectl command directly.
    Do NOT ask for a kubeconfig, a namespace, or which pods — use the namespace shown (or "default").
  * Only if it says "no kubeconfig" (or the task truly needs a connection target that is absent) return:
    {"needs_input":{"prompt":"<one clear question>","key":"<dotted key, e.g. kubernetes.kubeconfig, or empty>"}}
- Every action MUST have a non-empty command. Never output an action with an empty command.
- Use plain read-only commands. Do NOT pipe through grep/awk/sort or add flags the user did not ask for;
  prefer "kubectl get pods -n default" over "kubectl get pods | grep ...".
- Never use destructive flags (--all, --force, drop, truncate, delete) unless the user explicitly asked.
- Exactly one command per action. Do not chain commands with && or ;.
- If the task cannot be mapped to safe local commands, return {"actions":[]}.

Examples:
- Local context: kubeconfig present (current-context=minikube, namespace=default)
  User: show me all pods in the default namespace
  Output: {"actions":[{"kind":"kubectl","command":"kubectl get pods -n default","explanation":"list pods"}]}
- Local context: kubeconfig present (current-context=minikube, namespace=default)
  User: which pods are not running
  Output: {"actions":[{"kind":"kubectl","command":"kubectl get pods -n default","explanation":"list pods and their status so the user can see which are not Running"}]}
- Local context: no kubeconfig (namespace=default)
  User: check my k8s pods
  Output: {"needs_input":{"prompt":"I don't have a kubeconfig. What's the path to your kubeconfig?","key":"kubernetes.kubeconfig"}}`

func buildUserPrompt(task string, rag *LocalContext) string {
	ctxLine := "none"
	if rag != nil {
		if s := rag.Summary(); s != "" {
			ctxLine = s
		}
	}
	return fmt.Sprintf("Local context: %s\n\nTask: %s", ctxLine, task)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Stream         bool          `json:"stream"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func newChatRequest(model, system, user string, useSchema bool) chatRequest {
	req := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	}
	if useSchema {
		req.ResponseFormat = planResponseFormat()
	}
	return req
}

func planResponseFormat() any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "sentinel_plan",
			"strict": false,
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"actions": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"kind": map[string]any{
									"type": "string",
									"enum": []string{"kubectl", "shell"},
								},
								"command": map[string]any{
									"type":      "string",
									"minLength": 1,
								},
								"explanation": map[string]any{
									"type": "string",
								},
							},
							"required": []string{"command", "explanation"},
						},
					},
					"needs_input": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"prompt": map[string]any{"type": "string"},
							"key":    map[string]any{"type": "string"},
						},
						"required": []string{"prompt"},
					},
				},
			},
		},
	}
}

func openAIChat(ctx context.Context, baseURL, apiKey, model, system, user string, timeout time.Duration, useSchema bool) (string, error) {
	body, err := json.Marshal(newChatRequest(model, system, user, useSchema))
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("calling %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("inference endpoint %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("decoding response from %s: %w", url, err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("inference endpoint %s returned no choices", url)
	}
	return cr.Choices[0].Message.Content, nil
}

// parsePlan extracts the JSON object the model was asked to emit. A response
// that contains no usable actions is treated as an intent downgrade rather than
// a hard error, so the caller can prompt the user instead of guessing.
func parsePlan(raw, source, task string) (*Plan, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("%w: model did not return a JSON plan", ErrIntentDowngrade)
	}

	var parsed struct {
		Actions    []Action       `json:"actions"`
		NeedsInput *Clarification `json:"needs_input"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIntentDowngrade, err)
	}
	if parsed.NeedsInput != nil && strings.TrimSpace(parsed.NeedsInput.Prompt) != "" {
		parsed.NeedsInput.Prompt = strings.TrimSpace(parsed.NeedsInput.Prompt)
		parsed.NeedsInput.Key = strings.TrimSpace(parsed.NeedsInput.Key)
		return &Plan{Task: task, Source: source, NeedsInput: parsed.NeedsInput}, nil
	}
	if len(parsed.Actions) == 0 {
		return nil, ErrIntentDowngrade
	}
	for i := range parsed.Actions {
		parsed.Actions[i].Kind = ActionKind(strings.TrimSpace(string(parsed.Actions[i].Kind)))
		parsed.Actions[i].Command = strings.TrimSpace(parsed.Actions[i].Command)
		parsed.Actions[i].Explanation = strings.TrimSpace(parsed.Actions[i].Explanation)
		if parsed.Actions[i].Command == "" {
			return nil, fmt.Errorf("%w: model returned an action with an empty command", ErrIntentDowngrade)
		}
		if parsed.Actions[i].Kind == "" {
			parsed.Actions[i].Kind = ActionShell
		}
	}
	return &Plan{Task: task, Actions: parsed.Actions, Source: source}, nil
}
