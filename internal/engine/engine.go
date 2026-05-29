// Package engine is the on-device inference core (the LFM Engine). It converts
// a natural-language task plus local context into a concrete, executable Plan.
// Inference backends are pluggable via the Inferencer interface; all bundled
// model backends speak the OpenAI-compatible chat protocol so they can be
// swapped through configuration rather than code.
package engine

import (
	"context"
	"errors"
)

// ActionKind tags how an action should be interpreted by the executor.
type ActionKind string

const (
	ActionShell   ActionKind = "shell"
	ActionKubectl ActionKind = "kubectl"
)

// Action is a single concrete command the engine proposes.
type Action struct {
	Kind        ActionKind `json:"kind"`
	Command     string     `json:"command"`
	Explanation string     `json:"explanation"`
}

// Plan is the ordered set of actions produced for a task.
type Plan struct {
	Task       string         `json:"task"`
	Actions    []Action       `json:"actions"`
	Source     string         `json:"source"`
	NeedsInput *Clarification `json:"-"`
}

// Step is one bounded investigation decision for an on-device solve loop.
// It either proposes one read-only command, declares the task sufficiently
// investigated, or asks for one missing non-secret reference.
type Step struct {
	Command    string         `json:"command,omitempty"`
	Done       bool           `json:"done,omitempty"`
	Conclusion string         `json:"conclusion,omitempty"`
	NeedsInput *Clarification `json:"needs_input,omitempty"`
}

// Clarification is a model-requested question whose answer may be stored under
// a structured-memory dotted key.
type Clarification struct {
	Prompt string `json:"prompt"`
	Key    string `json:"key"`
}

// ErrIntentDowngrade signals that the local model could not produce a usable
// plan. Sentinel's privacy contract forbids escalating the task and its local
// context to any cloud model, so callers must surface this to the user instead
// of falling back to a remote service.
var ErrIntentDowngrade = errors.New("local model could not handle the intent; refusing to escalate off-device")

// Inferencer is implemented by every model backend.
type Inferencer interface {
	Name() string
	Plan(ctx context.Context, task string, rag *LocalContext) (*Plan, error)
	PlanNextStep(ctx context.Context, task string, rag *LocalContext, observations []string) (*Step, error)
}
