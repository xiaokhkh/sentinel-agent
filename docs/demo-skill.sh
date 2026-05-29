#!/usr/bin/env bash
# Records docs/assets/demo-skill.gif: a cloud agent triaging AND FIXING a
# production incident through the Sentinel Skill protocol (the `guard skill`
# JSON CLI) — including the human-approval gate before the mutating fix.
#
# Usage:
#   bash docs/demo-skill.sh            # record + render the GIF (needs asciinema + agg)
#   bash docs/demo-skill.sh --play     # just run the scripted session (used by the recorder)
#
# Live setup for an authentic capture (otherwise set SENTINEL_PROVIDER=mock):
#   minikube start --driver=docker                 # (unset *_PROXY first)
#   kubectl delete deploy payment-api -n shop 2>/dev/null; kubectl apply -f docs/incident-k8s.yaml
#   guard serve &                                   # warm the local model
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT:-$ROOT/docs/assets/demo-skill.gif}"
CAST="${CAST:-$(mktemp "${TMPDIR:-/tmp}/sentinel-skill.cast.XXXXXX")}"
FIX_VALUE="${FIX_VALUE:-postgres://payments-db:5432/payments}"

if [[ "${1:-}" == "--play" ]]; then
  cd "$ROOT"
  export TERM=xterm-256color
  export PATH="$ROOT/bin:$PATH"
  unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy ALL_PROXY all_proxy NO_PROXY no_proxy
  export SENTINEL_PROVIDER="${SENTINEL_PROVIDER:-llamacpp}"
  export SENTINEL_MODE="${SENTINEL_MODE:-readonly}"
  FIX_VALUE="$FIX_VALUE" python3 - "$ROOT/bin/guard" <<'PY'
import json, os, re, subprocess, sys, time

guard = sys.argv[1]
FIX = os.environ["FIX_VALUE"]
fix_cmd = f"kubectl set env deployment/payment-api PAYMENT_DB_URL={FIX} -n shop"
CY="\033[1;36m"; GY="\033[0;90m"; RD="\033[1;31m"; GR="\033[1;32m"; YL="\033[1;33m"; MG="\033[1;35m"; RS="\033[0m"

def banner(t): print(f"{GY}# {t}{RS}"); time.sleep(0.5)
def cmd(t): sys.stdout.write(f"{CY}❯ guard {t}{RS}\n"); time.sleep(0.2)
def run(*a):
    p = subprocess.run([guard, *a], text=True, capture_output=True)
    try: return json.loads(p.stdout)
    except Exception: return {"_raw": p.stdout.strip()}
def mark(s):
    s = re.sub(r"(\[REDACTED[^\]]*\])", RD+r"\1"+RS, s)
    s = re.sub(r"(CrashLoopBackOff|Error|FATAL[^\n]*)", RD+r"\1"+RS, s)
    s = re.sub(r"\b(Running)\b", GR+r"\1"+RS, s)
    return s
def pods(label):
    e = run("skill","exec","kubectl get pods -n shop")
    print(f"  {GY}# {label}{RS}")
    for ln in e.get("output","").rstrip().splitlines():
        print("  "+mark(ln))
    print()

subprocess.run(["clear"])
banner("A cloud agent triages AND fixes a prod incident — all via Sentinel Skill")
time.sleep(0.6)

cmd("skill context")
c = run("skill","context")
print(f"  kube_context={c.get('kube_context')}  namespace={c.get('namespace')}  (non-secret summary)\n")
time.sleep(1.0)

cmd("skill plan \"why are payment-api pods failing in shop\"")
for _ in range(5):  # the 1.2B occasionally emits invalid JSON; retry like a real integration would
    p = run("skill","plan","investigate why payment-api pods are failing in the shop namespace")
    if p.get("status") == "planned" and p.get("actions"):
        break
print(f"  provider={p.get('provider')} mode={p.get('mode')} -> {len(p.get('actions',[]))} screened action(s)")
for a in p.get("actions",[]):
    print(f"    [{a['decision'].upper()}] {a['command']}")
print()
time.sleep(1.1)

cmd("skill exec \"kubectl get pods -n shop\"")
pods("2 payment-api replicas are crash-looping; storefront + redis are healthy")
time.sleep(1.2)

cmd("skill exec \"kubectl logs -n shop -l app=payment-api --tail=1\"   # root cause")
e = run("skill","exec","kubectl logs -n shop -l app=payment-api --tail=1")
for ln in e.get("output","").rstrip().splitlines()[:1]:
    print("  "+mark(ln))
print()
time.sleep(1.3)

banner("Fix: set the missing env var — but that MUTATES the cluster")
cmd(f"skill exec \"{fix_cmd}\"")
e = run("skill","exec",fix_cmd)
print(f"  status={YL}{e.get('status')}{RS}  decision={e.get('decision').upper()} risk={e.get('risk')}")
print(f"  {GY}# read-only mode never auto-mutates — Sentinel asks for human approval{RS}\n")
time.sleep(1.3)

print(f"  {MG}👤 operator:{RS} reviews the command and approves the fix  {GR}✓{RS}\n")
time.sleep(1.2)

cmd(f"skill exec --mode auto \"{fix_cmd}\"")
e = run("skill","exec","--mode","auto",fix_cmd)
print(f"  status={GR}{e.get('status')}{RS}  {e.get('output','').strip()}\n")
time.sleep(0.8)

# wait for rollout silently so the recovery snapshot is real
subprocess.run(["kubectl","rollout","status","deployment/payment-api","-n","shop","--timeout=90s"],
               capture_output=True, text=True)
time.sleep(1.0)
cmd("skill exec \"kubectl get pods -n shop\"")
pods("payment-api recovered — all pods Running")
time.sleep(1.2)

banner("Even mid-incident, destructive ops stay blocked")
cmd("skill exec \"kubectl delete pods --all -n shop\"")
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
asciinema rec --overwrite --cols 100 --rows 40 --idle-time-limit 2.0 \
  -c "bash '$ROOT/docs/demo-skill.sh' --play" "$CAST"
agg --speed 1.3 --font-size 18 --theme asciinema "$CAST" "$OUT"
echo "wrote $OUT"
