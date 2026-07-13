#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

for profile in testnet demo; do
  services="$(docker compose --env-file .env.example --profile "${profile}" config --services | sort)"
  if [[ "${services}" != "postgres" ]]; then
    printf 'reserved unavailable profile unexpectedly starts a service: %s\n' "${profile}" >&2
    exit 1
  fi
done
printf 'reserved later-release profile placeholders are inert\n'
