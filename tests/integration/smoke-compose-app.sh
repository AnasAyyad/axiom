#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

image="${1:-axiom:local}"
project="axiom-a1-smoke-${$}"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/axiom-compose-smoke.XXXXXX")"
secret_dir="${temp_dir}/secrets"
market_dir="${temp_dir}/market-data"
mkdir -p "${secret_dir}" "${market_dir}"

compose() {
  COMPOSE_PROJECT_NAME="${project}" \
    APP_IMAGE="${image}" \
    APP_PULL_POLICY=never \
    SECRETS_DIR="${secret_dir}" \
    MARKET_DATA_HOST_PATH="${market_dir}" \
    HOST_BIND_IP=127.0.0.1 \
    API_HOST_PORT=0 \
    docker compose --env-file .env.example \
      --profile app --profile record --profile workers "$@"
}

cleanup() {
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "${temp_dir}"
}
trap cleanup EXIT HUP INT TERM

docker image inspect "${image}" >/dev/null

readonly -a secret_names=(
  postgres_owner_password
  postgres_migrator_password
  postgres_runtime_password
  postgres_readonly_password
  grafana_admin_password
)
for name in "${secret_names[@]}"; do
  openssl rand -base64 32 >"${secret_dir}/${name}"
  chmod 0640 "${secret_dir}/${name}"
done

docker run --rm --user 0:0 --entrypoint /bin/chgrp \
  --mount "type=bind,src=${secret_dir},dst=/secrets" \
  postgres:18.4-alpine 70 \
  /secrets/postgres_owner_password \
  /secrets/postgres_migrator_password \
  /secrets/postgres_runtime_password \
  /secrets/postgres_readonly_password >/dev/null
docker run --rm --user 0:0 --entrypoint /bin/chown \
  --mount "type=bind,src=${market_dir},dst=/market-data" \
  postgres:18.4-alpine 10001:70 /market-data >/dev/null

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
done

published_address="$(compose port api 8080)"
curl --fail --silent "http://${published_address}/health/live" >/dev/null
curl --fail --silent "http://${published_address}/health/ready" >/dev/null
curl --fail --silent "http://${published_address}/api/v1/system/status" | \
  rg --fixed-strings --quiet '"real_trading_enabled":false'
curl --fail --silent "http://${published_address}/" | \
  rg --fixed-strings --quiet '<div id="root"></div>'

printf 'image-backed Compose migration and application profile smoke passed\n'
