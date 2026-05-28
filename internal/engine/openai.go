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
	name    string
	baseURL string
	apiKey  string
	model   string
	timeout time.Duration
}

func (p *httpProvider) Name() string { return p.name }

func (p *httpProvider) Plan(ctx context.Context, task string, rag *LocalContext) (*Plan, error) {
	raw, err := openAIChat(ctx, p.baseURL, p.apiKey, p.model, planSystemPrompt, buildUserPrompt(task, rag), p.timeout)
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
- You are given the agent's local context/memory. If the task requires access details that are MISSING from it and you cannot proceed safely (for example you need a kubeconfig path / context / namespace, or a connection target, but none is provided), DO NOT guess or fabricate them.
- In that case return ONLY: {"needs_input":{"prompt":"<one clear question to ask the user>","key":"<dotted config key to save the answer, e.g. kubernetes.kubeconfig, or empty string>"}}
- Otherwise return the actions object as before.
- Prefer read-only/diagnostic commands. Never add destructive flags (--all, --force, drop, truncate, delete) unless the user explicitly requested them.
- Exactly one command per action. Do not chain commands with && or ;.
- If the task cannot be mapped to safe local commands, return {"actions":[]}.`

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
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func openAIChat(ctx context.Context, baseURL, apiKey, model, system, user string, timeout time.Duration) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	})
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
		if parsed.Actions[i].Kind == "" {
			parsed.Actions[i].Kind = ActionShell
		}
	}
	return &Plan{Task: task, Actions: parsed.Actions, Source: source}, nil
}
