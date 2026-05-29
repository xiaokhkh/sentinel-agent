#!/usr/bin/env bash
# Records docs/assets/demo-skill.gif: a cloud agent triaging a production
# incident through the Sentinel Skill protocol (the `guard skill ...` JSON CLI).
#
# Usage:
#   bash docs/demo-skill.sh            # record + render the GIF (needs asciinema + agg)
#   bash docs/demo-skill.sh --play     # just run the scripted session (used by the recorder)
#
# Live setup for an authentic capture (otherwise set SENTINEL_PROVIDER=mock):
#   minikube start --driver=docker                 # (unset *_PROXY first)
#   kubectl apply -f docs/incident-k8s.yaml         # shop stack; payment-api crash-loops on purpose
#   guard serve &                                   # warm the local model
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT:-$ROOT/docs/assets/demo-skill.gif}"
CAST="${CAST:-$(mktemp "${TMPDIR:-/tmp}/sentinel-skill.cast.XXXXXX")}"

if [[ "${1:-}" == "--play" ]]; then
  cd "$ROOT"
  export TERM=xterm-256color
  export PATH="$ROOT/bin:$PATH"
  unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy ALL_PROXY all_proxy NO_PROXY no_proxy
  export SENTINEL_PROVIDER="${SENTINEL_PROVIDER:-llamacpp}"
  export SENTINEL_MODE="${SENTINEL_MODE:-readonly}"
  clear
  python3 - "$ROOT/bin/guard" <<'PY'
import json, re, subprocess, sys, time

guard = sys.argv[1]
CY="\033[1;36m"; GY="\033[0;90m"; RD="\033[1;31m"; GR="\033[1;32m"; YL="\033[1;33m"; RS="\033[0m"

def banner(t): print(f"{GY}# {t}{RS}"); time.sleep(0.5)
def cmd(t):
    sys.stdout.write(f"{CY}❯ guard {t}{RS}\n"); time.sleep(0.2)
def run(*a):
    p = subprocess.run([guard, *a], text=True, capture_output=True)
    try: return json.loads(p.stdout)
    except Exception: return {"_raw": p.stdout.strip()}
def mark(s):
    s = re.sub(r"(\[REDACTED[^\]]*\])", RD+r"\1"+RS, s)
    s = re.sub(r"(CrashLoopBackOff|Error|FATAL[^\n]*)", RD+r"\1"+RS, s)
    s = re.sub(r"\b(Running)\b", GR+r"\1"+RS, s)
    return s

banner("A cloud agent triages a prod incident — every op goes through Sentinel Skill")
time.sleep(0.6)

cmd("skill context")
c = run("skill","context")
print(f"  kube_context={c.get('kube_context')}  namespace={c.get('namespace')}  (non-secret summary only)\n")
time.sleep(1.2)

cmd("skill plan \"investigate why payment-api pods are failing in shop\"")
p = run("skill","plan","investigate why payment-api pods are failing in the shop namespace")
print(f"  provider={p.get('provider')} mode={p.get('mode')} -> {len(p.get('actions',[]))} screened action(s)")
for a in p.get("actions",[]):
    print(f"    [{a['decision'].upper()}] {a['command']}")
print()
time.sleep(1.3)

cmd("skill exec \"kubectl get pods -n shop\"")
e = run("skill","exec","kubectl get pods -n shop")
for ln in e.get("output","").rstrip().splitlines():
    print("  "+mark(ln))
print()
time.sleep(1.4)

cmd("skill exec \"kubectl logs -n shop -l app=payment-api --tail=1\"   # root cause")
e = run("skill","exec","kubectl logs -n shop -l app=payment-api --tail=1")
for ln in e.get("output","").rstrip().splitlines():
    print("  "+mark(ln))
print()
time.sleep(1.4)

cmd("skill policy \"kubectl rollout restart deployment/payment-api -n shop\"")
r = run("skill","policy","kubectl rollout restart deployment/payment-api -n shop")
print(f"  decision={YL}{str(r.get('decision')).upper()}{RS} risk={r.get('risk')} -> needs human approval\n")
time.sleep(1.3)

cmd("skill exec \"kubectl delete pods --all -n shop\"   # destructive")
e = run("skill","exec","kubectl delete pods --all -n shop")
print(f"  status={RD}{e.get('status')}{RS}  {e.get('reason')}")
time.sleep(1.8)
PY
  exit 0
fi

command -v asciinema >/dev/null || { echo "need: brew install asciinema"; exit 1; }
command -v agg        >/dev/null || { echo "need: brew install agg"; exit 1; }
cd "$ROOT"
go build -o bin/guard ./cmd/guard
asciinema rec --overwrite --cols 100 --rows 30 -c "bash '$ROOT/docs/demo-skill.sh' --play" "$CAST"
agg --speed 1.3 --font-size 20 --theme asciinema "$CAST" "$OUT"
echo "wrote $OUT"
