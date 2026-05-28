// Package mcp serves Sentinel over the Model Context Protocol (stdio,
// JSON-RPC 2.0). It lets a powerful cloud orchestrator (Claude Desktop, Cursor,
// Codex, ...) delegate an ops task to this on-device agent: the cloud model
// sends a high-level intent, the LOCAL model plans it, the Policy Guard screens
// it, and only the screened result returns. Private context — kube/ssh config,
// secrets — never leaves the machine.
//
// The transport is newline-delimited JSON-RPC over stdin/stdout and depends
// only on the standard library.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/xiaokhkh/sentinel-agent/internal/config"
	"github.com/xiaokhkh/sentinel-agent/internal/engine"
	"github.com/xiaokhkh/sentinel-agent/internal/permission"
	"github.com/xiaokhkh/sentinel-agent/internal/policy"
	"github.com/xiaokhkh/sentinel-agent/internal/redact"
	"github.com/xiaokhkh/sentinel-agent/internal/skills"
)

const protocolVersion = "2024-11-05"

const maxOutputBytes = 8000

// Server is a minimal MCP stdio server.
type Server struct {
	in    io.Reader
	out   io.Writer
	cfg   config.Config
	guard *policy.Guard
	mode  permission.Mode
}

// NewServer builds a server reading from in and writing to out. The autonomy
// level comes from cfg.Mode (default readonly when unset/invalid).
func NewServer(in io.Reader, out io.Writer, cfg config.Config) *Server {
	mode, ok := permission.ParseMode(cfg.Mode)
	if !ok {
		mode = permission.ReadOnly
	}
	return &Server{in: in, out: out, cfg: cfg, guard: policy.New(), mode: mode}
}

// Serve runs the read-dispatch-respond loop until stdin closes.
func (s *Server) Serve() error {
	sc := bufio.NewScanner(s.in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.write(errorResponse(nil, -32700, "parse error"))
			continue
		}
		s.dispatch(req)
	}
	return sc.Err()
}

func (s *Server) dispatch(req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.write(result(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "sentinel-agent", "version": "0.1.0"},
		}))
	case "notifications/initialized", "notifications/cancelled":
		// notifications carry no id and need no response
	case "ping":
		s.write(result(req.ID, map[string]any{}))
	case "tools/list":
		s.write(result(req.ID, map[string]any{"tools": toolDefs()}))
	case "tools/call":
		s.handleToolCall(req)
	default:
		if req.ID != nil {
			s.write(errorResponse(req.ID, -32601, "method not found: "+req.Method))
		}
	}
}

func (s *Server) handleToolCall(req rpcRequest) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.write(errorResponse(req.ID, -32602, "invalid params"))
		return
	}

	switch p.Name {
	case "run_task":
		s.write(result(req.ID, s.toolRunTask(p.Arguments)))
	case "execute_step":
		s.write(result(req.ID, s.toolExecuteStep(p.Arguments)))
	case "policy_check":
		s.write(result(req.ID, s.toolPolicyCheck(p.Arguments)))
	case "local_context":
		s.write(result(req.ID, s.toolLocalContext()))
	case "list_skills":
		s.write(result(req.ID, s.toolListSkills()))
	default:
		s.write(result(req.ID, toolError("unknown tool: "+p.Name)))
	}
}

