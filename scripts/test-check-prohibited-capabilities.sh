#!/usr/bin/env bash

# Seed an isolated repository with both allowed evidence text and representative
# forbidden executable inputs. This is intentionally dependency-light so CI can
# validate the scanner before trusting it as a release gate.
set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
SCANNER="${SCRIPT_DIR}/check-prohibited-capabilities.sh"
TEMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/axiom-prohibited-self-test.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_ROOT}"
}
trap cleanup EXIT HUP INT TERM

mkdir -p \
  "${TEMP_ROOT}/scripts" \
  "${TEMP_ROOT}/docs" \
  "${TEMP_ROOT}/tests" \
  "${TEMP_ROOT}/internal/config" \
  "${TEMP_ROOT}/internal/domain" \
  "${TEMP_ROOT}/internal/exchanges/binance"
cp -- "${SCANNER}" "${TEMP_ROOT}/scripts/check-prohibited-capabilities.sh"
chmod +x "${TEMP_ROOT}/scripts/check-prohibited-capabilities.sh"

# Descriptive documents and test sources may state the complete denial matrix.
printf '%s\n' \
  '# Safety evidence' \
  'Tests deny testnet, demo, live, margin, futures, withdrawals, and signers.' \
  >"${TEMP_ROOT}/docs/safety.md"
printf '%s\n' \
  'package domain' \
  'type seededTestAmount float64' \
  >"${TEMP_ROOT}/internal/domain/money_test.go"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  '# BINANCE_TESTNET_API_KEY is forbidden evidence, not runtime configuration.' \
  >"${TEMP_ROOT}/tests/safety_test.sh"
printf '%s\n' \
  'package config' \
  'var prohibitedCapabilityTokens = []string{' \
  '  "LEVERAGE",' \
  '}' \
  >"${TEMP_ROOT}/internal/config/safety.go"

if ! "${TEMP_ROOT}/scripts/check-prohibited-capabilities.sh" \
  >"${TEMP_ROOT}/allowed.out" 2>&1; then
  printf 'scanner self-test failed: documentation/test evidence was not ignored\n' >&2
  exit 1
fi

printf '%s\n' \
  'services:' \
  '  engine-binance-testnet:' \
  '    profiles: [testnet]' \
  '    environment:' \
  '      EXECUTION_MODE: live' \
  '      BINANCE_TESTNET_API_KEY_FILE: /run/secrets/not-a-real-secret' \
  '      PRIVATE_ORDER_CAPABILITY: enabled' \
  >"${TEMP_ROOT}/docker-compose.yml"
printf '%s\n' \
  'package binance' \
  'const orderRoute = "https://api.binance.com/api/v3/order"' \
  'type Signer interface { SignRequest() }' \
  'func PlaceOrder() {}' \
  >"${TEMP_ROOT}/internal/exchanges/binance/client.go"
printf '%s\n' \
  'package domain' \
  'type Amount float64' \
  >"${TEMP_ROOT}/internal/domain/money.go"

set +e
"${TEMP_ROOT}/scripts/check-prohibited-capabilities.sh" \
  >"${TEMP_ROOT}/seeded-negative.out" 2>&1
status=$?
set -e

if ((status != 1)); then
  printf 'scanner self-test failed: seeded violations returned status %d, want 1\n' \
    "${status}" >&2
  exit 1
fi

for rule in \
  exchange-credential-key private-endpoint later-release-sandbox \
  external-live-mode signer-or-order-interface external-order-method \
  exchange-order-method prohibited-product financial-float; do
  if ! rg --fixed-strings --quiet "ERROR [${rule}]" \
    "${TEMP_ROOT}/seeded-negative.out"; then
    printf 'scanner self-test failed: missing diagnostic for rule %s\n' "${rule}" >&2
    exit 1
  fi
done

if rg --fixed-strings --quiet 'not-a-real-secret' \
  "${TEMP_ROOT}/seeded-negative.out"; then
  printf 'scanner self-test failed: diagnostic exposed a credential value\n' >&2
  exit 1
fi

printf 'prohibited-capability scanner self-test passed\n'
