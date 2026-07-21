#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

image="${1:-axiom:local}"
project="axiom-a5-smoke-${$}"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/axiom-compose-smoke.XXXXXX")"
secret_dir="${temp_dir}/secrets"
market_dir="${temp_dir}/market-data"
mkdir -p "${secret_dir}" "${market_dir}"
chmod 0770 "${market_dir}"

compose() {
  COMPOSE_PROJECT_NAME="${project}" \
    APP_IMAGE="${image}" \
    APP_PULL_POLICY=never \
    SECRETS_DIR="${secret_dir}" \
    MARKET_DATA_HOST_PATH="${market_dir}" \
    RECORDER_FLUSH_INTERVAL=2s \
    HOST_BIND_IP=127.0.0.1 \
    API_HOST_PORT=0 \
    GRAFANA_HOST_PORT=0 \
    docker compose --env-file .env.example \
      --profile app --profile record --profile workers --profile observability "$@"
}

cleanup() {
	status=$?
	if [[ ${status} -ne 0 ]]; then
		compose ps --all >&2 || true
		compose logs --no-color --tail 100 postgres migrate api engine-shadow recorder backtest-worker prometheus grafana >&2 || true
	fi
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "${temp_dir}"
	return "${status}"
}
trap cleanup EXIT HUP INT TERM

docker image inspect "${image}" >/dev/null

readonly -a secret_names=(
  postgres_owner_password
  postgres_migrator_password
  postgres_runtime_password
  postgres_recorder_password
  postgres_backup_password
  backup_encryption_key
  postgres_readonly_password
  grafana_admin_password
  health_detail_token
  csrf_key
  session_signing_key
)
for name in "${secret_names[@]}"; do
  openssl rand -base64 32 >"${secret_dir}/${name}"
  chmod 0640 "${secret_dir}/${name}"
done
printf '%s\n' 'owner@example.invalid' >"${secret_dir}/bootstrap_owner_email"
printf '%s\n' 'compose-smoke-password-only' | \
  go run ./scripts/generate_bootstrap_hash.go >"${secret_dir}/bootstrap_owner_password_hash"
chmod 0640 "${secret_dir}/bootstrap_owner_email" "${secret_dir}/bootstrap_owner_password_hash"

docker run --rm --user 0:0 --entrypoint /bin/chgrp \
  --mount "type=bind,src=${secret_dir},dst=/secrets" \
  postgres:18.4-alpine 70 \
  /secrets/postgres_owner_password \
  /secrets/postgres_migrator_password \
  /secrets/postgres_runtime_password \
  /secrets/postgres_recorder_password \
  /secrets/postgres_backup_password \
  /secrets/backup_encryption_key \
  /secrets/postgres_readonly_password \
  /secrets/health_detail_token >/dev/null
docker run --rm --user 0:0 --entrypoint /bin/chgrp \
  --mount "type=bind,src=${secret_dir},dst=/secrets" \
  postgres:18.4-alpine 70 \
  /secrets/bootstrap_owner_email \
  /secrets/bootstrap_owner_password_hash \
  /secrets/csrf_key \
  /secrets/session_signing_key >/dev/null
docker run --rm --user 0:0 --entrypoint /bin/chgrp \
  --mount "type=bind,src=${secret_dir},dst=/secrets" \
  postgres:18.4-alpine 0 /secrets/grafana_admin_password >/dev/null

printf 'header = "Authorization: Bearer %s"\n' "$(<"${secret_dir}/health_detail_token")" >"${temp_dir}/health-curl.conf"
chmod 0600 "${temp_dir}/health-curl.conf"
docker run --rm --user 0:0 --entrypoint /bin/chown \
  --mount "type=bind,src=${market_dir},dst=/market-data" \
  postgres:18.4-alpine "10001:$(id -g)" /market-data >/dev/null

compose up --detach --wait --wait-timeout 180

migrate_id="$(compose ps --all --quiet migrate)"
if [[ -z "${migrate_id}" ]] || \
  [[ "$(docker inspect --format '{{.State.ExitCode}}' "${migrate_id}")" != "0" ]]; then
  printf 'ERROR [compose-smoke] migration service did not complete successfully\n' >&2
  exit 1
fi

for service in api engine-shadow recorder backtest-worker; do
  container_id="$(compose ps --quiet "${service}")"
  if [[ -z "${container_id}" ]] || \
    [[ "$(docker inspect --format '{{.State.Health.Status}}' "${container_id}")" != "healthy" ]]; then
    printf 'ERROR [compose-smoke] %s is not healthy\n' "${service}" >&2
    exit 1
  fi
  runtime_security="$(docker inspect --format '{{.Config.User}}|{{.HostConfig.ReadonlyRootfs}}|{{json .HostConfig.CapDrop}}|{{json .HostConfig.SecurityOpt}}' "${container_id}")"
  if [[ "${runtime_security}" != *'10001:70|true|'* ]] || \
    [[ "${runtime_security}" != *'"ALL"'* ]] || \
    [[ "${runtime_security}" != *'no-new-privileges:true'* ]]; then
    printf 'ERROR [compose-smoke] %s runtime hardening differs from policy\n' "${service}" >&2
    exit 1
  fi
