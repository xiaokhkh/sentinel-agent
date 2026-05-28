package policy

import "regexp"

type rule struct {
	name     string
	pattern  *regexp.Regexp
	decision Decision
	risk     Risk
	reason   string
}

// defaultRules is the built-in interception set. It is intentionally
// illustrative rather than exhaustive — a roadmap item is to make this loadable
// from policy files and to add a semantic (model-scored) second pass. Order
// matters: the most dangerous, most specific patterns come first.
func defaultRules() []rule {
	mk := func(name string, d Decision, r Risk, reason, expr string) rule {
		return rule{
			name:     name,
			pattern:  regexp.MustCompile("(?i)" + expr),
			decision: d,
			risk:     r,
			reason:   reason,
		}
	}

	return []rule{
		// --- critical / high: block outright ---
		mk("rm-rf-root", Block, RiskCritical, "recursive force-delete targeting a root/home path",
			`\brm\b.*\s-[a-z]*[rf][a-z]*\b.*\s(/|~|\$home)(\s|/|$)`),
		mk("fork-bomb", Block, RiskCritical, "fork bomb",
			`:\s*\(\s*\)\s*\{.*\}\s*;?\s*:`),
		mk("disk-wipe", Block, RiskCritical, "raw disk write or filesystem format",
			`\b(mkfs(\.\w+)?|dd\s+if=)\b|>\s*/dev/(sd|disk|nvme)`),
		mk("k8s-delete-all", Block, RiskCritical, "bulk delete of Kubernetes resources",
			`kubectl\s+delete\b.*(--all\b|--all-namespaces\b)`),
		mk("k8s-delete-namespace", Block, RiskCritical, "deleting a namespace cascades to every resource in it",
			`kubectl\s+delete\s+(ns|namespace)\b`),
		mk("sql-drop", Block, RiskCritical, "dropping a database, table, or schema",
			`\bdrop\s+(database|table|schema)\b`),
		mk("sql-truncate", Block, RiskHigh, "truncating a table discards all rows",
			`\btruncate\s+(table\s+)?\w+`),
		mk("sql-delete-no-where", Block, RiskHigh, "DELETE with no WHERE clause affects every row",
			`\bdelete\s+from\s+[\w."]+\s*;?\s*$`),
		mk("creds-read", Block, RiskHigh, "reading private keys or credential stores",
			`\b(cat|less|more|cp|scp|curl|cur)\b.*\b(id_rsa|id_ed25519|\.ssh/|\.aws/credentials|\.kube/config)\b`),

		// --- medium / high: allow only after explicit confirmation ---
		mk("rm-recursive", Confirm, RiskHigh, "recursive delete",
			`\brm\b.*\s-[a-z]*[rf]`),
		mk("k8s-delete", Confirm, RiskHigh, "deleting a Kubernetes resource",
			`kubectl\s+delete\b`),
		mk("k8s-drain", Confirm, RiskHigh, "cordon/drain/taint evicts workloads from a node",
			`kubectl\s+(drain|cordon|uncordon|taint)\b`),
		mk("k8s-mutate", Confirm, RiskMedium, "mutating cluster state",
			`kubectl\s+(apply|replace|patch|edit|scale|set)\b`),
		mk("k8s-rollout-restart", Confirm, RiskMedium, "restarting a workload causes pod churn",
			`kubectl\s+rollout\s+restart\b`),
		mk("svc-control", Confirm, RiskHigh, "stopping/restarting/disabling a system service",
			`\bsystemctl\s+(stop|restart|disable|mask)\b`),
		mk("power", Confirm, RiskHigh, "host power-state change",
			`\b(shutdown|reboot|halt|poweroff)\b`),
		mk("chmod-world", Confirm, RiskMedium, "world-writable permissions",
			`\bchmod\s+(-r\s+)?[0-7]*777\b`),
		mk("git-force-push", Confirm, RiskMedium, "force-push can overwrite remote history",
			`\bgit\s+push\b.*(--force\b|-f\b|--force-with-lease\b)`),

		// --- low: read-only, safe to allow automatically ---
		mk("k8s-readonly", Allow, RiskLow, "read-only Kubernetes inspection",
			`kubectl\s+(get|describe|logs|top|explain|api-resources|cluster-info|version|config\s+view)\b`),
		mk("shell-readonly", Allow, RiskLow, "read-only shell inspection",
			`^\s*(ls|cat|less|tail|head|grep|rg|ps|top|df|du|echo|pwd|whoami|date|uname|kubectl\s+get)\b`),
	}
}
