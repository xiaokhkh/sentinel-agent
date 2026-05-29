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

func (p *httpProvider) PlanNextStep(ctx context.Context, task string, rag *LocalContext, observations []string) (*Step, error) {
	var responseFormat any
	if p.useSchema {
		responseFormat = stepResponseFormat()
	}
	raw, err := openAIChatWithResponseFormat(ctx, p.baseURL, p.apiKey, p.model, stepSystemPrompt, buildStepUserPrompt(task, rag, observations), p.timeout, responseFormat)
	if err != nil {
		return nil, err
	}
	return parseStep(raw)
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

const stepSystemPrompt = `You are Sentinel, an on-device read-only investigation agent.
Given one task, local context, and redacted observations gathered so far, decide the SINGLE next step.

Output ONLY one JSON object. No prose, no markdown code fences.

Allowed JSON shapes:
- {"command":"<one read-only diagnostic command>","done":false}
- {"done":true,"conclusion":"<one-line evidence summary>"}
- {"needs_input":{"prompt":"<one clear question for a missing non-secret reference>","key":"<dotted key or empty>"}}

Rules:
- Only propose read-only or diagnostic commands: kubectl get/describe/logs/top/explain/api-resources/cluster-info/version/config view, or shell inspection such as ls/cat/tail/head/grep/rg/ps/top/df/du/echo/pwd/whoami/date/uname.
- Never propose destructive or mutating commands: rm, delete, apply, patch, edit, scale, set, restart, drain, stop, reboot, drop, truncate, chmod, writes, redirects, pipes, or chained commands.
- Propose exactly one command. Do not use &&, ;, pipes, command substitution, or redirection.
- Do not repeat any command already present in observations.
- You MUST gather evidence before concluding: if "Redacted observations so far" is "none", you may NOT set done=true and you may NOT set needs_input — you MUST return a concrete read-only command (for a Kubernetes failure, start with "kubectl get pods -n <namespace>").
- Only set done=true once observations actually contain command output that answers the task; the conclusion must reference what the observations showed.
- Dig until you find the ROOT CAUSE, not just the symptom. If observations show a pod in CrashLoopBackOff/Error/Pending, the next step must fetch its logs (e.g. "kubectl logs -n <ns> -l <selector> --tail=20") or "kubectl describe pod" to learn WHY — do not conclude from pod status alone.
- Set needs_input only when a non-secret reference such as a namespace, kubeconfig path, or resource name is missing. Never ask for passwords, tokens, keys, or credential contents.`

func buildUserPrompt(task string, rag *LocalContext) string {
	ctxLine := "none"
	if rag != nil {
		if s := rag.Summary(); s != "" {
			ctxLine = s
		}
	}
	return fmt.Sprintf("Local context: %s\n\nTask: %s", ctxLine, task)
}

func buildStepUserPrompt(task string, rag *LocalContext, observations []string) string {
	ctxLine := "none"
	if rag != nil {
		if s := rag.Summary(); s != "" {
			ctxLine = s
		}
	}
	obsLine := "none"
	if len(observations) > 0 {
		obsLine = strings.Join(observations, "\n---\n")
	}
	return fmt.Sprintf("Local context: %s\n\nTask: %s\n\nRedacted observations so far:\n%s", ctxLine, task, obsLine)
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
	var responseFormat any
	if useSchema {
		responseFormat = planResponseFormat()
	}
	return newChatRequestWithResponseFormat(model, system, user, responseFormat)
}

func newChatRequestWithResponseFormat(model, system, user string, responseFormat any) chatRequest {
	req := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	}
	if responseFormat != nil {
		req.ResponseFormat = responseFormat
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

func stepResponseFormat() any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "sentinel_next_step",
			"strict": false,
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type": "string",
					},
					"done": map[string]any{
						"type": "boolean",
					},
					"conclusion": map[string]any{
						"type": "string",
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
	var responseFormat any
	if useSchema {
		responseFormat = planResponseFormat()
	}
	return openAIChatWithResponseFormat(ctx, baseURL, apiKey, model, system, user, timeout, responseFormat)
}

func openAIChatWithResponseFormat(ctx context.Context, baseURL, apiKey, model, system, user string, timeout time.Duration, responseFormat any) (string, error) {
	body, err := json.Marshal(newChatRequestWithResponseFormat(model, system, user, responseFormat))
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

func extractJSONObject(raw string) (string, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("%w: model did not return a JSON object", ErrIntentDowngrade)
	}
	return raw[start : end+1], nil
}

// parsePlan extracts the JSON object the model was asked to emit. A response
// that contains no usable actions is treated as an intent downgrade rather than
// a hard error, so the caller can prompt the user instead of guessing.
func parsePlan(raw, source, task string) (*Plan, error) {
	obj, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: model did not return a JSON plan", err)
	}

	var parsed struct {
		Actions    []Action       `json:"actions"`
		NeedsInput *Clarification `json:"needs_input"`
	}
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
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

func parseStep(raw string) (*Step, error) {
	obj, err := extractJSONObject(raw)
	if err != nil {
		return nil, err
	}

	var parsed Step
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIntentDowngrade, err)
	}
	parsed.Command = strings.TrimSpace(parsed.Command)
	parsed.Conclusion = strings.TrimSpace(parsed.Conclusion)
	if parsed.NeedsInput != nil {
		parsed.NeedsInput.Prompt = strings.TrimSpace(parsed.NeedsInput.Prompt)
		parsed.NeedsInput.Key = strings.TrimSpace(parsed.NeedsInput.Key)
		if parsed.NeedsInput.Prompt == "" {
			return nil, fmt.Errorf("%w: model returned needs_input without a prompt", ErrIntentDowngrade)
		}
		return &parsed, nil
	}
	if parsed.Done {
		if parsed.Conclusion == "" {
			return nil, fmt.Errorf("%w: model returned done without a conclusion", ErrIntentDowngrade)
		}
		return &parsed, nil
	}
	if parsed.Command == "" {
		return nil, fmt.Errorf("%w: model returned no command, conclusion, or needs_input", ErrIntentDowngrade)
	}
	return &parsed, nil
}
