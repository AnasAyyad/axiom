SHELL := /usr/bin/env bash

GO ?= go
NODE ?= node
COREPACK ?= corepack
SQLC ?= sqlc
PNPM := $(COREPACK) pnpm
PLATFORM := bin/platform
PLAN_FILE ?= /home/anas/.codex/attachments/7085c3d9-bb74-4587-8af7-85d8e499faf1/pasted-text-1.txt

.DEFAULT_GOAL := help

.PHONY: help preflight deps generate contracts contracts-check docs-check format format-check lint test test-backend test-frontend test-race fuzz-smoke benchmark-financial-arithmetic benchmark-deterministic-scheduler build build-backend build-frontend compose-validate compose-smoke security-static vulnerability verify dev-api dev-web migrate durable-storage-sqlc durable-storage-postgres-qualify strategy-execution-sqlc strategy-execution-postgres-qualify strategy-execution-local-qualify portfolio-risk-sqlc portfolio-risk-postgres-qualify portfolio-risk-model-qualify research-registry-sqlc research-registry-postgres-qualify research-registry-model-qualify research-registry-research-qualify owner-console-sqlc owner-console-postgres-qualify owner-console-contract-qualify owner-console-api-qualify owner-console-frontend-qualify owner-console-ui-fixture-qualify owner-console-e2e-qualify owner-console-security-qualify exchange-expansion-model-qualify exchange-expansion-postgres-qualify exchange-expansion-adapter-qualify exchange-expansion-security-qualify exchange-expansion-local-qualify exchange-expansion-live-qualify coherent-market-data-model-qualify coherent-market-data-postgres-qualify coherent-market-data-live-qualify coherent-market-data-local-qualify mean-reversion-sqlc mean-reversion-model-qualify mean-reversion-postgres-qualify mean-reversion-research-qualify mean-reversion-local-qualify triangular-arbitrage-sqlc triangular-arbitrage-model-qualify triangular-arbitrage-postgres-qualify triangular-arbitrage-local-qualify cross-exchange-arbitrage-sqlc cross-exchange-arbitrage-model-qualify cross-exchange-arbitrage-postgres-qualify cross-exchange-arbitrage-local-qualify inventory-rebalancing-sqlc inventory-rebalancing-model-qualify inventory-rebalancing-postgres-qualify inventory-rebalancing-security-qualify inventory-rebalancing-local-qualify research-promotion-sqlc research-promotion-model-qualify research-promotion-postgres-qualify research-promotion-research-qualify research-promotion-local-qualify multi-exchange-console-sqlc multi-exchange-console-model-qualify multi-exchange-console-postgres-qualify multi-exchange-console-api-qualify multi-exchange-console-frontend-qualify multi-exchange-console-security-qualify multi-exchange-console-live-qualify multi-exchange-console-local-qualify image backup-image backup-image-reproducibility image-reproducibility
.PHONY: public-data-soak-smoke exchange-expansion-soak-smoke binance-combined-triangle-live-probe credential-security-qualify authentication-control-qualify dispatcher-recovery-qualify binance-testnet-qualify bybit-demo-qualify sandbox-postgres-qualify sandbox-security-foundation sandbox-connectivity
.PHONY: sandbox-api-qualify sandbox-frontend-qualify sandbox-security-qualify sandbox-chaos-qualify sandbox-qualification-smoke sandbox-qualification-formal sandbox-qualification
.PHONY: owner-control-contract-qualify owner-control-api-qualify owner-control-postgres-qualify owner-control-security-qualify owner-control
.PHONY: owner-experience-contract-qualify owner-experience-frontend-qualify owner-experience-browser-qualify owner-experience-security-qualify owner-experience
.PHONY: run-lab-contract-qualify run-lab-api-qualify run-lab-postgres-qualify run-lab-frontend-qualify run-lab-browser-qualify run-lab-security-qualify run-lab
.PHONY: operational-evidence-contract-qualify operational-evidence-api-qualify operational-evidence-postgres-qualify operational-evidence-frontend-qualify operational-evidence-browser-qualify operational-evidence-security-qualify operational-evidence
.PHONY: operational-readiness-model-qualify operational-readiness-backup-qualify operational-readiness-postgres-qualify operational-readiness-hardening-qualify operational-readiness-chaos-qualify operational-readiness-smoke operational-readiness-security-qualify operational-readiness-formal operational-readiness
.PHONY: evaluation-campaign-postgres-qualify
.PHONY: release-certification-model-qualify release-certification-traceability-qualify release-certification-security-qualify release-certification-formal release-certify

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

semantic-naming-check: ## Reject delivery-stage terminology in active product surfaces.
	@$(NODE) scripts/check-semantic-naming.mjs

docs-check: ## Validate local documentation links and requirement-matrix consistency.
	@$(MAKE) semantic-naming-check NODE="$(NODE)"
	@$(NODE) scripts/check-doc-links.mjs
	@$(NODE) scripts/check-traceability-traceability.mjs $(if $(wildcard $(PLAN_FILE)),$(PLAN_FILE))
	@$(NODE) scripts/check-configuration-reference-config-reference.mjs
	@$(NODE) scripts/check-runtime-recovery-runtime-boundary.mjs
	@$(NODE) scripts/check-durable-storage-storage-boundary.mjs
	@$(NODE) scripts/check-observability-observability-boundary.mjs
	@$(NODE) scripts/check-exchange-integration-exchange-boundary.mjs
	@$(NODE) scripts/check-public-data-public-boundary.mjs
	@$(NODE) scripts/check-exchange-expansion-public-boundary.mjs
	@$(NODE) scripts/check-mean-reversion-strategy-boundary.mjs
	@$(NODE) scripts/check-triangular-arbitrage-strategy-boundary.mjs
	@$(NODE) scripts/check-cross-exchange-strategy-boundary.mjs
	@$(NODE) scripts/check-inventory-rebalancing-rebalancing-boundary.mjs
	@$(NODE) scripts/check-research-promotion-research-boundary.mjs
	@$(NODE) scripts/check-multi-exchange-console-console-boundary.mjs
	@$(NODE) scripts/check-research-registry-strategy-boundary.mjs
	@$(NODE) scripts/check-owner-console-console-boundary.mjs
	@$(NODE) scripts/check-sandbox-qualification-boundary.mjs
	@$(NODE) scripts/check-owner-control-boundary.mjs
	@$(NODE) scripts/check-owner-experience-boundary.mjs
	@$(NODE) scripts/check-run-lab-boundary.mjs
	@$(NODE) scripts/check-operational-evidence-boundary.mjs
	@$(NODE) scripts/check-operational-readiness-boundary.mjs
	@$(NODE) scripts/check-release-certification-boundary.mjs

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

benchmark-financial-arithmetic: ## Measure exact decimal arithmetic with allocation reporting.
	@$(GO) test ./internal/domain -run '^$$' -bench '^BenchmarkFinancialArithmetic$$' -benchmem -count 5

benchmark-deterministic-scheduler: ## Measure deterministic scheduler overhead with allocation reporting.
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

compose-smoke: ## Start the image-backed application baseline app, recorder, and worker profiles.
	@GO="$(GO)" tests/integration/smoke-compose-app.sh "$(IMAGE)"

security-static: ## Run secret and prohibited-capability scans with negative tests.
	@scripts/check-secret-patterns.sh
	@scripts/test-check-secret-patterns.sh
	@scripts/check-prohibited-capabilities.sh
	@scripts/test-check-prohibited-capabilities.sh
	@GO="$(GO)" scripts/check-exchange-integration-binary-boundary.sh
	@GO="$(GO)" scripts/check-public-data-binary-boundary.sh
	@scripts/check-sandbox-security-boundary.sh

