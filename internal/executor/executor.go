// Package executor applies a Plan under the Policy Guard and the configured
// permission mode. "Ask"-tier actions prompt the user (y/N) at the terminal;
// "refuse" never runs; "run" executes. Plan mode shows everything and runs
// nothing.
package executor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/xiaokhkh/sentinel-agent/internal/engine"
	"github.com/xiaokhkh/sentinel-agent/internal/permission"
	"github.com/xiaokhkh/sentinel-agent/internal/policy"
)

// Executor runs the actions of a plan at a given autonomy level.
type Executor struct {
	Mode permission.Mode
	In   io.Reader
	Out  io.Writer
}

// New returns an Executor wired to stdin/stdout.
func New(mode permission.Mode) *Executor {
	return &Executor{Mode: mode, In: os.Stdin, Out: os.Stdout}
}

// Result records what happened to one action.
type Result struct {
	Action  engine.Action
	Verdict policy.Verdict
	Ran     bool
	Skipped bool
	Err     error
}

// Evaluator classifies a command before execution.
type Evaluator func(command string) policy.Verdict

// RunPlan evaluates and (per mode) runs every action in order.
func (e *Executor) RunPlan(plan *engine.Plan, guard *policy.Guard) []Result {
	return e.RunPlanWithEvaluator(plan, guard.Evaluate)
}

// RunPlanWithEvaluator evaluates and (per mode) runs every action in order.
func (e *Executor) RunPlanWithEvaluator(plan *engine.Plan, evaluate Evaluator) []Result {
	results := make([]Result, 0, len(plan.Actions))
	for i, a := range plan.Actions {
		v := evaluate(a.Command)
		r := Result{Action: a, Verdict: v}
		e.printAction(i+1, a, v)

		switch permission.Decide(v.Decision, e.Mode) {
		case permission.Refuse:
			fmt.Fprintf(e.Out, "    -> BLOCKED by policy (%s); not executed\n", v.Rule)
			r.Skipped = true
		case permission.Ask:
			if e.confirm() {
				r.Err = e.run(a)
				r.Ran = r.Err == nil
			} else {
				fmt.Fprintln(e.Out, "    -> skipped by user")
				r.Skipped = true
			}
		case permission.Run:
			if v.Decision == policy.Block {
				fmt.Fprintln(e.Out, "    -> WARNING: executing a BLOCKED command (full mode)")
			}
			r.Err = e.run(a)
			r.Ran = r.Err == nil
		}
		if r.Err != nil {
			fmt.Fprintf(e.Out, "    -> error: %v\n", r.Err)
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
