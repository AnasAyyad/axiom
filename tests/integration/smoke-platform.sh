#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-platform-smoke.XXXXXX")"
recorder_root="${TEMP_DIR}/market-data"
mkdir -p "${recorder_root}"
cleanup() {
  if [[ -n "${service_pid:-}" ]]; then
    kill -TERM "${service_pid}" 2>/dev/null || true
    wait "${service_pid}" 2>/dev/null || true
  fi
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

secret_file="${TEMP_DIR}/postgres-password"
health_secret_file="${TEMP_DIR}/health-detail-token"
bootstrap_email_file="${TEMP_DIR}/bootstrap-owner-email"
bootstrap_password_hash_file="${TEMP_DIR}/bootstrap-owner-password-hash"
csrf_key_file="${TEMP_DIR}/csrf-key"
session_signing_key_file="${TEMP_DIR}/session-signing-key"
umask 077
openssl rand -base64 32 >"${secret_file}"
openssl rand -base64 32 >"${health_secret_file}"
printf '%s\n' 'smoke-owner@example.invalid' >"${bootstrap_email_file}"
printf '%s\n' 'platform-smoke-password-only' | \
  go run ./scripts/generate_bootstrap_hash.go >"${bootstrap_password_hash_file}"
openssl rand -base64 48 >"${csrf_key_file}"
openssl rand -base64 48 >"${session_signing_key_file}"
printf 'header = "Authorization: Bearer %s"\n' "$(<"${health_secret_file}")" >"${TEMP_DIR}/health-curl.conf"
source_commit="${GITHUB_SHA:-$(git rev-parse HEAD)}"
go_sum_hash="$(sha256sum go.sum | cut -d' ' -f1)"
pnpm_lock_hash="$(sha256sum pnpm-lock.yaml | cut -d' ' -f1)"
go build -trimpath \
  -ldflags "-X axiom/internal/buildinfo.Version=ci-smoke -X axiom/internal/buildinfo.Commit=${source_commit} -X axiom/internal/buildinfo.BuiltAt=ci-smoke -X axiom/internal/buildinfo.Dirty=false -X axiom/internal/buildinfo.GoSumHash=${go_sum_hash} -X axiom/internal/buildinfo.PNPMLockHash=${pnpm_lock_hash}" \
  -o "${TEMP_DIR}/platform" ./cmd/platform

export APP_FAIL_CLOSED=true
export APP_SHUTDOWN_TIMEOUT=5s
export BINANCE_PUBLIC_ENDPOINT_SET=market-data-only-v1
export DB_HOST="${AXIOM_SMOKE_DB_HOST:-127.0.0.1}"
export DB_PORT="${AXIOM_SMOKE_DB_PORT:-5432}"
export DB_NAME="${AXIOM_SMOKE_DB_NAME:-axiom}"
export DB_USER="${AXIOM_SMOKE_DB_USER:-postgres}"
export DB_PASSWORD_FILE="${secret_file}"
export DB_SSL_MODE=disable
export HEALTH_DETAIL_TOKEN_FILE="${health_secret_file}"
export RECORDER_ROOT="${recorder_root}"
export RISK_AUTO_UNPAUSE=false
export RISK_FAIL_CLOSED=true
export RISK_INITIAL_STATE=PAUSED

"${TEMP_DIR}/platform" admin migrate >"${TEMP_DIR}/migrate.out"
grep --fixed-strings --quiet '"event_code":"migration_complete"' "${TEMP_DIR}/migrate.out"
grep --fixed-strings --quiet '"phase":"A11"' "${TEMP_DIR}/migrate.out"
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
  local expected_readiness="$3"
  local status_response detailed_response metrics_response login_response cookie_jar readiness_status
  local -a role_environment=()
  shift 3
  local metrics_port="${port}"
  if [[ "${role}" == "api" ]]; then
    metrics_port=$((port + 100))
    role_environment=(env
      "AUTH_BOOTSTRAP_OWNER_EMAIL_FILE=${bootstrap_email_file}"
      "AUTH_BOOTSTRAP_OWNER_PASSWORD_HASH_FILE=${bootstrap_password_hash_file}"
      "AUTH_CSRF_KEY_FILE=${csrf_key_file}"
      "AUTH_SESSION_SIGNING_KEY_FILE=${session_signing_key_file}")
  fi
  HTTP_BIND_ADDRESS="127.0.0.1:${port}" \
    METRICS_BIND_ADDRESS="127.0.0.1:${metrics_port}" \
    "${role_environment[@]}" "${TEMP_DIR}/platform" "$@" \
    >"${TEMP_DIR}/${role}.out" 2>"${TEMP_DIR}/${role}.log" &
  service_pid=$!
  for _ in {1..200}; do
    if [[ "${expected_readiness}" == "ready" ]] && \
      curl --fail --silent "http://127.0.0.1:${port}/health/ready" >/dev/null; then
      break
    fi
    if [[ "${expected_readiness}" == "unready" ]] && \
      curl --fail --silent "http://127.0.0.1:${port}/health/live" >/dev/null; then
      break
    fi
    sleep 0.1
  done
  curl --fail --silent "http://127.0.0.1:${port}/health/live" >/dev/null
  if [[ "${expected_readiness}" == "ready" ]]; then
    curl --fail --silent "http://127.0.0.1:${port}/health/ready" >/dev/null
  else
    readiness_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
      "http://127.0.0.1:${port}/health/ready")"
    if [[ "${readiness_status}" != "503" ]]; then
      printf '%s readiness without recovery evidence is %s, want 503\n' \
        "${role}" "${readiness_status}" >&2
      exit 1
    fi
  fi
  if [[ "${role}" == "api" ]]; then
    cookie_jar="${TEMP_DIR}/api-cookies.txt"
    login_response="$(curl --fail --silent --cookie-jar "${cookie_jar}" \
      --header 'Origin: http://localhost:8080' \
      --header 'Content-Type: application/json' \
      --data '{"email":"smoke-owner@example.invalid","password":"platform-smoke-password-only"}' \
      "http://127.0.0.1:${port}/api/v1/session/login")"
    grep --fixed-strings --quiet '"csrf_token":' <<<"${login_response}"
    status_response="$(curl --fail --silent --cookie "${cookie_jar}" \
      "http://127.0.0.1:${port}/api/v1/system/status")"
  else
    status_response="$(curl --fail --silent "http://127.0.0.1:${port}/api/v1/system/status")"
  fi
  grep --fixed-strings --quiet '"real_trading_enabled":false' <<<"${status_response}"
  if [[ "${expected_readiness}" == "ready" ]]; then
    detailed_response="$(curl --fail --silent --config "${TEMP_DIR}/health-curl.conf" \
      "http://127.0.0.1:${port}/api/v1/system/health")"
  else
    detailed_response="$(curl --silent --config "${TEMP_DIR}/health-curl.conf" \
      "http://127.0.0.1:${port}/api/v1/system/health")"
  fi
  grep --fixed-strings --quiet '"name":"postgres"' <<<"${detailed_response}"
  metrics_response="$(curl --fail --silent "http://127.0.0.1:${metrics_port}/metrics")"
  grep --fixed-strings --quiet 'axiom_dependency_ready' <<<"${metrics_response}"
  stop_and_check
}

start_unready_and_check() {
  DB_PORT=1 \
    DB_CONNECTION_TIMEOUT=100ms \
    HTTP_BIND_ADDRESS=127.0.0.1:19084 \
    METRICS_BIND_ADDRESS=127.0.0.1:19184 \
    AUTH_BOOTSTRAP_OWNER_EMAIL_FILE="${bootstrap_email_file}" \
    AUTH_BOOTSTRAP_OWNER_PASSWORD_HASH_FILE="${bootstrap_password_hash_file}" \
    AUTH_CSRF_KEY_FILE="${csrf_key_file}" \
    AUTH_SESSION_SIGNING_KEY_FILE="${session_signing_key_file}" \
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

start_and_check api 18080 ready api
start_and_check engine 19081 unready trader --mode shadow
start_and_check recorder 19082 ready recorder
start_and_check worker 19083 ready worker
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
