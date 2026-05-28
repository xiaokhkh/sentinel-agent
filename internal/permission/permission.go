// Package permission grades how far Sentinel may act on its own, in the spirit
// of Claude Code's permission modes and Codex's sandbox/approval policies. The
// execution outcome for a command is a function of two inputs: the Policy
// Guard's risk verdict and the configured Mode.
package permission

import (
	"strings"

	"github.com/xiaokhkh/sentinel-agent/internal/policy"
)

// Mode is the configured autonomy level.
type Mode string

const (
	Plan     Mode = "plan"     // never execute; show/return the plan only
	ReadOnly Mode = "readonly" // auto-run read-only; ask on mutations; refuse blocked
	Auto     Mode = "auto"     // auto-run read-only + mutating; refuse blocked
	Full     Mode = "full"     // run everything, including blocked (dangerous)
)

// Outcome is what should happen to a single command.
type Outcome string

const (
	Run    Outcome = "run"    // execute now
	Ask    Outcome = "ask"    // requires explicit human/client approval first
	Refuse Outcome = "refuse" // do not execute
)

// ParseMode resolves a mode string. The empty string maps to ReadOnly, the safe
// default for autonomous (MCP) operation.
func ParseMode(s string) (Mode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "plan":
		return Plan, true
	case "", "readonly", "read-only":
		return ReadOnly, true
	case "auto", "auto-edit":
		return Auto, true
	case "full", "full-access", "danger":
		return Full, true
	default:
		return "", false
	}
}

// Decide maps a guard verdict and mode to an execution outcome.
func Decide(d policy.Decision, m Mode) Outcome {
	switch m {
	case Plan:
		return Refuse
	case Full:
		return Run
	case Auto:
		if d == policy.Block {
			return Refuse
		}
		return Run
	default: // ReadOnly
		switch d {
		case policy.Allow:
			return Run
		case policy.Confirm:
			return Ask
		default:
			return Refuse
		}
	}
}
