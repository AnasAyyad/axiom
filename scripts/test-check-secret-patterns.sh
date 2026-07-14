#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-secret-scan.XXXXXX")"
cleanup() { rm -rf -- "${TEMP_DIR}"; }
trap cleanup EXIT HUP INT TERM

mkdir -p "${TEMP_DIR}/scripts"
cp "${ROOT}/scripts/check-secret-patterns.sh" "${TEMP_DIR}/scripts/"
printf '%s\n' 'safe placeholder: CHANGE_ME' >"${TEMP_DIR}/safe.txt"
if ! (cd "${TEMP_DIR}" && bash scripts/check-secret-patterns.sh >/dev/null); then
  printf 'secret scanner rejected safe placeholder text\n' >&2
  exit 1
fi
printf '%s\n' 'password=abcdefghijklmnopqrstuvwxyz123456' >"${TEMP_DIR}/unsafe.env"
set +e
(cd "${TEMP_DIR}" && bash scripts/check-secret-patterns.sh >"${TEMP_DIR}/result" 2>&1)
status=$?
set -e
if ((status != 1)); then
  printf 'secret scanner did not reject seeded material\n' >&2
  exit 1
fi
if rg --fixed-strings --quiet 'abcdefghijklmnopqrstuvwxyz123456' "${TEMP_DIR}/result"; then
  printf 'secret scanner diagnostic exposed seeded material\n' >&2
  exit 1
fi
printf 'secret-pattern scanner self-test passed\n'
