#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT:-$ROOT/docs/assets/demo-cli.gif}"
CAST="${CAST:-$(mktemp "${TMPDIR:-/tmp}/sentinel-cli.cast.XXXXXX")}"

type_run() {
  local cmd="$1"
  local allow_nonzero="${2:-false}"
  printf "\033[1;36m$ "
  local i ch
  for ((i = 0; i < ${#cmd}; i++)); do
    ch="${cmd:i:1}"
    printf "%s" "$ch"
    sleep 0.018
  done
  printf "\033[0m\n"
  set +e
  eval "$cmd"
  local status=$?
  set -e
  printf "\n"
  sleep 0.35
  if [[ "$status" -ne 0 && "$allow_nonzero" != "true" ]]; then
    return "$status"
  fi
}

if [[ "${1:-}" == "--play" ]]; then
  cd "$ROOT"
  export TERM=xterm-256color
  export PATH="$ROOT/bin:$PATH"
  clear

  type_run "guard model"
  type_run "guard run --mode readonly 'show me all pods in the default namespace'"
  type_run "guard policy check 'kubectl delete pods --all -n default'" true
  exit 0
fi

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 1
  fi
}

need go
need kubectl
need asciinema
need agg

cd "$ROOT"
go build -o bin/guard ./cmd/guard
kubectl apply -f docs/demo-k8s.yaml >/dev/null

asciinema record \
  --overwrite \
  --quiet \
  --return \
  --window-size 96x24 \
  --idle-time-limit 1.0 \
  --command "$0 --play" \
  "$CAST"

agg \
  --quiet \
  --theme dracula \
  --font-size 15 \
  --speed 1.65 \
  --cols 96 \
  --rows 24 \
  "$CAST" \
  "$OUT"

echo "wrote $OUT"
