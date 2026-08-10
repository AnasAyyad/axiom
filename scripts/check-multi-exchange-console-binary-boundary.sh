#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

BINARY="${1:-bin/platform}"
NM="${NM:-/usr/bin/nm}"
RG="${RG:-rg}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-multi-exchange-console-binary.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

if [[ ! -f "${BINARY}" ]]; then
  printf 'ERROR [multi-exchange-console-binary-boundary] compiled platform is missing\n' >&2
  exit 1
fi

"${NM}" --defined-only "${BINARY}" > "${TEMP_DIR}/symbols"
if ! "${RG}" -q 'ScheduleReplayFault|ExportReport' "${TEMP_DIR}/symbols"; then
  printf 'ERROR [multi-exchange-console-binary-boundary] command boundary is not linked\n' >&2
  exit 1
fi

forbidden_pattern='SubmitProductionOrder|PlaceRealOrder|EnableRealTrading|(Execute|Initiate)(Transfer|Withdrawal)'
if "${RG}" -a -i -q --regexp "${forbidden_pattern}" -- "${BINARY}" ||
   "${RG}" -i -q --regexp "${forbidden_pattern}" -- "${TEMP_DIR}/symbols"; then
  printf 'ERROR [multi-exchange-console-binary-boundary] prohibited production execution capability linked\n' >&2
  exit 1
fi

printf 'Multi-exchange-console platform simulation-only command binary boundary passed\n'
