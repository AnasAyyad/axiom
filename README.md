# Axiom

Axiom is a professional, spot-only cryptocurrency research platform for historical backtesting, deterministic replay, live public-market shadow trading, realistic execution simulation, and carefully controlled exchange sandbox integration.

## Safety boundary

Axiom V1A-V1D never submits real-money production orders. It does not support withdrawals, transfers, margin, leverage, futures, borrowing, lending, staking, or short selling.

Binance Spot Testnet and Bybit Demo are planned only for later virtual-fund integration validation. They are not evidence that a strategy is profitable.

## Current repository state

A0 and A1 are verified. A1 contains the pinned Go/React health
skeleton, OpenAPI-generated model types, single platform command surface,
production Dockerfile, Compose contract, and CI governance. Trading, recording,
replay, accounting, risk, and strategy behavior remain unavailable until their
own phases pass.

- Product and release specification: [crypto_bot_v1_codex_spec.md](crypto_bot_v1_codex_spec.md)
- Agent instructions: [AGENTS.md](AGENTS.md)
- Local/server deployment guide: [deploy/README.md](deploy/README.md)
- Safe configuration template: [.env.example](.env.example)
- Compose deployment contract: [docker-compose.yml](docker-compose.yml)
- A0 review evidence: [docs/releases/evidence/a0-review.md](docs/releases/evidence/a0-review.md)
- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Coding standards: [docs/coding-standards.md](docs/coding-standards.md)

## Exact toolchains

- Go `1.26.5`
- Node.js `24.18.0`
- pnpm `11.12.0` through Corepack
- PostgreSQL `18.4-alpine`
- React `19.2.7`, TypeScript `7.0.2`, and Vite `8.1.4`

Run `make preflight` after installing Go, Node, Docker/Compose, and ripgrep. Go
tools are pinned in `go.mod`; JavaScript tools are pinned in `pnpm-lock.yaml`, so
no global linter, generator, or test runner is required.

## Local A1 setup

Install dependencies, generate contracts, and run the full local gate:

```bash
corepack enable
corepack install --global pnpm@11.12.0
pnpm install --frozen-lockfile
make generate
make verify
```

Use the deployment guide to prepare `.env`, the private database secret files,
and writable directory ownership. Build the image, then start PostgreSQL, the
one-shot A1 migration, API, and public-data shadow engine through the reviewed
image-based Compose profile:

```bash
make image
APP_IMAGE=axiom:local APP_PULL_POLICY=never \
  docker compose --env-file .env --profile app up -d --wait
```

For frontend development, run `make dev-web` in another terminal; Vite proxies
API requests to the loopback-published Compose API. `make compose-smoke` runs an
ephemeral full A1 application-profile walkthrough after an image has been built.

The API exposes `/health/live`, `/health/ready`, `/api/v1/system/version`,
`/api/v1/system/build`, and `/api/v1/system/status`. Readiness pings PostgreSQL;
it never mirrors liveness. The UI always displays `REAL TRADING DISABLED`.

Build the embedded binary or minimal `scratch` image with `make build` or
`make image`. `make image-reproducibility` rebuilds and compares the complete
runtime configuration and filesystem descriptors while retaining BuildKit's
provenance envelope. The image runs as numeric non-root user `10001:70` and
contains no shell or package manager. Stop the local application with
`docker compose --env-file .env --profile app down`.

## Release sequence

- **V1A:** deterministic public-data research core and first live-shadow strategy
- **V1B:** Binance/Bybit multi-exchange strategy research
- **V1C:** authenticated virtual-fund Binance Testnet and Bybit Demo integration
- **V1D:** complete dashboard, reporting, operations, and readiness certification

Release gates are cumulative: later work cannot weaken earlier safety, accounting, replay, or risk controls.
