#!/usr/bin/env bash

# Fail the V1A build when executable artifacts expose a later-release or
# real-money exchange capability. Diagnostics deliberately omit matching text:
# configuration lines may contain credential material even though they should
# never be committed.
set -euo pipefail
IFS=$'\n\t'
export LC_ALL=C

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPOSITORY_ROOT="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd -P)"
cd -- "${REPOSITORY_ROOT}"

if ! command -v rg >/dev/null 2>&1; then
  printf 'ERROR [scanner] ripgrep (rg) is required\n' >&2
  exit 2
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/axiom-prohibited-scan.XXXXXX")"
cleanup() {
  rm -rf -- "${TEMP_DIR}"
}
trap cleanup EXIT HUP INT TERM

readonly -a RG_OPTIONS=(
  --hidden
  --no-ignore-vcs
  --color never
  --no-heading
  --with-filename
  --line-number
  --sort path
  --pcre2
)

# Tests and descriptive safety documents are evidence, not executable release
# inputs. Fixtures are excluded for the same reason. The scanner and its own
# seeded-negative test necessarily contain every denied token.
readonly -a EXCLUDES=(
  --glob '!.git/**'
  --glob '!.secrets/**'
  --glob '!secrets/**'
  --glob '!.local/**'
  --glob '!node_modules/**'
  --glob '!web/dist/**'
  --glob '!internal/api/static/dist/**'
  --glob '!vendor/**'
  --glob '!docs/**'
  --glob '!**/*.md'
  --glob '!test/**'
  --glob '!tests/**'
  --glob '!**/test/**'
  --glob '!**/tests/**'
  --glob '!**/testdata/**'
  --glob '!**/fixtures/**'
  --glob '!**/__tests__/**'
  --glob '!**/__fixtures__/**'
  --glob '!**/e2e/**'
  --glob '!**/*_test.go'
  --glob '!**/*_test.py'
  --glob '!**/*.test.ts'
  --glob '!**/*.test.tsx'
  --glob '!**/*.test.js'
  --glob '!**/*.test.jsx'
  --glob '!**/*.spec.ts'
  --glob '!**/*.spec.tsx'
  --glob '!**/*.spec.js'
  --glob '!**/*.spec.jsx'
  --glob '!**/check-prohibited-capabilities.sh'
  --glob '!**/test-check-prohibited-capabilities.sh'
  --glob '!scripts/check-b1-public-boundary.mjs'
  --glob '!scripts/check-b6-rebalancing-boundary.mjs'
  --glob '!scripts/check-b8-console-boundary.mjs'
  --glob '!scripts/check-b8-binary-boundary.sh'
  # V1C authenticated sandbox inputs are governed by the narrower C1 scanner.
  --glob '!internal/config/v1c*.go'
  --glob '!internal/config/schema.go'
  --glob '!internal/config/mode.go'
  --glob '!internal/config/capabilities.go'
  --glob '!internal/config/validation.go'
  --glob '!internal/sandbox/**'
  --glob '!internal/egressproxy/**'
  --glob '!internal/exchanges/contracts/authenticated.go'
  --glob '!internal/exchanges/binance/authenticated_*.go'
  --glob '!internal/exchanges/bybit/authenticated_*.go'
  --glob '!internal/exchanges/sandboxemulator/**'
  --glob '!internal/authentication/sandbox_authorization.go'
  --glob '!internal/authentication/totp.go'
  --glob '!internal/bootstrap/application.go'
  --glob '!internal/bootstrap/command.go'
  --glob '!internal/storage/postgres/migrations/000021_v1c_auth_control.sql'
  --glob '!internal/storage/postgres/migrations/000022_v1c_sandbox_execution.sql'
  --glob '!internal/storage/postgres/v1c_*.go'
  --glob '!deploy/postgres/init/001-create-roles.sh'
  --glob '!docker-compose.yml'
  --glob '!.env.example'
  --glob '!scripts/check-compose-command-contract.mjs'
  --glob '!scripts/check-compose.sh'
  --glob '!scripts/check-v1c-security-boundary.sh'
  --glob '!scripts/check-v1c-pr3-boundary.mjs'
)

