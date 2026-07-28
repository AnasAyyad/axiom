#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

readonly -a profiles=(app record workers observability edge backup restore sandbox-foundation)
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
expected="$(printf '%s\n' api backup backtest-worker binance-testnet-egress bybit-demo-egress caddy engine-shadow grafana migrate postgres prometheus recorder restore | sort)"
if [[ "${actual}" != "${expected}" ]]; then
  printf 'ERROR [compose] rendered service set differs from the reviewed V1A/V1C PR1 set\n' >&2
  exit 1
fi

docker compose --env-file .env.example --profile '*' config --format json | \
  node scripts/check-compose-command-contract.mjs

printf 'all %d active Compose profile combinations render safely\n' "${combinations}"
