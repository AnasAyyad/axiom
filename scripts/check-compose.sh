#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

readonly -a profiles=(app record workers observability edge)
readonly combinations=$((1 << ${#profiles[@]}))

for ((mask = 0; mask < combinations; mask++)); do
  args=(--env-file .env.example)
  for ((index = 0; index < ${#profiles[@]}; index++)); do
    if (((mask & (1 << index)) != 0)); then
      args+=(--profile "${profiles[index]}")
    fi
  done
  docker compose "${args[@]}" config --quiet
done

actual="$(docker compose --env-file .env.example --profile '*' config --services | sort)"
expected="$(printf '%s\n' api backtest-worker caddy engine-shadow grafana migrate postgres prometheus recorder | sort)"
if [[ "${actual}" != "${expected}" ]]; then
  printf 'ERROR [compose] rendered service set differs from the reviewed V1A set\n' >&2
  exit 1
fi

docker compose --env-file .env.example --profile '*' config --format json | \
  node scripts/check-compose-command-contract.mjs

printf 'all %d active Compose profile combinations render safely\n' "${combinations}"