readonly -a ALL_INPUT_GLOBS=(
  --glob '*.go'
  --glob '*.rs'
  --glob '*.py'
  --glob '*.ts'
  --glob '*.tsx'
  --glob '*.js'
  --glob '*.jsx'
  --glob '*.mjs'
  --glob '*.cjs'
  --glob '*.sh'
  --glob '*.sql'
  --glob '*.proto'
  --glob '*.graphql'
  --glob '*.html'
  --glob '*.yaml'
  --glob '*.yml'
  --glob '*.json'
  --glob '*.toml'
  --glob '*.ini'
  --glob '*.cfg'
  --glob '*.conf'
  --glob '*.properties'
  --glob '.env.example'
  --glob '*.env.example'
  --glob 'Dockerfile'
  --glob 'Dockerfile.*'
  --glob 'Makefile'
  --glob '*.mk'
  --glob 'Caddyfile'
  --glob '*.symbols'
  --glob '*.packages'
  --glob '*.sbom'
  --glob '*.spdx'
  --glob '*.cdx'
  --glob '*manifest*.txt'
)

readonly -a CONFIG_INPUT_GLOBS=(
  --glob '*.sql'
  --glob '*.yaml'
  --glob '*.yml'
  --glob '*.json'
  --glob '*.toml'
  --glob '*.ini'
  --glob '*.cfg'
  --glob '*.conf'
  --glob '*.properties'
  --glob '.env.example'
  --glob '*.env.example'
  --glob 'Dockerfile'
  --glob 'Dockerfile.*'
  --glob 'Makefile'
  --glob '*.mk'
  --glob 'Caddyfile'
  --glob '*.symbols'
  --glob '*.packages'
  --glob '*.sbom'
  --glob '*.spdx'
  --glob '*.cdx'
  --glob '*manifest*.txt'
)

readonly -a GO_INPUT_GLOBS=(--glob '*.go')

VIOLATIONS=0
SCAN_ERRORS=0

# A capability descriptor may name a denied operation only when a typed source
# record makes the denial explicit on that same line. Boolean/config toggles are
# intentionally not allowlisted because they become dormant activation paths.
is_typed_unsupported_capability() {
  local file="$1"
  local line_text="${2,,}"
  local unsupported_re='(=|:)[[:space:]]*[^[:alnum:]_]{0,1}[[:alnum:]_]*(unsupported|unavailable)[^[:alnum:]_]{0,2}[[:space:]]*(//.*)?$'

  case "${file}" in
    */capabilit*.go | capabilit*.go | */capabilit*.ts | capabilit*.ts | */capabilit*.tsx | capabilit*.tsx)
      [[ "${line_text}" =~ ${unsupported_re} ]]
      ;;
    *)
      return 1
      ;;
  esac
}

# The compiled startup denylist must spell rejected key fragments literally.
# Only exact string-list entries in its single reviewed file are allowed; an
# assignment, field, flag, or occurrence in any other executable file still
# fails the scan.
is_compiled_rejection_literal() {
  local file="$1"
  local line_text="$2"
  local rejection_literal_re='^[[:space:]]*"(LIVE_TRADING|REAL_TRADING|WITHDRAW|TRANSFER_EXECUTION|MARGIN|FUTURES|PERPETUAL|LEVERAGE|BORROW|LENDING|STAKING|SHORT_SELL)",[[:space:]]*$'

  case "${file}" in
    ./internal/config/safety.go | internal/config/safety.go)
      [[ "${line_text}" =~ ${rejection_literal_re} ]]
      ;;
    *)
      return 1
      ;;
  esac
}

