#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

GO="${GO:-go}"
RG="${RG:-rg}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-exchange-integration-binary.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

dependencies="$(${GO} list -deps ./cmd/platform)"
for forbidden_dependency in \
  'axiom/internal/exchanges/emulator'; do
  if [[ "${dependencies}" == *"${forbidden_dependency}"* ]]; then
    printf 'ERROR [exchange-integration-binary-boundary] test-only dependency linked: %s\n' \
      "${forbidden_dependency}" >&2
    exit 1
  fi
done

CGO_ENABLED=0 "${GO}" build -trimpath -o "${TEMP_DIR}/platform" ./cmd/platform
forbidden_pattern='exchanges/emulator|api[0-9]*\.'"binance\.com"'|fapi\.'"binance\.com"'|dapi\.'"binance\.com"'|/(sapi|fapi|dapi|papi)/|/v5/(asset|pos''ition|lo''an|crypto-lo''an|trans''fer|with''draw)'
if "${RG}" -a -i -q --regexp "${forbidden_pattern}" -- "${TEMP_DIR}/platform"; then
  printf 'ERROR [exchange-integration-binary-boundary] test-only or production-private exchange literal linked\n' >&2
  exit 1
fi

printf 'Exchange-integration platform dependency and binary boundary passed\n'
