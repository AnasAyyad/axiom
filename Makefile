SHELL := /usr/bin/env bash

GO ?= go
NODE ?= node
COREPACK ?= corepack
SQLC ?= sqlc
PNPM := $(COREPACK) pnpm
PLATFORM := bin/platform
PLAN_FILE ?= /home/anas/.codex/attachments/7085c3d9-bb74-4587-8af7-85d8e499faf1/pasted-text-1.txt

.DEFAULT_GOAL := help

.PHONY: help preflight deps generate contracts contracts-check docs-check format format-check lint test test-backend test-frontend test-race fuzz-smoke benchmark-a2 benchmark-a3 build build-backend build-frontend compose-validate compose-smoke security-static vulnerability verify dev-api dev-web migrate a4-sqlc a4-postgres-qualify a8-sqlc a8-postgres-qualify a8-local-qualify a9-sqlc a9-postgres-qualify a9-model-qualify a10-sqlc a10-postgres-qualify a10-model-qualify a10-research-qualify a11-sqlc a11-postgres-qualify a11-contract-qualify a11-api-qualify a11-frontend-qualify a11-ui-fixture-qualify a11-e2e-qualify a11-security-qualify b1-model-qualify b1-postgres-qualify b1-adapter-qualify b1-security-qualify b1-local-qualify b1-live-qualify b2-model-qualify b2-postgres-qualify b2-live-qualify b2-local-qualify b3-sqlc b3-model-qualify b3-postgres-qualify b3-research-qualify b3-local-qualify b4-sqlc b4-model-qualify b4-postgres-qualify b4-local-qualify b5-sqlc b5-model-qualify b5-postgres-qualify b5-local-qualify b6-sqlc b6-model-qualify b6-postgres-qualify b6-security-qualify b6-local-qualify b7-sqlc b7-model-qualify b7-postgres-qualify b7-research-qualify b7-local-qualify b8-sqlc b8-model-qualify b8-postgres-qualify b8-api-qualify b8-frontend-qualify b8-security-qualify b8-live-qualify b8-local-qualify image backup-image backup-image-reproducibility image-reproducibility
.PHONY: a7-soak-smoke b1-soak-smoke c1-security-qualify c2-auth-qualify c3-recovery-qualify c4-binance-testnet-qualify c5-bybit-demo-qualify v1c-postgres-qualify v1c-pr1-local-qualify v1c-pr2-local-qualify
.PHONY: c6-api-qualify c6-frontend-qualify c6-security-qualify c6-chaos-qualify c6-soak-smoke c6-soak v1c-pr3-local-qualify
.PHONY: d1-contract-qualify d1-api-qualify d1-postgres-qualify d1-security-qualify v1d-d1-local-qualify
.PHONY: d2-contract-qualify d2-frontend-qualify d2-browser-qualify d2-security-qualify v1d-d2-local-qualify
.PHONY: d3-contract-qualify d3-api-qualify d3-postgres-qualify d3-frontend-qualify d3-browser-qualify d3-security-qualify v1d-d3-local-qualify
.PHONY: d4-contract-qualify d4-api-qualify d4-postgres-qualify d4-frontend-qualify d4-browser-qualify d4-security-qualify v1d-d4-local-qualify
.PHONY: d5-model-qualify d5-backup-qualify d5-postgres-qualify d5-hardening-qualify d5-chaos-qualify d5-soak-smoke d5-security-qualify d5-readiness v1d-d5-local-qualify
.PHONY: d6-certification-model-qualify d6-traceability-qualify d6-security-qualify d6-final-certification v1d-d6-local-qualify

IMAGE ?= axiom:local
BACKUP_IMAGE ?= axiom-backup:local
REBUILD_IMAGE ?= $(IMAGE)-rebuild
VULNDB ?= https://vuln.go.dev
VERSION ?= dev
COMMIT ?= unknown
BUILT_AT ?= unknown
DIRTY ?= true

help: ## List stable repository commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

preflight: ## Verify exact toolchains and required local commands.
	@GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)" scripts/preflight.sh

deps: ## Install exact locked Go and pnpm dependencies.
	@$(GO) mod download
	@$(PNPM) install --frozen-lockfile

generate: contracts build-frontend ## Generate contracts and embedded frontend assets.
	@$(NODE) scripts/embed-web-assets.mjs

contracts: ## Generate Go and TypeScript models from OpenAPI.
	@$(GO) tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
	@$(PNPM) contracts

contracts-check: ## Prove generated OpenAPI models are current.
	@GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)" scripts/check-generated.sh

semantic-naming-check: ## Reject newly introduced delivery-stage terminology in active product surfaces.
	@$(NODE) scripts/check-semantic-naming.mjs

docs-check: ## Validate local documentation links and requirement-matrix consistency.
	@$(NODE) scripts/check-doc-links.mjs
	@$(NODE) scripts/check-a0-traceability.mjs $(if $(wildcard $(PLAN_FILE)),$(PLAN_FILE))
	@$(NODE) scripts/check-a2-config-reference.mjs
	@$(NODE) scripts/check-a3-runtime-boundary.mjs
	@$(NODE) scripts/check-a4-storage-boundary.mjs
	@$(NODE) scripts/check-a5-observability-boundary.mjs
	@$(NODE) scripts/check-a6-exchange-boundary.mjs
	@$(NODE) scripts/check-a7-public-boundary.mjs
	@$(NODE) scripts/check-b1-public-boundary.mjs
	@$(NODE) scripts/check-b3-strategy-boundary.mjs
	@$(NODE) scripts/check-b4-strategy-boundary.mjs
	@$(NODE) scripts/check-b5-strategy-boundary.mjs
	@$(NODE) scripts/check-b6-rebalancing-boundary.mjs
	@$(NODE) scripts/check-b7-research-boundary.mjs
	@$(NODE) scripts/check-a10-strategy-boundary.mjs
	@$(NODE) scripts/check-a11-console-boundary.mjs
	@$(NODE) scripts/check-v1c-pr3-boundary.mjs
	@$(NODE) scripts/check-v1d-d1-boundary.mjs
	@$(NODE) scripts/check-v1d-d2-boundary.mjs
	@$(NODE) scripts/check-v1d-d3-boundary.mjs
	@$(NODE) scripts/check-v1d-d4-boundary.mjs
	@$(NODE) scripts/check-v1d-d5-boundary.mjs
	@$(NODE) scripts/check-v1d-d6-boundary.mjs

format: ## Format owned Go, JavaScript, TypeScript, CSS, JSON, and YAML.
	@$(GO) fmt ./...
	@$(PNPM) format

format-check: ## Reject formatting drift without modifying source.
	@GO="$(GO)" COREPACK="$(COREPACK)" scripts/check-format.sh

lint: ## Run Go vet/staticcheck, frontend ESLint, and source policy checks.
	@$(GO) vet ./...
	@$(GO) tool staticcheck ./...
	@$(PNPM) lint
	@$(GO) run scripts/check_go_policy.go
	@scripts/check-file-policy.sh

test: test-backend test-frontend ## Run focused backend and frontend unit tests.

test-backend: ## Run all Go unit and table-driven tests.
	@$(GO) test ./...

test-frontend: ## Run Vitest, React Testing Library, and axe smoke tests.
	@$(PNPM) test

