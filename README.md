# Axiom

Axiom is a professional, spot-only cryptocurrency research platform for historical backtesting, deterministic replay, live public-market shadow trading, realistic execution simulation, and carefully controlled exchange sandbox integration.

## Safety boundary

Axiom V1A-V1D never submits real-money production orders. It does not support withdrawals, transfers, margin, leverage, futures, borrowing, lending, staking, or short selling.

Binance Spot Testnet and Bybit Demo integrations exist only as separately
gated, default-off virtual-fund plumbing. They are not evidence that a strategy
is profitable and cannot target production-private endpoints.

## Current repository state

The repository implements the cumulative V1A-V1D application through D5 and the
D6 repository certification controls: deterministic public-data recording and
replay, virtual accounting/allocation/risk, six research strategy/advisory
families, backtest/replay/shadow labs, closed Testnet/Demo engines, the React
command center, reports/incidents/audit/alerts, backup/lifecycle hardening, and
fail-closed signed release evidence validation.

Implementation is not final acceptance. B2 and C6 formal 72-hour verdicts, D5's
seven-day declared-server verdict, several earlier cumulative owner/security
acceptances, current independent reviews, and exact-candidate evidence remain
missing. Axiom V1 is **NOT CERTIFIED**.

- Product and release specification: [crypto_bot_v1_codex_spec.md](crypto_bot_v1_codex_spec.md)
- Agent instructions: [AGENTS.md](AGENTS.md)
- Local/server deployment guide: [deploy/README.md](deploy/README.md)
- Safe configuration template: [.env.example](.env.example)
- Compose deployment contract: [docker-compose.yml](docker-compose.yml)
- A0 review evidence: [docs/releases/evidence/a0-review.md](docs/releases/evidence/a0-review.md)
- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Coding standards: [docs/coding-standards.md](docs/coding-standards.md)
- Current implementation state: [docs/implementation-status.md](docs/implementation-status.md)
- D6 readiness and blockers: [docs/releases/v1d-d6-readiness.md](docs/releases/v1d-d6-readiness.md)
- Section 35 acceptance matrix: [docs/releases/v1d-section-35-matrix.md](docs/releases/v1d-section-35-matrix.md)
- Complete operations runbook: [docs/operations/runbook.md](docs/operations/runbook.md)

## Exact toolchains

- Go `1.26.5`
- Node.js `24.18.0`
- pnpm `11.12.0` through Corepack
- PostgreSQL `18.4-alpine`
- React `19.2.7`, React Router `8.3.0`, TypeScript `7.0.2`, and Vite `8.1.5`

Run `make preflight` after installing Go, Node, Docker/Compose, and ripgrep. Go
tools are pinned in `go.mod`; JavaScript tools are pinned in `pnpm-lock.yaml`, so
no global linter, generator, or test runner is required.

## Local setup and verification

Install dependencies, generate contracts, and run the full local gate:

```bash
corepack enable
corepack install --global pnpm@11.12.0
pnpm install --frozen-lockfile
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

The API exposes versioned health, build, operational, lab, qualification,
reporting, incident, and audit surfaces. Readiness checks required dependencies;
it never mirrors liveness. The UI always displays `REAL TRADING DISABLED`.

Build the embedded binary or minimal `scratch` image with `make build` or
`make image`. `make image-reproducibility` rebuilds and compares the complete
runtime configuration and filesystem descriptors while retaining BuildKit's
provenance envelope. The image runs as numeric non-root user `10001:70` and
contains no shell or package manager. Stop the local application with
`docker compose --env-file .env --profile app down`.

## Release and certification sequence

- **V1A:** deterministic public-data research core and first live-shadow strategy
- **V1B:** Binance/Bybit multi-exchange strategy research
- **V1C:** authenticated virtual-fund Binance Testnet and Bybit Demo integration
- **V1D:** complete dashboard, reporting, operations, and readiness certification

Release gates are cumulative: later work cannot weaken earlier safety, accounting, replay, or risk controls.

Use `make v1d-d6-local-qualify` for the repository-verifiable non-soak D6
aggregate. PostgreSQL phase gates that require disposable databases are run
separately with their dedicated DSNs. `make d6-final-certification` is
deliberately default-off and must reject unless every current signed formal
prerequisite and independent review is present. Never use local or hosted CI
success as qualification or certification.
