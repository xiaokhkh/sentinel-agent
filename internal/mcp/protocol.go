package mcp

import "encoding/json"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func result(id json.RawMessage, res any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: res}
}

func errorResponse(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// toolText wraps a plain string in an MCP tool-result content block.
func toolText(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

// toolJSON marshals v and returns it as a text content block. MCP tool results
// carry text; structured data travels as a JSON string the client can parse.
func toolJSON(v any) map[string]any {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolError("failed to encode result: " + err.Error())
	}
	return toolText(string(b))
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": msg}},
	}
}

// toolDefs lists the tools advertised via tools/list.
func toolDefs() []map[string]any {
	strProp := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return []map[string]any{
		{
			"name":        "run_task",
			"description": "Plan an ops task with the on-device model and screen every action through the Policy Guard. Returns the screened plan (commands + allow/confirm/block verdicts). Does not execute — run via the guard CLI with a human in the loop. Local context never leaves the machine.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"task": strProp("natural-language ops task, e.g. 'diagnose not-ready pods in default'")},
				"required":   []string{"task"},
			},
		},
		{
			"name":        "policy_check",
			"description": "Classify a single shell/kubectl command as allow, confirm, or block using the Policy Guard.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": strProp("the command to evaluate")},
				"required":   []string{"command"},
			},
		},
		{
			"name":        "local_context",
			"description": "Return a non-secret summary of local ops context (hostname, current kube context, config presence). File contents and credentials are never exposed.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "list_skills",
			"description": "List Sentinel's capability packs and their status.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}