// toolRunTask plans a task with the local model and screens every action with
// the Policy Guard. It never executes — execution stays with the human-driven
// CLI. The cloud client receives only the screened plan, not raw local context.
func (s *Server) toolRunTask(args json.RawMessage) map[string]any {
	var a struct {
		Task string `json:"task"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Task == "" {
		return toolError("missing required argument: task")
	}

	inf, err := engine.NewProvider(engine.ProviderConfig{
		Name: s.cfg.Provider, BaseURL: s.cfg.BaseURL, Model: s.cfg.Model,
		APIKey: s.cfg.APIKey, Timeout: s.cfg.Timeout,
	})
	if err != nil {
		return toolError(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout+5*time.Second)
	defer cancel()

	plan, err := inf.Plan(ctx, a.Task, engine.LoadLocalContext())
	if err != nil {
		// Intent downgrade and inference failures are reported as content, not
		// protocol errors: the cloud client must NOT retry off-device.
		return toolText(fmt.Sprintf("no plan produced (%v). Per Sentinel's privacy policy the task was not escalated off-device.", err))
	}
	if plan.NeedsInput != nil {
		return toolJSON(map[string]any{
			"status": "needs_input",
			"prompt": plan.NeedsInput.Prompt,
			"key":    plan.NeedsInput.Key,
			"note":   "answer via guard config set or include it and call run_task again",
		})
	}

	type screened struct {
		Kind        string `json:"kind"`
		Command     string `json:"command"`
		Explanation string `json:"explanation"`
		Decision    string `json:"decision"`
		Risk        string `json:"risk"`
		Rule        string `json:"rule"`
		Outcome     string `json:"outcome_under_mode"`
	}
	out := struct {
		Task     string     `json:"task"`
		Provider string     `json:"provider"`
		Mode     string     `json:"mode"`
		Actions  []screened `json:"actions"`
		Note     string     `json:"note"`
	}{
		Task: a.Task, Provider: plan.Source, Mode: string(s.mode),
		Note: "call execute_step to run an action; mutating steps may need approval per the server mode",
	}

	for _, ac := range plan.Actions {
		v := s.guard.Evaluate(ac.Command)
		out.Actions = append(out.Actions, screened{
			Kind: string(ac.Kind), Command: ac.Command, Explanation: ac.Explanation,
			Decision: string(v.Decision), Risk: string(v.Risk), Rule: v.Rule,
			Outcome: string(permission.Decide(v.Decision, s.mode)),
		})
	}
	return toolJSON(out)
}

// toolExecuteStep runs a single command under the Policy Guard and the server's
// permission mode. Only "run"-tier commands execute; their output is REDACTED
// before being returned, so secrets never reach the cloud client. "ask"-tier
// (mutating) commands are not run — they require approval via the MCP client or
// the guard CLI.
func (s *Server) toolExecuteStep(args json.RawMessage) map[string]any {
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Command == "" {
		return toolError("missing required argument: command")
	}

	v := s.guard.Evaluate(a.Command)
	res := map[string]any{
		"command": a.Command, "decision": v.Decision, "risk": v.Risk,
		"rule": v.Rule, "mode": s.mode,
	}

	switch permission.Decide(v.Decision, s.mode) {
	case permission.Refuse:
		res["status"] = "refused"
		res["reason"] = "blocked by policy: " + v.Reason
	case permission.Ask:
		res["status"] = "approval_required"
		res["reason"] = "mutating command not auto-executed; approve via your MCP client or run it through the guard CLI"
	case permission.Run:
		out, err := exec.Command("sh", "-c", a.Command).CombinedOutput()
		text := string(out)
		if len(text) > maxOutputBytes {
			text = text[:maxOutputBytes] + "\n...[truncated]"
		}
		res["status"] = "executed"
		res["output"] = redact.Redact(text) // desensitized before leaving the machine
		if err != nil {
			res["error"] = err.Error()
		}
	}
	return toolJSON(res)
}

func (s *Server) toolPolicyCheck(args json.RawMessage) map[string]any {
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Command == "" {
		return toolError("missing required argument: command")
	}
	v := s.guard.Evaluate(a.Command)
	return toolJSON(map[string]any{
		"command": a.Command, "decision": v.Decision, "risk": v.Risk,
		"rule": v.Rule, "reason": v.Reason,
	})
}

func (s *Server) toolLocalContext() map[string]any {
	rag := engine.LoadLocalContext()
	return toolJSON(map[string]any{
		"hostname":       rag.Hostname,
		"has_kubeconfig": rag.HasKubeConfig,
		"kube_context":   rag.KubeContext,
		"namespace":      rag.Namespace,
		"has_ssh_config": rag.HasSSHConfig,
		"memory":         rag.Facts,
		"note":           "non-secret summary only; file contents and credentials are never exposed",
	})
}

func (s *Server) toolListSkills() map[string]any {
	type sk struct {
		Name        string `json:"name"`
		Status      string `json:"status"`
		Description string `json:"description"`
	}
	var list []sk
	for _, x := range skills.All() {
		list = append(list, sk{x.Name, x.Status, x.Description})
	}
	return toolJSON(map[string]any{"skills": list})
}

func (s *Server) write(v any) {
	b, _ := json.Marshal(v)
	s.out.Write(b)
	s.out.Write([]byte("\n"))
}