# B1 compiles only Bybit's reviewed production-public hosts and explicit
# credential-header denials. The allowlist is intentionally file- and
# line-shaped so no arbitrary endpoint, credential input, or capability can
# become dormant executable configuration.
is_b1_public_boundary_literal() {
  local rule_id="$1"
  local file="$2"
  local line_text="$3"
  case "${rule_id}:${file}" in
    private-endpoint:./internal/exchanges/bybit/endpoint_policy.go | \
    private-endpoint:internal/exchanges/bybit/endpoint_policy.go | \
    private-endpoint:./internal/exchanges/bybit/public_connection.go | \
    private-endpoint:internal/exchanges/bybit/public_connection.go | \
    private-endpoint:./internal/exchanges/bybit/public_websocket_transport.go | \
    private-endpoint:internal/exchanges/bybit/public_websocket_transport.go | \
    private-endpoint:./internal/config/v1b.go | private-endpoint:internal/config/v1b.go | \
    private-endpoint:./internal/config/validation.go | private-endpoint:internal/config/validation.go | \
    private-endpoint:./deploy/config/platform-shadow-v1b.json | private-endpoint:deploy/config/platform-shadow-v1b.json)
      [[ "${line_text}" == *"https://api.bybit.com"* || "${line_text}" == *"wss://stream.bybit.com/v5/public/spot"* ||
         "${line_text}" == *'api.bybit.com'* || "${line_text}" == *'stream.bybit.com'* ]]
      ;;
    exchange-credential-key:./internal/exchanges/bybit/endpoint_policy.go | \
    exchange-credential-key:internal/exchanges/bybit/endpoint_policy.go)
      [[ "${line_text}" == *'"Authorization", "Cookie", "X-Bapi-Api-Key", "X-Bapi-Sign", "X-Bapi-Timestamp", "X-Bapi-Recv-Window"'* ]]
      ;;
    prohibited-product:./internal/exchanges/bybit/payloads.go | \
    prohibited-product:internal/exchanges/bybit/payloads.go)
      [[ "${line_text}" == *'MarginTrading'*'string'*'`json:"marginTrading"`'* ]]
      ;;
    *) return 1 ;;
  esac
}

# V1C's closed Testnet/Demo boundary is executable by design and is governed
# by the stricter C1 scanner, fixed route contracts, and binary destination
# scan. This allowlist is rule- and file-specific so production-private hosts,
# live mode, and forbidden product families remain rejected everywhere.
is_v1c_sandbox_boundary_literal() {
  local rule_id="$1"
  local file="${2#./}"
  local line_text="$3"
  case "${rule_id}:${file}" in
    private-endpoint:internal/exchanges/binance/private_stream.go | \
    private-endpoint:internal/exchanges/binance/private_subscription.go | \
    private-endpoint:internal/exchanges/bybit/private_stream.go | \
    private-endpoint:internal/storage/postgres/migrations/000023_v1c_private_stream_runtime.sql)
      return 0
      ;;
    later-release-sandbox:internal/exchanges/binance/private_stream.go | \
    later-release-sandbox:internal/exchanges/binance/private_subscription.go | \
    later-release-sandbox:internal/exchanges/binance/sandbox_recovery.go | \
    later-release-sandbox:internal/exchanges/binance/sandbox_reset.go | \
    later-release-sandbox:internal/exchanges/bybit/private_stream.go | \
    later-release-sandbox:internal/exchanges/bybit/sandbox_adapter.go | \
    later-release-sandbox:internal/bootstrap/sandbox_engine_attestation.go | \
    later-release-sandbox:internal/storage/postgres/migrations/000023_v1c_private_stream_runtime.sql)
      return 0
      ;;
    later-release-sandbox:api/openapi.yaml | \
    later-release-sandbox:internal/api/generated/types.gen.go | \
    later-release-sandbox:internal/bootstrap/runtime.go | \
    later-release-sandbox:internal/observability/metrics.go | \
    later-release-sandbox:internal/qualification/c6/model.go | \
    later-release-sandbox:internal/storage/postgres/migrations/000024_v1c_c6_console_qualification.sql | \
    later-release-sandbox:monitoring/alerts.yml | \
    later-release-sandbox:monitoring/grafana/dashboards/axiom-operations.json | \
    later-release-sandbox:web/src/api/c6Validation.ts | \
    later-release-sandbox:web/src/api/generated/schema.ts | \
    later-release-sandbox:web/src/app/AppShell.tsx | \
    later-release-sandbox:web/src/app/Sandbox*.tsx | \
    later-release-sandbox:web/src/app/sandboxPresentation.ts)
      return 0
      ;;
    later-release-sandbox:Makefile)
      [[ "${line_text}" == *"c4-binance-testnet-qualify"* ||
         "${line_text}" == *"c5-bybit-demo-qualify"* ||
         "${line_text}" == *"v1c-pr2-local-qualify"* ]]
      ;;
    later-release-sandbox:deploy/docker/Dockerfile)
      [[ "${line_text}" == *"/out/platform-binance-testnet-v1c.json"* ||
         "${line_text}" == *"/out/platform-bybit-demo-v1c.json"* ]]
      ;;
    prohibited-product:internal/exchanges/bybit/sandbox_filters.go)
      [[ "${line_text}" == *'item.MarginTrading != "none"'* ]]
      ;;
    exchange-order-method:internal/exchanges/binance/private_stream.go | \
    exchange-order-method:internal/exchanges/binance/private_subscription.go | \
    exchange-order-method:internal/exchanges/binance/sandbox_adapter.go | \
    exchange-order-method:internal/exchanges/bybit/private_stream.go | \
    exchange-order-method:internal/exchanges/bybit/sandbox_adapter.go)
      return 0
      ;;
    *) return 1 ;;
  esac
}

