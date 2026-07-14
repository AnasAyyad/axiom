#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

GO="${GO:-go}"
GOFMT="${GOFMT:-$("${GO}" env GOROOT)/bin/gofmt}"
COREPACK="${COREPACK:-corepack}"
unformatted="$(find cmd internal scripts -type f -name '*.go' -print0 | xargs -0 "${GOFMT}" -l)"
if [[ -n "${unformatted}" ]]; then
  printf 'ERROR [format] Go formatting drift detected\n' >&2
  exit 1
fi
"${COREPACK}" pnpm format:check