done

published_address="$(compose port api 8080)"
curl --fail --silent "http://${published_address}/health/live" >/dev/null
curl --fail --silent "http://${published_address}/health/ready" >/dev/null
cookie_jar="${temp_dir}/api-cookies.txt"
login_response="$(curl --fail --silent --cookie-jar "${cookie_jar}" \
  --header 'Origin: http://localhost:8080' \
  --header 'Content-Type: application/json' \
  --data '{"email":"owner@example.invalid","password":"compose-smoke-password-only"}' \
  "http://${published_address}/api/v1/session/login")"
csrf_token="$(sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p' <<<"${login_response}")"
if [[ -z "${csrf_token}" ]]; then
  printf 'ERROR [compose-smoke] authenticated API login did not return a CSRF token\n' >&2
  exit 1
fi
printf 'header = "Origin: http://localhost:8080"\nheader = "X-CSRF-Token: %s"\n' \
  "${csrf_token}" >"${temp_dir}/api-curl.conf"
chmod 0600 "${temp_dir}/api-curl.conf"
status_response="$(curl --fail --silent --cookie "${cookie_jar}" \
  "http://${published_address}/api/v1/system/status")"
rg --fixed-strings --quiet '"real_trading_enabled":false' <<<"${status_response}"
detailed_response="$(curl --fail --silent --config "${temp_dir}/health-curl.conf" \
  "http://${published_address}/api/v1/system/health")"
rg --fixed-strings --quiet '"name":"postgres"' <<<"${detailed_response}"
ui_response="$(curl --fail --silent "http://${published_address}/")"
rg --fixed-strings --quiet '<div id="root"></div>' <<<"${ui_response}"
curl --fail --silent --output /dev/null --cookie "${cookie_jar}" \
  --config "${temp_dir}/api-curl.conf" \
  --request POST "http://${published_address}/api/v1/session/logout"

prometheus_id="$(compose ps --quiet prometheus)"
grafana_id="$(compose ps --quiet grafana)"
for service_id in "${prometheus_id}" "${grafana_id}"; do
  if [[ -z "${service_id}" ]] || [[ "$(docker inspect --format '{{.State.Running}}' "${service_id}")" != "true" ]]; then
    printf 'ERROR [compose-smoke] observability service is not running\n' >&2
    exit 1
  fi
  runtime_security="$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}|{{json .HostConfig.CapDrop}}|{{json .HostConfig.SecurityOpt}}' "${service_id}")"
  if [[ "${runtime_security}" != 'true|'* ]] || [[ "${runtime_security}" != *'"ALL"'* ]] || \
    [[ "${runtime_security}" != *'no-new-privileges:true'* ]]; then
    printf 'ERROR [compose-smoke] observability runtime hardening differs from policy\n' >&2
    exit 1
  fi
done

targets_ready=false
for _ in $(seq 1 30); do
  if docker exec "${prometheus_id}" /bin/promtool query instant \
    http://127.0.0.1:9090 'count(up{job=~"api|engine-shadow|recorder|backtest-worker"} == 1)' 2>/dev/null | \
    rg --quiet '=> 4(\.0+)? @'; then
    targets_ready=true
    break
  fi
  sleep 2
done
if [[ "${targets_ready}" != "true" ]]; then
  printf 'ERROR [compose-smoke] Prometheus did not scrape all four application roles\n' >&2
  exit 1
fi

grafana_address="$(compose port grafana 3000)"
grafana_ready=false
for _ in $(seq 1 30); do
  if grafana_health="$(curl --fail --silent "http://${grafana_address}/api/health")" && \
    rg --quiet '"database"[[:space:]]*:[[:space:]]*"ok"' <<<"${grafana_health}" && \
    grafana_search="$(curl --fail --silent --user "admin:$(<"${secret_dir}/grafana_admin_password")" \
      "http://${grafana_address}/api/search?query=Axiom")" && \
    rg --fixed-strings --quiet 'Axiom V1A Operations' <<<"${grafana_search}"; then
    grafana_ready=true
    break
  fi
  sleep 2
done
if [[ "${grafana_ready}" != "true" ]]; then
  printf 'ERROR [compose-smoke] Grafana did not provision the Axiom dashboard\n' >&2
  exit 1
fi

printf 'image-backed Compose application and observability profile smoke passed\n'
