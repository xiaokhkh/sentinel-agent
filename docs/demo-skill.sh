#!/usr/bin/env bash
# Records docs/assets/demo-skill.gif: a cloud agent delegates ONE task to the
# on-device Sentinel agent (`guard skill solve`), which autonomously runs a
# bounded READ-ONLY investigation loop, returns desensitized evidence + root
# cause, then escalates the mutating fix for human approval before it is applied.
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
    s = re.sub(r"(CrashLoopBackOff|Error|FATAL|PAYMENT_DB_URL[^\n]*)", RD+r"\1"+RS, s)
    s = re.sub(r"\b(Running)\b", GR+r"\1"+RS, s)
    return s

subprocess.run(["clear"])
banner("Cloud delegates ONE task; the on-device agent investigates autonomously")
time.sleep(0.6)

# 1) the on-device agent loop: autonomous, read-only, desensitized
cmd("skill solve \"investigate why payment-api is failing in shop\"")
for _ in range(6):  # 1.2B is non-deterministic; retry until it returns a substantive root cause
    s = run("skill","solve","--max-steps","4","investigate why payment-api pods are failing in the shop namespace")
    concl = s.get("conclusion","")
    if s.get("status") in ("needs_approval","blocked") and s.get("steps"):
        break
    if s.get("status") == "completed" and s.get("steps") and "repeated" not in concl and len(concl) > 40:
        break
print(f"  {GY}# on-device agent ran {len(s.get('steps',[]))} read-only step(s), all output desensitized{RS}")
for st in s.get("steps",[]):
    print(f"    {CY}→{RS} {st['command']}")
print(f"  {GR}root cause:{RS} {mark(s.get('conclusion',''))}")
print(f"  {GY}status={s.get('status')} — evidence returned to cloud{RS}\n")
time.sleep(1.6)

# 2) the fix mutates the cluster -> must be approved
banner("Fix = set the missing env var, but that MUTATES the cluster")
cmd(f"skill exec \"{fix_cmd}\"")
e = run("skill","exec",fix_cmd)
print(f"  status={YL}{e.get('status')}{RS}  decision={e.get('decision').upper()} risk={e.get('risk')}")
print(f"  {GY}# read-only mode never auto-mutates — Sentinel asks for human approval{RS}\n")
time.sleep(1.3)
print(f"  {MG}👤 operator:{RS} reviews the command and approves the fix  {GR}✓{RS}\n")
time.sleep(1.1)

cmd(f"skill exec --mode auto \"{fix_cmd}\"")
e = run("skill","exec","--mode","auto",fix_cmd)
print(f"  status={GR}{e.get('status')}{RS}  {e.get('output','').strip()}\n")
time.sleep(0.6)

# 3) verify recovery
subprocess.run(["kubectl","rollout","status","deployment/payment-api","-n","shop","--timeout=90s"],
               capture_output=True, text=True)
time.sleep(0.8)
cmd("skill exec \"kubectl get pods -n shop\"")
e = run("skill","exec","kubectl get pods -n shop")
print(f"  {GY}# payment-api recovered{RS}")
for ln in e.get("output","").rstrip().splitlines():
    print("  "+mark(ln))
print()
time.sleep(1.3)

# 4) destructive stays blocked throughout
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
asciinema rec --overwrite --cols 102 --rows 40 --idle-time-limit 2.0 \
  -c "bash '$ROOT/docs/demo-skill.sh' --play" "$CAST"
agg --speed 1.3 --font-size 18 --theme asciinema "$CAST" "$OUT"
echo "wrote $OUT"
