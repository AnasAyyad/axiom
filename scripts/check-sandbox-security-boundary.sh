#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "${ROOT}"
GO="${GO:-go}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-sandbox-security.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'ERROR [sandbox-security] %s\n' "$1" >&2
  exit 1
}

command -v rg >/dev/null 2>&1 || fail "ripgrep is required"

AUTHENTICATED_SOURCES=(
  internal/exchanges/binance/authenticated_client.go
  internal/exchanges/binance/authenticated_operations.go
  internal/exchanges/binance/authenticated_policy.go
  internal/exchanges/binance/authenticated_response.go
  internal/exchanges/binance/private_decoder.go
  internal/exchanges/binance/private_subscription.go
  internal/exchanges/binance/private_stream.go
  internal/exchanges/binance/private_transport.go
  internal/exchanges/binance/sandbox_adapter.go
  internal/exchanges/binance/sandbox_adapter_commands.go
  internal/exchanges/binance/sandbox_clock.go
  internal/exchanges/binance/sandbox_eligibility.go
  internal/exchanges/binance/sandbox_snapshot.go
  internal/exchanges/binance/sandbox_filters.go
  internal/exchanges/binance/sandbox_filter_validation.go
  internal/exchanges/binance/sandbox_normalize.go
  internal/exchanges/binance/sandbox_normalize_fills.go
  internal/exchanges/binance/sandbox_rate.go
  internal/exchanges/binance/sandbox_recovery.go
  internal/exchanges/binance/sandbox_reset.go
  internal/exchanges/bybit/authenticated_client.go
  internal/exchanges/bybit/authenticated_operations.go
  internal/exchanges/bybit/authenticated_policy.go
  internal/exchanges/bybit/authenticated_response.go
  internal/exchanges/bybit/private_decoder.go
  internal/exchanges/bybit/private_stream.go
  internal/exchanges/bybit/private_transport.go
  internal/exchanges/bybit/sandbox_adapter.go
  internal/exchanges/bybit/sandbox_adapter_orders.go
  internal/exchanges/bybit/sandbox_balances.go
  internal/exchanges/bybit/sandbox_budget.go
  internal/exchanges/bybit/sandbox_clock.go
  internal/exchanges/bybit/sandbox_eligibility.go
  internal/exchanges/bybit/sandbox_fills.go
  internal/exchanges/bybit/sandbox_filter_helpers.go
  internal/exchanges/bybit/sandbox_filters.go
  internal/exchanges/bybit/sandbox_history.go
  internal/exchanges/bybit/sandbox_normalize.go
  internal/exchanges/bybit/sandbox_payloads.go
  internal/exchanges/bybit/sandbox_rate.go
  internal/exchanges/bybit/sandbox_snapshot.go
)

CREDENTIAL_FREE_PUBLIC_SOURCES=(
  internal/exchanges/bybit/sandbox_public.go
)

if rg -q --pcre2 \
  '(?i)(apiKey|apiSecret|signature|authorization|cookie|X-Bapi-(Api-Key|Sign|Timestamp|Recv-Window)|AuthenticatedEvidence)' \
  -- "${CREDENTIAL_FREE_PUBLIC_SOURCES[@]}"; then
  fail "a credential-free production-public path can access private material"
fi

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
  api-demo.bybit.com stream-demo.bybit.com api.bybit.com stream.bybit.com; do
  rg -q --fixed-strings "${required}" internal/egressproxy internal/exchanges ||
    fail "required compiled sandbox host is absent"
done

for required in \
  AXIOM_BINANCE_TESTNET_API_KEY_FILE AXIOM_BINANCE_TESTNET_API_SECRET_FILE \
  AXIOM_BYBIT_DEMO_API_KEY_FILE AXIOM_BYBIT_DEMO_API_SECRET_FILE AXIOM_TOTP_SEED_FILE; do
  rg -q --fixed-strings "${required}" internal/config internal/sandbox ||
    fail "required file-only secret reference is absent"
done

for source_variable in \
  BINANCE_CANARY_REQUEST_SOURCE_FILE BYBIT_CANARY_REQUEST_SOURCE_FILE; do
  rg -q --fixed-strings "${source_variable}:-./deploy/config/canary-request-unavailable" \
    docker-compose.yml ||
    fail "${source_variable} does not fail closed to the invalid placeholder"
done
rg -q --fixed-strings "intentionally invalid, non-secret" \
  deploy/config/canary-request-unavailable ||
  fail "canary request placeholder is missing or ambiguous"

api_block="$(sed -n '/^  api:/,/^  [a-zA-Z0-9_-]*:/p' docker-compose.yml)"
if [[ "${api_block}" == *"binance_testnet_api_"* || "${api_block}" == *"bybit_demo_api_"* ]]; then
  fail "API service receives exchange credentials"
fi

for service in binance-testnet-egress bybit-demo-egress bybit-public-egress; do
  rg -q "^  ${service}:" docker-compose.yml ||
    fail "closed egress service is absent"
done
for service in binance-sandbox-engine bybit-sandbox-engine; do
  rg -q "^  ${service}:" docker-compose.yml ||
    fail "authenticated sandbox engine service is absent"
done

CGO_ENABLED=0 "${GO}" build -trimpath -o "${TEMP_DIR}/platform" ./cmd/platform
if rg -a -i -q \
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

rg -q --fixed-strings "func (store *SandboxRuntimeDispatcherStore) RecordAuthenticatedRequest" \
  internal/storage/postgres/sandbox_runtime_authenticated_evidence_store.go ||
  fail "authenticated request evidence has no durable sink"
rg -q --fixed-strings "CREATE TABLE v1c_authenticated_request_evidence" internal/storage/postgres/migrations/000022_v1c_sandbox_execution.sql ||
  fail "authenticated request evidence has no durable schema"
evidence_table="$(sed -n \
  '/^CREATE TABLE v1c_authenticated_request_evidence (/,/^);$/p' internal/storage/postgres/migrations/000022_v1c_sandbox_execution.sql)"
if rg -q --pcre2 \
  '^[[:space:]]*(api_key|api_secret|signature|headers|private_payload|price|quantity|totp|session)[[:space:]]' \
  <<<"${evidence_table}"; then
  fail "durable authenticated request evidence exposes private material"
fi
for required in host method path field_names enumerated_fields request_hash configuration_id recorded_at; do
  rg -q --pcre2 "^[[:space:]]*${required}[[:space:]]" <<<"${evidence_table}" ||
    fail "durable authenticated evidence omits ${required}"
done

printf 'Sandbox security boundary scan passed\n'