credential-security-qualify: ## Prove the closed credential, signer, endpoint, proxy, evidence, and emulator security boundary.
	@$(GO) test ./internal/config ./internal/security ./internal/egressproxy \
		./internal/exchanges/contracts ./internal/exchanges/binance \
		./internal/exchanges/bybit ./internal/exchanges/sandboxemulator ./internal/sandbox -count=1
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres \
		-run '^(TestSandboxRuntimeMigrationsDefineClosedDurableAuthenticatedEvidence|TestSandboxRuntimeEngineGrantIncludesClosedExecutionAndAlertTables)$$' \
		-count=1
	@scripts/check-sandbox-security-boundary.sh

authentication-control-qualify: ## Exercise authentication control password/TOTP, replay, one-use authorization, RBAC, audit, session, and rotation models.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/authentication ./internal/sandbox ./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/authentication ./internal/sandbox -count=1

dispatcher-recovery-qualify: ## Exercise dispatcher recovery atomic caps, durable dispatch, fencing, inbox/reducer, startup, and crash recovery.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/sandbox ./internal/execution ./internal/reconciliation \
		./internal/runtime ./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/sandbox ./internal/execution ./internal/reconciliation ./internal/runtime -count=1

binance-testnet-qualify: ## Prove the complete closed Binance Spot Testnet adapter and recovery behavior.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/exchanges/contracts ./internal/exchanges/binance \
		./internal/exchanges/sandboxemulator ./internal/sandbox ./internal/execution \
		./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/exchanges/binance ./internal/exchanges/sandboxemulator \
		./internal/sandbox ./internal/execution -count=1
	@$(GO) test ./internal/exchanges/binance -run '^$$' \
		-fuzz '^FuzzBinancePrivateEventDecoder$$' -fuzztime 3s
	@scripts/check-sandbox-security-boundary.sh

bybit-demo-qualify: ## Prove the complete closed Bybit Demo Spot adapter and recovery behavior.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/config ./internal/bootstrap ./internal/egressproxy \
		./internal/exchanges/contracts ./internal/exchanges/bybit \
		./internal/exchanges/sandboxemulator ./internal/sandbox ./internal/execution \
		./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/bootstrap ./internal/exchanges/bybit \
		./internal/exchanges/sandboxemulator ./internal/sandbox ./internal/execution \
		-count=1
	@$(GO) test ./internal/exchanges/bybit -run '^$$' \
		-fuzz '^FuzzBybitDemoPrivateDecoder$$' -fuzztime 3s
	@scripts/check-sandbox-security-boundary.sh

