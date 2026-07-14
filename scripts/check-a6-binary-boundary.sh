#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

GO="${GO:-go}"
RG="${RG:-rg}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-a6-binary.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

dependencies="$(${GO} list -deps ./cmd/platform)"
for forbidden_dependency in \
  'axiom/internal/exchanges/emulator'; do
  if [[ "${dependencies}" == *"${forbidden_dependency}"* ]]; then
    printf 'ERROR [a6-binary-boundary] test-only dependency linked: %s\n' \
      "${forbidden_dependency}" >&2
    exit 1
  fi
done

CGO_ENABLED=0 "${GO}" build -trimpath -o "${TEMP_DIR}/platform" ./cmd/platform
forbidden_pattern='exchanges/emulator|Request''Signer|Signed''Transport|Authenticated''Client|Place''Order|Submit''Order|Create''Order|Order''Broker|test''net|bybit.{0,20}de''mo'
if "${RG}" -a -i -q --regexp "${forbidden_pattern}" -- "${TEMP_DIR}/platform"; then
  printf 'ERROR [a6-binary-boundary] forbidden exchange symbol or literal linked\n' >&2
  exit 1
fi

printf 'A6 platform dependency and binary boundary passed\n'
