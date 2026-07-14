SHELL := /usr/bin/env bash

GO ?= go
NODE ?= node
COREPACK ?= corepack
PNPM := $(COREPACK) pnpm
PLATFORM := bin/platform
PLAN_FILE ?= /home/anas/.codex/attachments/7085c3d9-bb74-4587-8af7-85d8e499faf1/pasted-text-1.txt

.DEFAULT_GOAL := help

.PHONY: help preflight deps generate contracts contracts-check docs-check format format-check lint test test-backend test-frontend test-race fuzz-smoke build build-backend build-frontend compose-validate compose-smoke security-static vulnerability verify dev-api dev-web migrate image image-reproducibility

IMAGE ?= axiom:local
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

fuzz-smoke: ## Run the A1 execution-mode fuzz target briefly.
	@$(GO) test ./internal/config -run '^$$' -fuzz '^FuzzParseExecutionMode$$' -fuzztime 3s

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
	@tests/integration/smoke-compose-app.sh "$(IMAGE)"

security-static: ## Run secret and prohibited-capability scans with negative tests.
	@scripts/check-secret-patterns.sh
	@scripts/test-check-secret-patterns.sh
	@scripts/check-prohibited-capabilities.sh
	@scripts/test-check-prohibited-capabilities.sh

vulnerability: ## Scan the Go dependency graph for known vulnerabilities.
	@$(GO) tool govulncheck ./...

verify: preflight format-check contracts-check docs-check lint test test-race fuzz-smoke build compose-validate security-static vulnerability ## Run the complete local A1 quality gate.

dev-api: ## Run the local API health application.
	@$(GO) run ./cmd/platform api

dev-web: ## Run Vite with the API proxy.
	@$(PNPM) --filter @axiom/web dev

migrate: ## Run the exact A1 migration command surface.
	@$(GO) run ./cmd/platform admin migrate

image: ## Build the pinned minimal Axiom image.
	@docker build --file deploy/docker/Dockerfile --tag "$(IMAGE)" \
		--build-arg "VERSION=$(VERSION)" \
		--build-arg "COMMIT=$(COMMIT)" \
		--build-arg "BUILT_AT=$(BUILT_AT)" \
		--build-arg "DIRTY=$(DIRTY)" .

image-reproducibility: image ## Rebuild and compare the complete runtime image payload.
	@VERSION="$(VERSION)" COMMIT="$(COMMIT)" BUILT_AT="$(BUILT_AT)" DIRTY="$(DIRTY)" \
		scripts/check-image-reproducibility.sh "$(IMAGE)" "$(REBUILD_IMAGE)"