# run_rule ID DESCRIPTION REGEX ALLOW_POLICY GLOB_ARRAY TARGET...
run_rule() {
  local rule_id="$1"
  local description="$2"
  local expression="$3"
  local allow_policy="$4"
  local glob_array_name="$5"
  shift 5

  local -n input_globs="${glob_array_name}"
  local error_file="${TEMP_DIR}/${rule_id}.err"
  local matches=''
  local status=0
  local file
  local line_number
  local line_text

  set +e
  # Inclusion globs come first; the later exclusions take precedence in rg.
  matches="$(rg "${RG_OPTIONS[@]}" "${input_globs[@]}" "${EXCLUDES[@]}" \
    --regexp "${expression}" -- "$@" 2>"${error_file}")"
  status=$?
  set -e

  if ((status == 1)); then
    return 0
  fi
  if ((status != 0)); then
    printf 'ERROR [scanner] ripgrep failed while evaluating rule %s\n' "${rule_id}" >&2
    SCAN_ERRORS=$((SCAN_ERRORS + 1))
    return 0
  fi

  while IFS=: read -r file line_number line_text; do
    # Descriptive comments document the denial policy but do not compile into a
    # capability or configure a service. Inline values remain fully scanned.
    if [[ "${line_text}" =~ ^[[:space:]]*(#|//|--|/\*|\*|\<\!--) ]]; then
      continue
    fi

    if [[ "${allow_policy}" =~ ^(typed-unsupported|explicit-or-typed)$ ]] && \
      is_typed_unsupported_capability "${file}" "${line_text}"; then
      continue
    fi

    if [[ "${allow_policy}" =~ ^(explicit-rejection|explicit-or-typed)$ ]] && \
      is_compiled_rejection_literal "${file}" "${line_text}"; then
      continue
    fi

    if is_b1_public_boundary_literal "${rule_id}" "${file}" "${line_text}"; then
      continue
    fi

    if is_v1c_sandbox_boundary_literal "${rule_id}" "${file}" "${line_text}"; then
      continue
    fi

    printf 'ERROR [%s] %s:%s: %s\n' \
      "${rule_id}" "${file}" "${line_number}" "${description}" >&2
    VIOLATIONS=$((VIOLATIONS + 1))
  done <<<"${matches}"
}

readonly EXCHANGE_CREDENTIAL_RE='(?i)(?:\b(?:binance|bybit|exchange|trading)[A-Za-z0-9_.-]*(?:api[_-]?(?:key|secret|token)|secret[_-]?key|private[_-]?key|credentials?|passphrase)[A-Za-z0-9_.-]*\b|\b(?:api[_-]?(?:key|secret|token)|secret[_-]?key|private[_-]?key|credentials?|passphrase)[A-Za-z0-9_.-]*(?:binance|bybit|exchange|trading)[A-Za-z0-9_.-]*\b|\bX-MBX-APIKEY\b|\bX-BAPI-API-KEY\b)'
readonly PRIVATE_ENDPOINT_RE='(?i)(?:\b(?:api[0-9]*\.binance\.com|testnet\.binance\.vision|testnet\.binancefuture\.com|fapi\.binance\.com|dapi\.binance\.com|api(?:-testnet|-demo)?\.bybit\.com|stream(?:-testnet|-demo)?\.bybit\.com)\b|/(?:sapi|fapi|dapi|papi)/|/api/v3/(?:account|order(?:/test)?|openorders|allorders|mytrades|userdata[^/[:space:]]*|listenkey)\b|/v5/(?:order|position|account|asset|user|loan|crypto-loan)\b|/(?:ws|v[0-9]+)/private\b)'
readonly LATER_SANDBOX_RE='(?i)\b(?:testnet|demo)\b'
readonly EXTERNAL_LIVE_MODE_RE='(?i)(?:\b(?:execution[_-]?mode|mode|environment)[[:space:]]*[:=][[:space:]]*[^A-Za-z0-9_]{0,2}live\b|\bmode[_-]?live\b|\blive[_-]?mode\b|\bprofiles?\b[[:space:]]*:[^\r\n]*\blive\b|^[[:space:]]*[A-Za-z0-9_.-]*(?:engine|broker|trader)[A-Za-z0-9_.-]*live[A-Za-z0-9_.-]*[[:space:]]*:)'
readonly SIGNER_INTERFACE_RE='(?i)(?:\b(?:type[[:space:]]+|interface[[:space:]]+)(?:Signer|RequestSigner|ExchangeSigner|APISigner|SignedTransport|AuthenticatedTransport|AuthenticatedClient|AuthenticatedExchangeClient|OrderBroker|OrderClient|ExternalOrderBroker|ProductionBroker)\b|\b(?:SignRequest|SignExchangeRequest|RequestSigner|ExchangeSigner|APISigner|SignedTransport|AuthenticatedTransport|AuthenticatedExchangeClient|ExternalOrderBroker|ProductionBroker)\b)'
readonly GLOBAL_EXTERNAL_ORDER_RE='(?i)\b(?:PlaceOrder|SubmitOrder|SendOrder|PlaceExternalOrder|SubmitExternalOrder|PlaceProductionOrder|SubmitProductionOrder)\b'
readonly EXCHANGE_ORDER_RE='(?i)\b(?:PlaceOrder|SubmitOrder|SendOrder|CreateOrder|CancelOrder|AmendOrder|ReplaceOrder|QueryOrder|GetOrder|GetOpenOrders|SubscribePrivateEvents|ListenKey|UserDataStream|AccountClient|PrivateClient|OrderClient|OrderBroker|hmac\.New)\b'
readonly PROHIBITED_IDENTIFIER_RE='(?i)\b(?:margin[_-]?trading|futures?|perpetuals?|options[_-]?trading|leverage|leveraged[_-]?tokens?|borrowing|lending|staking|short[_-]?sell(?:ing)?|withdraw(?:al|als)?|automated[_-]?transfers?|blockchain[_-]?transfers?|production[_-]?(?:orders?|broker)|private[_-]?order[_-]?capability|real[_-]?money[_-]?orders?)\b'
readonly PROHIBITED_LITERAL_RE='(?i)(?:\x22|\x27|\x60)(?:margin|futures?|perpetuals?|options[_ -]?trading|leverage|leveraged[_ -]?tokens?|borrowing|lending|staking|short[_ -]?selling|withdraw(?:al|als)?|automated[_ -]?transfers?|blockchain[_ -]?transfers?|production[_ -]?orders?|private[_ -]?order[_ -]?capability)(?:\x22|\x27|\x60)'
readonly PROHIBITED_CONFIG_KEY_RE='(?i)\b(?:margin|futures?|perpetuals?|options[_-]?trading|leverage|leveraged[_-]?tokens?|borrow(?:ing)?|lend(?:ing)?|staking|short[_-]?sell(?:ing)?|withdraw(?:al|als)?|automated[_-]?transfers?|blockchain[_-]?transfers?|production[_-]?orders?|private[_-]?order[_-]?capability)(?:[_-](?:enabled|mode|credentials?|api[_-]?(?:key|secret)|endpoint|url|host|service|profile|capability))?(?:\x22|\x27)?[[:space:]]*[:=]'
readonly FINANCIAL_FLOAT_RE='(?x)(?:\btype[[:space:]]+[A-Za-z_][A-Za-z0-9_]*(?:\[[^]]+\])?[[:space:]]+float(?:32|64)\b|\b(?:var|const)[[:space:]]+[^=\r\n]*\bfloat(?:32|64)\b|^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*(?:[[:space:]]*,[[:space:]]*[A-Za-z_][A-Za-z0-9_]*)*[[:space:]]+(?:\[\])?float(?:32|64)\b|\bfunc\b[^\r\n{]*(?:\([^)]*\bfloat(?:32|64)\b|\)[[:space:]]*(?:\([^)]*\))?[[:space:]]*float(?:32|64)\b)|:=[^\r\n]*\b(?:\[\])?float(?:32|64)\b)'

run_rule 'exchange-credential-key' \
  'authenticated exchange credential key/reference is forbidden in V1A' \
  "${EXCHANGE_CREDENTIAL_RE}" 'b1-public' ALL_INPUT_GLOBS .
run_rule 'private-endpoint' \
  'private, account, order, or later-release exchange endpoint is forbidden in V1A' \
  "${PRIVATE_ENDPOINT_RE}" 'b1-public' ALL_INPUT_GLOBS .
run_rule 'later-release-sandbox' \
  'testnet/demo executable input is outside the reviewed V1C boundary' \
  "${LATER_SANDBOX_RE}" 'typed-unsupported' ALL_INPUT_GLOBS .
run_rule 'external-live-mode' \
  'live external execution mode/service is forbidden in every V1 release' \
  "${EXTERNAL_LIVE_MODE_RE}" 'none' ALL_INPUT_GLOBS .
run_rule 'signer-or-order-interface' \
  'exchange signer, authenticated transport, or external order interface is forbidden in V1A' \
  "${SIGNER_INTERFACE_RE}" 'none' ALL_INPUT_GLOBS .
run_rule 'external-order-method' \
  'external order submission method is forbidden in V1A' \
  "${GLOBAL_EXTERNAL_ORDER_RE}" 'none' ALL_INPUT_GLOBS .
run_rule 'prohibited-product' \
  'prohibited product or external capability is present in an executable input' \
  "${PROHIBITED_IDENTIFIER_RE}|${PROHIBITED_LITERAL_RE}" \
  'explicit-or-typed' ALL_INPUT_GLOBS .
run_rule 'prohibited-config-key' \
  'prohibited product/capability configuration key is present, even if disabled' \
  "${PROHIBITED_CONFIG_KEY_RE}" 'none' CONFIG_INPUT_GLOBS .

EXCHANGE_SOURCE_TARGETS=()
for candidate in internal/exchanges internal/exchange pkg/exchanges pkg/exchange src/exchanges src/exchange adapters; do
  if [[ -d "${candidate}" ]]; then
    EXCHANGE_SOURCE_TARGETS+=("${candidate}")
  fi
done
if ((${#EXCHANGE_SOURCE_TARGETS[@]} > 0)); then
  run_rule 'exchange-order-method' \
    'account/private/order/signing method exists at the exchange boundary' \
    "${EXCHANGE_ORDER_RE}" 'none' ALL_INPUT_GLOBS "${EXCHANGE_SOURCE_TARGETS[@]}"
fi

AUTHORITATIVE_GO_TARGETS=()
for candidate in \
  internal/domain internal/config internal/accounting internal/portfolio \
  internal/risk internal/execution internal/simulation internal/reconciliation \
  internal/strategies internal/backtest internal/replay internal/marketdata/orderbook; do
  if [[ -d "${candidate}" ]]; then
    AUTHORITATIVE_GO_TARGETS+=("${candidate}")
  fi
done
if ((${#AUTHORITATIVE_GO_TARGETS[@]} > 0)); then
  run_rule 'financial-float' \
    'binary floating-point declaration is forbidden in authoritative Go packages' \
    "${FINANCIAL_FLOAT_RE}" 'none' GO_INPUT_GLOBS "${AUTHORITATIVE_GO_TARGETS[@]}"
fi

if ((SCAN_ERRORS > 0)); then
  printf 'prohibited-capability scan could not complete: %d scanner error(s)\n' \
    "${SCAN_ERRORS}" >&2
  exit 2
fi

if ((VIOLATIONS > 0)); then
  printf 'prohibited-capability scan failed: %d violation location(s)\n' \
    "${VIOLATIONS}" >&2
  exit 1
fi

printf 'prohibited-capability scan passed\n'
