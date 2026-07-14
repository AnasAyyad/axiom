#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-platform-smoke.XXXXXX")"
cleanup() {
  if [[ -n "${service_pid:-}" ]]; then
    kill -TERM "${service_pid}" 2>/dev/null || true
    wait "${service_pid}" 2>/dev/null || true
  fi
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

secret_file="${TEMP_DIR}/postgres-password"
umask 077
openssl rand -base64 32 >"${secret_file}"
go build -trimpath -o "${TEMP_DIR}/platform" ./cmd/platform

export APP_FAIL_CLOSED=true
export APP_SHUTDOWN_TIMEOUT=5s
export BINANCE_PUBLIC_ENDPOINT_SET=market-data-only-v1
export DB_HOST="${AXIOM_SMOKE_DB_HOST:-127.0.0.1}"
export DB_PORT="${AXIOM_SMOKE_DB_PORT:-5432}"
export DB_NAME="${AXIOM_SMOKE_DB_NAME:-axiom}"
export DB_USER="${AXIOM_SMOKE_DB_USER:-postgres}"
export DB_PASSWORD_FILE="${secret_file}"
export DB_SSL_MODE=disable
export RISK_AUTO_UNPAUSE=false
export RISK_FAIL_CLOSED=true
export RISK_INITIAL_STATE=PAUSED

"${TEMP_DIR}/platform" admin migrate >"${TEMP_DIR}/migrate.out"
grep --fixed-strings --quiet '"applied":0' "${TEMP_DIR}/migrate.out"
if DB_PORT=1 DB_CONNECTION_TIMEOUT=100ms \
  "${TEMP_DIR}/platform" admin migrate \
  >"${TEMP_DIR}/migrate-negative.out" 2>"${TEMP_DIR}/migrate-negative.log"; then
  printf 'migration unexpectedly succeeded with an unavailable database\n' >&2
  exit 1
fi

stop_and_check() {
  kill -TERM "${service_pid}"
  for _ in {1..100}; do
    if ! kill -0 "${service_pid}" 2>/dev/null; then
      wait "${service_pid}"
      service_pid=
      return
    fi
    sleep 0.05
  done
  printf 'process exceeded the configured graceful-shutdown ceiling\n' >&2
  exit 1
}

start_and_check() {
  local role="$1"
  local port="$2"
  shift 2
  HTTP_BIND_ADDRESS="127.0.0.1:${port}" \
    METRICS_BIND_ADDRESS="127.0.0.1:${port}" \
    "${TEMP_DIR}/platform" "$@" >"${TEMP_DIR}/${role}.out" 2>"${TEMP_DIR}/${role}.log" &
  service_pid=$!
  for _ in {1..50}; do
    if curl --fail --silent "http://127.0.0.1:${port}/health/ready" >/dev/null; then
      break
    fi
    sleep 0.1
  done
  curl --fail --silent "http://127.0.0.1:${port}/health/live" >/dev/null
  curl --fail --silent "http://127.0.0.1:${port}/health/ready" >/dev/null
  curl --fail --silent "http://127.0.0.1:${port}/api/v1/system/status" | \
    grep --fixed-strings --quiet '"real_trading_enabled":false'
  stop_and_check
}

start_unready_and_check() {
  DB_PORT=1 \
    DB_CONNECTION_TIMEOUT=100ms \
    HTTP_BIND_ADDRESS=127.0.0.1:19084 \
    METRICS_BIND_ADDRESS=127.0.0.1:19084 \
    "${TEMP_DIR}/platform" api \
    >"${TEMP_DIR}/api-unready.out" 2>"${TEMP_DIR}/api-unready.log" &
  service_pid=$!
  for _ in {1..50}; do
    if curl --fail --silent "http://127.0.0.1:19084/health/live" >/dev/null; then
      break
    fi
    sleep 0.1
  done
  curl --fail --silent "http://127.0.0.1:19084/health/live" >/dev/null
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
    "http://127.0.0.1:19084/health/ready")"
  if [[ "${status}" != "503" ]]; then
    printf 'readiness status with unavailable database is %s, want 503\n' "${status}" >&2
    exit 1
  fi
  stop_and_check
}

start_and_check api 18080 api
start_and_check engine 19081 trader --mode shadow
start_and_check recorder 19082 recorder
start_and_check worker 19083 worker
start_unready_and_check

for mode in testnet demo live; do
  if "${TEMP_DIR}/platform" trader --mode "${mode}" >/dev/null 2>&1; then
    printf 'unsafe trader mode accepted: %s\n' "${mode}" >&2
    exit 1
  fi
done
if "${TEMP_DIR}/platform" admin migrate up >/dev/null 2>&1; then
  printf 'noncanonical migration command accepted\n' >&2
  exit 1
fi

printf 'platform command, readiness, safety, and shutdown smoke passed\n'
