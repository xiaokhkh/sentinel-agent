// Package executor applies a Plan under the Policy Guard with a human in the
// loop. It defaults to plan-only (dry) mode: actions are printed but never run
// unless Execute is set. Blocked actions are never run regardless of mode.
package executor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/xiaokhkh/sentinel/internal/engine"
	"github.com/xiaokhkh/sentinel/internal/policy"
)

// Executor runs the actions of a plan.
type Executor struct {
	Execute bool // when false (default) actions are printed but not executed
	AutoYes bool // skip confirmation prompts; still refuses blocked actions
	In      io.Reader
	Out     io.Writer
}

// New returns an Executor wired to stdin/stdout.
func New(execute, autoYes bool) *Executor {
	return &Executor{Execute: execute, AutoYes: autoYes, In: os.Stdin, Out: os.Stdout}
}

// Result records what happened to one action.
type Result struct {
	Action  engine.Action
	Verdict policy.Verdict
	Ran     bool
	Skipped bool
	Err     error
}

// RunPlan evaluates and (optionally) runs every action in order.
func (e *Executor) RunPlan(plan *engine.Plan, guard *policy.Guard) []Result {
	results := make([]Result, 0, len(plan.Actions))
	for i, a := range plan.Actions {
		v := guard.Evaluate(a.Command)
		r := Result{Action: a, Verdict: v}
		e.printAction(i+1, a, v)

		switch {
		case v.Decision == policy.Block:
			fmt.Fprintf(e.Out, "    -> BLOCKED by policy (%s); will not execute\n", v.Rule)
			r.Skipped = true
		case !e.Execute:
			fmt.Fprintln(e.Out, "    -> plan mode: not executing (pass --execute to run)")
			r.Skipped = true
		case v.Decision == policy.Confirm && !e.confirm():
			fmt.Fprintln(e.Out, "    -> skipped by user")
			r.Skipped = true
		default:
			r.Err = e.run(a)
			r.Ran = r.Err == nil
			if r.Err != nil {
				fmt.Fprintf(e.Out, "    -> error: %v\n", r.Err)
			}
		}
		results = append(results, r)
	}
	return results
}

func (e *Executor) printAction(idx int, a engine.Action, v policy.Verdict) {
	fmt.Fprintf(e.Out, "[%d] %s | %s (risk=%s, rule=%s)\n",
		idx, a.Kind, strings.ToUpper(string(v.Decision)), v.Risk, v.Rule)
	fmt.Fprintf(e.Out, "    cmd: %s\n", a.Command)
	if a.Explanation != "" {
		fmt.Fprintf(e.Out, "    why: %s\n", a.Explanation)
	}
}

func (e *Executor) confirm() bool {
	if e.AutoYes {
		return true
	}
	fmt.Fprint(e.Out, "    Proceed? [y/N]: ")
	line, _ := bufio.NewReader(e.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func (e *Executor) run(a engine.Action) error {
	fmt.Fprintf(e.Out, "    -> executing: %s\n", a.Command)
	cmd := exec.Command("sh", "-c", a.Command)
	cmd.Stdout = e.Out
	cmd.Stderr = e.Out
	return cmd.Run()
}
