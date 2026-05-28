// Package policy is the security execution fence (the Policy Guard). It screens
// every proposed command before it can run, classifying it as allow, confirm,
// or block based on an ordered set of interception rules.
package policy

// Decision is the guard's verdict for a command.
type Decision string

const (
	Allow   Decision = "allow"
	Confirm Decision = "confirm"
	Block   Decision = "block"
)

// Risk is an advisory severity label attached to a verdict.
type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)

// Verdict is the result of evaluating one command.
type Verdict struct {
	Decision Decision
	Rule     string
	Reason   string
	Risk     Risk
}

// Guard holds the active rule set.
type Guard struct {
	rules []rule
}

// New returns a Guard loaded with the built-in rule set.
func New() *Guard {
	return &Guard{rules: defaultRules()}
}

// Evaluate classifies a single command. The first matching rule wins, so rules
// are ordered specific-to-general. A command matching no rule defaults to
// Confirm: unknown actions still require a human, never silent execution.
func (g *Guard) Evaluate(command string) Verdict {
	for _, r := range g.rules {
		if r.pattern.MatchString(command) {
			return Verdict{Decision: r.decision, Rule: r.name, Reason: r.reason, Risk: r.risk}
		}
	}
	return Verdict{
		Decision: Confirm,
		Rule:     "default",
		Reason:   "unrecognized command; manual review required",
		Risk:     RiskMedium,
	}
}
