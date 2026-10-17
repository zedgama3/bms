#!/usr/bin/env bash
# Runs the Playwright smoke tests against a real `bin/bms serve` (app.md "The Makefile").
# Loads the development environment, migrates, starts the server on a free port, waits for
# /readyz, runs the tests, stops the server with SIGTERM, and fails when the server's exit
# status is not 0. BMS_E2E_MODE=hello-disabled starts it with BMS_MODULES_DISABLED=hello.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
. deploy/compose/.env
set +a

data=$(mktemp -d)
log=$(mktemp)
pid=""
cleanup() {
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$data" "$log"
}
trap cleanup EXIT

port=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
export BMS_DATA_DIR="$data"
export BMS_HTTP_ADDR="127.0.0.1:$port"
# bms refuses unknown BMS_* variables, so the mode goes to Playwright only.
mode=${BMS_E2E_MODE:-default}
unset BMS_E2E_MODE
if [[ "$mode" == "hello-disabled" ]]; then
  export BMS_MODULES_DISABLED=hello
fi

bin/bms migrate

bin/bms serve >"$log" 2>&1 &
pid=$!
ready=0
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$port/readyz" >/dev/null 2>&1; then ready=1; break; fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.2
done
if [[ "$ready" != 1 ]]; then
  echo "bms did not become ready; its output:" >&2
  cat "$log" >&2
  kill "$pid" 2>/dev/null || true
  exit 1
fi

tests=0
BMS_E2E_MODE="$mode" BMS_E2E_URL="http://127.0.0.1:$port" pnpm -C web test:e2e || tests=$?

kill -TERM "$pid"
status=0
wait "$pid" || status=$?
pid="" # stopped and reaped: the trap has nothing left to kill
if [[ "$status" != 0 ]]; then
  echo "bms serve exited with status $status; its output:" >&2
  cat "$log" >&2
  exit 1
fi
exit "$tests"
