#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

GO="${GO:-go}"
NODE="${NODE:-node}"
COREPACK="${COREPACK:-corepack}"

require_version() {
  local name="$1"
  local actual="$2"
  local expected="$3"
  if [[ "${actual}" != "${expected}" ]]; then
    printf 'ERROR [preflight] %s version is %s, want %s\n' "${name}" "${actual}" "${expected}" >&2
    exit 1
  fi
}

require_version go "$("${GO}" version | awk '{print $3}')" go1.26.5
require_version node "$("${NODE}" --version)" v24.18.0
require_version pnpm "$("${COREPACK}" pnpm --version)" 11.12.0

for command in docker rg sha256sum; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    printf 'ERROR [preflight] required command is unavailable: %s\n' "${command}" >&2
    exit 1
  fi
done
docker compose version >/dev/null
printf 'A1 toolchain preflight passed\n'
