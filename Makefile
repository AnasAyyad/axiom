SHELL := /usr/bin/env bash

GO ?= go
NODE ?= node
COREPACK ?= corepack
SQLC ?= sqlc
PNPM := $(COREPACK) pnpm
PLATFORM := bin/platform
PLAN_FILE ?= /home/anas/.codex/attachments/7085c3d9-bb74-4587-8af7-85d8e499faf1/pasted-text-1.txt

.DEFAULT_GOAL := help

.PHONY: help preflight deps generate contracts contracts-check docs-check format format-check lint test test-backend test-frontend test-race fuzz-smoke benchmark-a2 benchmark-a3 build build-backend build-frontend compose-validate compose-smoke security-static vulnerability verify dev-api dev-web migrate a4-sqlc a4-postgres-qualify a8-sqlc a8-postgres-qualify a8-local-qualify a9-sqlc a9-postgres-qualify a9-model-qualify a10-sqlc a10-postgres-qualify a10-model-qualify a10-research-qualify a11-sqlc a11-postgres-qualify a11-contract-qualify a11-api-qualify a11-frontend-qualify a11-ui-fixture-qualify a11-e2e-qualify a11-security-qualify b1-model-qualify b1-postgres-qualify b1-adapter-qualify b1-security-qualify b1-local-qualify b1-live-qualify b2-model-qualify b2-postgres-qualify b2-live-qualify b2-local-qualify b3-sqlc b3-model-qualify b3-postgres-qualify b3-research-qualify b3-local-qualify b4-model-qualify b4-postgres-qualify b5-model-qualify b5-postgres-qualify b6-model-qualify b6-postgres-qualify b6-security-qualify b7-model-qualify b7-postgres-qualify b7-research-qualify b8-model-qualify b8-postgres-qualify b8-api-qualify b8-frontend-qualify b8-security-qualify b8-live-qualify image backup-image image-reproducibility

IMAGE ?= axiom:local
BACKUP_IMAGE ?= axiom-backup:local
REBUILD_IMAGE ?= $(IMAGE)-rebuild
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
	@$(NODE) scripts/check-a10-strategy-boundary.mjs
	@$(NODE) scripts/check-a11-console-boundary.mjs

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

vulnerability: ## Scan the Go dependency graph for known vulnerabilities.
	@$(GO) tool govulncheck ./...

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

b4-model-qualify b4-postgres-qualify b5-model-qualify b5-postgres-qualify \
b6-model-qualify b6-postgres-qualify b6-security-qualify \
b7-model-qualify b7-postgres-qualify b7-research-qualify \
b8-model-qualify b8-postgres-qualify b8-api-qualify b8-frontend-qualify \
b8-security-qualify b8-live-qualify:
	@echo "$@ is reserved for its sequential V1B phase and is not implemented by B3" >&2
	@exit 2

image: ## Build the pinned minimal Axiom image.
	@docker build --file deploy/docker/Dockerfile --tag "$(IMAGE)" \
		--build-arg "VERSION=$(VERSION)" \
		--build-arg "COMMIT=$(COMMIT)" \
		--build-arg "BUILT_AT=$(BUILT_AT)" \
		--build-arg "DIRTY=$(DIRTY)" .

backup-image: ## Build the pinned PostgreSQL-tooling backup image.
	@docker build --file deploy/backup/Dockerfile --tag "$(BACKUP_IMAGE)" .

image-reproducibility: image ## Rebuild and compare the complete runtime image payload.
	@VERSION="$(VERSION)" COMMIT="$(COMMIT)" BUILT_AT="$(BUILT_AT)" DIRTY="$(DIRTY)" \
		scripts/check-image-reproducibility.sh "$(IMAGE)" "$(REBUILD_IMAGE)"