test-race: ## Run the Go race detector across the skeleton.
	@$(GO) test -race ./...

fuzz-smoke: ## Run required execution-mode and financial parsing fuzz targets briefly.
	@$(GO) test ./internal/config -run '^$$' -fuzz '^FuzzParseExecutionMode$$' -fuzztime 3s
	@$(GO) test ./internal/config -run '^$$' -fuzz '^FuzzDecodeConfiguration$$' -fuzztime 3s
	@$(GO) test ./internal/domain -run '^$$' -fuzz '^FuzzParseFinancial$$' -fuzztime 3s
	@$(GO) test ./internal/runtime -run '^$$' -fuzz '^FuzzReplayOrdering$$' -fuzztime 3s
	@$(GO) test ./internal/exchanges/binance -run '^$$' -fuzz '^FuzzNormalizePublicPayload$$' -fuzztime 3s
	@$(GO) test ./internal/exchanges/binance -run '^$$' -fuzz '^FuzzBinanceAuthenticatedCreatePolicy$$' -fuzztime 3s
	@$(GO) test ./internal/exchanges/bybit -run '^$$' -fuzz '^FuzzBybitAuthenticatedCreatePolicy$$' -fuzztime 3s
	@$(GO) test ./internal/sandbox -run '^$$' -fuzz '^FuzzPrivateEventContract$$' -fuzztime 3s

benchmark-a2: ## Measure exact decimal arithmetic with allocation reporting.
	@$(GO) test ./internal/domain -run '^$$' -bench '^BenchmarkFinancialArithmetic$$' -benchmem -count 5

benchmark-a3: ## Measure deterministic scheduler overhead with allocation reporting.
	@$(GO) test ./internal/runtime -run '^$$' -bench '^BenchmarkDeterministicScheduler$$' -benchmem -count 5

build: generate build-backend ## Build the embedded React/platform artifact.

build-backend: ## Build the single platform binary.
	@mkdir -p bin
	@CGO_ENABLED=0 $(GO) build -trimpath -o $(PLATFORM) ./cmd/platform

build-frontend: ## Type-check and build the React application.
	@$(PNPM) typecheck
	@$(PNPM) build

compose-validate: ## Render every active Compose profile combination safely.
	@scripts/check-compose.sh
	@tests/integration/check-unavailable-profiles.sh

compose-smoke: ## Start the image-backed A1 app, recorder, and worker profiles.
	@GO="$(GO)" tests/integration/smoke-compose-app.sh "$(IMAGE)"

security-static: ## Run secret and prohibited-capability scans with negative tests.
	@scripts/check-secret-patterns.sh
	@scripts/test-check-secret-patterns.sh
	@scripts/check-prohibited-capabilities.sh
	@scripts/test-check-prohibited-capabilities.sh
	@GO="$(GO)" scripts/check-a6-binary-boundary.sh
	@GO="$(GO)" scripts/check-a7-binary-boundary.sh
	@scripts/check-v1c-security-boundary.sh

c1-security-qualify: ## Prove the closed C1 credential, signer, endpoint, proxy, evidence, and emulator boundary.
	@$(GO) test ./internal/config ./internal/security ./internal/egressproxy \
		./internal/exchanges/contracts ./internal/exchanges/binance \
		./internal/exchanges/bybit ./internal/exchanges/sandboxemulator ./internal/sandbox -count=1
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres \
		-run '^(TestV1CMigrationsDefineClosedDurableAuthenticatedEvidence|TestV1CEngineGrantIncludesOnlyClosedExecutionTables)$$' \
		-count=1
	@scripts/check-v1c-security-boundary.sh

c2-auth-qualify: ## Exercise C2 password/TOTP, replay, one-use authorization, RBAC, audit, session, and rotation models.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/authentication ./internal/sandbox ./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/authentication ./internal/sandbox -count=1

c3-recovery-qualify: ## Exercise C3 atomic caps, durable dispatch, fencing, inbox/reducer, startup, and crash recovery.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/sandbox ./internal/execution ./internal/reconciliation \
		./internal/runtime ./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/sandbox ./internal/execution ./internal/reconciliation ./internal/runtime -count=1

c4-binance-testnet-qualify: ## Prove the complete closed Binance Spot Testnet adapter and recovery behavior.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/exchanges/contracts ./internal/exchanges/binance \
		./internal/exchanges/sandboxemulator ./internal/sandbox ./internal/execution \
		./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/exchanges/binance ./internal/exchanges/sandboxemulator \
		./internal/sandbox ./internal/execution -count=1
	@$(GO) test ./internal/exchanges/binance -run '^$$' \
		-fuzz '^FuzzBinancePrivateEventDecoder$$' -fuzztime 3s
	@scripts/check-v1c-security-boundary.sh

c5-bybit-demo-qualify: ## Prove the complete closed Bybit Demo Spot adapter and recovery behavior.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/config ./internal/bootstrap ./internal/egressproxy \
		./internal/exchanges/contracts ./internal/exchanges/bybit \
		./internal/exchanges/sandboxemulator ./internal/sandbox ./internal/execution \
		./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/bootstrap ./internal/exchanges/bybit \
		./internal/exchanges/sandboxemulator ./internal/sandbox ./internal/execution \
		-count=1
	@$(GO) test ./internal/exchanges/bybit -run '^$$' \
		-fuzz '^FuzzBybitDemoPrivateDecoder$$' -fuzztime 3s
	@scripts/check-v1c-security-boundary.sh

