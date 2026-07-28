#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "${ROOT}"
GO="${GO:-go}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-v1c-security.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'ERROR [v1c-security] %s\n' "$1" >&2
  exit 1
}

command -v rg >/dev/null 2>&1 || fail "ripgrep is required"

AUTHENTICATED_SOURCES=(
  internal/exchanges/binance/authenticated_client.go
  internal/exchanges/binance/authenticated_policy.go
  internal/exchanges/bybit/authenticated_client.go
  internal/exchanges/bybit/authenticated_policy.go
)

if rg -q --pcre2 \
  '(?i)(api[0-9]*\.binance\.com|fapi\.binance\.com|dapi\.binance\.com|api\.bybit\.com|stream\.bybit\.com)' \
  -- "${AUTHENTICATED_SOURCES[@]}"; then
  fail "an authenticated source contains a production-private destination"
fi

if rg -q --pcre2 \
  '(?i)(/(sapi|fapi|dapi|papi)/|/v5/(asset|position|loan|crypto-loan|transfer|withdraw))' \
  -- "${AUTHENTICATED_SOURCES[@]}"; then
  fail "an authenticated source contains a forbidden API family"
fi

if rg -q --pcre2 \
  '^func[[:space:]]+[A-Z][A-Za-z0-9_]*[^\n]*(url\.(URL|Values)|http\.(Header|Request)|endpoint|proxyURL)' \
  -- "${AUTHENTICATED_SOURCES[@]}"; then
  fail "an authenticated package exports a generic request or destination surface"
fi

if rg -q --pcre2 \
  'AXIOM_(BINANCE|BYBIT)_[A-Z0-9_]*(API_KEY|API_SECRET|TOKEN|SIGNATURE)(?!_FILE)' \
  --glob '*.go' --glob '*.yml' --glob '*.yaml' --glob '*.env.example' \
  --glob '!**/*_test.go' -- .; then
  fail "a raw exchange credential variable is executable"
fi

for required in \
  testnet.binance.vision ws-api.testnet.binance.vision stream.testnet.binance.vision \
  api-demo.bybit.com stream-demo.bybit.com; do
  rg -q --fixed-strings "${required}" internal/egressproxy internal/exchanges ||
    fail "required compiled sandbox host is absent"
done

for required in \
  AXIOM_BINANCE_TESTNET_API_KEY_FILE AXIOM_BINANCE_TESTNET_API_SECRET_FILE \
  AXIOM_BYBIT_DEMO_API_KEY_FILE AXIOM_BYBIT_DEMO_API_SECRET_FILE AXIOM_TOTP_SEED_FILE; do
  rg -q --fixed-strings "${required}" internal/config internal/sandbox ||
    fail "required file-only secret reference is absent"
done

api_block="$(sed -n '/^  api:/,/^  [a-zA-Z0-9_-]*:/p' docker-compose.yml)"
if [[ "${api_block}" == *"binance_testnet_api_"* || "${api_block}" == *"bybit_demo_api_"* ]]; then
  fail "API service receives exchange credentials"
fi

for service in binance-testnet-egress bybit-demo-egress; do
  rg -q "^  ${service}:" docker-compose.yml ||
    fail "closed egress service is absent"
done

CGO_ENABLED=0 "${GO}" build -trimpath -o "${TEMP_DIR}/platform" ./cmd/platform
if rg -a -i -q --pcre2 \
  '(api[0-9]*\.binance\.com|fapi\.binance\.com|dapi\.binance\.com|/(sapi|fapi|dapi|papi)/|/v5/(asset|position|loan|crypto-loan|transfer|withdraw))' \
  -- "${TEMP_DIR}/platform"; then
  fail "the platform binary contains a production-private or forbidden API destination"
fi

if rg -q --pcre2 '(?i)(secret|signature|authorization|cookie|header|payload|price|quantity)' \
  <(sed -n '/type AuthenticatedRequestEvidence struct/,/^}/p' \
    internal/exchanges/contracts/authenticated.go); then
  # The type name contains Authenticated; inspect only field declarations.
  fields="$(sed -n '/type AuthenticatedRequestEvidence struct/,/^}/p' \
    internal/exchanges/contracts/authenticated.go | tail -n +2 | head -n -1)"
  if [[ "${fields}" == *"Secret"* || "${fields}" == *"Signature"* ||
        "${fields}" == *"Header"* || "${fields}" == *"Payload"* ||
        "${fields}" == *"Price"* || "${fields}" == *"Quantity"* ]]; then
    fail "authenticated request evidence exposes private material"
  fi
fi

rg -q --fixed-strings "func (store *V1CDispatcherStore) RecordAuthenticatedRequest" \
  internal/storage/postgres/v1c_authenticated_evidence_store.go ||
  fail "authenticated request evidence has no durable sink"
rg -q --fixed-strings "CREATE TABLE v1c_authenticated_request_evidence" \
  internal/storage/postgres/migrations/000022_v1c_sandbox_execution.sql ||
  fail "authenticated request evidence has no durable schema"
evidence_table="$(sed -n \
  '/^CREATE TABLE v1c_authenticated_request_evidence (/,/^);$/p' \
  internal/storage/postgres/migrations/000022_v1c_sandbox_execution.sql)"
if rg -q --pcre2 \
  '^[[:space:]]*(api_key|api_secret|signature|headers|private_payload|price|quantity|totp|session)[[:space:]]' \
  <<<"${evidence_table}"; then
  fail "durable authenticated request evidence exposes private material"
fi
for required in host method path field_names enumerated_fields request_hash configuration_id recorded_at; do
  rg -q --pcre2 "^[[:space:]]*${required}[[:space:]]" <<<"${evidence_table}" ||
    fail "durable authenticated evidence omits ${required}"
done

printf 'V1C C1 security boundary scan passed\n'
