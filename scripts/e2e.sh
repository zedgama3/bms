#!/usr/bin/env bash
# Runs the Playwright smoke tests against a real `bin/bms serve` (app.md "The Makefile").
# Loads the development environment, migrates, starts the server on a free port, waits for
# /readyz, runs the tests, stops the server with SIGTERM, and fails when the server's exit
# status is not 0. BMS_E2E_MODE=hello-disabled starts it with BMS_MODULES_DISABLED=hello;
# BMS_E2E_MODE=readonly migrates a throw-away database, freezes it the way an operator would
# (ALTER DATABASE ... SET default_transaction_read_only = on) and serves from it.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
. deploy/compose/.env
set +a

compose=(docker compose -f deploy/compose/docker-compose.yml --env-file deploy/compose/.env)
psql() { timeout 30 "${compose[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -qAt -U bms -d postgres "$@"; }

data=$(mktemp -d)
log=$(mktemp)
pid=""
tmpdb=""
cleanup() {
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [[ -n "$tmpdb" ]]; then
    psql -c "DROP DATABASE IF EXISTS $tmpdb WITH (FORCE)" >/dev/null || true
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
case "$mode" in
  default) ;;
  hello-disabled) export BMS_MODULES_DISABLED=hello ;;
  readonly)
    if [[ -z "${BMS_DB_URL:-}" ]]; then
      echo "readonly mode needs BMS_DB_URL in deploy/compose/.env (not BMS_DB_URL_FILE)" >&2; exit 2
    fi
    tmpdb="bms_e2e_ro_$$"
    psql -c "CREATE DATABASE $tmpdb" >/dev/null
    # Same server and role, another database: swap the URL's path, keep everything else.
    BMS_DB_URL=$(python3 -c 'import sys, urllib.parse as u; p = u.urlsplit(sys.argv[1]); print(u.urlunsplit(p._replace(path="/" + sys.argv[2])))' "$BMS_DB_URL" "$tmpdb")
    export BMS_DB_URL
    ;;
  *) echo "unknown BMS_E2E_MODE: $mode (default, hello-disabled, readonly)" >&2; exit 2 ;;
esac

bin/bms migrate
if [[ "$mode" == "readonly" ]]; then
  psql -c "ALTER DATABASE $tmpdb SET default_transaction_read_only = on" >/dev/null
fi

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
