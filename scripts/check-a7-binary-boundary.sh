#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

GO="${GO:-go}"
RG="${RG:-rg}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-a7-binary.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

dependencies="$(${GO} list -deps ./cmd/platform)"
for required_dependency in \
  'axiom/internal/exchanges/binance' \
  'axiom/internal/marketdata' \
  'axiom/internal/recorder' \
  'golang.org/x/net/websocket'; do
  if [[ "${dependencies}" != *"${required_dependency}"* ]]; then
    printf 'ERROR [a7-binary-boundary] public recorder dependency absent: %s\n' \
      "${required_dependency}" >&2
    exit 1
  fi
done
if [[ "${dependencies}" == *'axiom/internal/exchanges/emulator'* ]]; then
  printf 'ERROR [a7-binary-boundary] emulator linked into platform\n' >&2
  exit 1
fi

CGO_ENABLED=0 "${GO}" build -trimpath -o "${TEMP_DIR}/platform" ./cmd/platform
for required_literal in \
  'data-api.binance.vision' \
  'data-stream.binance.vision' \
  'market-data-only-v1'; do
  if ! "${RG}" -a -F -q -- "${required_literal}" "${TEMP_DIR}/platform"; then
    printf 'ERROR [a7-binary-boundary] compiled public policy absent: %s\n' \
      "${required_literal}" >&2
    exit 1
  fi
done
for forbidden_literal in \
  'api.'"binance.com" \
  'test'"net.binance"; do
  if "${RG}" -a -F -q -- "${forbidden_literal}" "${TEMP_DIR}/platform"; then
    printf 'ERROR [a7-binary-boundary] forbidden Binance origin linked: %s\n' \
      "${forbidden_literal}" >&2
    exit 1
  fi
done

printf 'A7 platform production-public dependency and binary boundary passed\n'