v1c-postgres-qualify: ## Run V1C clean-install and exact B8-upgrade qualification on dedicated PostgreSQL 18 databases.
	@test -n "$(AXIOM_V1C_TEST_DSN)" || { echo "AXIOM_V1C_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_V1C_UPGRADE_TEST_DSN)" || { echo "AXIOM_V1C_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_V1C_TEST_DSN="$(AXIOM_V1C_TEST_DSN)" \
		AXIOM_V1C_UPGRADE_TEST_DSN="$(AXIOM_V1C_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestV1CPostgres(CleanInstall|B8ToV1CUpgrade)Qualification$$' -count=1 -v

v1c-pr1-local-qualify: c1-security-qualify c2-auth-qualify c3-recovery-qualify v1c-postgres-qualify ## Pass every C1-C3 PR1 phase gate plus cumulative repository verification.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

v1c-pr2-local-qualify: c1-security-qualify c2-auth-qualify c3-recovery-qualify c4-binance-testnet-qualify c5-bybit-demo-qualify v1c-postgres-qualify ## Pass every C1-C5 PR2 phase gate plus cumulative repository verification.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

c6-api-qualify: ## Prove C6 contracts, redacted projections, durable controls, RBAC, and storage boundaries.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/... ./internal/authentication \
			./internal/bootstrap ./internal/storage/postgres -count=1

c6-frontend-qualify: ## Type-check, lint, test, build, and inspect the C6 sandbox console fixtures.
	@$(MAKE) a11-frontend-qualify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@AXIOM_A11_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'C6 sandbox'

c6-security-qualify: ## Prove C6 endpoint, secret, production-target, and prohibited-capability denial.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/authentication ./internal/qualification/c6 \
			./internal/storage/postgres -count=1
	@scripts/check-v1c-security-boundary.sh
	@$(NODE) scripts/check-v1c-pr3-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"

c6-chaos-qualify: ## Exercise deterministic C6 fault, race, reset, reconnect, and recovery scenarios.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/qualification/c6 ./internal/sandbox \
			./internal/exchanges/sandboxemulator ./internal/exchanges/binance \
			./internal/exchanges/bybit ./internal/execution \
			./internal/reconciliation ./internal/bootstrap \
			./internal/storage/postgres -count=1

c6-soak-smoke: ## Run only the short deterministic C6 smoke runner; never grants formal qualification.
	@$(GO) test ./cmd/c6-soak ./internal/qualification/c6 \
		-run '^TestC6' -count=1 -timeout=2m -v

c6-soak: ## MANUAL: run the default-off exact 72-hour observer; requires explicit identity and evidence variables.
	@test "$(AXIOM_C6_SOAK_ENABLED)" = "1" || { echo "AXIOM_C6_SOAK_ENABLED=1 is required" >&2; exit 1; }
	@test "$(AXIOM_C6_SOAK_MODE)" = "formal" || { echo "AXIOM_C6_SOAK_MODE=formal is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_RUN_ID)" || { echo "AXIOM_C6_RUN_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_COMMIT_SHA)" || { echo "AXIOM_C6_COMMIT_SHA is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_BUILD_HASH)" || { echo "AXIOM_C6_BUILD_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_EXECUTABLE_HASH)" || { echo "AXIOM_C6_EXECUTABLE_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_IMAGE_HASH)" || { echo "AXIOM_C6_IMAGE_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_CONFIGURATION_HASH)" || { echo "AXIOM_C6_CONFIGURATION_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_C6_EVIDENCE_PATH)" || { echo "AXIOM_C6_EVIDENCE_PATH is required" >&2; exit 1; }
	@$(GO) run ./cmd/c6-soak

v1c-pr3-local-qualify: c1-security-qualify c2-auth-qualify c3-recovery-qualify c4-binance-testnet-qualify c5-bybit-demo-qualify c6-api-qualify c6-frontend-qualify c6-security-qualify c6-chaos-qualify c6-soak-smoke v1c-postgres-qualify ## Pass every V1C non-soak gate; formal C6 soak remains separate and pending.
	@AXIOM_V1C_TEST_DSN= AXIOM_V1C_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

d1-contract-qualify: ## Prove the compatible generated V1D D1 OpenAPI contract and source catalogue.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-v1d-d1-boundary.mjs

d1-api-qualify: ## Exercise D1 validation, authorization, idempotency, revisions, projections, exports, and streams.
	@AXIOM_D1_TEST_DSN= AXIOM_D1_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/console ./internal/authentication \
			./internal/bootstrap ./internal/storage/postgres -count=1
	@AXIOM_D1_TEST_DSN= AXIOM_D1_UPGRADE_TEST_DSN= \
		$(GO) test -race ./internal/api/console ./internal/authentication -count=1

d1-postgres-qualify: ## Run D1 clean-install and exact V1C-upgrade gates on dedicated PostgreSQL 18 databases.
	@test -n "$(AXIOM_D1_TEST_DSN)" || { echo "AXIOM_D1_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_D1_UPGRADE_TEST_DSN)" || { echo "AXIOM_D1_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_D1_TEST_DSN="$(AXIOM_D1_TEST_DSN)" \
		AXIOM_D1_UPGRADE_TEST_DSN="$(AXIOM_D1_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestV1DD1Postgres(CleanInstall|V1CToD1Upgrade)Qualification$$' -count=1 -v

d1-security-qualify: ## Prove D1 redaction, secret, role, stream, and prohibited-capability boundaries.
	@$(NODE) scripts/check-v1d-d1-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

v1d-d1-local-qualify: d1-contract-qualify d1-api-qualify d1-postgres-qualify d1-security-qualify ## Pass every D1 implementation gate; merge and formal cumulative acceptance remain separate.

d2-contract-qualify: ## Prove the D2 browser consumes the compatible generated D1 contract.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-v1d-d1-boundary.mjs
	@$(NODE) scripts/check-v1d-d2-boundary.mjs

d2-frontend-qualify: ## Type-check, lint, test, build, and inspect the accessible D2 command center.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build
	@$(NODE) scripts/check-v1d-d2-boundary.mjs

d2-browser-qualify: ## Run D2 workflows in Chromium, Firefox, WebKit, tablet, and mobile fixtures.
	@AXIOM_A11_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'D2 command center'

d2-security-qualify: ## Prove D2 has no arbitrary execution surface or forbidden V1 capability.
	@$(NODE) scripts/check-v1d-d2-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

v1d-d2-local-qualify: d2-contract-qualify d2-frontend-qualify d2-browser-qualify d2-security-qualify ## Pass every local D2 implementation gate; merge and cumulative acceptance remain separate.

d3-contract-qualify: ## Prove D3 uses compatible generated lab and evidence contracts.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-v1d-d1-boundary.mjs
	@$(NODE) scripts/check-v1d-d2-boundary.mjs
	@$(NODE) scripts/check-v1d-d3-boundary.mjs

d3-api-qualify: ## Exercise D3 authorization, lifecycle, manifest, replay, export, and shadow projections.
	@AXIOM_A11_TEST_DSN= $(GO) test ./internal/api/... ./internal/replay ./internal/backtest ./internal/storage/postgres -count=1

d3-postgres-qualify: ## Run D3 durable lifecycle and evidence projections against PostgreSQL 18.
	@test -n "$(AXIOM_A11_TEST_DSN)" || { echo "AXIOM_A11_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_A11_TEST_DSN="$(AXIOM_A11_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestA11PostgresAuthenticationCommandsAndConsoleQualification$$' -count=1 -v

d3-frontend-qualify: ## Type-check, lint, test, build, and inspect the complete D3 laboratories.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build
	@$(NODE) scripts/check-v1d-d3-boundary.mjs

d3-browser-qualify: ## Run D3 lab workflows in Chromium, Firefox, WebKit, tablet, and mobile fixtures.
	@AXIOM_A11_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'D3 labs'

d3-security-qualify: ## Prove D3 preserves redaction and every forbidden V1 capability boundary.
	@$(NODE) scripts/check-v1d-d3-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

v1d-d3-local-qualify: d3-contract-qualify d3-api-qualify d3-postgres-qualify d3-frontend-qualify d3-browser-qualify d3-security-qualify ## Pass every local D3 implementation gate; merge and cumulative acceptance remain separate.

d4-contract-qualify: ## Prove D4 uses compatible generated operational-evidence contracts.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-v1d-d1-boundary.mjs
	@$(NODE) scripts/check-v1d-d2-boundary.mjs
	@$(NODE) scripts/check-v1d-d3-boundary.mjs
	@$(NODE) scripts/check-v1d-d4-boundary.mjs

d4-api-qualify: ## Exercise D4 authorization, schedules, reports, incidents, alerts, audit, and artifacts.
	@AXIOM_D4_TEST_DSN= AXIOM_D4_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/... ./internal/alerting ./internal/reporting \
			./internal/storage/postgres ./internal/bootstrap -count=1
	@$(GO) test -race ./internal/api/console ./internal/alerting ./internal/reporting -count=1

d4-postgres-qualify: ## Run D4 clean-install and exact D1-to-D4 upgrade gates on dedicated PostgreSQL 18 databases.
	@test -n "$(AXIOM_D4_TEST_DSN)" || { echo "AXIOM_D4_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_D4_UPGRADE_TEST_DSN)" || { echo "AXIOM_D4_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_D4_TEST_DSN="$(AXIOM_D4_TEST_DSN)" \
		AXIOM_D4_UPGRADE_TEST_DSN="$(AXIOM_D4_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestV1DD4Postgres(OperationalEvidence|D1ToD4Upgrade)Qualification$$' -count=1 -v

d4-frontend-qualify: ## Type-check, lint, test, build, and inspect the D4 operational workflows.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build
	@$(NODE) scripts/check-v1d-d4-boundary.mjs

d4-browser-qualify: ## Run D4 workflows in Chromium, Firefox, WebKit, tablet, and mobile fixtures.
	@AXIOM_A11_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'D4 operational evidence workflows'

d4-security-qualify: ## Prove D4 redaction, audit, hold, outbound, role, and prohibited-capability boundaries.
	@$(NODE) scripts/check-v1d-d4-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

v1d-d4-local-qualify: d4-contract-qualify d4-api-qualify d4-postgres-qualify d4-frontend-qualify d4-browser-qualify d4-security-qualify ## Pass every local D4 implementation gate; merge and cumulative acceptance remain separate.

d5-model-qualify: ## Exercise D5 pressure, retention, lifecycle, runner, and fail-closed runtime models.
	@AXIOM_D5_TEST_DSN= AXIOM_D5_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/pressure ./internal/storage/segments \
			./internal/qualification/d5 ./internal/config ./internal/bootstrap \
			./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/storage/pressure ./internal/qualification/d5 \
		./internal/bootstrap -count=1

d5-backup-qualify: ## Prove independent mount rejection, encryption, retention, and authenticated restore evidence.
	@$(GO) test ./internal/backup ./cmd/storage-backup -count=1
	@$(GO) test -race ./internal/backup -count=1

d5-postgres-qualify: ## Run D5 clean-install and exact D4-to-D5 upgrade gates on PostgreSQL 18.
	@test -n "$(AXIOM_D5_TEST_DSN)" || { echo "AXIOM_D5_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_UPGRADE_TEST_DSN)" || { echo "AXIOM_D5_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_D5_TEST_DSN="$(AXIOM_D5_TEST_DSN)" \
		AXIOM_D5_UPGRADE_TEST_DSN="$(AXIOM_D5_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestV1DD5Postgres(OperationalReadiness|D4ToD5Upgrade)Qualification$$' -count=1 -v

d5-hardening-qualify: ## Validate D5 Compose retention, remote backup, digest, and lifecycle boundaries.
	@$(NODE) scripts/check-v1d-d5-boundary.mjs
	@scripts/check-compose.sh

d5-chaos-qualify: ## Exercise terminal failure, no-replace evidence, races, restart, and kill-point models.
	@$(GO) test ./internal/qualification/d5 ./internal/backup ./internal/storage/pressure \
		./internal/execution ./internal/reconciliation ./internal/sandbox -count=1

d5-soak-smoke: ## Run only deterministic D5 smoke; output is always non-qualifying.
	@$(GO) test ./internal/qualification/d5 -run '^(TestSmokeRunner|TestRunnerFails|TestFileStore)' \
		-count=1 -timeout=2m -v

d5-security-qualify: ## Prove observation-only D5 code, secret redaction, and prohibited-capability denial.
	@$(NODE) scripts/check-v1d-d5-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

d5-readiness: ## MANUAL: run the default-off exact seven-day D5 observer on the approved server.
	@test "$(AXIOM_D5_READINESS_ENABLED)" = "1" || { echo "AXIOM_D5_READINESS_ENABLED=1 is required" >&2; exit 1; }
	@test "$(AXIOM_D5_MODE)" = "formal" || { echo "AXIOM_D5_MODE=formal is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_RUN_FILE)" || { echo "AXIOM_D5_RUN_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_TEST_MANIFEST_FILE)" || { echo "AXIOM_D5_TEST_MANIFEST_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_FAULT_SCHEDULE_FILE)" || { echo "AXIOM_D5_FAULT_SCHEDULE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_PREFLIGHT_FILE)" || { echo "AXIOM_D5_PREFLIGHT_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_SAMPLE_FILE)" || { echo "AXIOM_D5_SAMPLE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_FAULT_EVIDENCE_FILE)" || { echo "AXIOM_D5_FAULT_EVIDENCE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D5_SIGNING_KEY_FILE)" || { echo "AXIOM_D5_SIGNING_KEY_FILE is required" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "formal D5 requires a clean exact source" >&2; exit 1; }
	@$(GO) run -ldflags "-X axiom/internal/buildinfo.Commit=$$(git rev-parse HEAD) -X axiom/internal/buildinfo.Dirty=false" ./cmd/d5-readiness

v1d-d5-local-qualify: d5-model-qualify d5-backup-qualify d5-postgres-qualify d5-hardening-qualify d5-chaos-qualify d5-soak-smoke d5-security-qualify ## Pass local D5 implementation gates; the reference-server seven-day verdict remains separate.

d6-certification-model-qualify: ## Exercise exact-identity, signature, expiry, tamper, duplicate, and fail-closed certification rules.
	@$(GO) test ./internal/certification ./cmd/d6-certify -count=1
	@$(GO) test -race ./internal/certification -count=1

d6-traceability-qualify: ## Validate D6 boundaries, complete documentation paths, and all 22 Section 35 dispositions.
	@$(NODE) scripts/check-v1d-d6-boundary.mjs
	@$(NODE) scripts/check-doc-links.mjs

d6-security-qualify: d6-certification-model-qualify d6-traceability-qualify ## Re-run V1 capability, secret, binary, and D6 release-input controls.
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(GO) test ./internal/exchanges/binance ./internal/exchanges/bybit \
		./internal/exchanges/sandboxemulator ./internal/egressproxy -count=1

d6-final-certification: ## MANUAL: issue a signed verdict only from complete current formal evidence; default-off and expected to reject today.
	@test "$(AXIOM_D6_FINAL_CERTIFICATION_ENABLED)" = "1" || { echo "AXIOM_D6_FINAL_CERTIFICATION_ENABLED=1 is required" >&2; exit 1; }
	@test -n "$(AXIOM_D6_CANDIDATE_FILE)" || { echo "AXIOM_D6_CANDIDATE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D6_TRUSTED_REVIEWERS_FILE)" || { echo "AXIOM_D6_TRUSTED_REVIEWERS_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D6_RELEASE_SIGNING_KEY_FILE)" || { echo "AXIOM_D6_RELEASE_SIGNING_KEY_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_D6_VERDICT_DIRECTORY)" || { echo "AXIOM_D6_VERDICT_DIRECTORY is required" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "final certification requires a clean exact source" >&2; exit 1; }
	@$(GO) run -ldflags "-X axiom/internal/buildinfo.Commit=$$(git rev-parse HEAD) -X axiom/internal/buildinfo.Dirty=false" ./cmd/d6-certify

v1d-d6-local-qualify: d6-security-qualify d1-contract-qualify d1-api-qualify d1-security-qualify d2-contract-qualify d2-frontend-qualify d2-browser-qualify d2-security-qualify d3-contract-qualify d3-api-qualify d3-frontend-qualify d3-browser-qualify d3-security-qualify d4-contract-qualify d4-api-qualify d4-frontend-qualify d4-browser-qualify d4-security-qualify d5-model-qualify d5-backup-qualify d5-hardening-qualify d5-chaos-qualify d5-security-qualify c1-security-qualify c2-auth-qualify c3-recovery-qualify c4-binance-testnet-qualify c5-bybit-demo-qualify c6-api-qualify c6-frontend-qualify c6-security-qualify c6-chaos-qualify ## Pass repository-verifiable D6 checks without invoking any formal or smoke soak target.
	@$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

vulnerability: ## Scan the Go dependency graph for known vulnerabilities.
	@$(GO) tool govulncheck -db "$(VULNDB)" ./...

verify: preflight format-check contracts-check docs-check lint test test-race fuzz-smoke build compose-validate security-static vulnerability ## Run the complete local A1 quality gate.

dev-api: ## Run the local API health application.
	@$(GO) run ./cmd/platform api

dev-web: ## Run Vite with the API proxy.
	@$(PNPM) --filter @axiom/web dev

migrate: ## Run the exact A1 migration command surface.
	@$(GO) run ./cmd/platform admin migrate

a4-sqlc: ## Generate and compile the reviewed A4 PostgreSQL queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_A4_TEST_DSN= $(GO) test ./internal/storage/postgres/...

a4-postgres-qualify: ## Run the destructive A4 gate against a dedicated *_a4_test database.
	@test -n "$(AXIOM_A4_TEST_DSN)" || { echo "AXIOM_A4_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) a4-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_A4_TEST_DSN="$(AXIOM_A4_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestA4PostgresMigrationJournalAndReservationIntegration$$' -count=1 -v

a7-soak-smoke: ## Run the 20-second two-instrument Binance forensic soak harness.
	@test -n "$(AXIOM_A7_SOURCE_COMMIT)" || { echo "AXIOM_A7_SOURCE_COMMIT is required" >&2; exit 1; }
	@AXIOM_A7_SOAK_SMOKE=1 AXIOM_A7_SOURCE_COMMIT="$(AXIOM_A7_SOURCE_COMMIT)" \
		$(GO) test ./internal/qualification -run '^TestA7PublicSoakHarnessSmoke$$' -count=1 -timeout=2m -v

a8-sqlc: ## Generate and compile the reviewed A8 PostgreSQL queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_A8_TEST_DSN= $(GO) test ./internal/storage/postgres/...

a8-postgres-qualify: ## Run the A8 atomic repository gate against a dedicated *_a8_test database.
	@test -n "$(AXIOM_A8_TEST_DSN)" || { echo "AXIOM_A8_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) a8-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_A8_TEST_DSN="$(AXIOM_A8_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestA8PostgresAtomicOrderFillJournalCheckpoint$$' -count=1 -v

a8-local-qualify: ## Verify and stream the ignored A7 engineering recordings without exporting payloads.
	@AXIOM_A8_DATASET_43_ROOT=$(CURDIR)/.local/a7-soak-a641cd4 \
		AXIOM_A8_DATASET_R2_ROOT=$(CURDIR)/.local/a7-soak-a641cd4-r2 \
		$(GO) test ./internal/backtest -run '^TestA8IgnoredLocalDatasetQualification$$' -count=1 -v

a9-sqlc: ## Generate and compile the reviewed A9 PostgreSQL queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_A9_TEST_DSN= $(GO) test ./internal/storage/postgres/...

a9-postgres-qualify: ## Run the A9 ownership/risk/recovery gate against a dedicated *_a9_test database.
	@test -n "$(AXIOM_A9_TEST_DSN)" || { echo "AXIOM_A9_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) a9-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_A9_TEST_DSN="$(AXIOM_A9_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestA9PostgresPortfolioRiskRecoveryQualification$$' -count=1 -v

a9-model-qualify: ## Exercise exact A9 portfolio, risk, reconciliation, and shared A8 pipeline models.
	@$(GO) test ./internal/portfolio ./internal/risk ./internal/reconciliation -count=1
	@$(GO) test ./internal/backtest -run '^TestA9.*Pipeline.*$$' -count=1 -v

a10-sqlc: ## Generate and compile the reviewed A10 Trend and research queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_A10_TEST_DSN= $(GO) test ./internal/storage/postgres/...

a10-postgres-qualify: ## Run the A10 immutable research gate against a dedicated *_a10_test database.
	@test -n "$(AXIOM_A10_TEST_DSN)" || { echo "AXIOM_A10_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) a10-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_A10_TEST_DSN="$(AXIOM_A10_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestA10PostgresTrendResearchQualification$$' -count=1 -v

a10-model-qualify: ## Exercise exact Trend decisions through the shared allocator/risk pipeline.
	@$(GO) test ./internal/strategies/trend -count=1 -v
	@$(GO) test ./internal/backtest -count=1
	@$(NODE) scripts/check-a10-strategy-boundary.mjs

a10-research-qualify: ## Verify deterministic Go research and the independent locked Python checker.
	@python3 -c 'import sys; assert sys.version_info[:3] == (3, 12, 3), sys.version'
	@PYTHONPATH=research/src python3 -m unittest discover -s research/tests
	@$(GO) test ./internal/research -count=1 -v

a11-sqlc: ## Generate and compile reviewed A11 authentication and console queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_A11_TEST_DSN= $(GO) test ./internal/storage/postgres/...

a11-postgres-qualify: ## Run A11 auth, command, projection, stream, and immutability qualification.
	@test -n "$(AXIOM_A11_TEST_DSN)" || { echo "AXIOM_A11_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) a11-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_A11_TEST_DSN="$(AXIOM_A11_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestA11PostgresAuthenticationCommandsAndConsoleQualification$$' -count=1 -v

a11-contract-qualify: ## Prove exact OpenAPI operations, generated models, and boundary ownership.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-a11-console-boundary.mjs
	@$(GO) test ./internal/api/... -count=1

a11-api-qualify: ## Exercise A11 authentication, authorization, API, bootstrap, and storage policy.
	@$(GO) test ./internal/authentication ./internal/api/... ./internal/bootstrap ./internal/config -count=1

a11-frontend-qualify: ## Type-check, lint, test, and build the routed accessible console.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build

a11-ui-fixture-qualify: ## Run deterministic desktop/mobile UI coverage with contract-shaped fixtures.
	@AXIOM_A11_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e

a11-e2e-qualify: ## Run the unmocked authenticated workflow against a clean integrated A11 environment.
	@test -n "$(AXIOM_A11_E2E_BASE_URL)" || { echo "AXIOM_A11_E2E_BASE_URL is required" >&2; exit 1; }
	@test -n "$(AXIOM_A11_E2E_CONFIGURATION_ID)" || { echo "AXIOM_A11_E2E_CONFIGURATION_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_A11_E2E_DATASET_ID)" || { echo "AXIOM_A11_E2E_DATASET_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_A11_E2E_RESEARCH_GENERATION_ID)" || { echo "AXIOM_A11_E2E_RESEARCH_GENERATION_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_A11_E2E_PORTFOLIO_ID)" || { echo "AXIOM_A11_E2E_PORTFOLIO_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_A11_E2E_EVIDENCE_SHADOW_ID)" || { echo "AXIOM_A11_E2E_EVIDENCE_SHADOW_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_A11_E2E_PASSWORD)" || { echo "AXIOM_A11_E2E_PASSWORD is required" >&2; exit 1; }
	@$(PNPM) --filter @axiom/web test:e2e

a11-security-qualify: ## Run A11 ownership checks plus repository secret/capability scans.
	@$(NODE) scripts/check-a11-console-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"

b1-model-qualify: ## Exercise common public contracts, Bybit semantics, local books, and recorder linkage.
	@$(GO) test ./internal/exchanges/contracts ./internal/exchanges/binance ./internal/exchanges/bybit ./internal/exchanges/emulator ./internal/marketdata ./internal/recorder -count=1

b1-postgres-qualify: ## Run clean-install and V1A-upgrade B1 gates on PostgreSQL 18 *_b1_test databases.
	@test -n "$(AXIOM_B1_TEST_DSN)" || { echo "AXIOM_B1_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B1_UPGRADE_TEST_DSN)" || { echo "AXIOM_B1_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_B1_TEST_DSN="$(AXIOM_B1_TEST_DSN)" \
		AXIOM_B1_UPGRADE_TEST_DSN="$(AXIOM_B1_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres -run '^TestB1Postgres(CleanInstall|V1AToB1Upgrade)Qualification$$' -count=1 -v

b1-adapter-qualify: ## Run Bybit normalization, endpoint, lifecycle, conformance, and fuzz qualification.
	@$(GO) test ./internal/exchanges/bybit -count=1 -v
	@$(GO) test ./internal/exchanges/bybit -run '^$$' -fuzz '^FuzzNormalizeBybitPublicStream$$' -fuzztime 3s

b1-security-qualify: ## Prove B1 remains credential-free, public-only, and free of order/transfer methods.
	@$(NODE) scripts/check-b1-public-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"

b1-local-qualify: b1-model-qualify b1-postgres-qualify b1-adapter-qualify b1-security-qualify verify ## Pass every non-live B1 phase gate cumulatively.

b1-live-qualify: ## Run explicitly enabled short Bybit production-public qualification.
	@test "$(AXIOM_B1_LIVE_PUBLIC)" = "1" || { echo "AXIOM_B1_LIVE_PUBLIC=1 is required" >&2; exit 1; }
	@AXIOM_B1_LIVE_PUBLIC=1 $(GO) test ./internal/exchanges/bybit \
		-run '^TestProductionPublicBybit(Surface|WebSocketRecording|RecorderManifest)$$' -count=1 -v

b1-soak-smoke: ## Run the 20-second two-instrument Bybit forensic soak harness.
	@test -n "$(AXIOM_B1_SOURCE_COMMIT)" || { echo "AXIOM_B1_SOURCE_COMMIT is required" >&2; exit 1; }
	@AXIOM_B1_SOAK_SMOKE=1 AXIOM_B1_SOURCE_COMMIT="$(AXIOM_B1_SOURCE_COMMIT)" \
		$(GO) test ./internal/qualification -run '^TestB1PublicSoakHarnessSmoke$$' -count=1 -timeout=2m -v

b2-model-qualify: ## Exercise B2 clocks, book evidence, deterministic joins, recovery, and Tier-A manifests.
	@$(GO) test ./internal/exchanges/contracts ./internal/exchanges/binance ./internal/exchanges/bybit \
		./internal/marketdata ./internal/runtime ./internal/recorder ./internal/qualification -count=1

b2-postgres-qualify: ## Run clean-install and B1-upgrade B2 gates on PostgreSQL 18 *_b2_test databases.
	@test -n "$(AXIOM_B2_TEST_DSN)" || { echo "AXIOM_B2_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B2_UPGRADE_TEST_DSN)" || { echo "AXIOM_B2_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_B2_TEST_DSN="$(AXIOM_B2_TEST_DSN)" \
		AXIOM_B2_UPGRADE_TEST_DSN="$(AXIOM_B2_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres -run '^TestB2Postgres(CleanInstall|B1ToB2Upgrade)Qualification$$' -count=1 -v

b2-live-qualify: ## Run the explicitly enabled short public-only Binance/Bybit coherent-view qualification; no soak.
	@test "$(AXIOM_B2_LIVE_PUBLIC)" = "1" || { echo "AXIOM_B2_LIVE_PUBLIC=1 is required" >&2; exit 1; }
	@AXIOM_B2_LIVE_PUBLIC=1 \
		AXIOM_B2_LIVE_EVIDENCE_ROOT="$(AXIOM_B2_LIVE_EVIDENCE_ROOT)" \
		AXIOM_B2_COLLECTOR_REGION="$(AXIOM_B2_COLLECTOR_REGION)" \
		$(GO) test ./internal/qualification -run '^TestB2ProductionPublicRecordOnlyAndCoherentQualification$$' -count=1 -v

b2-local-qualify: b2-model-qualify b2-postgres-qualify verify ## Pass every non-soak B2 gate cumulatively.

b2-soak-smoke: ## Run the 20-second non-formal six-collector B2 qualification harness.
	@test -n "$(AXIOM_B2_SOURCE_COMMIT)" || { echo "AXIOM_B2_SOURCE_COMMIT is required" >&2; exit 1; }
	@test -n "$(AXIOM_B2_SOAK_OUTPUT)" || { echo "AXIOM_B2_SOAK_OUTPUT is required" >&2; exit 1; }
	@test -n "$(AXIOM_B2_COLLECTOR_REGION)" || { echo "AXIOM_B2_COLLECTOR_REGION is required" >&2; exit 1; }
	@test "$(AXIOM_B2_SOURCE_COMMIT)" = "$$(git rev-parse HEAD)" || { echo "AXIOM_B2_SOURCE_COMMIT must equal committed HEAD" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "B2 smoke requires an exact clean committed source" >&2; exit 1; }
	@AXIOM_B2_SOAK_SMOKE=1 AXIOM_B2_SOURCE_COMMIT="$(AXIOM_B2_SOURCE_COMMIT)" AXIOM_B2_SOAK_OUTPUT="$(AXIOM_B2_SOAK_OUTPUT)" AXIOM_B2_COLLECTOR_REGION="$(AXIOM_B2_COLLECTOR_REGION)" $(GO) test ./internal/qualification -run '^TestB2PublicSoakHarnessSmoke$$' -count=1 -timeout=5m -v

b2-soak-qualify: ## Run the explicit formal 72-hour B2 qualification; never use this target for smoke.
	@test "$(AXIOM_B2_SOAK)" = "1" || { echo "AXIOM_B2_SOAK=1 explicit opt-in is required" >&2; exit 1; }
	@test -n "$(AXIOM_B2_SOURCE_COMMIT)" || { echo "AXIOM_B2_SOURCE_COMMIT is required" >&2; exit 1; }
	@test -n "$(AXIOM_B2_SOAK_OUTPUT)" || { echo "AXIOM_B2_SOAK_OUTPUT is required" >&2; exit 1; }
	@test -n "$(AXIOM_B2_COLLECTOR_REGION)" || { echo "AXIOM_B2_COLLECTOR_REGION is required" >&2; exit 1; }
	@test "$(AXIOM_B2_SOURCE_COMMIT)" = "$$(git rev-parse HEAD)" || { echo "AXIOM_B2_SOURCE_COMMIT must equal committed HEAD" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "formal B2 qualification requires a clean committed source" >&2; exit 1; }
	@AXIOM_B2_SOAK=1 AXIOM_B2_SOURCE_COMMIT="$(AXIOM_B2_SOURCE_COMMIT)" AXIOM_B2_SOAK_OUTPUT="$(AXIOM_B2_SOAK_OUTPUT)" AXIOM_B2_COLLECTOR_REGION="$(AXIOM_B2_COLLECTOR_REGION)" $(GO) test ./internal/qualification -run '^TestB2Continuous72HourPublicSoak$$' -count=1 -timeout=73h -v

b3-sqlc: ## Generate and compile the reviewed B3 mean-reversion and research queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= $(GO) test ./internal/storage/postgres/...

b3-model-qualify: ## Exercise exact B3 decisions through shared allocation, risk, execution, simulation, and accounting.
	@$(GO) test ./internal/strategies/meanreversion ./internal/portfolio ./internal/risk ./internal/backtest -count=1 -v
	@$(GO) test -race ./internal/strategies/meanreversion ./internal/portfolio ./internal/risk -count=1
	@$(NODE) scripts/check-b3-strategy-boundary.mjs

b3-postgres-qualify: ## Run clean-install and B2-upgrade B3 gates on PostgreSQL 18 *_b3_test databases.
	@test -n "$(AXIOM_B3_TEST_DSN)" || { echo "AXIOM_B3_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B3_UPGRADE_TEST_DSN)" || { echo "AXIOM_B3_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) b3-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_B3_TEST_DSN="$(AXIOM_B3_TEST_DSN)" \
		AXIOM_B3_UPGRADE_TEST_DSN="$(AXIOM_B3_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres -run '^TestB3Postgres(CleanInstall|B2ToB3Upgrade)Qualification$$' -count=1 -v

b3-research-qualify: ## Verify separate deterministic B3 research contracts and the independent Python checker.
	@python3 -c 'import sys; assert sys.version_info[:3] == (3, 12, 3), sys.version'
	@PYTHONPATH=research/src python3 -m unittest discover -s research/tests
	@$(GO) test ./internal/research -count=1 -v

b3-local-qualify: b3-model-qualify b3-postgres-qualify b3-research-qualify ## Pass every non-soak B3 phase gate cumulatively.
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

b4-sqlc: ## Generate and compile the reviewed B4 triangular-arbitrage queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

b4-model-qualify: ## Exercise exact B4 evaluation, atomic claims, central risk, sequential recovery, lifetime, and accounting.
	@$(GO) test ./internal/config ./internal/accounting ./internal/execution ./internal/portfolio \
		./internal/risk ./internal/strategies/arbitrage ./internal/strategies/triangular -count=1 -v
	@$(GO) test -race ./internal/portfolio ./internal/execution \
		./internal/strategies/arbitrage ./internal/strategies/triangular -count=1
	@$(GO) test ./internal/strategies/triangular -run '^$$' \
		-bench '^BenchmarkTriangularEvaluator$$' -benchmem -count=1
	@$(GO) test ./internal/strategies/triangular -run '^$$' \
		-fuzz '^FuzzTriangularExactCycles$$' -fuzztime 3s
	@$(NODE) scripts/check-b4-strategy-boundary.mjs

b4-postgres-qualify: ## Run clean-install and exact B3-upgrade B4 gates on PostgreSQL 18 *_b4_test databases.
	@test -n "$(AXIOM_B4_TEST_DSN)" || { echo "AXIOM_B4_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B4_UPGRADE_TEST_DSN)" || { echo "AXIOM_B4_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) b4-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_B4_TEST_DSN="$(AXIOM_B4_TEST_DSN)" \
		AXIOM_B4_UPGRADE_TEST_DSN="$(AXIOM_B4_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestB4Postgres(CleanInstall|B3ToB4Upgrade)Qualification$$' -count=1 -v

b4-local-qualify: b4-model-qualify b4-postgres-qualify ## Pass every non-soak B4 phase gate cumulatively.
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

b5-sqlc: ## Generate and compile the reviewed B5 cross-exchange-arbitrage queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

b5-model-qualify: ## Exercise exact B5 coherent evaluation, closed-cycle economics, atomic claims, concurrent recovery, inventory, and accounting.
	@$(GO) test ./internal/config ./internal/accounting ./internal/execution ./internal/portfolio \
		./internal/risk ./internal/strategies/arbitrage ./internal/strategies/crossarb -count=1 -v
	@$(GO) test -race ./internal/portfolio ./internal/execution \
		./internal/strategies/arbitrage ./internal/strategies/crossarb -count=1
	@$(GO) test ./internal/strategies/crossarb -run '^$$' \
		-bench '^BenchmarkCrossExchangeEvaluator$$' -benchmem -count=1
	@$(GO) test ./internal/strategies/crossarb -run '^$$' \
		-fuzz '^FuzzCrossExchangeClosedCycle$$' -fuzztime 3s
	@$(NODE) scripts/check-b5-strategy-boundary.mjs

b5-postgres-qualify: ## Run clean-install and exact B4-upgrade B5 gates on PostgreSQL 18 *_b5_test databases.
	@test -n "$(AXIOM_B5_TEST_DSN)" || { echo "AXIOM_B5_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B5_UPGRADE_TEST_DSN)" || { echo "AXIOM_B5_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) b5-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_B5_TEST_DSN="$(AXIOM_B5_TEST_DSN)" \
		AXIOM_B5_UPGRADE_TEST_DSN="$(AXIOM_B5_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestB5Postgres(CleanInstall|B4ToB5Upgrade)Qualification$$' -count=1 -v

b5-local-qualify: b4-model-qualify b4-postgres-qualify b5-model-qualify b5-postgres-qualify ## Pass every non-soak B4 and B5 phase gate cumulatively.
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

b6-sqlc: ## Generate and compile the reviewed B6 advisory-rebalancing queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

b6-model-qualify: ## Exercise reviewed facts, exact route costs, natural reversal, deterministic search, and advisory evidence.
	@$(GO) test ./internal/config ./internal/rebalancing ./internal/portfolio -count=1 -v
	@$(GO) test -race ./internal/rebalancing ./internal/portfolio -count=1
	@$(GO) test ./internal/rebalancing -run '^$$' \
		-bench '^BenchmarkAdvisoryOptimizer$$' -benchmem -count=1
	@$(GO) test ./internal/rebalancing -run '^$$' \
		-fuzz '^FuzzAdvisoryOptimizerPreservesExactNonNegativeCost$$' -fuzztime 3s
	@$(NODE) scripts/check-b6-rebalancing-boundary.mjs

b6-postgres-qualify: ## Run clean-install and exact B5-upgrade B6 gates on PostgreSQL 18 *_b6_test databases.
	@test -n "$(AXIOM_B6_TEST_DSN)" || { echo "AXIOM_B6_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B6_UPGRADE_TEST_DSN)" || { echo "AXIOM_B6_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) b6-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_B6_TEST_DSN="$(AXIOM_B6_TEST_DSN)" \
		AXIOM_B6_UPGRADE_TEST_DSN="$(AXIOM_B6_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestB6Postgres(CleanInstall|B5ToB6Upgrade)Qualification$$' -count=1 -v

b6-security-qualify: ## Prove B6 has no external asset-movement execution surface in source, API, UI, config, or binary.
	@$(NODE) scripts/check-b6-rebalancing-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"
	@$(MAKE) build-backend GO="$(GO)"
	@bash scripts/check-b6-binary-boundary.sh "$(PLATFORM)"

b6-local-qualify: b4-model-qualify b4-postgres-qualify b5-model-qualify b5-postgres-qualify b6-model-qualify b6-postgres-qualify b6-security-qualify ## Pass every non-soak B4, B5, and B6 phase gate cumulatively.
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

b7-sqlc: ## Generate and compile the reviewed B7 research-governance queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

b7-model-qualify: ## Exercise preregistration, locked suites, statistics, evidence eligibility, comparison, and promotion.
	@$(GO) test ./internal/research -count=1 -v
	@$(GO) test -race ./internal/research -count=1
	@$(GO) test ./internal/research -run '^$$' \
		-bench '^BenchmarkB7ValidationSuite$$' -benchmem -count=1
	@$(GO) test ./internal/research -run '^$$' \
		-fuzz '^FuzzB7MultipleTestingPreservesProbabilityBounds$$' -fuzztime 3s
	@$(NODE) scripts/check-b7-research-boundary.mjs

b7-postgres-qualify: ## Run clean-install and exact B6-upgrade B7 gates on PostgreSQL 18 *_b7_test databases.
	@test -n "$(AXIOM_B7_TEST_DSN)" || { echo "AXIOM_B7_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B7_UPGRADE_TEST_DSN)" || { echo "AXIOM_B7_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) b7-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_B7_TEST_DSN="$(AXIOM_B7_TEST_DSN)" \
		AXIOM_B7_UPGRADE_TEST_DSN="$(AXIOM_B7_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestB7Postgres(CleanInstall|B6ToB7Upgrade)Qualification$$' -count=1 -v

b7-research-qualify: ## Independently recalculate B7 statistics and eligibility outside the Go runtime.
	@python3 -c 'import sys; assert sys.version_info[:3] == (3, 12, 3), sys.version'
	@PYTHONPATH=research/src python3 -m unittest discover -s research/tests
	@$(NODE) scripts/check-b7-research-boundary.mjs

b7-local-qualify: b4-model-qualify b4-postgres-qualify b5-model-qualify b5-postgres-qualify b6-model-qualify b6-postgres-qualify b6-security-qualify b7-model-qualify b7-postgres-qualify b7-research-qualify ## Pass every non-soak B4, B5, B6, and B7 phase gate cumulatively.
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

b8-sqlc: ## Generate and compile the reviewed B8 multi-exchange console queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

b8-model-qualify: ## Exercise deterministic replay faults and fail-closed B8 request boundaries.
	@$(GO) test ./internal/replay ./internal/api/console -count=1 -v
	@$(GO) test -race ./internal/replay ./internal/api/console -count=1

b8-postgres-qualify: ## Run clean-install and exact B7-upgrade B8 gates on PostgreSQL 18 *_b8_test databases.
	@test -n "$(AXIOM_B8_TEST_DSN)" || { echo "AXIOM_B8_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_B8_UPGRADE_TEST_DSN)" || { echo "AXIOM_B8_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) b8-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_B8_TEST_DSN="$(AXIOM_B8_TEST_DSN)" \
		AXIOM_B8_UPGRADE_TEST_DSN="$(AXIOM_B8_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestB8Postgres(CleanInstall|B7ToB8Upgrade)Qualification$$' -count=1 -v

b8-api-qualify: ## Verify generated B8 OpenAPI contracts, generic projections, commands, and SSE envelopes.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/console ./internal/storage/postgres \
		-run 'B8|Stream|Cursor|Filter' -count=1 -v
	@$(NODE) scripts/check-b8-console-boundary.mjs

b8-frontend-qualify: ## Typecheck, lint, test, and build the accessible responsive B8 console.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build

b8-security-qualify: ## Prove B8 remains public-data, virtual, advisory, and unable to submit real orders or move assets.
	@$(NODE) scripts/check-b8-console-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"
	@$(MAKE) build-backend GO="$(GO)"
	@bash scripts/check-b6-binary-boundary.sh "$(PLATFORM)"
	@bash scripts/check-b8-binary-boundary.sh "$(PLATFORM)"

b8-live-qualify: ## Verify B8 navigation, responsive layout, keyboard flow, and simulation lock in Chromium.
	@$(PNPM) --filter @axiom/web test:e2e --grep 'B8 multi-exchange'

b8-local-qualify: b4-model-qualify b4-postgres-qualify b5-model-qualify b5-postgres-qualify b6-model-qualify b6-postgres-qualify b6-security-qualify b7-model-qualify b7-postgres-qualify b7-research-qualify b8-model-qualify b8-postgres-qualify b8-api-qualify b8-frontend-qualify b8-security-qualify b8-live-qualify ## Pass every non-soak B4-B8 phase gate cumulatively.
	@AXIOM_B3_TEST_DSN= AXIOM_B3_UPGRADE_TEST_DSN= \
		AXIOM_B4_TEST_DSN= AXIOM_B4_UPGRADE_TEST_DSN= \
		AXIOM_B5_TEST_DSN= AXIOM_B5_UPGRADE_TEST_DSN= \
		AXIOM_B6_TEST_DSN= AXIOM_B6_UPGRADE_TEST_DSN= \
		AXIOM_B7_TEST_DSN= AXIOM_B7_UPGRADE_TEST_DSN= \
		AXIOM_B8_TEST_DSN= AXIOM_B8_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

image: ## Build the pinned minimal Axiom image.
	@docker build --file deploy/docker/Dockerfile --tag "$(IMAGE)" \
		--build-arg "VERSION=$(VERSION)" \
		--build-arg "COMMIT=$(COMMIT)" \
		--build-arg "BUILT_AT=$(BUILT_AT)" \
		--build-arg "DIRTY=$(DIRTY)" .

backup-image: ## Build the pinned PostgreSQL-tooling backup image.
	@docker build --file deploy/backup/Dockerfile --tag "$(BACKUP_IMAGE)" .
	@scripts/inspect-backup-image.sh "$(BACKUP_IMAGE)"

backup-image-reproducibility: backup-image ## Rebuild without layer cache and compare the complete backup runtime payload.
	@scripts/check-backup-image-reproducibility.sh "$(BACKUP_IMAGE)" "$(BACKUP_IMAGE)-rebuild"

image-reproducibility: image ## Rebuild and compare the complete runtime image payload.
	@VERSION="$(VERSION)" COMMIT="$(COMMIT)" BUILT_AT="$(BUILT_AT)" DIRTY="$(DIRTY)" \
		scripts/check-image-reproducibility.sh "$(IMAGE)" "$(REBUILD_IMAGE)"
