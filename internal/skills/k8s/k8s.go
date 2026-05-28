// Package k8s is the Kubernetes capability pack. Importing it for side effect
// registers the skill with the global registry.
package k8s

import "github.com/xiaokhkh/sentinel-agent/internal/skills"

func init() {
	skills.Register(skills.Skill{
		Name:        "k8s",
		Description: "Kubernetes pod diagnosis, restart, and log inspection (read-only by default)",
		Examples: []string{
			`guard run "诊断 default 命名空间里未就绪的 pod"`,
			`guard run "查看 payment 服务最近的日志"`,
			`guard run "重启 nginx deployment"`,
		},
		Status: "experimental",
	})
}