sandbox-postgres-qualify: ## Run sandbox runtime clean-install and exact multi-exchange console-upgrade qualification on dedicated PostgreSQL 18 databases.
	@test -n "$(AXIOM_SANDBOX_RUNTIME_TEST_DSN)" || { echo "AXIOM_SANDBOX_RUNTIME_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN)" || { echo "AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN="$(AXIOM_SANDBOX_RUNTIME_TEST_DSN)" \
		AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN="$(AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestSandboxRuntimePostgres(CleanInstall|MultiExchangeConsoleToSandboxRuntimeUpgrade)Qualification$$' -count=1 -v

sandbox-security-foundation: credential-security-qualify authentication-control-qualify dispatcher-recovery-qualify sandbox-postgres-qualify ## Pass the credential, authentication, dispatcher-recovery, PostgreSQL, and cumulative repository security gates.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

sandbox-connectivity: credential-security-qualify authentication-control-qualify dispatcher-recovery-qualify binance-testnet-qualify bybit-demo-qualify sandbox-postgres-qualify ## Pass the credential, authentication, dispatcher-recovery, sandbox-connectivity, PostgreSQL, and cumulative repository gates.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

sandbox-api-qualify: ## Prove sandbox qualification contracts, redacted projections, durable controls, RBAC, and storage boundaries.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/... ./internal/authentication \
			./internal/bootstrap ./internal/storage/postgres -count=1

sandbox-frontend-qualify: ## Type-check, lint, test, build, and inspect the sandbox console fixtures.
	@$(MAKE) owner-console-frontend-qualify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@AXIOM_OWNER_CONSOLE_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'Exchange sandbox workflows'

sandbox-security-qualify: ## Prove sandbox qualification endpoint, secret, production-target, and prohibited-capability denial.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/authentication ./internal/qualification/sandboxqualification \
			./internal/storage/postgres -count=1
	@scripts/check-sandbox-security-boundary.sh
	@$(NODE) scripts/check-sandbox-qualification-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"

sandbox-chaos-qualify: ## Exercise deterministic sandbox qualification fault, race, reset, reconnect, and recovery scenarios.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/qualification/sandboxqualification ./internal/sandbox \
			./internal/exchanges/sandboxemulator ./internal/exchanges/binance \
			./internal/exchanges/bybit ./internal/execution \
			./internal/reconciliation ./internal/bootstrap \
			./internal/storage/postgres -count=1

sandbox-qualification-smoke: ## Run only the short deterministic sandbox qualification smoke runner; never grants formal qualification.
	@$(GO) test ./cmd/sandbox-qualification ./internal/qualification/sandboxqualification \
		-run '^TestSandboxQualification' -count=1 -timeout=2m -v

sandbox-qualification-formal: ## MANUAL: run the default-off exact 72-hour observer; requires explicit identity and evidence variables.
	@test "$(AXIOM_SANDBOX_QUALIFICATION_ENABLED)" = "1" || { echo "AXIOM_SANDBOX_QUALIFICATION_ENABLED=1 is required" >&2; exit 1; }
	@test "$(AXIOM_SANDBOX_QUALIFICATION_MODE)" = "formal" || { echo "AXIOM_SANDBOX_QUALIFICATION_MODE=formal is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_RUN_ID)" || { echo "AXIOM_SANDBOX_QUALIFICATION_RUN_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_COMMIT_SHA)" || { echo "AXIOM_SANDBOX_QUALIFICATION_COMMIT_SHA is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_BUILD_HASH)" || { echo "AXIOM_SANDBOX_QUALIFICATION_BUILD_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_EXECUTABLE_HASH)" || { echo "AXIOM_SANDBOX_QUALIFICATION_EXECUTABLE_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_IMAGE_HASH)" || { echo "AXIOM_SANDBOX_QUALIFICATION_IMAGE_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_CONFIGURATION_HASH)" || { echo "AXIOM_SANDBOX_QUALIFICATION_CONFIGURATION_HASH is required" >&2; exit 1; }
	@test -n "$(AXIOM_SANDBOX_QUALIFICATION_EVIDENCE_PATH)" || { echo "AXIOM_SANDBOX_QUALIFICATION_EVIDENCE_PATH is required" >&2; exit 1; }
	@$(GO) run ./cmd/sandbox-qualification

sandbox-qualification: credential-security-qualify authentication-control-qualify dispatcher-recovery-qualify binance-testnet-qualify bybit-demo-qualify sandbox-api-qualify sandbox-frontend-qualify sandbox-security-qualify sandbox-chaos-qualify sandbox-qualification-smoke sandbox-postgres-qualify ## Pass every sandbox runtime non-soak gate; formal sandbox qualification soak remains separate and pending.
	@AXIOM_SANDBOX_RUNTIME_TEST_DSN= AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

owner-control-contract-qualify: ## Prove the compatible generated owner control OpenAPI contract and source catalogue.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-owner-control-boundary.mjs

owner-control-api-qualify: ## Exercise owner control validation, authorization, idempotency, revisions, projections, exports, and streams.
	@AXIOM_OWNER_CONTROL_TEST_DSN= AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/console ./internal/authentication \
			./internal/bootstrap ./internal/storage/postgres -count=1
	@AXIOM_OWNER_CONTROL_TEST_DSN= AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN= \
		$(GO) test -race ./internal/api/console ./internal/authentication -count=1

owner-control-postgres-qualify: ## Run owner control clean-install and exact sandbox runtime-upgrade gates on dedicated PostgreSQL 18 databases.
	@test -n "$(AXIOM_OWNER_CONTROL_TEST_DSN)" || { echo "AXIOM_OWNER_CONTROL_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN)" || { echo "AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_OWNER_CONTROL_TEST_DSN="$(AXIOM_OWNER_CONTROL_TEST_DSN)" \
		AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN="$(AXIOM_OWNER_CONTROL_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestOwnerControlPostgres(CleanInstall|SandboxRuntimeToOwnerControlUpgrade)Qualification$$' -count=1 -v

owner-control-security-qualify: ## Prove owner control redaction, secret, role, stream, and prohibited-capability boundaries.
	@$(NODE) scripts/check-owner-control-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

owner-control: owner-control-contract-qualify owner-control-api-qualify owner-control-postgres-qualify owner-control-security-qualify ## Pass every owner control implementation gate; merge and formal cumulative acceptance remain separate.

owner-experience-contract-qualify: ## Prove the owner experience browser consumes the compatible generated owner control contract.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-owner-control-boundary.mjs
	@$(NODE) scripts/check-owner-experience-boundary.mjs

owner-experience-frontend-qualify: ## Type-check, lint, test, build, and inspect the accessible owner experience command center.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build
	@$(NODE) scripts/check-owner-experience-boundary.mjs

owner-experience-browser-qualify: ## Run owner experience workflows in Chromium, Firefox, WebKit, tablet, and mobile fixtures.
	@AXIOM_OWNER_CONSOLE_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'Owner command center'

owner-experience-security-qualify: ## Prove owner experience has no arbitrary execution surface or forbidden V1 capability.
	@$(NODE) scripts/check-owner-experience-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

owner-experience: owner-experience-contract-qualify owner-experience-frontend-qualify owner-experience-browser-qualify owner-experience-security-qualify ## Pass every local owner experience implementation gate; merge and cumulative acceptance remain separate.

run-lab-contract-qualify: ## Prove run lab uses compatible generated lab and evidence contracts.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-owner-control-boundary.mjs
	@$(NODE) scripts/check-owner-experience-boundary.mjs
	@$(NODE) scripts/check-run-lab-boundary.mjs

run-lab-api-qualify: ## Exercise run lab authorization, lifecycle, manifest, replay, export, and shadow projections.
	@AXIOM_OWNER_CONSOLE_TEST_DSN= $(GO) test ./internal/api/... ./internal/replay ./internal/backtest ./internal/storage/postgres -count=1

run-lab-postgres-qualify: ## Run run lab durable lifecycle and evidence projections against PostgreSQL 18.
	@test -n "$(AXIOM_OWNER_CONSOLE_TEST_DSN)" || { echo "AXIOM_OWNER_CONSOLE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_OWNER_CONSOLE_TEST_DSN="$(AXIOM_OWNER_CONSOLE_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestOwnerConsolePostgresAuthenticationCommandsAndConsoleQualification$$' -count=1 -v

run-lab-frontend-qualify: ## Type-check, lint, test, build, and inspect the complete run-lab interface.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build
	@$(NODE) scripts/check-run-lab-boundary.mjs

run-lab-browser-qualify: ## Run run-lab workflows in Chromium, Firefox, WebKit, tablet, and mobile fixtures.
	@AXIOM_OWNER_CONSOLE_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'Unified runs preserve immutable identity'

run-lab-security-qualify: ## Prove run lab preserves redaction and every forbidden V1 capability boundary.
	@$(NODE) scripts/check-run-lab-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

run-lab: run-lab-contract-qualify run-lab-api-qualify run-lab-postgres-qualify run-lab-frontend-qualify run-lab-browser-qualify run-lab-security-qualify ## Pass every local run lab implementation gate; merge and cumulative acceptance remain separate.

operational-evidence-contract-qualify: ## Prove operational evidence uses compatible generated operational-evidence contracts.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-owner-control-boundary.mjs
	@$(NODE) scripts/check-owner-experience-boundary.mjs
	@$(NODE) scripts/check-run-lab-boundary.mjs
	@$(NODE) scripts/check-operational-evidence-boundary.mjs

operational-evidence-api-qualify: ## Exercise operational evidence authorization, schedules, reports, incidents, alerts, audit, and artifacts.
	@AXIOM_OPERATIONAL_EVIDENCE_TEST_DSN= AXIOM_OPERATIONAL_EVIDENCE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/... ./internal/alerting ./internal/reporting \
			./internal/storage/postgres ./internal/bootstrap -count=1
	@$(GO) test -race ./internal/api/console ./internal/alerting ./internal/reporting -count=1

operational-evidence-postgres-qualify: ## Run operational evidence clean-install and exact owner control-to-operational evidence upgrade gates on dedicated PostgreSQL 18 databases.
	@test -n "$(AXIOM_OPERATIONAL_EVIDENCE_TEST_DSN)" || { echo "AXIOM_OPERATIONAL_EVIDENCE_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_EVIDENCE_UPGRADE_TEST_DSN)" || { echo "AXIOM_OPERATIONAL_EVIDENCE_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_OPERATIONAL_EVIDENCE_TEST_DSN="$(AXIOM_OPERATIONAL_EVIDENCE_TEST_DSN)" \
		AXIOM_OPERATIONAL_EVIDENCE_UPGRADE_TEST_DSN="$(AXIOM_OPERATIONAL_EVIDENCE_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestOperationalEvidencePostgres(OperationalEvidence|OwnerControlToOperationalEvidenceUpgrade)Qualification$$' -count=1 -v

operational-evidence-frontend-qualify: ## Type-check, lint, test, build, and inspect the operational-evidence workflows.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build
	@$(NODE) scripts/check-operational-evidence-boundary.mjs

operational-evidence-browser-qualify: ## Run operational evidence workflows in Chromium, Firefox, WebKit, tablet, and mobile fixtures.
	@AXIOM_OWNER_CONSOLE_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e --grep 'Operational evidence workflows'

operational-evidence-security-qualify: ## Prove operational evidence redaction, audit, hold, outbound, role, and prohibited-capability boundaries.
	@$(NODE) scripts/check-operational-evidence-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

operational-evidence: operational-evidence-contract-qualify operational-evidence-api-qualify operational-evidence-postgres-qualify operational-evidence-frontend-qualify operational-evidence-browser-qualify operational-evidence-security-qualify ## Pass every local operational evidence implementation gate; merge and cumulative acceptance remain separate.

operational-readiness-model-qualify: ## Exercise operational readiness pressure, retention, lifecycle, runner, and fail-closed runtime models.
	@AXIOM_OPERATIONAL_READINESS_TEST_DSN= AXIOM_OPERATIONAL_READINESS_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/pressure ./internal/storage/segments \
			./internal/qualification/operationalreadiness ./internal/config ./internal/bootstrap \
			./internal/storage/postgres -count=1
	@$(GO) test -race ./internal/storage/pressure ./internal/qualification/operationalreadiness \
		./internal/bootstrap -count=1

operational-readiness-backup-qualify: ## Prove independent mount rejection, encryption, retention, and authenticated restore evidence.
	@$(GO) test ./internal/backup ./cmd/storage-backup -count=1
	@$(GO) test -race ./internal/backup -count=1

operational-readiness-postgres-qualify: ## Run operational readiness clean-install and exact operational evidence-to-operational readiness upgrade gates on PostgreSQL 18.
	@test -n "$(AXIOM_OPERATIONAL_READINESS_TEST_DSN)" || { echo "AXIOM_OPERATIONAL_READINESS_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_UPGRADE_TEST_DSN)" || { echo "AXIOM_OPERATIONAL_READINESS_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_OPERATIONAL_READINESS_TEST_DSN="$(AXIOM_OPERATIONAL_READINESS_TEST_DSN)" \
		AXIOM_OPERATIONAL_READINESS_UPGRADE_TEST_DSN="$(AXIOM_OPERATIONAL_READINESS_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestOperationalReadinessPostgres(OperationalReadiness|OperationalEvidenceToOperationalReadinessUpgrade)Qualification$$' -count=1 -v

evaluation-campaign-postgres-qualify: ## Run evaluation campaign clean-install and exact migration-54 upgrade gates on PostgreSQL 18.
	@test -n "$(AXIOM_EVALUATION_CAMPAIGN_TEST_DSN)" || { echo "AXIOM_EVALUATION_CAMPAIGN_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_EVALUATION_CAMPAIGN_UPGRADE_TEST_DSN)" || { echo "AXIOM_EVALUATION_CAMPAIGN_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_EVALUATION_CAMPAIGN_TEST_DSN="$(AXIOM_EVALUATION_CAMPAIGN_TEST_DSN)" \
		AXIOM_EVALUATION_CAMPAIGN_UPGRADE_TEST_DSN="$(AXIOM_EVALUATION_CAMPAIGN_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestEvaluationCampaignPostgres(CleanInstall|SemanticRuntimeToCampaignUpgrade)Qualification$$' -count=1 -v

operational-readiness-hardening-qualify: ## Validate operational readiness Compose retention, remote backup, digest, and lifecycle boundaries.
	@$(NODE) scripts/check-operational-readiness-boundary.mjs
	@scripts/check-compose.sh

operational-readiness-chaos-qualify: ## Exercise terminal failure, no-replace evidence, races, restart, and kill-point models.
	@$(GO) test ./internal/qualification/operationalreadiness ./internal/backup ./internal/storage/pressure \
		./internal/execution ./internal/reconciliation ./internal/sandbox -count=1

operational-readiness-smoke: ## Run only deterministic operational readiness smoke; output is always non-qualifying.
	@$(GO) test ./internal/qualification/operationalreadiness -run '^(TestSmokeRunner|TestRunnerFails|TestFileStore)' \
		-count=1 -timeout=2m -v

operational-readiness-security-qualify: ## Prove observation-only operational readiness code, secret redaction, and prohibited-capability denial.
	@$(NODE) scripts/check-operational-readiness-boundary.mjs
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

operational-readiness-formal: ## MANUAL: run the default-off exact seven-day operational readiness observer on the approved server.
	@test "$(AXIOM_OPERATIONAL_READINESS_ENABLED)" = "1" || { echo "AXIOM_OPERATIONAL_READINESS_ENABLED=1 is required" >&2; exit 1; }
	@test "$(AXIOM_OPERATIONAL_READINESS_MODE)" = "formal" || { echo "AXIOM_OPERATIONAL_READINESS_MODE=formal is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_RUN_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_RUN_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_TEST_MANIFEST_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_TEST_MANIFEST_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_FAULT_SCHEDULE_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_FAULT_SCHEDULE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_PREFLIGHT_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_PREFLIGHT_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_FAULT_EVIDENCE_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_FAULT_EVIDENCE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_OPERATIONAL_READINESS_SIGNING_KEY_FILE)" || { echo "AXIOM_OPERATIONAL_READINESS_SIGNING_KEY_FILE is required" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "formal operational readiness requires a clean exact source" >&2; exit 1; }
	@$(GO) run -ldflags "-X axiom/internal/buildinfo.Commit=$$(git rev-parse HEAD) -X axiom/internal/buildinfo.Dirty=false" ./cmd/operational-readiness

operational-readiness: operational-readiness-model-qualify operational-readiness-backup-qualify operational-readiness-postgres-qualify operational-readiness-hardening-qualify operational-readiness-chaos-qualify operational-readiness-smoke operational-readiness-security-qualify ## Pass local operational readiness implementation gates; the reference-server seven-day verdict remains separate.

release-certification-model-qualify: ## Exercise exact-identity, signature, expiry, tamper, duplicate, and fail-closed certification rules.
	@$(GO) test ./internal/certification ./cmd/release-certify -count=1
	@$(GO) test -race ./internal/certification -count=1

release-certification-traceability-qualify: ## Validate release certification boundaries, complete documentation paths, and all 22 Section 35 dispositions.
	@$(NODE) scripts/check-release-certification-boundary.mjs
	@$(NODE) scripts/check-doc-links.mjs

release-certification-security-qualify: release-certification-model-qualify release-certification-traceability-qualify ## Re-run V1 capability, secret, binary, and release certification release-input controls.
	@$(MAKE) security-static GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(GO) test ./internal/exchanges/binance ./internal/exchanges/bybit \
		./internal/exchanges/sandboxemulator ./internal/egressproxy -count=1

release-certification-formal: ## MANUAL: issue a signed verdict only from complete current formal evidence; default-off and expected to reject today.
	@test "$(AXIOM_RELEASE_CERTIFICATION_ENABLED)" = "1" || { echo "AXIOM_RELEASE_CERTIFICATION_ENABLED=1 is required" >&2; exit 1; }
	@test -n "$(AXIOM_RELEASE_CERTIFICATION_CANDIDATE_FILE)" || { echo "AXIOM_RELEASE_CERTIFICATION_CANDIDATE_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_RELEASE_CERTIFICATION_TRUSTED_REVIEWERS_FILE)" || { echo "AXIOM_RELEASE_CERTIFICATION_TRUSTED_REVIEWERS_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_RELEASE_CERTIFICATION_SIGNING_KEY_FILE)" || { echo "AXIOM_RELEASE_CERTIFICATION_SIGNING_KEY_FILE is required" >&2; exit 1; }
	@test -n "$(AXIOM_RELEASE_CERTIFICATION_VERDICT_DIRECTORY)" || { echo "AXIOM_RELEASE_CERTIFICATION_VERDICT_DIRECTORY is required" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "final certification requires a clean exact source" >&2; exit 1; }
	@$(GO) run -ldflags "-X axiom/internal/buildinfo.Commit=$$(git rev-parse HEAD) -X axiom/internal/buildinfo.Dirty=false" ./cmd/release-certify

release-certify: release-certification-security-qualify owner-control-contract-qualify owner-control-api-qualify owner-control-security-qualify owner-experience-contract-qualify owner-experience-frontend-qualify owner-experience-browser-qualify owner-experience-security-qualify run-lab-contract-qualify run-lab-api-qualify run-lab-frontend-qualify run-lab-browser-qualify run-lab-security-qualify operational-evidence-contract-qualify operational-evidence-api-qualify operational-evidence-frontend-qualify operational-evidence-browser-qualify operational-evidence-security-qualify operational-readiness-model-qualify operational-readiness-backup-qualify operational-readiness-hardening-qualify operational-readiness-chaos-qualify operational-readiness-security-qualify credential-security-qualify authentication-control-qualify dispatcher-recovery-qualify binance-testnet-qualify bybit-demo-qualify sandbox-api-qualify sandbox-frontend-qualify sandbox-security-qualify sandbox-chaos-qualify ## Pass repository-verifiable release certification checks without invoking any formal or smoke soak target.
	@$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

vulnerability: ## Scan the Go dependency graph for known vulnerabilities.
	@$(GO) tool govulncheck -db "$(VULNDB)" ./...

verify: preflight format-check contracts-check docs-check lint test test-race fuzz-smoke build compose-validate security-static vulnerability ## Run the complete local application baseline quality gate.

dev-api: ## Run the local API health application.
	@$(GO) run ./cmd/platform api

dev-web: ## Run Vite with the API proxy.
	@$(PNPM) --filter @axiom/web dev

migrate: ## Run the exact application baseline migration command surface.
	@$(GO) run ./cmd/platform admin migrate

durable-storage-sqlc: ## Generate and compile the reviewed durable storage PostgreSQL queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_DURABLE_STORAGE_TEST_DSN= $(GO) test ./internal/storage/postgres/...

durable-storage-postgres-qualify: ## Run the destructive durable storage gate against a dedicated *_durable_storage_test database.
	@test -n "$(AXIOM_DURABLE_STORAGE_TEST_DSN)" || { echo "AXIOM_DURABLE_STORAGE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) durable-storage-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_DURABLE_STORAGE_TEST_DSN="$(AXIOM_DURABLE_STORAGE_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestDurableStoragePostgresMigrationJournalAndReservationIntegration$$' -count=1 -v

public-data-soak-smoke: ## Run the 20-second two-instrument Binance forensic soak harness.
	@test -n "$(AXIOM_PUBLIC_DATA_SOURCE_COMMIT)" || { echo "AXIOM_PUBLIC_DATA_SOURCE_COMMIT is required" >&2; exit 1; }
	@AXIOM_PUBLIC_DATA_SOAK_SMOKE=1 AXIOM_PUBLIC_DATA_SOURCE_COMMIT="$(AXIOM_PUBLIC_DATA_SOURCE_COMMIT)" \
		$(GO) test ./internal/qualification -run '^TestPublicDataQualificationPublicSoakHarnessSmoke$$' -count=1 -timeout=2m -v

strategy-execution-sqlc: ## Generate and compile the reviewed strategy execution PostgreSQL queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_STRATEGY_EXECUTION_TEST_DSN= $(GO) test ./internal/storage/postgres/...

strategy-execution-postgres-qualify: ## Run the strategy execution atomic repository gate against a dedicated *_strategy_execution_test database.
	@test -n "$(AXIOM_STRATEGY_EXECUTION_TEST_DSN)" || { echo "AXIOM_STRATEGY_EXECUTION_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) strategy-execution-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_STRATEGY_EXECUTION_TEST_DSN="$(AXIOM_STRATEGY_EXECUTION_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestStrategyExecutionPostgresAtomicOrderFillJournalCheckpoint$$' -count=1 -v

strategy-execution-local-qualify: ## Verify and stream the ignored public-data qualification engineering recordings without exporting payloads.
	@AXIOM_STRATEGY_EXECUTION_DATASET_43_ROOT=$(CURDIR)/.local/a7-soak-a641cd4 \
		AXIOM_STRATEGY_EXECUTION_DATASET_R2_ROOT=$(CURDIR)/.local/a7-soak-a641cd4-r2 \
		$(GO) test ./internal/backtest -run '^TestStrategyExecutionIgnoredLocalDatasetQualification$$' -count=1 -v

portfolio-risk-sqlc: ## Generate and compile the reviewed portfolio risk PostgreSQL queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_PORTFOLIO_RISK_TEST_DSN= $(GO) test ./internal/storage/postgres/...

portfolio-risk-postgres-qualify: ## Run the portfolio risk ownership/risk/recovery gate against a dedicated *_portfolio_risk_test database.
	@test -n "$(AXIOM_PORTFOLIO_RISK_TEST_DSN)" || { echo "AXIOM_PORTFOLIO_RISK_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) portfolio-risk-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_PORTFOLIO_RISK_TEST_DSN="$(AXIOM_PORTFOLIO_RISK_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestPortfolioRiskPostgresPortfolioRiskRecoveryQualification$$' -count=1 -v

portfolio-risk-model-qualify: ## Exercise exact portfolio risk portfolio, risk, reconciliation, and shared strategy execution pipeline models.
	@$(GO) test ./internal/portfolio ./internal/risk ./internal/reconciliation -count=1
	@$(GO) test ./internal/backtest -run '^TestPortfolioRisk.*Pipeline.*$$' -count=1 -v

research-registry-sqlc: ## Generate and compile the reviewed research registry Trend and research queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_RESEARCH_REGISTRY_TEST_DSN= $(GO) test ./internal/storage/postgres/...

research-registry-postgres-qualify: ## Run the research registry immutable research gate against a dedicated *_research_registry_test database.
	@test -n "$(AXIOM_RESEARCH_REGISTRY_TEST_DSN)" || { echo "AXIOM_RESEARCH_REGISTRY_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) research-registry-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_RESEARCH_REGISTRY_TEST_DSN="$(AXIOM_RESEARCH_REGISTRY_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestResearchRegistryPostgresTrendResearchQualification$$' -count=1 -v

research-registry-model-qualify: ## Exercise exact Trend decisions through the shared allocator/risk pipeline.
	@$(GO) test ./internal/strategies/trend -count=1 -v
	@$(GO) test ./internal/backtest -count=1
	@$(NODE) scripts/check-research-registry-strategy-boundary.mjs

research-registry-research-qualify: ## Verify deterministic Go research and the independent locked Python checker.
	@python3 -c 'import sys; assert sys.version_info[:3] == (3, 12, 3), sys.version'
	@PYTHONPATH=research/src python3 -m unittest discover -s research/tests
	@$(GO) test ./internal/research -count=1 -v

owner-console-sqlc: ## Generate and compile reviewed owner console authentication and console queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_OWNER_CONSOLE_TEST_DSN= $(GO) test ./internal/storage/postgres/...

owner-console-postgres-qualify: ## Run owner console auth, command, projection, stream, and immutability qualification.
	@test -n "$(AXIOM_OWNER_CONSOLE_TEST_DSN)" || { echo "AXIOM_OWNER_CONSOLE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) owner-console-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_OWNER_CONSOLE_TEST_DSN="$(AXIOM_OWNER_CONSOLE_TEST_DSN)" $(GO) test ./internal/storage/postgres \
		-run '^TestOwnerConsolePostgresAuthenticationCommandsAndConsoleQualification$$' -count=1 -v

owner-console-contract-qualify: ## Prove exact OpenAPI operations, generated models, and boundary ownership.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@$(NODE) scripts/check-owner-console-console-boundary.mjs
	@$(GO) test ./internal/api/... -count=1

owner-console-api-qualify: ## Exercise owner console authentication, authorization, API, bootstrap, and storage policy.
	@$(GO) test ./internal/authentication ./internal/api/... ./internal/bootstrap ./internal/config -count=1

owner-console-frontend-qualify: ## Type-check, lint, test, and build the routed accessible console.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build

owner-console-ui-fixture-qualify: ## Run deterministic desktop/mobile UI coverage with contract-shaped fixtures.
	@AXIOM_OWNER_CONSOLE_E2E_BASE_URL= $(PNPM) --filter @axiom/web test:e2e

owner-console-e2e-qualify: ## Run the unmocked authenticated workflow against a clean integrated owner console environment.
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_BASE_URL)" || { echo "AXIOM_OWNER_CONSOLE_E2E_BASE_URL is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_CONFIGURATION_ID)" || { echo "AXIOM_OWNER_CONSOLE_E2E_CONFIGURATION_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_DATASET_ID)" || { echo "AXIOM_OWNER_CONSOLE_E2E_DATASET_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_RESEARCH_GENERATION_ID)" || { echo "AXIOM_OWNER_CONSOLE_E2E_RESEARCH_GENERATION_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_PORTFOLIO_ID)" || { echo "AXIOM_OWNER_CONSOLE_E2E_PORTFOLIO_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_EVIDENCE_SHADOW_ID)" || { echo "AXIOM_OWNER_CONSOLE_E2E_EVIDENCE_SHADOW_ID is required" >&2; exit 1; }
	@test -n "$(AXIOM_OWNER_CONSOLE_E2E_PASSWORD)" || { echo "AXIOM_OWNER_CONSOLE_E2E_PASSWORD is required" >&2; exit 1; }
	@$(PNPM) --filter @axiom/web test:e2e

owner-console-security-qualify: ## Run owner console ownership checks plus repository secret/capability scans.
	@$(NODE) scripts/check-owner-console-console-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"

exchange-expansion-model-qualify: ## Exercise common public contracts, Bybit semantics, local books, and recorder linkage.
	@$(GO) test ./internal/exchanges/contracts ./internal/exchanges/binance ./internal/exchanges/bybit ./internal/exchanges/emulator ./internal/marketdata ./internal/recorder -count=1

exchange-expansion-postgres-qualify: ## Run clean-install and trend foundation-upgrade exchange expansion gates on PostgreSQL 18 *_exchange_expansion_test databases.
	@test -n "$(AXIOM_EXCHANGE_EXPANSION_TEST_DSN)" || { echo "AXIOM_EXCHANGE_EXPANSION_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_EXCHANGE_EXPANSION_UPGRADE_TEST_DSN)" || { echo "AXIOM_EXCHANGE_EXPANSION_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_EXCHANGE_EXPANSION_TEST_DSN="$(AXIOM_EXCHANGE_EXPANSION_TEST_DSN)" \
		AXIOM_EXCHANGE_EXPANSION_UPGRADE_TEST_DSN="$(AXIOM_EXCHANGE_EXPANSION_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres -run '^TestExchangeExpansionPostgres(CleanInstall|TrendFoundationToExchangeExpansionUpgrade)Qualification$$' -count=1 -v

exchange-expansion-adapter-qualify: ## Run Bybit normalization, endpoint, lifecycle, conformance, and fuzz qualification.
	@$(GO) test ./internal/exchanges/bybit -count=1 -v
	@$(GO) test ./internal/exchanges/bybit -run '^$$' -fuzz '^FuzzNormalizeBybitPublicStream$$' -fuzztime 3s

exchange-expansion-security-qualify: ## Prove exchange expansion remains credential-free, public-only, and free of order/transfer methods.
	@$(NODE) scripts/check-exchange-expansion-public-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"

exchange-expansion-local-qualify: exchange-expansion-model-qualify exchange-expansion-postgres-qualify exchange-expansion-adapter-qualify exchange-expansion-security-qualify verify ## Pass every non-live exchange expansion validation gate cumulatively.

exchange-expansion-live-qualify: ## Run explicitly enabled short Bybit production-public qualification.
	@test "$(AXIOM_EXCHANGE_EXPANSION_LIVE_PUBLIC)" = "1" || { echo "AXIOM_EXCHANGE_EXPANSION_LIVE_PUBLIC=1 is required" >&2; exit 1; }
	@AXIOM_EXCHANGE_EXPANSION_LIVE_PUBLIC=1 $(GO) test ./internal/exchanges/bybit \
		-run '^TestProductionPublicBybit(Surface|WebSocketRecording|RecorderManifest)$$' -count=1 -v

exchange-expansion-soak-smoke: ## Run the 20-second two-instrument Bybit forensic soak harness.
	@test -n "$(AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT)" || { echo "AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT is required" >&2; exit 1; }
	@AXIOM_EXCHANGE_EXPANSION_SOAK_SMOKE=1 AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT="$(AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT)" \
		$(GO) test ./internal/qualification -run '^TestExchangeExpansionPublicSoakHarnessSmoke$$' -count=1 -timeout=2m -v

coherent-market-data-model-qualify: ## Exercise coherent market data clocks, book evidence, deterministic joins, recovery, and Tier-A manifests.
	@$(GO) test ./internal/exchanges/contracts ./internal/exchanges/binance ./internal/exchanges/bybit \
		./internal/marketdata ./internal/runtime ./internal/recorder ./internal/qualification -count=1

coherent-market-data-postgres-qualify: ## Run clean-install and exchange expansion-upgrade coherent market data gates on PostgreSQL 18 *_coherent_market_data_test databases.
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_TEST_DSN)" || { echo "AXIOM_COHERENT_MARKET_DATA_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_UPGRADE_TEST_DSN)" || { echo "AXIOM_COHERENT_MARKET_DATA_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@AXIOM_COHERENT_MARKET_DATA_TEST_DSN="$(AXIOM_COHERENT_MARKET_DATA_TEST_DSN)" \
		AXIOM_COHERENT_MARKET_DATA_UPGRADE_TEST_DSN="$(AXIOM_COHERENT_MARKET_DATA_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres -run '^TestCoherentMarketDataPostgres(CleanInstall|ExchangeExpansionToCoherentMarketDataUpgrade)Qualification$$' -count=1 -v

coherent-market-data-live-qualify: ## Run the explicitly enabled short public-only Binance/Bybit coherent-view qualification; no soak.
	@test "$(AXIOM_COHERENT_MARKET_DATA_LIVE_PUBLIC)" = "1" || { echo "AXIOM_COHERENT_MARKET_DATA_LIVE_PUBLIC=1 is required" >&2; exit 1; }
	@AXIOM_COHERENT_MARKET_DATA_LIVE_PUBLIC=1 \
		AXIOM_COHERENT_MARKET_DATA_LIVE_EVIDENCE_ROOT="$(AXIOM_COHERENT_MARKET_DATA_LIVE_EVIDENCE_ROOT)" \
		AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION="$(AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION)" \
		$(GO) test ./internal/qualification -run '^TestCoherentMarketDataProductionPublicRecordOnlyAndCoherentQualification$$' -count=1 -v

binance-combined-triangle-live-probe: ## Measure three Binance depth streams on one public WebSocket; experimental and non-qualifying.
	@test "$(AXIOM_BINANCE_COMBINED_TRIANGLE_LIVE)" = "1" || { echo "AXIOM_BINANCE_COMBINED_TRIANGLE_LIVE=1 is required" >&2; exit 1; }
	@AXIOM_BINANCE_COMBINED_TRIANGLE_LIVE=1 \
		AXIOM_BINANCE_COMBINED_TRIANGLE_DURATION="$(AXIOM_BINANCE_COMBINED_TRIANGLE_DURATION)" \
		AXIOM_BINANCE_COMBINED_TRIANGLE_REGION="$(AXIOM_BINANCE_COMBINED_TRIANGLE_REGION)" \
		$(GO) test ./internal/exchanges/binance \
		-run '^TestProductionPublicBinanceCombinedTriangleCoherenceProbe$$' -count=1 -timeout=7m -v

coherent-market-data-local-qualify: coherent-market-data-model-qualify coherent-market-data-postgres-qualify verify ## Pass every non-soak coherent market data gate cumulatively.

coherent-market-data-soak-smoke: ## Run the 20-second non-formal six-collector coherent market data qualification harness.
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT)" || { echo "AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT is required" >&2; exit 1; }
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT)" || { echo "AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT is required" >&2; exit 1; }
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION)" || { echo "AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION is required" >&2; exit 1; }
	@test "$(AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT)" = "$$(git rev-parse HEAD)" || { echo "AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT must equal committed HEAD" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "coherent market data smoke requires an exact clean committed source" >&2; exit 1; }
	@AXIOM_COHERENT_MARKET_DATA_SOAK_SMOKE=1 AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT="$(AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT)" AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT="$(AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT)" AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION="$(AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION)" $(GO) test ./internal/qualification -run '^TestCoherentMarketDataPublicSoakHarnessSmoke$$' -count=1 -timeout=5m -v

coherent-market-data-soak-qualify: ## Run the explicit formal 72-hour coherent market data qualification; never use this target for smoke.
	@test "$(AXIOM_COHERENT_MARKET_DATA_SOAK)" = "1" || { echo "AXIOM_COHERENT_MARKET_DATA_SOAK=1 explicit opt-in is required" >&2; exit 1; }
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT)" || { echo "AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT is required" >&2; exit 1; }
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT)" || { echo "AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT is required" >&2; exit 1; }
	@test -n "$(AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION)" || { echo "AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION is required" >&2; exit 1; }
	@test "$(AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT)" = "$$(git rev-parse HEAD)" || { echo "AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT must equal committed HEAD" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "formal coherent market data qualification requires a clean committed source" >&2; exit 1; }
	@AXIOM_COHERENT_MARKET_DATA_SOAK=1 AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT="$(AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT)" AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT="$(AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT)" AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION="$(AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION)" $(GO) test ./internal/qualification -run '^TestCoherentMarketDataContinuous72HourPublicSoak$$' -count=1 -timeout=73h -v

mean-reversion-sqlc: ## Generate and compile the reviewed mean-reversion and research queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= $(GO) test ./internal/storage/postgres/...

mean-reversion-model-qualify: ## Exercise exact mean reversion decisions through shared allocation, risk, execution, simulation, and accounting.
	@$(GO) test ./internal/strategies/meanreversion ./internal/portfolio ./internal/risk ./internal/backtest -count=1 -v
	@$(GO) test -race ./internal/strategies/meanreversion ./internal/portfolio ./internal/risk -count=1
	@$(NODE) scripts/check-mean-reversion-strategy-boundary.mjs

mean-reversion-postgres-qualify: ## Run clean-install and coherent market data-upgrade mean reversion gates on PostgreSQL 18 *_mean_reversion_test databases.
	@test -n "$(AXIOM_MEAN_REVERSION_TEST_DSN)" || { echo "AXIOM_MEAN_REVERSION_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN)" || { echo "AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) mean-reversion-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_MEAN_REVERSION_TEST_DSN="$(AXIOM_MEAN_REVERSION_TEST_DSN)" \
		AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN="$(AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres -run '^TestMeanReversionPostgres(CleanInstall|CoherentMarketDataToMeanReversionUpgrade)Qualification$$' -count=1 -v

mean-reversion-research-qualify: ## Verify separate deterministic mean reversion research contracts and the independent Python checker.
	@python3 -c 'import sys; assert sys.version_info[:3] == (3, 12, 3), sys.version'
	@PYTHONPATH=research/src python3 -m unittest discover -s research/tests
	@$(GO) test ./internal/research -count=1 -v

mean-reversion-local-qualify: mean-reversion-model-qualify mean-reversion-postgres-qualify mean-reversion-research-qualify ## Pass every non-soak mean reversion validation gate cumulatively.
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

triangular-arbitrage-sqlc: ## Generate and compile the reviewed triangular arbitrage triangular-arbitrage queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

triangular-arbitrage-model-qualify: ## Exercise exact triangular arbitrage evaluation, atomic claims, central risk, sequential recovery, lifetime, and accounting.
	@$(GO) test ./internal/config ./internal/accounting ./internal/execution ./internal/portfolio \
		./internal/risk ./internal/strategies/arbitrage ./internal/strategies/triangular -count=1 -v
	@$(GO) test -race ./internal/portfolio ./internal/execution \
		./internal/strategies/arbitrage ./internal/strategies/triangular -count=1
	@$(GO) test ./internal/strategies/triangular -run '^$$' \
		-bench '^BenchmarkTriangularEvaluator$$' -benchmem -count=1
	@$(GO) test ./internal/strategies/triangular -run '^$$' \
		-fuzz '^FuzzTriangularExactCycles$$' -fuzztime 3s
	@$(NODE) scripts/check-triangular-arbitrage-strategy-boundary.mjs

triangular-arbitrage-postgres-qualify: ## Run clean-install and exact mean reversion-upgrade triangular arbitrage gates on PostgreSQL 18 *_triangular_arbitrage_test databases.
	@test -n "$(AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN)" || { echo "AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN)" || { echo "AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) triangular-arbitrage-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN="$(AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN)" \
		AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN="$(AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestTriangularArbitragePostgres(CleanInstall|MeanReversionToTriangularArbitrageUpgrade)Qualification$$' -count=1 -v

triangular-arbitrage-local-qualify: triangular-arbitrage-model-qualify triangular-arbitrage-postgres-qualify ## Pass every non-soak triangular arbitrage validation gate cumulatively.
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

cross-exchange-arbitrage-sqlc: ## Generate and compile the reviewed cross-exchange arbitrage cross-exchange-arbitrage queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

cross-exchange-arbitrage-model-qualify: ## Exercise exact cross-exchange arbitrage coherent evaluation, closed-cycle economics, atomic claims, concurrent recovery, inventory, and accounting.
	@$(GO) test ./internal/config ./internal/accounting ./internal/execution ./internal/portfolio \
		./internal/risk ./internal/strategies/arbitrage ./internal/strategies/crossarb -count=1 -v
	@$(GO) test -race ./internal/portfolio ./internal/execution \
		./internal/strategies/arbitrage ./internal/strategies/crossarb -count=1
	@$(GO) test ./internal/strategies/crossarb -run '^$$' \
		-bench '^BenchmarkCrossExchangeEvaluator$$' -benchmem -count=1
	@$(GO) test ./internal/strategies/crossarb -run '^$$' \
		-fuzz '^FuzzCrossExchangeClosedCycle$$' -fuzztime 3s
	@$(NODE) scripts/check-cross-exchange-strategy-boundary.mjs

cross-exchange-arbitrage-postgres-qualify: ## Run clean-install and exact triangular arbitrage-upgrade cross-exchange arbitrage gates on PostgreSQL 18 *_cross_exchange_arbitrage_test databases.
	@test -n "$(AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN)" || { echo "AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN)" || { echo "AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) cross-exchange-arbitrage-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN="$(AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN)" \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN="$(AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestCrossExchangeArbitragePostgres(CleanInstall|TriangularArbitrageToCrossExchangeArbitrageUpgrade)Qualification$$' -count=1 -v

cross-exchange-arbitrage-local-qualify: triangular-arbitrage-model-qualify triangular-arbitrage-postgres-qualify cross-exchange-arbitrage-model-qualify cross-exchange-arbitrage-postgres-qualify ## Pass every non-soak triangular arbitrage and cross-exchange arbitrage validation gate cumulatively.
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

inventory-rebalancing-sqlc: ## Generate and compile the reviewed inventory rebalancing advisory-rebalancing queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

inventory-rebalancing-model-qualify: ## Exercise reviewed facts, exact route costs, natural reversal, deterministic search, and advisory evidence.
	@$(GO) test ./internal/config ./internal/rebalancing ./internal/portfolio -count=1 -v
	@$(GO) test -race ./internal/rebalancing ./internal/portfolio -count=1
	@$(GO) test ./internal/rebalancing -run '^$$' \
		-bench '^BenchmarkAdvisoryOptimizer$$' -benchmem -count=1
	@$(GO) test ./internal/rebalancing -run '^$$' \
		-fuzz '^FuzzAdvisoryOptimizerPreservesExactNonNegativeCost$$' -fuzztime 3s
	@$(NODE) scripts/check-inventory-rebalancing-rebalancing-boundary.mjs

inventory-rebalancing-postgres-qualify: ## Run clean-install and exact cross-exchange arbitrage-upgrade inventory rebalancing gates on PostgreSQL 18 *_inventory_rebalancing_test databases.
	@test -n "$(AXIOM_INVENTORY_REBALANCING_TEST_DSN)" || { echo "AXIOM_INVENTORY_REBALANCING_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN)" || { echo "AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) inventory-rebalancing-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_INVENTORY_REBALANCING_TEST_DSN="$(AXIOM_INVENTORY_REBALANCING_TEST_DSN)" \
		AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN="$(AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestInventoryRebalancingPostgres(CleanInstall|CrossExchangeArbitrageToInventoryRebalancingUpgrade)Qualification$$' -count=1 -v

inventory-rebalancing-security-qualify: ## Prove inventory rebalancing has no external asset-movement execution surface in source, API, UI, config, or binary.
	@$(NODE) scripts/check-inventory-rebalancing-rebalancing-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"
	@$(MAKE) build-backend GO="$(GO)"
	@bash scripts/check-inventory-rebalancing-binary-boundary.sh "$(PLATFORM)"

inventory-rebalancing-local-qualify: triangular-arbitrage-model-qualify triangular-arbitrage-postgres-qualify cross-exchange-arbitrage-model-qualify cross-exchange-arbitrage-postgres-qualify inventory-rebalancing-model-qualify inventory-rebalancing-postgres-qualify inventory-rebalancing-security-qualify ## Pass every non-soak triangular arbitrage, cross-exchange arbitrage, and inventory rebalancing validation gate cumulatively.
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

research-promotion-sqlc: ## Generate and compile the reviewed research promotion research-governance queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

research-promotion-model-qualify: ## Exercise preregistration, locked suites, statistics, evidence eligibility, comparison, and promotion.
	@$(GO) test ./internal/research -count=1 -v
	@$(GO) test -race ./internal/research -count=1
	@$(GO) test ./internal/research -run '^$$' \
		-bench '^BenchmarkResearchPromotionValidationSuite$$' -benchmem -count=1
	@$(GO) test ./internal/research -run '^$$' \
		-fuzz '^FuzzResearchPromotionMultipleTestingPreservesProbabilityBounds$$' -fuzztime 3s
	@$(NODE) scripts/check-research-promotion-research-boundary.mjs

research-promotion-postgres-qualify: ## Run clean-install and exact inventory rebalancing-upgrade research promotion gates on PostgreSQL 18 *_research_promotion_test databases.
	@test -n "$(AXIOM_RESEARCH_PROMOTION_TEST_DSN)" || { echo "AXIOM_RESEARCH_PROMOTION_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN)" || { echo "AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) research-promotion-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_RESEARCH_PROMOTION_TEST_DSN="$(AXIOM_RESEARCH_PROMOTION_TEST_DSN)" \
		AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN="$(AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestResearchPromotionPostgres(CleanInstall|InventoryRebalancingToResearchPromotionUpgrade)Qualification$$' -count=1 -v

research-promotion-research-qualify: ## Independently recalculate research promotion statistics and eligibility outside the Go runtime.
	@python3 -c 'import sys; assert sys.version_info[:3] == (3, 12, 3), sys.version'
	@PYTHONPATH=research/src python3 -m unittest discover -s research/tests
	@$(NODE) scripts/check-research-promotion-research-boundary.mjs

research-promotion-local-qualify: triangular-arbitrage-model-qualify triangular-arbitrage-postgres-qualify cross-exchange-arbitrage-model-qualify cross-exchange-arbitrage-postgres-qualify inventory-rebalancing-model-qualify inventory-rebalancing-postgres-qualify inventory-rebalancing-security-qualify research-promotion-model-qualify research-promotion-postgres-qualify research-promotion-research-qualify ## Pass every non-soak triangular arbitrage, cross-exchange arbitrage, inventory rebalancing, and research promotion validation gate cumulatively.
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(MAKE) verify GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"

multi-exchange-console-sqlc: ## Generate and compile the reviewed multi-exchange console multi-exchange console queries.
	@command -v "$(SQLC)" >/dev/null || { echo "sqlc executable is required" >&2; exit 1; }
	@$(SQLC) generate --file sqlc.yaml
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/storage/postgres/...

multi-exchange-console-model-qualify: ## Exercise deterministic replay faults and fail-closed multi-exchange console request boundaries.
	@$(GO) test ./internal/replay ./internal/api/console -count=1 -v
	@$(GO) test -race ./internal/replay ./internal/api/console -count=1

multi-exchange-console-postgres-qualify: ## Run clean-install and exact research promotion-upgrade multi-exchange console gates on PostgreSQL 18 *_multi_exchange_console_test databases.
	@test -n "$(AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN)" || { echo "AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN is required" >&2; exit 1; }
	@test -n "$(AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN)" || { echo "AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN is required" >&2; exit 1; }
	@$(MAKE) multi-exchange-console-sqlc GO="$(GO)" SQLC="$(SQLC)"
	@AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN="$(AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN)" \
		AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN="$(AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN)" \
		$(GO) test ./internal/storage/postgres \
		-run '^TestMultiExchangeConsolePostgres(CleanInstall|ResearchPromotionToMultiExchangeConsoleUpgrade)Qualification$$' -count=1 -v

multi-exchange-console-api-qualify: ## Verify generated multi-exchange console OpenAPI contracts, generic projections, commands, and SSE envelopes.
	@$(MAKE) contracts-check GO="$(GO)" NODE="$(NODE)" COREPACK="$(COREPACK)"
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
		$(GO) test ./internal/api/console ./internal/storage/postgres \
		-run 'multi-exchange console|Stream|Cursor|Filter' -count=1 -v
	@$(NODE) scripts/check-multi-exchange-console-console-boundary.mjs

multi-exchange-console-frontend-qualify: ## Typecheck, lint, test, and build the accessible responsive multi-exchange console.
	@$(PNPM) --filter @axiom/web typecheck
	@$(PNPM) --filter @axiom/web lint
	@$(PNPM) --filter @axiom/web test
	@$(PNPM) --filter @axiom/web build

multi-exchange-console-security-qualify: ## Prove multi-exchange console remains public-data, virtual, advisory, and unable to submit real orders or move assets.
	@$(NODE) scripts/check-multi-exchange-console-console-boundary.mjs
	@$(MAKE) security-static GO="$(GO)"
	@$(MAKE) build-backend GO="$(GO)"
	@bash scripts/check-inventory-rebalancing-binary-boundary.sh "$(PLATFORM)"
	@bash scripts/check-multi-exchange-console-binary-boundary.sh "$(PLATFORM)"

multi-exchange-console-live-qualify: ## Verify multi-exchange console navigation, responsive layout, keyboard flow, and simulation lock in Chromium.
	@$(PNPM) --filter @axiom/web test:e2e --grep 'Multi-exchange workflows'

multi-exchange-console-local-qualify: triangular-arbitrage-model-qualify triangular-arbitrage-postgres-qualify cross-exchange-arbitrage-model-qualify cross-exchange-arbitrage-postgres-qualify inventory-rebalancing-model-qualify inventory-rebalancing-postgres-qualify inventory-rebalancing-security-qualify research-promotion-model-qualify research-promotion-postgres-qualify research-promotion-research-qualify multi-exchange-console-model-qualify multi-exchange-console-postgres-qualify multi-exchange-console-api-qualify multi-exchange-console-frontend-qualify multi-exchange-console-security-qualify multi-exchange-console-live-qualify ## Pass every non-soak triangular arbitrage-multi-exchange console validation gate cumulatively.
	@AXIOM_MEAN_REVERSION_TEST_DSN= AXIOM_MEAN_REVERSION_UPGRADE_TEST_DSN= \
		AXIOM_TRIANGULAR_ARBITRAGE_TEST_DSN= AXIOM_TRIANGULAR_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_CROSS_EXCHANGE_ARBITRAGE_TEST_DSN= AXIOM_CROSS_EXCHANGE_ARBITRAGE_UPGRADE_TEST_DSN= \
		AXIOM_INVENTORY_REBALANCING_TEST_DSN= AXIOM_INVENTORY_REBALANCING_UPGRADE_TEST_DSN= \
		AXIOM_RESEARCH_PROMOTION_TEST_DSN= AXIOM_RESEARCH_PROMOTION_UPGRADE_TEST_DSN= \
		AXIOM_MULTI_EXCHANGE_CONSOLE_TEST_DSN= AXIOM_MULTI_EXCHANGE_CONSOLE_UPGRADE_TEST_DSN= \
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
