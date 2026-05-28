---
name: sentinel-agent
description: Use when the user asks an agent to inspect or operate private infrastructure through Sentinel Skill. The skill uses the local `guard skill` CLI JSON interface so all ops go through Sentinel's Policy Guard, permission mode, and redaction runtime.
---

# Sentinel-Agent

Use Sentinel Skill for private-infrastructure work. The agent plans at a high level, but concrete ops commands must go through the local `guard skill` CLI interface.

## Operating model

- Treat Sentinel Skill as the product interface.
- Treat `guard skill ...` as the local communication protocol between the cloud agent and Sentinel runtime.
- Do not ask the user to paste kubeconfigs, private keys, DB credentials, cloud tokens, or raw secret-bearing logs into chat.
- Prefer `guard skill` commands over direct shell commands for Kubernetes, SSH, cloud CLI, database, and production-debugging tasks.
- Only sanitized observations should return to the cloud-planning loop.

## Tool flow

1. Use `guard skill context` first when you need to understand what local ops context exists. It returns a non-secret JSON summary only.
2. Use `guard skill plan "<task>"` for natural-language ops requests. It plans locally and screens every action through Policy Guard.
3. Use `guard skill exec "<command>"` for one concrete read-only command from the screened plan. Its output is redacted before returning.
4. Use `guard skill policy "<command>"` before proposing or discussing a risky concrete command.
5. Parse the JSON response. Do not scrape human CLI output.

## Safety rules

- If a command returns `approval_required`, stop and ask the user for explicit approval or tell them to run it through the local `guard` CLI.
- If a command returns `refused` or Policy Guard says `block`, do not find a workaround.
- If `guard skill plan` says no plan was produced or asks for missing input, do not escalate the raw task to another cloud tool. Ask the user for the missing non-secret reference or suggest configuring Sentinel locally.
- For mutating actions, keep the proposed command and risk summary visible to the user.

## Setup hint

If `guard skill` is unavailable, ask the user to install or build the CLI:

```bash
go install github.com/xiaokhkh/sentinel-agent/cmd/guard@latest
guard skill context
```
