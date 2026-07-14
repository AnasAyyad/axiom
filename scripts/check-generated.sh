#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

GO="${GO:-go}"
NODE="${NODE:-node}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-generated.XXXXXX")"
cleanup() { rm -rf -- "${TEMP_DIR}"; }
trap cleanup EXIT HUP INT TERM

"${GO}" tool oapi-codegen \
  --generate types,skip-prune \
  --package generated \
  -o "${TEMP_DIR}/types.gen.go" \
  api/openapi.yaml
"${NODE}" scripts/generate-openapi-types.mjs \
  api/openapi.yaml "${TEMP_DIR}/schema.ts"

if ! cmp -s internal/api/generated/types.gen.go "${TEMP_DIR}/types.gen.go"; then
  printf 'ERROR [generated] Go OpenAPI models are stale\n' >&2
  exit 1
fi
if ! cmp -s web/src/api/generated/schema.ts "${TEMP_DIR}/schema.ts"; then
  printf 'ERROR [generated] TypeScript OpenAPI models are stale\n' >&2
  exit 1
fi
printf 'generated OpenAPI contracts are current\n'
