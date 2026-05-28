#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT:-$ROOT/docs/assets/demo-skill.gif}"
CAST="${CAST:-$(mktemp "${TMPDIR:-/tmp}/sentinel-skill.cast.XXXXXX")}"

type_line() {
  local text="$1"
  printf "\033[1;36m$ "
  local i ch
  for ((i = 0; i < ${#text}; i++)); do
    ch="${text:i:1}"
    printf "%s" "$ch"
    sleep 0.016
  done
  printf "\033[0m\n"
  sleep 0.25
}

if [[ "${1:-}" == "--play" ]]; then
  cd "$ROOT"
  export TERM=xterm-256color
  export PATH="$ROOT/bin:$PATH"
  export SENTINEL_PROVIDER="${SENTINEL_PROVIDER:-mock}"
  export SENTINEL_MODE="${SENTINEL_MODE:-readonly}"
  clear

  type_line "Sentinel Skill -> guard skill (CLI JSON)"
  python3 - "$ROOT/bin/guard" <<'PY'
import json
import re
import subprocess
import sys

guard = sys.argv[1]

def run_json(*args):
    proc = subprocess.run([guard, *args], check=True, text=True, capture_output=True)
    return json.loads(proc.stdout)

def mark(text):
    text = re.sub(r"(\[REDACTED[^\]]*\])", "\033[1;31m\\1\033[0m", text)
    text = re.sub(r"\b(BLOCK|block|refused)\b", "\033[1;31m\\1\033[0m", text)
    return text

print("guard skill context -> non-secret local summary")
ctx = run_json("skill", "context")
print(f"  kube_context={ctx['kube_context']} namespace={ctx['namespace']}")
print()

print("guard skill plan: diagnose not-ready pods")
plan = run_json("skill", "plan", "--provider", "mock", "diagnose not-ready pods in default")
print(f"  provider={plan['provider']} mode={plan['mode']} actions={len(plan['actions'])}")
for action in plan["actions"]:
    print(f"  - {action['decision'].upper():5} {action['command']}")
print()

print("guard skill exec: readonly output is desensitized")
executed = run_json("skill", "exec", "echo 'token: sk-demo-secret owner: ops@example.com'")
print(f"  status={executed['status']} rule={executed['rule']}")
print("  output=" + mark(executed["output"].strip()))
print()

print("guard skill policy: dangerous bulk delete")
policy = run_json("skill", "policy", "kubectl delete pods --all -n default")
print(mark(f"  decision={policy['decision'].upper()} risk={policy['risk']} rule={policy['rule']}"))
PY
  sleep 1
  exit 0
fi

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 1
  fi
}

need go
need python3
need asciinema
need agg

cd "$ROOT"
go build -o bin/guard ./cmd/guard

asciinema record \
  --overwrite \
  --quiet \
  --return \
  --window-size 96x18 \
  --idle-time-limit 1.0 \
  --command "$0 --play" \
  "$CAST"

agg \
  --quiet \
  --theme dracula \
  --font-size 15 \
  --speed 1.65 \
  --cols 96 \
  --rows 18 \
  "$CAST" \
  "$OUT"

echo "wrote $OUT"
