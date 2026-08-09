#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

BINARY="${1:-bin/platform}"
NM="${NM:-/usr/bin/nm}"
RG="${RG:-rg}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-inventory-rebalancing-binary.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

if [[ ! -f "${BINARY}" ]]; then
  printf 'ERROR [inventory-rebalancing-binary-boundary] compiled platform is missing\n' >&2
  exit 1
fi
if [[ ! -x "${NM}" ]] || ! command -v "${RG}" >/dev/null 2>&1; then
  printf 'ERROR [inventory-rebalancing-binary-boundary] nm and rg are required\n' >&2
  exit 2
fi

"${NM}" --defined-only "${BINARY}" > "${TEMP_DIR}/symbols"
forbidden_pattern='(Execute|Submit|Initiate)(Trans''fer|With''drawal)|(Trans''fer|With''drawal)Client'
if "${RG}" -i -q --regexp "${forbidden_pattern}" -- "${TEMP_DIR}/symbols"; then
  printf 'ERROR [inventory-rebalancing-binary-boundary] external asset-movement execution symbol linked\n' >&2
  exit 1
fi
if "${RG}" -a -i -q --regexp "${forbidden_pattern}" -- "${BINARY}"; then
  printf 'ERROR [inventory-rebalancing-binary-boundary] external asset-movement execution literal linked\n' >&2
  exit 1
fi

printf 'Inventory-rebalancing platform no-execution binary boundary passed\n'
