# Axiom
## Spot Crypto Research & Shadow-Trading Platform
## Production-Grade Product Requirements, Architecture, Trading Logic, Engineering Standards, and Codex Release Plan

**Document status:** Normative implementation and release specification  
**Primary implementation agent:** Codex  
**Primary owner:** Anas Abu-Sulik  
**Program safety status:** Real-money trading must be impossible in every release defined here  
**Last updated:** 12 July 2026

This document defines one mature product delivered through four cumulative releases: **V1A**, **V1B**, **V1C**, and **V1D**. The release boundaries are evidence gates, not scope reductions. V1D includes the complete professional research, simulation, sandbox-execution, dashboard, reporting, security, and operational scope described here. None of these releases may submit real-money production orders.

---

# 0. Instructions to Codex

This document is the source of truth for the complete V1A-V1D program. Read it completely before creating or modifying code.

Codex must:

1. Implement the project release by release and phase by phase in the order defined here. Do not skip a release gate merely because a later feature appears independent.
2. Keep all real-money trading, withdrawals, margin, futures, leverage, borrowing, lending, staking, and short-selling unavailable in V1.
3. Use reusable exchange contracts that allow another adapter to be added without changing strategy, allocator, accounting, or risk-domain code, while implementing only Binance and Bybit in this program.
4. Implement all agreed strategy families by V1D for historical backtesting, deterministic replay, live shadow trading, and simulated execution. Earlier releases must remain usable, deployable, and independently verifiable.
5. Use official exchange demo/test environments only to validate order integration. Never treat demo/testnet performance as evidence of strategy profitability.
6. Update project documentation, implementation status, architecture decision records, API documentation, and tests whenever behavior changes.
7. Prefer correctness, determinism, auditability, and safety over cleverness or premature optimization.
8. Never bypass the portfolio allocator, risk engine, order state machine, or reconciliation system.
9. Never use binary floating-point values for balances, prices, quantities, fees, or P&L.
10. Keep files focused and small. A file should have one clear responsibility. Split a file before it becomes difficult to review.
11. Add comments that explain business rules, formulas, invariants, edge cases, exchange-specific behavior, and the reason behind non-obvious decisions. Do not add comments that merely repeat the code.
12. Write tests with every feature. Do not postpone core tests until the end.
13. Do not silently invent requirements. Record unavoidable assumptions in an Architecture Decision Record, or mark the item as blocked in `docs/implementation-status.md`.
14. Complete each phase’s acceptance criteria before treating that phase as complete.
15. Run formatting, linting, static analysis, unit tests, integration tests, race detection, frontend type checking, and frontend builds before completing a phase.

## Required Codex workflow

For every release and phase:

1. Read this specification and all existing ADRs.
2. Update `docs/implementation-status.md` with the planned slice.
3. Write or update tests first where practical.
4. Implement the smallest coherent vertical slice.
5. Run all relevant quality checks.
6. Update API documentation, configuration examples, database migrations, UI documentation, and operational runbooks.
7. Record important decisions in `docs/adr/`.
8. Mark the phase item complete only after its acceptance tests pass.

The project must remain runnable after every completed phase. Every completed release must be deployable, observable, recoverable, and supported by an evidence bundle containing test results, reproducibility metadata, known limitations, and unresolved risks.

---

# 1. Product vision

Build a professional, spot-only cryptocurrency research and shadow-trading platform that can:

- Collect and normalize live and historical market data.
- Maintain reliable local order books.
- Backtest and deterministically replay strategy logic.
- Run multiple strategies concurrently against live production market data without sending real orders.
- Simulate realistic execution, fees, slippage, latency, partial fills, and failures.
- Validate authenticated order integration using official demo/test environments.
- Allocate virtual capital across strategies and exchanges.
- Enforce centralized risk controls.
- Reconcile all simulated or demo orders and balances.
- Explain every decision and rejection.
- Provide a professional React dashboard for monitoring, research, comparison, reporting, and incident investigation.
- Support additional exchanges later without changing strategy or risk logic.

The platform is not a guaranteed-profit machine. It is a controlled research and execution framework that determines whether a strategy has positive net expectancy after realistic costs and risks.

---

# 2. Non-negotiable boundaries

## 2.1 Spot-only rules

The system may only model or place spot orders involving assets already owned by the relevant virtual or demo account.

The following are forbidden in V1:

- Futures
- Perpetual contracts
- Options
- Margin trading
- Leverage
- Borrowing
- Lending
- Interest or earn products
- Staking integrations
- Short selling
- Selling an asset not currently owned
- Leveraged tokens
- Tokenized derivatives
- Automated withdrawals
- Automated blockchain transfers
- Real-money production orders

Every exchange adapter must declare supported product categories. V1 adapters must reject every product category except `spot`.

## 2.2 Asset-screening boundary

The application must not make religious rulings. It must implement a configurable asset registry with these states:

- `approved`
- `scan_only`
- `blocked`
- `pending_review`

Only `approved` assets may be used by any order-capable or fill-simulating mode, including paper, shadow, testnet, and demo. `scan_only` assets may be observed but not traded or treated as executable inventory. The default approved assets are:

- USDT as the virtual settlement asset
- BTC
- ETH

The registry must record who changed a status, when it changed, and the reason.

## 2.3 V1 real-money hard lock

V1 must make production order submission impossible through multiple independent controls:

- No production trading credentials are required.
- Production private endpoints are disabled by configuration schema.
- The real-order implementation is absent or compiled behind a future explicit build capability that V1 does not enable.
- The backend rejects `execution_mode=live`.
- The database does not permit a live execution mode in V1.
- The frontend contains no control that can enable live trading.
- Every screen clearly labels balances and trades as virtual, shadow, testnet, or demo.
- Startup fails if prohibited configuration keys such as withdrawal or margin credentials are detected.
- Production-public market-data clients and authenticated test/demo clients are separate types with separate constructors and transports.
- A credential-bearing client may target only code-owned allowlisted test/demo hosts and authenticated routes. Private base URLs and private-route allowlists are not environment-configurable.
- Public clients cannot receive credentials or sign requests.
- Signed requests are rejected before network I/O if the destination host, environment, method, route, product category, or request fields are outside the compiled allowlist.
- The Bybit demo serializer must force `category=spot` and `isLeverage=0`; any margin, collateral, borrowing, leverage, or non-spot field must fail closed.
- Asset approval is enforced again immediately before every testnet/demo submission.
- CI must capture outbound requests and prove that no signed order request can reach a production host.
- Demo/testnet integrations and test-order submission are disabled by default and require separate gates plus a short-lived audited administrative arm.

Required execution modes:

- `backtest`
- `replay`
- `shadow`
- `paper` for historical or synthetic market data with simulated execution; unlike shadow mode it never consumes a live production feed
- `testnet`
- `demo`

Forbidden mode in every release:

- `live`

---

# 3. Core decisions

## 3.1 Exchanges

Design the platform to be exchange-extensible without changing strategy, allocator, accounting, or risk-domain code. Extensibility is proven when a new adapter passes the common conformance suite; the specification does not claim a literally unlimited operational exchange count.

Implement these concrete V1 adapters:

1. **Binance Spot**
2. **Bybit Spot**

Reasons:

- Both provide mature spot REST and WebSocket interfaces.
- Binance provides an official Spot Testnet.
- Bybit provides an official demo-trading environment, with demo private endpoints and production public market data.
- The pair allows cross-exchange research while keeping V1 integration scope manageable.

Future adapters, not part of V1 implementation:

- Kraken
- KuCoin
- Gate.io
- Coinbase or another approved exchange

The core domain, strategies, risk engine, simulator, recorder, and UI must not depend on Binance- or Bybit-specific models.

## 3.2 Languages and platform

### Backend and trading engine

- Go 1.26.x as the initial toolchain line, pinned to an exact supported security patch in `go.mod`, CI, and the build image. Upgrades require an ADR and replay-compatibility evidence.
- Standard library first.
- Use a lightweight HTTP router only when it adds clear value.
- Use bounded goroutines and typed channels for concurrent pipelines.
- Use `context.Context` for cancellation, deadlines, and shutdown propagation.
- Use `log/slog` JSON logging, `pgx/v5` for PostgreSQL, `sqlc` for reviewed type-safe queries, and a version-controlled migration tool such as Goose unless an ADR selects an equivalent.
- Wrap a reviewed decimal implementation behind the project’s financial types; benchmark `govalues/decimal` and `cockroachdb/apd` before selecting one. Third-party decimal types must never leak into domain APIs.
- Prefer `parquet-go` or Apache Arrow Go for Parquet after a Phase A4 compatibility and performance spike.
- Use Prometheus client instrumentation and OpenTelemetry-compatible tracing interfaces. Tracing is optional at runtime and must not block the hot path.

### Research and offline analysis

- Python for notebooks, exploratory analysis, statistical validation, and report prototypes.
- Pin a supported Python release and lock research dependencies with `uv` or an equivalent reproducible tool.
- Prefer Polars/PyArrow for large recorded datasets; NumPy, SciPy, statsmodels, and pandas may be used where their statistical ecosystem adds value.
- Use Jupyter only as a presentation/exploration layer. Reusable loading, validation, bootstrap, multiple-testing, and reporting logic belongs in tested Python modules.
- Use Hypothesis for research-data and statistical-invariant property tests where useful.
- Production strategy decisions must run in Go.
- Python must never become a required component of the live shadow hot path.

### Frontend

- React 19.2.x, pinned to an exact security-patched version
- TypeScript with strict mode, including `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes`
- Vite 8.x, pinned to an exact version, with Node.js 24 LTS and `pnpm` pinned through Corepack
- Feature-based architecture
- TanStack Query for server state and TanStack Table for large research tables
- React Router for route ownership and URL-addressable filters
- Zod at untrusted client boundaries, with primary API types generated from OpenAPI
- Apache ECharts or TradingView Lightweight Charts behind project-owned chart adapters; the selected library must pass accessibility, performance, licensing, and maintenance review
- Radix UI primitives or an equivalent accessible headless component system; application styling remains project-owned
- Vitest, React Testing Library, Playwright, and axe-core for unit, integration, end-to-end, and accessibility testing
- WebSocket or server-sent events for live dashboard updates

### Storage

Use two complementary storage types:

1. **PostgreSQL 18** for transactional and relational data, pinned to the current supported minor release in deployment manifests:
   - configuration metadata
   - strategy versions
   - decisions
   - virtual portfolios
   - orders and fills
   - backtest runs
   - metrics summaries
   - incidents
   - audit records

2. **Compressed append-only market-data files**, preferably Parquet with Zstandard compression, for high-volume raw market events:
   - order-book snapshots
   - order-book deltas
   - trades
   - tickers
   - candles
   - exchange and local timestamps

Do not put every raw depth event into PostgreSQL. Store file-segment metadata and checksums in PostgreSQL.

### Infrastructure

- Docker and Docker Compose for local development.
- Docker Compose for single-server shadow/test deployment, using profiles to isolate public-only, authenticated sandbox, worker, observability, and edge services.
- Linux for deployed shadow/test environments.
- Prometheus-compatible metrics.
- Grafana-compatible dashboards where useful, while the React application remains the business dashboard.
- Structured JSON logs.
- GitHub Actions or equivalent CI.
- Caddy or an equivalent reviewed reverse proxy for automatic HTTPS on a single server.
- Do not add Redis, Kafka, NATS, Kubernetes, or a service mesh until profiling or availability requirements demonstrate a concrete need. PostgreSQL-backed durable jobs and an in-process event bus are the default.

## 3.3 Architecture style

Use a **modular monolith** for V1.

The hot path must run inside one Go process without internal HTTP calls, JSON serialization, or database round trips between strategy, allocator, risk, and execution modules.

Separate processes are allowed for:

- API/dashboard service if operationally useful
- market-data recorder
- offline backtest workers
- scheduled report generation

However, the initial implementation should remain as simple as possible while preserving clean module boundaries.

## 3.4 Normative V1 process topology

The supported single-server deployment topology is explicit:

- `api`: browser API, authentication, administrative command intake, and live-state fan-out. It receives no exchange credentials and cannot call an order endpoint.
- `engine-shadow`: the single owner of the live public-market hot path, strategies, allocator, risk evaluation, simulation broker, and virtual ledger for shadow sessions. It has no private exchange credentials.
- `recorder`: public market-data recording and segment finalization. It has no private exchange credentials.
- `engine-binance-testnet`: optional, profile-gated single owner of Binance Spot Testnet account state and orders. It receives only Binance Testnet credentials.
- `engine-bybit-demo`: optional, profile-gated single owner of Bybit demo account state and orders. It receives only Bybit demo credentials.
- `worker`: scalable offline backtest, replay, and report work. It receives no exchange credentials; recorded datasets are read-only except for declared output locations.
- `migrate`: a one-shot database migrator using a distinct least-privilege migration role.

The authenticated engines must not run until explicitly enabled. They must acquire a database-backed execution lease with a fencing epoch scoped to exchange account and environment. Loss of the lease, database durability, or fencing validity locks the engine. A deployment or restart overlap must never create two active brokers for the same account.

Administrative commands use a durable idempotent command table or equivalent inbox. The API must not invoke the engine through an unaudited in-memory shortcut. Full high-availability leader election remains future work, but V1 requires single-writer enforcement and split-brain tests.

Engine-to-API business events are committed to a durable outbox. PostgreSQL `LISTEN/NOTIFY` may be used only as a wake-up hint; the API resumes from durable revisions after loss/reconnect. High-rate dashboard book/metric views are sampled or coalesced projections, never a database round trip in the decision path, and the engine never waits for UI delivery.

The standalone recorder collects the broad configured dataset. Each live engine also owns an embedded decision-input recorder for the exact events, connection lifecycle, clock samples, market-view versions, and model inputs that could affect its decisions. These streams use distinct session directories/manifests and may share the same storage root safely. If the embedded audit recorder cannot preserve required decision evidence, the engine pauses new decisions rather than relying on a separate subscription as an exact substitute.

Startup order for every order-capable engine is:

```text
acquire fencing lease
-> enter LOCKED
-> validate build and configuration safety manifest
-> load immutable configuration versions
-> recover incomplete intents and reservations
-> reconcile orders, fills, and balances
-> synchronize required market data
-> become READY
-> require an authorized short-lived arm before test/demo entries
```

Shutdown stops new entries first, persists/checkpoints state, resolves or quarantines nonterminal orders, releases safe reservations, flushes audit events, and only then releases the fencing lease. Readiness and liveness are separate: a stale exchange makes the engine unready/degraded without creating an automatic restart loop.

---

# 4. Default virtual capital, ownership, and valuation model

The combined default virtual portfolio is **500.00 USDT**. USDT is the trading and risk numeraire. The UI must not display the portfolio as `$500` unless a separate, versioned, quality-checked USDT/USD mark is available for the same valuation timestamp.

Every unit of every asset must have exactly one owning portfolio and sub-ledger, or one exclusive reservation. Strategy budgets are marked-net-asset-value ceilings; they are not overlapping claims on a shared exchange balance.

## 4.1 Exchange allocation

| Exchange | Virtual value (USDT) |
|---|---:|
| Binance | 250.00 |
| Bybit | 250.00 |
| Total | 500.00 |

## 4.2 Cross-exchange-only benchmark allocation per exchange

The following inventory-heavy allocation applies to the independent **cross-exchange-arbitrage-only** 500 USDT benchmark. It does not apply automatically to trend-only, mean-reversion-only, triangular-only, or combined portfolios.

| Asset | Percentage | USDT value per exchange |
|---|---:|---:|
| USDT | 60% | 150.00 |
| BTC | 25% | 62.50 equivalent |
| ETH | 15% | 37.50 equivalent |
| Total | 100% | 250.00 |

BTC and ETH quantities must be calculated using the selected initialization timestamp and a documented reference price. Store the actual quantities, reference prices, and initialization event.

The system must distinguish:

- Stablecoin balance
- BTC inventory
- ETH inventory
- Reserved balance
- Available balance
- Inventory market P&L
- Strategy trading P&L

## 4.3 Combined strategy budgets and ownership

| Strategy family | Percentage | Budget (USDT) |
|---|---:|---:|
| Cross-exchange arbitrage | 40% | 200.00 |
| Trend following | 25% | 125.00 |
| Mean reversion | 10% | 50.00 |
| Triangular arbitrage | 10% | 50.00 |
| Global reserve | 15% | 75.00 |
| Total | 100% | 500.00 |

These are capital ceilings, not required position sizes or gross-turnover limits.

The normative combined-portfolio initialization is:

| Owner | Total budget | Binance USDT | Binance BTC value | Binance ETH value | Bybit USDT | Bybit BTC value | Bybit ETH value |
|---|---:|---:|---:|---:|---:|---:|---:|
| Cross-exchange arbitrage | 200.00 | 50.00 | 31.25 | 18.75 | 50.00 | 31.25 | 18.75 |
| Trend following | 125.00 | 62.50 | 0 | 0 | 62.50 | 0 | 0 |
| Mean reversion | 50.00 | 25.00 | 0 | 0 | 25.00 | 0 | 0 |
| Triangular arbitrage | 50.00 | 25.00 | 0 | 0 | 25.00 | 0 | 0 |
| Global reserve | 75.00 | 37.50 | 0 | 0 | 37.50 | 0 | 0 |
| **Total** | **500.00** | **200.00** | **31.25** | **18.75** | **200.00** | **31.25** | **18.75** |

Values in the BTC and ETH columns are USDT valuation amounts converted into exact quantities at initialization. The initialization journal records the price source, book version, timestamp, quantity, rounding, and residual USDT.

Rules:

- Trend and mean-reversion signals may choose an eligible execution venue, but a combined portfolio may hold only one open position per strategy/asset unless a versioned strategy explicitly permits otherwise.
- The triangular strategy starts and ends in USDT. Its configured size ladder is clipped by its per-exchange available budget, fees, worst-case rounding, and reserve requirements; a configured 50 or 100 USDT ladder point is not automatically executable in the default combined portfolio.
- Cross-exchange arbitrage reserves buy-side USDT, sell-side owned inventory, both fee buffers, and the one-leg recovery allowance atomically.
- The global reserve is USDT-only, cannot be consumed by a strategy without an explicit allocator policy, and any approved transfer is an immutable ledger transaction.
- Strategy-to-strategy or exchange-to-exchange virtual ownership transfers are never implicit.
- Independent experiments and the combined portfolio use distinct accounts and database constraints so their balances can never be aggregated accidentally.

## 4.4 Independent benchmark portfolios

In addition to the combined portfolio, the research system must support independent virtual experiments, each with its own 500 USDT starting capital:

- Trend-only
- Mean-reversion-only
- Triangular-arbitrage-only
- Cross-exchange-arbitrage-only
- Combined-strategies

These portfolios are separate experiments and must never be presented as one real combined balance.

Initialization rules:

- Trend-only, mean-reversion-only, and triangular-only portfolios start 100% in USDT, normally 250 USDT per exchange so venue selection can be compared without introducing initial volatile inventory.
- Cross-exchange-arbitrage-only uses the inventory-heavy allocation in Section 4.2.
- Combined-strategies uses the ownership matrix in Section 4.3.
- An experiment may use a different initialization only through a versioned run configuration that records ownership, quantities, valuation marks, and rationale.

The backtest and shadow engines must also support configurable capital levels such as:

- 500 USDT
- 1,000 USDT
- 5,000 USDT
- 10,000 USDT

This is required because minimum order sizes, fees, depth, and slippage produce different results at different capital levels.

## 4.5 Valuation, cost basis, and stablecoin risk

- USDT is the ledger numeraire; USD is a reporting currency only.
- USD reporting requires an independent reference source selected in an ADR. Store source, timestamp, bid/ask or executable mark, quality, and staleness with every valuation snapshot.
- Mark volatile inventory using a conservative executable liquidation value at the configured size, not last trade price. Midpoint may be shown only as a secondary analytical value.
- Use weighted-average cost basis initially. FIFO may be implemented later only as a separately versioned reporting method; it must not change historical results silently.
- Realized P&L, unrealized P&L, inventory P&L, fees, slippage, latency deterioration, recovery loss, rebalancing cost, and USDT/USD valuation effect remain separate.
- Add a configurable USDT depeg warning and lock policy. A missing or stale USD reference fails USD reporting closed but does not invent a one-dollar mark.
- Daily loss uses UTC calendar days and also maintains rolling 24-hour loss. Risk may enforce the stricter result.

---

# 5. Repository structure

Use a monorepo with clearly separated backend, frontend, research, deployment, and documentation.

```text
axiom/
├── cmd/
│   ├── platform/
│   ├── api/
│   ├── trader/
│   ├── recorder/
│   ├── backtester/
│   ├── replay/
│   └── admin/
├── internal/
│   ├── domain/
│   ├── config/
│   ├── clock/
│   ├── eventbus/
│   ├── exchanges/
│   │   ├── contracts/
│   │   ├── binance/
│   │   ├── bybit/
│   │   └── simulator/
│   ├── marketdata/
│   │   ├── normalization/
│   │   ├── orderbook/
│   │   ├── candles/
│   │   ├── trades/
│   │   ├── quality/
│   │   └── recorder/
│   ├── strategies/
│   │   ├── contracts/
│   │   ├── trend/
│   │   ├── meanreversion/
│   │   ├── triangular/
│   │   ├── crossarb/
│   │   └── regime/
│   ├── graph/
│   ├── portfolio/
│   ├── risk/
│   ├── execution/
│   ├── simulation/
│   ├── reconciliation/
│   ├── accounting/
│   ├── backtest/
│   ├── replay/
│   ├── reporting/
│   ├── monitoring/
│   ├── security/
│   ├── storage/
│   │   ├── postgres/
│   │   ├── parquet/
│   │   └── migrations/
│   └── api/
├── web/
│   ├── src/
│   │   ├── app/
│   │   ├── features/
│   │   ├── components/
│   │   ├── hooks/
│   │   ├── services/
│   │   ├── stores/
│   │   ├── types/
│   │   └── utils/
│   ├── public/
│   └── tests/
├── research/
│   ├── notebooks/
│   ├── reports/
│   ├── datasets/
│   └── src/
├── configs/
│   ├── examples/
│   └── schemas/
├── deploy/
│   ├── docker/
│   ├── compose/
│   └── scripts/
├── docs/
│   ├── adr/
│   ├── architecture/
│   ├── strategies/
│   ├── exchange-adapters/
│   ├── operations/
│   ├── api/
│   ├── security/
│   ├── implementation-status.md
│   └── glossary.md
├── testdata/
│   ├── exchange-payloads/
│   ├── orderbooks/
│   ├── replays/
│   └── golden/
├── scripts/
├── Makefile
├── go.mod
├── docker-compose.yml
└── README.md
```

`cmd/platform` builds the hardened deployment image entrypoint `/app/platform` with explicit subcommands such as `api`, `trader`, `recorder`, `worker`, `admin migrate`, and `healthcheck`. The focused command directories may remain as thin development/specialized entrypoints, but business logic stays in `internal/` and must not diverge between binaries.

## 5.1 File and package rules

- One file, one clear purpose.
- Prefer files below 300 lines.
- Files above 400 lines require review and likely splitting.
- Files above 500 lines are prohibited unless they are generated code, static fixtures, migrations, or explicitly justified in an ADR.
- Keep functions small enough to understand without scrolling excessively.
- Avoid generic packages named `utils`, `helpers`, or `common` unless they contain a narrowly defined concern.
- Define interfaces near the consuming package, not automatically beside implementations.
- Do not expose exchange DTOs outside exchange adapters.
- Do not create circular package dependencies.
- Keep domain models free from HTTP, database, and exchange-specific details.
- Generated code must be clearly marked and excluded from manual style limits.

## 5.2 Comment and documentation rules

- Every exported Go symbol must have a meaningful documentation comment.
- Strategy packages must document formulas, assumptions, timeframes, parameters, and invalidation conditions.
- Exchange adapters must document sequence rules, heartbeat behavior, time semantics, and non-standard API behavior.
- Inline comments should explain **why**, safety invariants, or non-obvious exchange behavior.
- Do not comment obvious assignments or loops.
- Each module needs a package-level overview.
- Each strategy needs a dedicated Markdown document in `docs/strategies/`.
- Every configuration field needs documentation and validation constraints.

---

# 6. Core domain model

## 6.1 Monetary values

Never use `float32` or `float64` for:

- price
- quantity
- balance
- fee
- notional
- P&L
- exchange rate
- percentage used in financial calculations

Implement internal fixed-point decimal domain types behind a narrow API.

Required types include:

- `Price`
- `Quantity`
- `Money`
- `Rate`
- `Percent`
- `Fee`
- `Notional`

Requirements:

- Explicit scale handling
- Checked overflow
- Deterministic rounding
- Exchange-specific rounding modes
- Serialization without precision loss
- Property tests for arithmetic
- Benchmarks for hot-path operations

Represent display-only chart values and non-authoritative statistical analytics as floating point only after exact financial inputs are fixed. Floating-point values must never directly authorize an order, book a ledger entry, reserve capital, determine a filter-valid quantity, or declare an arbitrage cycle profitable.

For triangular arbitrage, V1 must exhaustively enumerate the small approved cycle set using exact fixed-point conversions. A logarithmic floating-point graph may be used later as a discovery accelerator only when conservative error bounds prove it cannot exclude an exact eligible cycle; every candidate still requires exhaustive exact validation.

## 6.2 Canonical identifiers

Use canonical domain identifiers:

- `ExchangeID`
- `AssetID`
- `InstrumentID`
- `StrategyID`
- `StrategyVersionID`
- `PortfolioID`
- `DecisionID`
- `OpportunityID`
- `OrderID`
- `ClientOrderID`
- `FillID`
- `IncidentID`
- `BacktestRunID`

Do not use untyped strings throughout the domain.

## 6.3 Instruments

A canonical spot instrument contains:

- exchange
- exchange-native symbol
- canonical base asset
- canonical quote asset
- status
- price tick size
- quantity step size
- minimum quantity
- maximum quantity
- minimum notional
- supported order types
- supported time-in-force values
- maker fee
- taker fee
- metadata version timestamp

Instrument metadata must be refreshed and versioned. A backtest or simulation must use the metadata applicable to its period when available, or clearly record a configured approximation.

Metadata versions include `valid_from`, `valid_to` when known, `observed_at`, environment, account mode, and raw-source hash. Filters may vary by side, order type, quantity unit, notional, and dynamic price bands; the broker revalidates the exact rounded request against the latest eligible metadata immediately before submission.

Instrument maker/taker fields are estimates only. Fee models are versioned by exchange, account/environment, tier, symbol, time, order type, discount setting, and possible commission asset. Actual test/demo fill fees and fee assets are authoritative reconciliation facts. Simulation reserves the conservative possible fee asset and handles rebates, third-asset discounts, and dust explicitly.

## 6.4 Timestamps

Every market event must record:

- exchange-generated timestamp, when supplied
- local receive timestamp
- local processing timestamp
- source connection identifier
- sequence identifier, when supplied

All persistent timestamps use UTC.

The system must monitor:

- local clock drift
- event age
- inter-exchange timestamp uncertainty
- reconnect gaps

Use the monotonic component of the local clock for durations, age, latency, and deadlines. Use UTC wall time for persisted human timestamps. A wall-clock correction must never make an event age negative or reorder an already-ingested event.

## 6.5 Event envelope and deterministic ordering

Every recorded or decision-relevant event has an immutable envelope containing:

- schema version
- stable event ID
- recorder/session ID
- source connection ID and connection generation
- exchange and instrument, when applicable
- exchange timestamp and exchange sequence, when supplied
- local monotonic receive offset from session start
- local UTC receive timestamp
- a session-local monotonically increasing `ingest_ordinal` assigned before concurrent fan-out
- payload hash
- normalization/parser version
- raw-segment reference, when applicable

Within one dataset, `ingest_ordinal` is the authoritative total-order tie-breaker. Exchange sequence rules remain authoritative for validating an individual book but do not define cross-exchange order. Replay sorts by recorded logical time and `ingest_ordinal`; it must not depend on goroutine scheduling, map iteration, operating-system timing, or a shared random-number stream.

Each multi-market decision stores a market-view version vector identifying the exact exchange, instrument, connection generation, book version, receive time, and ingest ordinal used for every input. Cross-exchange evaluations use an explicit deterministic as-of join and maximum-skew policy; two independently fresh books are not automatically a coherent comparison.

Randomness uses deterministic keyed streams derived from run ID, strategy version, decision ID, order/leg ID, model version, and configured seed. Adding unrelated work must not change another order’s random draws.

---

# 7. Exchange adapter architecture

## 7.1 Adapter contract

Exchange integration is composed from narrow consumer-owned interfaces rather than one broad client that can reach every endpoint. Required interfaces include `MarketDataSource`, `InstrumentCatalog`, `HistoricalReader`, `TestAccountReader`, `DemoOrderBroker`, and `Reconciler`. Public clients cannot implement order methods or receive credentials. Each adapter also exposes an environment-specific capability descriptor and normalized operations.

Conceptual capabilities:

```text
PublicMarketData
HistoricalCandles
HistoricalTrades
OrderBookSnapshots
OrderBookDeltas
PrivateAccountData
DemoOrTestOrders
ProductionOrders
Withdrawals
SpotOnly
SupportedOrderTypes
SupportedTimeInForce
SequenceValidation
Checksums
```

The V1 binary must not contain production-order or withdrawal methods. A descriptive capability record may report these as unavailable for UI/audit purposes, but they are not callable interface methods.

Conceptual adapter operations:

```text
LoadExchangeInfo
LoadInstruments
LoadFeeSchedule
GetServerTime
GetOrderBookSnapshot
SubscribeOrderBook
SubscribeTrades
SubscribeTicker
SubscribeCandles
GetHistoricalCandles
GetHistoricalTrades
GetBalances
GetOpenOrders
GetOrder
PlaceDemoOrder
CancelDemoOrder
SubscribePrivateEvents
Health
Close
```

Not every exchange supports every method. Unsupported operations must return typed capability errors, not silent no-ops.

Capabilities are versioned by exchange, environment, account mode, and observation time. A boolean alone is insufficient when an operation has constraints such as supported depth, order type, time-in-force, quantity unit, account mode, or private-stream availability.

## 7.2 Binance adapter

Implement:

- Spot public WebSocket streams
- REST snapshots
- server-time synchronization
- instrument/filter loading
- historical candles
- public trades where required
- Spot Testnet authenticated account/order operations
- testnet user-data events
- explicit handling of periodic testnet resets as external test-account reinitialization events, never as ordinary strategy P&L or silent local correction
- rate-limit telemetry
- reconnect and resubscribe behavior

The adapter must correctly implement Binance local-order-book synchronization:

1. Start buffering depth events.
2. Fetch a REST snapshot.
3. Discard events older than or equal to the snapshot boundary according to official sequence rules.
4. Apply the first valid bridging event.
5. Apply subsequent deltas in order.
6. Stop the affected book on a sequence gap.
7. rebuild from a new snapshot.

## 7.3 Bybit adapter

Implement:

- Spot public WebSocket order books
- public trades
- tickers
- instrument information
- server time
- snapshots and deltas using Bybit sequence fields
- demo account authenticated REST operations
- demo private WebSocket events where supported
- capability handling for functions not supported by demo
- category hard-coded and validated as `spot`
- `isLeverage` hard-coded to `0` and an allowlist of serialized request fields; unknown, margin, collateral, borrowing, and leverage fields fail before signing

Bybit demo notes:

- Demo trading is an isolated account environment.
- Public market data comes from production public streams.
- Demo private streams use demo endpoints.
- WebSocket order entry is not supported in demo; use supported authenticated REST operations and private streams.
- REST submission acknowledgement is not final order state; private events and reconciliation are authoritative inputs to the idempotent order reducer.

## 7.4 Normalization rules

All exchange payloads must be converted immediately into canonical domain events.

Examples:

- `BTCUSDT`, `BTC/USDT`, and other native representations become canonical `BTC-USDT` plus exchange identity.
- Native order statuses map to canonical order states.
- Native precision and fees remain recorded for audit.
- Unknown enum values must be preserved in raw payload archives and surfaced as adapter errors.

## 7.5 Rate limits

- Discover limits dynamically where APIs expose them.
- Maintain exchange-specific weighted request budgets.
- Reserve capacity for reconciliation and emergency cancellation.
- Do not allow historical download jobs to exhaust live-account limits.
- Show usage and throttling in metrics and the UI.

Use typed error classes for rate limit, transient exchange outage, authentication, timestamp/receive-window failure, invalid filter, duplicate client ID, insufficient funds, maintenance, prohibited capability, permanent validation failure, and ambiguous timeout. Retry policy is per operation and versioned: idempotent public reads may use bounded exponential backoff with jitter; private reads reconcile; create-order ambiguity never triggers a blind POST retry. Honor official retry/rate headers and share account/IP budgets across processes where applicable.

## 7.6 Exchange health state

Each exchange connection has a state machine:

```text
DISABLED
CONNECTING
SYNCING
HEALTHY
DEGRADED
STALE
DISCONNECTED
PAUSED
```

Strategies may consume data only from `HEALTHY` markets unless explicitly running a failure simulation.

---

# 8. Market-data engine

## 8.1 Data types

Support normalized events for:

- order-book snapshot
- order-book delta
- best bid and offer
- public trade
- ticker
- candle open/update/close
- exchange status
- instrument update
- heartbeat
- connection lifecycle

## 8.2 Local order books

Maintain one ordered writer per exchange/instrument book.

Requirements:

- deterministic price-level ordering
- configurable retained depth
- snapshot/delta synchronization
- sequence-gap detection
- optional checksum verification
- stale-book detection
- executable VWAP calculation by side and quantity
- depth query by notional
- immutable read snapshots or safe versioned views for strategy readers
- no concurrent unordered mutation

The system must calculate executable prices, not only top-of-book prices.

Required operations:

- `BestBid`
- `BestAsk`
- `VWAPToBuyBase`
- `VWAPToSellBase`
- `MaxBaseWithinSlippage`
- `DepthAtPrice`
- `SnapshotVersion`
- `Age`

## 8.3 Concurrency model

Use bounded event queues.

Partition ordered processing by exchange and instrument:

```text
Binance/BTC-USDT -> one ordered book worker
Binance/ETH-USDT -> one ordered book worker
Bybit/BTC-USDT   -> one ordered book worker
Bybit/ETH-USDT   -> one ordered book worker
```

Different books, exchanges, strategies, persistence writers, and dashboard publishers may run concurrently.

If a queue falls behind:

- mark the affected stream degraded
- stop strategy decisions for that stream
- discard unsafe stale work
- resynchronize
- record an incident

Never process stale queued opportunities merely to avoid dropping events.

Queue behavior is defined by event class:

| Event class | Overflow policy |
|---|---|
| Order-book snapshot/delta | Never skip an individual delta. Invalidate the affected book, discard the unsafe generation, and resynchronize from a new snapshot. |
| Private order/fill/account event | Never drop or coalesce. Persist through a durable inbox or lock the affected engine. |
| Reservation, ledger, risk, or administrative command | Never drop. Reject new work and fail closed when durable capacity is unavailable. |
| Raw recorder event | Spill to bounded durable staging when possible; otherwise finalize an explicit dataset gap, mark the dataset ineligible for full-confidence research, and pause decisions if policy requires complete recording. |
| Derived ticker/dashboard update | May be coalesced by key because REST/current-state recovery is authoritative. |
| Expired opportunity | Drop with an auditable reason and metric; never execute merely because it was queued. |

Expose capacity, current depth, oldest-event age, saturation duration, coalesced count, dropped count, and resynchronization count without high-cardinality metric labels.

## 8.4 Hot path and cold path

### Hot path

```text
WebSocket bytes
-> decode
-> validate sequence/time
-> update in-memory book
-> produce normalized market snapshot
-> evaluate strategy
-> allocate virtual capital
-> run risk checks
-> create simulated execution plan
```

The hot path must not wait for:

- React
- chart generation
- database analytics queries
- report generation
- notification delivery
- raw event compression

### Cold path

Process asynchronously:

- raw event persistence
- aggregate metrics
- dashboard fan-out
- reports
- alerts
- long-term analytics

Critical order and reservation state must still be persisted safely before a state transition is considered durable.

## 8.5 Market-data quality score

Calculate a quality score per exchange/instrument using:

- stream age
- sequence continuity
- reconnect frequency
- snapshot rebuild frequency
- timestamp drift
- message lag
- missing candles
- crossed-book anomalies
- negative or invalid quantities

Strategies may require a configurable minimum quality score.

Hard safety defects such as an unresolved sequence gap, crossed/invalid book, stale generation, excessive clock uncertainty, or failed checksum cannot be averaged away by a high composite quality score. The composite score is useful only after hard eligibility checks pass.

## 8.6 Cross-venue coherent market views

Cross-exchange comparison uses local monotonic receive time plus measured clock-offset uncertainty. A view is eligible only when:

- each source book independently passes sequence and freshness checks
- both books belong to active connection generations
- the receive-time intervals overlap within the configured uncertainty budget
- maximum inter-book skew is below the versioned limit
- the decision records both book versions and the collector region/instance

V1 uses a deterministic as-of join against the latest committed eligible view at or before the trigger event. A future release may add watermark/barrier evaluation, but the selected algorithm must be versioned and replayable. Exchange timestamps are evidence, not a globally trustworthy clock.

---

# 9. Raw data recording and deterministic replay

## 9.1 Recorder

Record production public data from Binance and Bybit for approved pairs.

For decision-grade datasets, record both:

- the immutable wire envelope or exact response payload needed to reproduce parsing and exchange-specific behavior
- the normalized canonical event with schema, parser, and normalization versions

Also record subscription commands/results, connection generations, heartbeat/lifecycle events, REST snapshot request/response identity, resynchronizations, gaps, decoder errors, and clock-offset samples.

Private test/demo payloads are sensitive operational records. Store only fields required for reconciliation/audit, redact signatures/tokens/headers, segregate access and retention from public datasets, and encrypt them at rest or within the encrypted database/backup boundary. They are never exported in a public research dataset.

Default fully recorded instruments:

- BTC-USDT
- ETH-USDT
- ETH-BTC, or the exchange-native inverse orientation required to complete the BTC/ETH/USDT triangle

If an exchange does not list the required BTC/ETH spot instrument, triangular execution for that exchange must be disabled by capability/universe validation rather than approximated through unrelated prices.

The universe system may add scan-only pairs later, but V1 acceptance testing centers on BTC and ETH.

Partition raw files by:

- exchange
- event type
- instrument
- UTC date
- hour or size-limited segment

Each segment metadata record includes:

- start/end timestamps
- first/last sequence
- record count
- schema version
- checksum
- compressed file path
- corruption status

Segment commit protocol:

1. write to a uniquely named `.partial` file
2. flush and `fsync` file contents
3. calculate checksum and ordered content hash
4. atomically rename to its immutable final name on the same filesystem
5. `fsync` the parent directory where supported
6. commit the PostgreSQL manifest row as `ready`

Startup must discover incomplete, orphaned, duplicate, and manifest-missing files. It may safely finalize a provably complete segment or quarantine it; it may never silently present an incomplete segment as complete. Dataset manifests record ordered segment hashes, gaps, schema versions, time coverage, quality tier, and compatibility requirements.

## 9.2 Replay

Replay must use the same canonical market events and the same production strategy code.

Features:

- deterministic ordering
- original timing mode
- accelerated timing mode
- maximum-speed mode
- pause
- single-event step
- seek by indexed segment/time
- configurable network/decision latency injection
- disconnect injection
- sequence-gap injection
- order rejection injection
- partial-fill injection
- restart-at-event testing

A replay using the same data, configuration, software version, and random seed must produce the same decisions and P&L.

The reproducibility identity also includes toolchain version, dependency lock hashes, build flags, operating architecture where behavior could differ, event schema/parser versions, canonical serialization version, and deterministic scheduler version. Result checksums use one documented canonical byte representation.

## 9.3 Historical data limitations

Candles are sufficient for baseline trend research but not for reliable arbitrage execution research.

Triangular and cross-exchange arbitrage must primarily use recorded synchronized order-book data. When only candles or top-of-book data are available, label the run as a low-confidence approximation and do not compare it directly with full-depth runs.

---

# 10. Strategy framework

## 10.1 Strategy contract

A strategy never sends an order directly.

A strategy receives approved normalized state and produces an `OpportunityCandidate`.

Conceptual candidate fields:

```text
ID
StrategyID
StrategyVersion
DecisionTime
MarketSnapshotIDs
RequiredExchanges
RequiredInstruments
ActionLegs
RequestedNotional
ExpectedGrossReturn
EstimatedFees
EstimatedSpreadCost
EstimatedSlippage
EstimatedLatencyCost
ExpectedNetReturn
WorstCaseNetReturn
MaximumOneLegLoss
HoldingPeriodClass
ConfidenceFlags
ExpirationTime
ReasonCodes
```

The candidate then passes through:

1. portfolio allocator
2. central risk engine
3. execution planner
4. simulator or testnet/demo broker

## 10.2 Versioning

Every strategy version records:

- code commit
- parameter set
- data dependencies
- supported instruments
- supported modes
- creation date
- author
- approval status
- champion/challenger role
- notes

Never overwrite historical strategy parameters.

## 10.3 Online learning restriction

V1 must not automatically modify strategy rules based on one recent result.

Use a controlled champion/challenger process:

1. Collect decision and outcome data.
2. Analyze offline.
3. create a challenger version.
4. backtest it.
5. validate out of sample.
6. replay recorded books.
7. run in live shadow mode.
8. compare against champion.
9. require explicit promotion.

## 10.4 Strategy maturity and research governance

Strategy maturity is separate from platform release status:

```text
EXPERIMENTAL
BACKTEST_VALIDATED
REPLAY_VALIDATED
SHADOW_VALIDATED
SANDBOX_INTEGRATION_VALIDATED
REJECTED
```

No maturity label means “profitable in production.” Testnet/demo results validate integration only and cannot advance statistical research maturity.

Every experiment is registered before examining its final test results and records hypothesis, primary metric, parameter-search space, training/validation/test windows, fee/slippage/latency models, benchmarks, minimum sample size, stopping rule, and promotion rule. The final test window is locked and may be consumed once per registered strategy version; reuse creates a new research generation and must be disclosed.

Promotion evidence includes:

- chronological and walk-forward out-of-sample results
- block-bootstrap confidence intervals suitable for serially correlated returns
- parameter-neighborhood stability
- fee, spread, slippage, latency, gap, and missed-fill stress tests
- capacity curves by notional
- multiple-comparison control and a deflated or probabilistic Sharpe analysis when many variants were tested
- minimum trade count and minimum shadow duration defined before evaluation
- comparison with no-trade cash, buy-and-hold, and the relevant static inventory benchmark
- explicit failure and rejection criteria

Low-confidence candle/top-of-book arbitrage approximations and demo/testnet results are never eligible as primary promotion evidence.

## 10.5 Normative parameter schema

Every configurable strategy, risk, execution, quality, fee, and latency parameter records:

- stable parameter ID and semantic description
- exact formula or algorithm version
- decimal-string default, unit, valid range, and inclusivity
- fixed-point scale and rounding direction
- evaluation cadence and time zone
- required data warm-up
- whether it is immutable for a run, reloadable for new decisions, or restart-only
- behavior for existing positions/orders when changed
- actor, approval, timestamp, reason, and configuration hash

Every decision stores the exact strategy, risk, asset-registry, instrument-metadata, fee, latency, valuation, and configuration versions used. A reload is atomic for new decisions and never changes the rules attached to an in-flight order or historical result.

## 10.6 Candle and indicator conventions

- Baseline candles are UTC-aligned exchange candles cross-checked against recorded trades where coverage permits. A candle is final only after its close event plus the configured finalization delay.
- EMA uses `alpha = 2 / (period + 1)` and is seeded by the simple mean of the first complete `period` values.
- True range is the maximum of high-low, absolute high-previous-close, and absolute low-previous-close. ATR uses Wilder smoothing and an initial simple mean.
- ADX 14 uses Wilder’s directional-movement and smoothing definition. Baseline mean-reversion eligibility requires ADX below `25.0`.
- Baseline mean-reversion z-score is `(close - mean(close, 20)) / population_stddev(close, 20)` using the last 20 completed strategy candles. Zero standard deviation produces no candidate.
- “Strongly declining EMA 200” means the higher-timeframe EMA 200 has fallen at least `0.50%` across the previous 10 completed higher-timeframe candles. Both threshold and lookback are versioned parameters.
- Baseline higher timeframe for the 1-hour mean-reversion strategy is 4 hours; for the 15-minute challenger it is 1 hour.
- Candle-only backtests submit after the signal close and never fill at that close. Entry uses the next available executable observation after modeled latency.
- If candle data cannot determine the order of multiple intrabar triggers, the baseline uses the adverse/conservative ordering. Runs using a different ambiguity policy are separately versioned and labelled lower confidence.
- Protective stops are triggers, not guaranteed prices. Position sizing and results include gap, slippage, fee, and non-fill stress beyond the nominal stop distance.

---

# 11. Strategy A: Trend-following breakout

## 11.1 Purpose

Capture sustained upward BTC or ETH trends while remaining in USDT when conditions are unfavorable.

## 11.2 Default instruments

- BTC-USDT
- ETH-USDT

## 11.3 Initial research timeframe

Primary:

- 4-hour completed candles

Optional challenger:

- 1-hour completed candles

Do not act on incomplete candles in the baseline strategy.

## 11.4 Baseline rules

All parameters must be configurable and versioned.

Initial baseline:

- Long-term regime: close above EMA 200.
- Trend confirmation: EMA 50 above EMA 200.
- Entry trigger: close breaks above the highest high of the previous 20 completed candles.
- Volatility measure: ATR 14.
- Initial protective exit distance: 2.5 ATR from entry.
- Trailing exit distance: 3 ATR from the highest favorable close after entry.
- Secondary trend exit: completed close below EMA 50.
- Cooldown after protective loss: 3 completed strategy candles.
- Maximum one open trend position per asset per portfolio.
- No averaging down.
- No position increase after an adverse move.

## 11.5 Position sizing

Baseline virtual risk per trade:

- 0.5% of the trend virtual portfolio equity

Formula:

```text
risk_budget = equity * risk_percent
unit_risk = entry_price - protective_exit_price
quantity = risk_budget / unit_risk
```

Then apply:

- available USDT
- maximum notional
- exchange minimum notional
- quantity step size
- global BTC/ETH exposure limits
- correlation limits

## 11.6 Execution simulation

Trend entries and exits should simulate a marketable limit order by default:

- Limit protection prevents unlimited slippage.
- If not filled inside the configured validity interval, cancel the virtual order and record a missed signal.
- The simulator must not assume candle close execution without spread and latency.

## 11.7 Metrics

- net return
- maximum drawdown
- time in market
- average holding period
- breakout failure rate
- average win/loss
- profit factor
- benchmark versus buy-and-hold BTC or ETH
- performance by market regime
- fee and slippage contribution

---

# 12. Strategy B: Regime-filtered mean reversion

## 12.1 Purpose

Trade temporary downside deviations only when the market is judged to be non-trending or moderately constructive.

## 12.2 Default instruments

- BTC-USDT
- ETH-USDT

## 12.3 Initial research timeframe

Primary:

- 1-hour completed candles

Optional challenger:

- 15-minute completed candles

## 12.4 Baseline regime filters

Initial configurable baseline:

- Higher-timeframe price must not be below a strongly declining EMA 200.
- ADX 14 must be below the configured trend threshold.
- Current spread and market-data quality must pass risk limits.
- No exchange-wide risk pause.

## 12.5 Signal rules

Use a simple, testable deviation model rather than many indicators.

Initial baseline:

- Calculate rolling mean and standard deviation of price deviation over 20 completed candles.
- Calculate z-score.
- Entry candidate when z-score <= -2.0 and regime filters pass.
- Exit when z-score >= -0.25.
- Protective exit at a configurable ATR distance or z-score <= -3.5, whichever triggers first.
- Maximum holding period: 12 strategy candles.
- No averaging down.
- One open mean-reversion position per asset per portfolio.
- Cooldown after loss: 3 completed strategy candles by default.

## 12.6 Risk

Baseline virtual risk per trade:

- 0.25% of mean-reversion portfolio equity

Baseline sizing uses the same stressed-loss structure as trend:

```text
risk_budget = equity * risk_percent
nominal_stop = min(entry_price - 2.5 * ATR_14, price_at_z_score_minus_3_5)
stressed_exit = nominal_stop - configured_gap_and_slippage_allowance
unit_risk = entry_price - stressed_exit + per_unit_entry_and_exit_fees
quantity = risk_budget / unit_risk
```

Then apply available USDT, notional and quantity filters, maximum exposure, correlation, and conservative side-specific rounding. A nonpositive or uncomputable `unit_risk` rejects the candidate.

The strategy allocation is intentionally lower than trend allocation because persistent downtrends are dangerous for mean reversion.

## 12.7 Required reporting

- results by regime classification
- results when trend filter is disabled for comparison
- failure rate during fast declines
- maximum adverse excursion
- holding-time distribution
- effect of spread and fees

---

# 13. Strategy C: Triangular arbitrage

## 13.1 Purpose

Detect and simulate profitable three-leg conversion cycles inside one exchange.

## 13.2 Default settlement asset

- Start and end in USDT.

## 13.3 Initial asset universe

Default approved cycle assets:

- USDT
- BTC
- ETH

Required instruments for the baseline cycle are BTC-USDT, ETH-USDT, and ETH-BTC or the exchange-native inverse orientation. Availability must be verified from instrument metadata.

This supports cycles such as:

```text
USDT -> BTC -> ETH -> USDT
USDT -> ETH -> BTC -> USDT
```

The graph architecture must support more assets later.

## 13.4 Graph model

Each asset is a node.

Each trade direction is an edge. The edge must use executable depth, not last price.

Examples:

- USDT -> BTC consumes asks on BTC-USDT.
- BTC -> USDT consumes bids on BTC-USDT.
- BTC -> ETH uses the relevant side of ETH-BTC or BTC-ETH according to the native instrument orientation.

Effective rate must include:

- taker fee
- spread
- depth/VWAP
- configured latency deterioration
- safety margin

A logarithmic graph transform may be used only as a future non-authoritative discovery accelerator:

```text
weight = -log(effective_rate)
```

V1 exhaustively enumerates the approved three-asset cycles with exact fixed-point conversions. If a future accelerator is added, conservative numeric error bounds must prove it cannot hide an eligible exact cycle, and final validation must still recalculate the exact conversion through all legs for each candidate amount.

## 13.5 Trade-size ladder

Default virtual settlement-asset sizes:

- 10 USDT
- 25 USDT
- 50 USDT
- 100 USDT

Also support dynamic sizes derived from portfolio balance and depth.

An opportunity may be profitable at one size and unprofitable at another.

## 13.6 Qualification

A cycle is eligible only when:

- every book is healthy and fresh
- all three instruments are approved
- all three legs meet minimum quantity and notional rules
- sufficient depth exists
- expected net return is positive after all costs
- worst-case net return exceeds a configurable minimum
- opportunity is younger than its expiration
- virtual capital is reserved successfully

Initial conservative safety threshold:

- estimated costs plus at least 0.15 percentage points of additional margin

This threshold is configurable and must be validated rather than assumed optimal.

## 13.7 Simulation

Simulate all legs sequentially using actual market state at each assumed arrival time.

For each leg:

- apply configured dispatch latency
- read the future replay/live-shadow book at arrival
- calculate fill quantity and VWAP
- apply fee
- round to instrument rules
- pass resulting asset quantity to the next leg

Record:

- full success
- partial cycle
- missed leg
- negative cycle after latency
- stranded asset
- recovery cost

## 13.8 Recovery

If a cycle fails after one or two simulated legs:

- evaluate immediate conversion back to USDT
- calculate recovery loss
- apply risk policy
- never hide stranded-asset P&L

---

# 14. Strategy D: Cross-exchange arbitrage

## 14.1 Purpose

Detect and simulate simultaneous spot buying on the cheaper exchange and selling owned inventory on the more expensive exchange.

## 14.2 Default instruments

- BTC-USDT
- ETH-USDT

## 14.3 Prefunded inventory

Each exchange starts with USDT, BTC, and ETH according to the default portfolio allocation.

To simulate:

```text
Buy BTC on Binance
Sell BTC on Bybit
```

The Bybit virtual account must already own the BTC sold.

The strategy may evaluate both directions:

- buy Binance / sell Bybit
- buy Bybit / sell Binance

## 14.4 Opportunity calculation

For a proposed base quantity:

```text
buy_cost = executable ask-side VWAP + buy fee
sell_proceeds = executable bid-side VWAP - sell fee
expected_net = sell_proceeds - buy_cost - latency allowance - recovery allowance - marginal inventory replacement cost
```

The system must calculate:

- gross spread
- total fees
- spread cost
- depth slippage
- estimated latency deterioration
- expected net profit
- worst-case net profit
- maximum one-leg loss
- post-trade inventory
- marginal inventory shadow price
- expected rebalancing or natural-reversal cost and delay
- exchange/counterparty and USDT concentration penalty
- expected profit over the closed inventory cycle

An apparent spread is not classified as profitable merely because the immediate two legs make USDT while depleting scarce sell inventory. Ranking and long-run P&L must include the marginal cost of restoring target inventory, even when actual transfers remain manual/advisory.

## 14.5 Freshness

Initial configurable rule:

- both books must be healthy
- both books must be below a maximum age such as 250 ms
- inter-exchange timestamp uncertainty must be within limits

The final threshold must be measured and configurable.

## 14.6 Two-leg simulation

Simulate both legs concurrently using exchange-specific latency distributions.

Possible outcomes:

- both filled
- buy filled, sell failed
- sell filled, buy failed
- partial fill on one or both legs
- both missed
- opportunity became negative before arrival

## 14.7 One-leg recovery

If only one leg fills:

1. Verify the other leg is not merely delayed or unknown.
2. Attempt a bounded retry if allowed by risk policy.
3. Otherwise unwind on the filled exchange using a protected simulated order.
4. Record all losses as arbitrage execution losses.
5. Never leave an unintended position silently.

## 14.8 Inventory-aware rules

Track target bands per exchange and asset.

For BTC or ETH, `inventory_share` is the marked value of strategy-owned units of that asset at one exchange divided by the total marked value of strategy-owned units of the same asset across all eligible exchanges. USDT uses its own separate venue-distribution band. Reserved inventory remains attributed to its owner and cannot make another strategy appear funded.

Initial research bands:

- minimum target share: 30%
- preferred target: 50%
- maximum target share: 70%

If an exchange has too little BTC or ETH inventory:

- reduce opportunities that sell that asset there
- prefer reverse-direction opportunities
- pause that direction when necessary
- generate a rebalancing recommendation

## 14.9 P&L separation

Always report separately:

- arbitrage execution P&L
- BTC inventory market P&L
- ETH inventory market P&L
- stablecoin valuation effect
- rebalancing cost
- combined portfolio P&L

Do not describe arbitrage as profitable when inventory losses make the combined result negative.

---

# 15. Control E: Cash and market-risk regime

This is a mandatory control policy owned by the central risk engine, not an order-producing strategy or merely a reporting feature. It may influence candidate eligibility and size but never bypasses the allocator or risk engine.

## 15.1 States

```text
NORMAL
CAUTIOUS
PAUSED
LOCKED
```

## 15.2 Inputs

- market-data quality
- exchange health
- realized daily loss
- current drawdown
- volatility shock
- spread expansion
- repeated order failures
- reconciliation mismatch
- unknown order state
- event queue lag
- clock drift

## 15.3 Behavior

### NORMAL

All enabled strategies may submit candidates.

### CAUTIOUS

- reduce virtual position sizes
- require larger safety margin
- disable fragile pairs

### PAUSED

- reject new entries
- continue managing existing virtual/demo orders
- allow cancellations, exits, reconciliation, and approved risk-reducing recovery
- continue recording and analysis

### LOCKED

- cancel eligible demo/test orders
- prevent all new `ENTRY` intents
- allow reconciliation and idempotent order-state resolution
- allow `CANCEL` intents and only those `EXIT` or `RECOVERY` intents explicitly approved by the locked-state recovery policy; otherwise quarantine the exposure for manual action
- require explicit administrative unlock with audit record

## 15.4 Intent permissions and state precedence

| Risk state | ENTRY | EXIT | CANCEL | RECOVERY | Reconciliation |
|---|---|---|---|---|---|
| NORMAL | allowed after normal checks | allowed | allowed | allowed after recovery checks | required on schedule |
| CAUTIOUS | reduced size and stricter edge | allowed | allowed | allowed after recovery checks | increased cadence |
| PAUSED | rejected | allowed when risk-reducing | allowed | allowed when bounded and risk-reducing | required |
| LOCKED | rejected | policy-gated only | allowed | policy-gated only; otherwise manual | mandatory |

Platform, exchange-account, exchange, instrument, strategy, and portfolio states are persisted separately. The effective state is the most restrictive applicable state, except that a more restrictive state does not silently prohibit an explicitly approved risk-reducing cancellation or recovery action. Every evaluation stores the contributing states and policy version.

Escalation is immediate and automatic. `CAUTIOUS` may return automatically to `NORMAL` only after the configured healthy hysteresis interval, initially 5 minutes. `PAUSED` and `LOCKED` never auto-unpause. Unlock requires current reconciliation, healthy persistence, fresh required books, resolved unknown orders, recent reauthentication, a reason, and an immutable audit event.

---

# 16. Route and rebalancing optimizer

## 16.1 Purpose

Recommend how to restore asset distribution across exchanges at the lowest realistic cost and risk.

V1 is advisory only. It must not submit withdrawals or transfers.

## 16.2 Graph

Use nodes such as:

```text
USDT@Binance
BTC@Binance
ETH@Binance
USDT@Bybit
BTC@Bybit
ETH@Bybit
```

Edge types:

- trade edge within an exchange
- transfer edge between exchanges

Trade-edge cost:

- fee
- spread
- depth slippage
- expected delay

Transfer-edge cost:

- withdrawal fee
- configured network fee
- minimum withdrawal
- expected confirmation time
- deposit/withdrawal availability
- network compatibility
- volatility risk penalty

V1 transfer-edge facts come only from reviewed public sources or manually entered, versioned operational data. Each fact records source, observer, network/chain identifier, timestamp, expiry, confidence, and approval. The optimizer must not introduce production-private credentials merely to query a transfer route. Stale, missing, ambiguous, or incompatible network facts make the route unavailable rather than cheap by default.

## 16.3 Output

A recommendation includes:

- starting node
- destination node
- path
- expected total cost
- expected duration range
- required network
- compatibility warnings
- operational risk score
- manual steps

Natural reverse arbitrage should be preferred over transfers when practical.

---

# 17. Portfolio allocator

## 17.1 Responsibilities

The allocator receives candidates from all strategies and determines which may reserve virtual capital.

It must:

- enforce strategy budgets
- reserve exchange balances atomically
- prevent double spending
- prevent conflicting ownership
- account for BTC/ETH correlation
- maintain reserve capital
- rank competing opportunities
- release expired reservations
- keep separate strategy ledgers

## 17.2 Candidate ranking

Do not rank only by raw expected return.

A configurable score may include:

- worst-case net return
- expected profit amount
- capital efficiency
- opportunity half-life
- liquidity quality
- one-leg risk
- strategy confidence
- inventory benefit or harm
- current drawdown

Store the score components for audit.

## 17.3 Strategy isolation

Each strategy has a virtual sub-ledger:

- cash
- inventory
- reservations
- positions
- realized P&L
- unrealized P&L
- fees
- drawdown

A strategy may not consume another strategy’s reserved capital unless an explicit allocator policy allows it and records the transfer.

---

# 18. Central risk engine

No strategy or admin API may bypass the risk engine.

## 18.1 Global limits

Configurable controls:

- maximum virtual account drawdown
- maximum daily loss
- maximum loss per strategy
- maximum exposure per asset
- maximum exposure per exchange
- maximum open orders
- maximum reserved capital
- minimum reserve percentage
- stablecoin concentration limit
- stablecoin reference staleness and depeg warning/lock thresholds when USD reporting is enabled
- maximum unresolved reconciliation/suspense amount, normally zero for continued entries
- stale-data limit
- maximum spread
- maximum slippage
- maximum candidate age
- maximum queue lag

Conservative program defaults, expressed as percentage values where `1.00` means one percent:

| Control | Initial default |
|---|---:|
| Initial startup state | `PAUSED` |
| Automatic unpause | disabled |
| Maximum account drawdown | 5.00% |
| Maximum UTC-day or rolling-24h loss, whichever is stricter | 1.00% |
| Maximum strategy drawdown/loss limit | 3.00% |
| Maximum one volatile-asset exposure | 30.00% of equity |
| Maximum combined BTC+ETH exposure | 50.00% of equity |
| Maximum marked exposure to one exchange | 60.00% of equity |
| Minimum global reserve | 15.00% of equity |
| Maximum reserved capital | 85.00% of equity |
| Maximum open orders | 8 virtual; 1 test/demo by default |
| Maximum spread | 100 bps unless a stricter strategy limit applies |
| Maximum simulated slippage | 50 bps unless a stricter strategy limit applies |
| Arbitrage additional safety margin | 15 bps |
| Cross-exchange maximum book age | 250 ms |
| Maximum event-queue lag | 250 ms |
| Maximum local clock drift estimate | 100 ms |
| Minimum quality score after hard checks | 90/100 |
| Maximum individual test/demo order | 10.00 USDT |
| Maximum test/demo daily submitted notional | 50.00 USDT |

These are hard safety starting caps, not claims of optimal strategy parameters. A release may tighten them. Loosening them requires a versioned policy, authenticated confirmation, audit record, and new validation evidence. Missing, invalid, stale, or overflowed risk inputs fail closed.

## 18.2 Strategy-specific checks

### Trend

- risk per trade
- total correlated BTC/ETH exposure
- stop distance validity
- no duplicate position

### Mean reversion

- regime validation
- no averaging down
- holding-time limit
- smaller risk budget

### Triangular arbitrage

- all legs valid
- cost and depth validation
- cycle expiration
- stranded-asset recovery estimate

### Cross-exchange arbitrage

- balances on both exchanges
- two healthy books
- post-trade inventory bands
- worst-case edge
- one-leg loss estimate

## 18.3 Circuit breakers

Trigger pause or lock when:

- repeated sequence gaps
- account mismatch
- unknown order unresolved
- excessive simulated slippage
- daily loss exceeded
- drawdown exceeded
- database cannot persist critical state
- system clock drift exceeds threshold
- exchange API errors exceed threshold
- event loop lag exceeds threshold

All circuit-breaker changes require audit events.

---

# 19. Execution and simulation engine

## 19.1 Broker abstraction

Implement a common broker contract with:

- historical simulation broker
- deterministic replay broker
- live shadow broker
- Binance testnet broker
- Bybit demo broker

No live production broker in V1.

## 19.2 Order state machine

Canonical states:

```text
CREATED
VALIDATING
RESERVED
APPROVED
SUBMITTING
ACKNOWLEDGED
PARTIALLY_FILLED
FILLED
CANCEL_PENDING
CANCELED
REJECTED
EXPIRED
UNKNOWN
RECOVERY_REQUIRED
RECOVERED
```

State transitions must be validated through an idempotent reducer. Duplicate, stale, and out-of-order exchange events are recorded and safely ignored or merged when their cumulative facts add nothing. Semantically impossible regressions, conflicting immutable identifiers, decreasing cumulative fills, or broken accounting invariants fail loudly and create incidents. Exchange status and cumulative executed quantity/fees are stored separately because a canceled or expired order may still have fills.

## 19.3 Client order IDs

Generate deterministic, collision-resistant client order IDs containing enough traceability to locate:

- environment
- strategy
- decision
- leg
- attempt

Do not expose sensitive internal data unnecessarily.

## 19.4 Latency model

Maintain measured distributions per exchange and operation:

- market-data delivery
- strategy evaluation
- risk validation
- order dispatch
- order acknowledgement
- cancel acknowledgement

Shadow simulations should default to a conservative percentile such as measured p95, while also supporting p50/p99 scenario runs.

Store latency samples and model version.

## 19.5 Fill model

For taker or marketable orders:

- use order-book state at simulated arrival
- consume levels by quantity
- apply exchange rounding
- support partial fills
- reject quantity beyond configured depth/slippage
- reserve and consume deterministic run-local displayed liquidity so concurrent orders in a combined portfolio cannot all claim the same level
- apply configured own-order market-impact and adverse-selection haircuts, even when the virtual order is too small to move the observed public book materially

For maker orders:

- do not assume immediate fill
- model queue position conservatively using book and trade events
- label low-confidence maker simulations

V1 arbitrage strategies should primarily use taker/IOC-style simulation.

Simulation contexts are explicit:

- **Independent counterfactual:** separate benchmark portfolios may reuse the same external book because each answers a separate “what if” question. Results must never be aggregated.
- **Combined portfolio:** all strategies share one deterministic order scheduler and run-local liquidity-consumption view. Earlier virtual fills affect later simulated availability and portfolio balances.

Confidence tiers:

| Tier | Available data | Permitted claim |
|---|---|---|
| A | synchronized full depth, trades, exact fees/filters, measured public latency, complete dataset | decision-grade shadow/replay estimate, still not evidence of production fill |
| B | full depth with documented gaps or modeled fee/latency components | research estimate with sensitivity bounds |
| C | top-of-book/trades without full depth | opportunity scan only; not promotion evidence for arbitrage |
| D | candles only | candle-strategy research or explicitly low-confidence illustration; never execution-grade arbitrage |

Public data cannot prove a hypothetical fill, queue position, hidden liquidity, or production order latency. Testnet/demo latency and fills validate integration only and must be stored in separate latency/fill model namespaces from production-public shadow assumptions.

## 19.6 Unknown state handling

A timeout does not mean failure.

For testnet/demo orders:

1. mark state `UNKNOWN`
2. query by client order ID
3. inspect private order events
4. inspect fills and balances
5. resolve before retrying

Never submit a blind duplicate order.

## 19.7 Crash-safe submission and event reduction protocol

For every testnet/demo order:

1. Atomically persist the exclusive reservation, logical order, deterministic client order ID, submission attempt, intended serialized-request hash, initial order event, configuration/policy versions, and durable outbox item.
2. Commit before network I/O.
3. The fenced broker worker marks the attempt dispatching and sends the exact approved request. A timeout is `UNKNOWN`, never an assumed rejection.
4. REST acknowledgements, private-stream events, query results, and reconciliation facts enter one durable inbox and are reduced serially per order or through optimistic version checks.
5. Fill insertion, cumulative quantities/fees, ledger journal entries, position/inventory updates, and reservation consumption/release commit atomically.
6. Safe reads may retry with bounded backoff. An ambiguous create-order request is never blindly repeated; query by client order ID and reconcile first.

Database uniqueness requirements include:

- `(exchange_account_id, account_epoch, client_order_id)`
- `(exchange_id, exchange_fill_id)` when the exchange supplies a stable fill ID
- `(order_id, exchange_event_identity)` or canonical payload identity for deduplication
- one active fencing lease per exchange account/environment

Reservations do not expire or release while related orders are active, cancel-pending, unknown, or recovery-required. Reservation expiry uses compare-and-swap/fencing so it cannot race with approval or submission.

The engine must handle and test process termination after every numbered boundary, fill-before-REST-ack, duplicate fill, partial-fill-then-cancel/expire, fill-during-cancel, cancel rejection, late fill after reconnect, unknown order, and account reset. Recovery must produce neither a duplicate order nor a lost fill.

Multi-leg execution uses a separate persisted plan/saga aggregate containing leg dependencies, concurrent/sequential dispatch policy, reservations, remaining exposure, recovery state, and final disposition. A leg order reaching a terminal state does not by itself make the plan terminal.

---

# 20. Accounting and reconciliation

## 20.1 Source of truth

- In backtest/replay/shadow: the simulation ledger is the source of truth.
- In testnet/demo: the exchange account is the source of truth.

## 20.2 Double-entry accounting

Use an immutable multi-commodity double-entry journal for every asset movement. “Double-entry-style” is not sufficient: the chart of accounts, balancing scope, posting rules, valuation, and correction behavior are normative.

Required journal structure:

- immutable journal transaction header with transaction type, causation/correlation IDs, source mode, portfolio, exchange account, strategy, order/fill, policy/configuration versions, UTC time, ingest ordinal, and reversal reference
- immutable journal lines containing account, asset/commodity, exact quantity, debit/credit direction, optional functional-currency valuation, cost-basis lot/reference, and rounding metadata
- balances derived from journal lines or transactionally maintained projections that can be rebuilt and verified from the journal

Required account classes include external/equity, available asset, reserved asset, strategy inventory, exchange inventory, fee expense, spread/slippage/latency attribution, realized P&L, inventory valuation, rebalancing expense, recovery loss, rounding/dust, and reconciliation suspense.

Examples:

- USDT decreases on buy
- BTC increases on buy
- fee asset decreases
- reserved balance becomes available or consumed
- realized P&L posts separately

Every journal transaction must balance independently per asset/commodity. BTC is not numerically balanced against USDT; the trade contains balanced BTC lines and balanced USDT lines, with valuation and realized P&L derived through the documented weighted-average cost-basis policy.

Fees use the actual fee asset when known and a versioned estimated fee asset in simulation. Third-asset fees, rebates, discounts, taxes, and rounding dust require explicit lines. Available and reserved balances may never become negative. Historical entries are never edited; corrections and testnet resets use compensating/external-adjustment transactions with incidents and attribution excluded from strategy P&L.

## 20.3 Reconciliation

For testnet/demo:

- load balances
- load open orders
- load recent orders/fills
- compare with local records
- flag discrepancies
- stop execution when unresolved

Reconciliation never silently overwrites local history. Differences are classified as timing, expected external test-environment adjustment/reset, missing local event, unknown order/fill, fee difference, rounding tolerance, or unexplained mismatch. Unresolved value posts to reconciliation suspense, quarantines the affected balance, and blocks entries until resolved or explicitly adjusted with evidence.

Run reconciliation:

- at startup
- after reconnect
- periodically
- after unknown orders
- after critical errors

## 20.4 P&L categories

Report:

- gross trading P&L
- maker/taker fees
- spread cost
- slippage cost
- latency deterioration
- recovery losses
- inventory market P&L
- rebalancing cost
- net P&L

---

# 21. Backtesting framework

## 21.1 Shared logic

The same strategy package must be used by:

- historical backtest
- replay
- shadow mode
- testnet/demo

Do not create a simplified duplicate strategy implementation for backtests.

## 21.2 No-look-ahead rules

- Use only information available at the decision timestamp.
- Baseline candle strategies act only on completed candles.
- Indicators must not include future values.
- Orders execute after configured latency, not at the signal’s historical price.
- Parameter normalization must use training-window data only.

## 21.3 Validation workflow

Support:

- chronological train/validation/test splits
- walk-forward testing
- untouched final test window
- fee stress test
- slippage stress test
- latency stress test
- parameter-neighborhood stability
- market-regime breakdown
- benchmark comparison

## 21.4 Metrics

Required metrics:

- total net return
- annualized return when meaningful
- maximum drawdown
- current drawdown
- Sharpe ratio
- Sortino ratio
- Calmar ratio
- profit factor
- expectancy
- win rate
- average win
- average loss
- largest win/loss
- turnover
- exposure
- number of trades
- fee percentage of gross profit
- slippage percentage of gross profit
- recovery loss
- time in market
- performance by asset
- performance by exchange
- performance by strategy
- performance by regime

## 21.5 Reproducibility

Each run stores:

- immutable run ID
- code commit
- strategy version
- configuration hash
- dataset segment checksums
- random seed
- starting balances
- fee model
- latency model
- result checksum

---

# 22. Live shadow mode

## 22.1 Behavior

Live shadow mode consumes real production public market data and performs every decision and simulation step without submitting an order.

It must:

- generate candidates
- allocate virtual funds
- run risk checks
- create simulated orders
- apply measured latency
- inspect future live books at assumed arrival
- simulate fills
- manage exits
- reconcile the virtual ledger
- grade the decision

## 22.2 Decision grading

Do not reduce outcomes to `right` or `wrong`.

Use categories:

- valid process, profitable outcome
- valid process, losing outcome
- invalid signal
- missed opportunity
- expired before execution
- insufficient liquidity
- risk rejection
- data-quality rejection
- execution-model failure
- recovery required

## 22.3 Opportunity lifetime

For arbitrage, track:

- first detection time
- last profitable observation
- peak net edge
- edge at simulated arrival
- total lifetime
- whether the opportunity survived p50/p95/p99 latency

---

# 23. PostgreSQL model

At minimum, implement tables or equivalent models for:

- users
- audit_events
- exchanges
- exchange_capabilities
- instruments
- instrument_metadata_versions
- assets
- asset_screening_versions
- market_data_segments
- dataset_manifests
- data_quality_events
- strategy_definitions
- strategy_versions
- strategy_parameters
- portfolios
- strategy_portfolios
- virtual_accounts
- virtual_balances
- reservations
- opportunities
- decisions
- decision_inputs
- experiment_registrations
- risk_evaluations
- orders
- order_attempts
- order_events
- execution_plans
- execution_plan_legs
- fills
- journal_transactions
- ledger_entries
- positions
- account_snapshots
- backtest_runs
- replay_runs
- shadow_sessions
- latency_models
- fee_models
- incidents
- alerts
- configuration_versions
- command_requests
- inbox_events
- outbox_events
- execution_leases
- reconciliation_cases
- reconciliation_suspense

Requirements:

- migrations are forward-only and version controlled
- foreign keys for relational integrity
- immutable historical decision and fill records
- soft deletion only where audit history requires it
- indexes designed from query patterns
- UTC timestamps
- JSON only for truly variable metadata, not as a substitute for schema design
- database-enforced uniqueness for client order IDs, exchange fill IDs, inbox identities, immutable run IDs, and active execution leases
- append-only journal, audit, fill, decision, configuration-version, and order-event history, with corrections through compensating records
- transaction boundaries that atomically couple fill reduction, journal posting, balance projections, and reservation changes

---

# 24. Backend API

Use versioned APIs such as `/api/v1`.

## 24.1 API groups

- authentication/session
- system status
- exchange health
- instruments and asset universe
- portfolios and balances
- strategies and versions
- opportunities
- decisions
- orders and fills
- risk status
- backtests
- replays
- shadow sessions
- reports
- incidents
- configuration
- audit

## 24.2 Live updates

Provide a versioned live stream for:

- exchange health
- current opportunities
- decisions
- order-state transitions
- fills
- risk events
- portfolio snapshots
- system alerts

The live stream must not be the only source of data. The UI must recover current state through REST after reconnect.

Every live event has a stream name, schema version, monotonic stream revision, event ID, entity revision, UTC time, and correlation/causation IDs. Support bounded replay from a resume cursor where retained. The client recovery sequence is `fetch snapshot with revision -> subscribe after revision -> apply monotonic events`; this closes the snapshot/stream race. A cursor outside retention forces a fresh snapshot.

## 24.3 API quality

- Generate an OpenAPI specification.
- Validate inputs at boundaries.
- Use typed error codes.
- Include correlation IDs.
- Paginate large lists.
- Support time-range filtering.
- Apply authorization to administrative actions.
- Never return secrets or raw credential material.
- Require idempotency keys for mutating run, administrative, and sandbox-order commands.
- Use optimistic concurrency or entity revisions for configuration, strategy promotion, incident, and risk-state mutations.
- Long-running backtest, replay, export, and report jobs have explicit `QUEUED`, `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, `CANCEL_REQUESTED`, `CANCELED`, `SUCCEEDED`, and `FAILED` lifecycle states where applicable.
- Apply per-user and global resource quotas; allow safe cancel/retry without duplicating a run.

---

# 25. React frontend requirements

The React application is a professional research, monitoring, risk, and audit console—not a decorative profit screen.

## 25.1 Frontend architecture

Use feature folders such as:

```text
features/
├── command-center/
├── opportunities/
├── strategies/
├── backtests/
├── replay/
├── shadow/
├── portfolios/
├── risk/
├── exchanges/
├── universe/
├── rebalancing/
├── system-health/
├── incidents/
├── reports/
└── settings/
```

Rules:

- strict TypeScript
- generated API types where practical
- no `any` without documented justification
- small components
- business logic in hooks/services, not giant JSX files
- reusable accessible components
- keyboard navigation
- responsive desktop-first layout
- dark and light themes
- loading, empty, stale, error, and reconnect states
- UTC and user-local time display options

## 25.2 Persistent global header

Show:

- environment and execution mode
- explicit `REAL TRADING DISABLED`
- current shadow/test session
- overall engine status
- exchange connection indicators
- global risk state
- server clock and drift warning
- active critical alerts
- pause controls where applicable
- user menu

The environment label must remain visible on every page.

## 25.3 Command Center

### Portfolio summary

- total virtual equity
- starting equity
- net P&L
- realized P&L
- unrealized P&L
- inventory P&L
- fees
- slippage
- maximum/current drawdown
- available and reserved capital

### Strategy summary

For every strategy:

- enabled status
- version
- champion/challenger state
- virtual capital
- daily and total P&L
- drawdown
- open positions
- decisions
- approved/rejected counts
- win rate
- profit factor
- last decision

### Exchange summary

- connection state
- last message age
- data quality
- REST latency
- WebSocket lag
- reconnects
- sequence gaps
- rate-limit usage
- active instruments

### Activity timeline

Show human-readable events with filters and links to details.

## 25.4 Opportunity Scanner

### Cross-exchange view

Columns:

- instrument
- buy exchange
- sell exchange
- gross spread
- net spread
- tested size
- maximum size
- expected profit
- worst-case profit
- opportunity age
- lifetime
- status

Detail drawer/page:

- synchronized order books
- depth consumed
- fee breakdown
- latency assumptions
- inventory before/after
- risk decision
- simulated leg results
- recovery analysis
- raw decision timeline

### Triangular view

Show:

- cycle path
- exchange
- starting asset
- size
- gross return
- fees
- slippage
- net return
- weakest leg
- maximum executable size
- opportunity lifetime
- final simulated result

## 25.5 Strategy Center

Each strategy page shows:

- explanation
- version and parameters
- supported assets/timeframes
- risk limits
- current signals
- historical decisions
- rejected candidates
- performance by asset
- performance by regime
- benchmark comparison
- champion/challenger comparison
- parameter-sensitivity results
- version history

## 25.6 Backtest Lab

Inputs:

- strategy/version
- exchanges
- instruments
- date range
- starting capital
- portfolio allocation
- fee model
- slippage model
- latency model
- position/risk limits
- walk-forward settings

Outputs:

- equity curve
- drawdown curve
- monthly return table
- trade list
- fee/slippage attribution
- all required metrics
- benchmark comparison
- regime analysis
- parameter stability
- reproducibility metadata
- CSV/JSON export

## 25.7 Replay Lab

Controls:

- dataset selection
- start/end time
- speed: 1x, 10x, 100x, maximum
- pause
- single-step
- seek
- fault injection
- latency scenario

Visuals:

- order-book ladder
- trades
- strategy state
- candidate timeline
- simulated orders
- balances
- P&L
- risk events

## 25.8 Shadow Trading Center

Show:

- active shadow sessions
- virtual orders
- expected versus simulated fills
- expected versus realized simulated P&L
- opportunity survival
- decision grading
- latency attribution
- session comparison

## 25.9 Portfolio and Inventory

Views by:

- exchange
- asset
- strategy
- portfolio

Show:

- available
- reserved
- target
- minimum/maximum bands
- cost basis
- market value
- inventory P&L
- status

## 25.10 Risk Center

Show:

- global risk state
- daily loss
- drawdown
- exposure per asset/exchange/strategy
- reserve percentage
- stale feeds
- unknown orders
- circuit-breaker history
- active limits and usage

Controls:

- pause strategy
- pause instrument
- pause exchange
- pause new entries
- lock platform
- unlock with confirmation and audit

## 25.11 Exchange Connections

For each exchange:

- public/private connection status
- environment
- capabilities
- last event
- event rate
- sequence gaps
- reconnects
- REST p50/p95/p99
- WebSocket lag
- rate-limit usage
- loaded instruments
- metadata freshness
- test/demo account reconciliation status

## 25.12 Asset Universe

Show:

- screening status
- exchanges/pairs
- liquidity score
- spread score
- depth score
- data quality
- historical coverage
- strategy eligibility
- reason for current status
- audit history

## 25.13 Rebalancing Center

Show:

- inventory imbalance
- reverse-arbitrage opportunities
- recommended routes
- expected cost
- expected duration
- required networks
- warnings
- manual checklist

No transfer execution button in V1.

## 25.14 System Health

Show:

- CPU
- memory
- disk
- database size
- raw-data storage
- queue lag
- dropped events
- order-book rebuilds
- clock drift
- database latency
- hot-path p50/p95/p99
- strategy p50/p95/p99
- risk p50/p95/p99
- server uptime

## 25.15 Incidents and Audit

Search and filter:

- connection failures
- sequence gaps
- circuit breakers
- reconciliation mismatches
- unknown orders
- configuration changes
- user actions
- restarts
- strategy promotions

Allow an incident to be opened in replay mode from the relevant recorded data when available.

## 25.16 Reports

Generate on-demand and scheduled virtual reports:

- daily platform summary
- strategy performance report
- exchange quality report
- rejected-opportunity report
- missed-opportunity report
- arbitrage lifetime report
- fee and slippage report
- inventory exposure report
- drawdown and risk report
- champion/challenger report

Support export to CSV and JSON. PDF export may be added later but is not required for core V1.

---

# 26. Security

## 26.1 Secrets

- Never commit secrets.
- Never return secrets to the browser.
- Never log API secrets or signatures.
- Use environment injection, Docker secrets, or a secret manager.
- Separate Binance testnet and Bybit demo credentials.
- Validate permissions at startup.

## 26.2 Authentication

For the private single-owner V1 console:

- secure session cookies
- CSRF protection
- password stored with a modern memory-hard hash
- Argon2id parameters selected and recorded through a security ADR, with automatic rehash on successful login when parameters are obsolete
- optional TOTP support
- session expiration
- server-side session revocation, token rotation after login/privilege change, and hashed session-token storage
- login rate limiting
- audit log of administrative actions
- recent reauthentication and TOTP, when enabled, for unlocks, test-order arming, credential changes, and risk-limit loosening
- `Secure`, `HttpOnly`, host-only cookies with `SameSite=Strict` outside documented cross-site deployments
- WebSocket/SSE Origin validation and the same authorization policy as REST

Design authentication so external OIDC can be added later.

Audit events include actor/session, action, target, before/after hashes or redacted values, reason, approval, correlation/causation, source address, result, UTC time, and monotonic revision. High-risk events are append-only and hash-chained or periodically sealed so deletion/reordering is detectable. Audit redaction must preserve evidence without storing secrets or credential material.

## 26.3 Network

- HTTPS outside localhost
- firewall
- no public database
- no unauthenticated admin endpoints
- strict CORS
- security headers
- static IP for test/demo authenticated exchange access where supported
- application-level signed-request host/route allowlists; Docker networks and DNS are not treated as sufficient egress controls
- SSRF-resistant outbound webhook policy with HTTPS, resolved-IP validation, redirect limits, timeouts, body limits, and optional destination allowlist

## 26.4 Dependency policy

- Minimize dependencies.
- Pin versions.
- Use automated vulnerability scanning.
- Review exchange and cryptography packages carefully.
- Avoid abandoned libraries.
- Wrap third-party exchange clients behind our adapter interfaces.
- Generate an SBOM, scan container images and lockfiles, sign release images/provenance where supported, and deploy immutable image digests.
- Run application containers as non-root with a read-only root filesystem, dropped Linux capabilities, `no-new-privileges`, bounded processes/memory/CPU, and explicit writable mounts.
- Use least-privilege PostgreSQL roles for migrations, runtime, read-only reporting, and monitoring.
- Encrypt backups and protect secret files with mode `0600`; Compose secret mounts are delivery mechanics, not encrypted secret storage.

---

# 27. Observability and alerting

## 27.1 Structured logs

Every log includes where applicable:

- timestamp
- level
- service
- correlation ID
- exchange
- instrument
- strategy
- decision/order ID
- event code

Do not rely on free-form text alone.

## 27.2 Metrics

Required metrics include:

- WebSocket messages/sec
- decode failures
- sequence gaps
- reconnects
- book age
- event queue depth
- dropped events
- strategy evaluations/sec
- candidate counts
- rejection reason counts
- risk-check duration
- execution simulation duration
- REST latency
- WebSocket lag
- test/demo order acknowledgements
- reconciliation mismatch count
- virtual P&L and drawdown

## 27.3 Alerts

Alert levels:

- info
- warning
- critical

Critical examples:

- unresolved order state
- reconciliation mismatch
- database failure for critical state
- repeated sequence gap
- global risk lock
- clock drift
- unexpected adapter capability change

Implement an alert sink interface. V1 must support in-app alerts and at least one configurable webhook or email-compatible sink.

## 27.4 Initial service objectives

The reference server profile must be recorded before performance certification. Until measured and revised through an ADR, use these acceptance targets:

| Objective | Initial target |
|---|---|
| Decision made from a book already beyond its configured age/skew limit | zero |
| Order-book delta gap detected without invalidating that book generation | zero |
| Duplicate test/demo order caused by restart/retry | zero |
| Lost or double-posted fill in fault tests | zero |
| Unbalanced committed journal transaction | zero |
| Deterministic replay mismatch across 10 identical runs | zero |
| p99 decode + sequence validation + book update at declared load | <= 10 ms |
| p99 strategy + allocator + risk evaluation at declared load | <= 25 ms |
| p95 gap-to-healthy resynchronization when exchange REST is available | <= 15 s |
| Critical in-app alert creation after detection | <= 5 s |
| External alert delivery p95 when sink is available | <= 60 s |
| Graceful shutdown | <= 60 s |
| Shadow restart/recovery readiness RTO | <= 5 min |
| Test/demo restart/reconciliation readiness RTO | <= 10 min |
| Critical database state RPO | zero after acknowledged commit |
| Raw recorder RPO | <= configured flush interval or an explicit dataset gap |
| V1A soak | >= 72 continuous hours |
| V1D readiness soak | >= 7 continuous days |
| Sustained memory behavior | bounded by configured limit with no positive leak trend after warm-up |
| Backup cadence / database RPO | daily / <= 24 h initially |
| Tested restore RTO | <= 4 h initially |
| Accessibility | WCAG 2.2 AA for critical workflows |

Prometheus labels must remain bounded. Exchange, instrument, strategy family, mode, state, and reason-code enums are acceptable; order IDs, decision IDs, client IDs, user IDs, file paths, raw URLs, and arbitrary error text are not metric labels.

---

# 28. Testing strategy

## 28.1 Unit tests

Required for:

- fixed-point arithmetic
- rounding
- fee calculations
- indicator calculations
- graph conversion logic
- strategy rules
- risk rules
- order state transitions
- ledger balancing
- inventory bands

## 28.2 Property and fuzz tests

Use Go fuzzing/property tests for:

- malformed exchange payloads
- decimal arithmetic invariants
- book delta ordering
- sequence-gap handling
- graph cycles
- order-state transitions
- ledger balance invariants

## 28.3 Adapter tests

- golden payload fixtures from official APIs
- snapshot/delta reconstruction
- reconnect/resubscribe
- unknown enums
- rate-limit handling
- authentication signing against official examples
- testnet/demo integration tests behind explicit environment flags

The conformance suite must run primarily against a deterministic local exchange emulator, not an external test environment. The emulator supports REST and WebSocket fixtures, snapshots/deltas, sequence gaps, disconnects, throttles, server-time drift, filter changes, asynchronous acknowledgement, partial and late fills, duplicate/out-of-order private events, cancel/fill races, ambiguous timeouts, account resets, and reconciliation snapshots.

## 28.4 Replay golden tests

Given a fixed dataset and configuration, assert:

- exact decision IDs/order
- candidate values
- order results
- ending balances
- result checksum

## 28.5 Race and load tests

- run `go test -race`
- simulate high event rates
- verify bounded memory
- verify no goroutine leaks
- verify shutdown completes
- verify queue overload causes safe pause, not stale execution

## 28.6 Chaos tests

Inject:

- WebSocket disconnect
- duplicate event
- missing event
- out-of-order event
- slow database
- unavailable database
- REST timeout
- partial fill
- unknown order
- process restart
- clock drift
- process kill after every durable submission boundary
- fencing-lease loss and overlapping engine startup
- disk-full and partial-segment finalization
- duplicate/live-stream resume cursor races

Use model-based tests for the order aggregate, multi-leg saga, risk-state/intent matrix, reservation ownership, and journal. Reservation tests must demonstrate linearizable exclusive ownership under concurrency. Network safety tests must capture every signed request and assert the exact allowlisted host, method, path, category, and prohibited-field absence.

## 28.7 Frontend tests

- unit tests for formatting and calculations
- component tests for critical states
- integration tests for API errors/reconnects
- end-to-end tests for backtest launch, replay control, risk pause, and incident review
- accessibility checks

---

# 29. Performance standards

Do not claim performance without benchmarks.

Initial qualitative goals on the declared reference development machine, supplemented by the numeric objectives in Section 27.4:

- no unbounded queues
- stable memory during multi-hour recording
- deterministic replay faster than real time
- market event hot path measured in microseconds to low milliseconds locally
- no database or frontend dependency in candidate evaluation
- p95/p99 metrics exposed

The platform is not intended to compete with colocated microsecond HFT systems. It should be optimized for reliable public-Internet spot arbitrage research and medium-frequency strategies.

Profile before optimizing. Add benchmarks for:

- order-book update
- VWAP calculation
- triangular-cycle evaluation
- cross-exchange candidate calculation
- risk checks
- decimal arithmetic

---

# 30. Configuration and environment contracts

Use typed configuration with schema validation.

Configuration areas:

- environment
- enabled exchanges
- public endpoints
- test/demo endpoints
- credentials references
- instruments
- asset-screening statuses
- recorder settings
- order-book depth
- queue sizes
- freshness limits
- fee models
- latency models
- strategy parameters
- portfolio allocations
- risk limits
- storage paths
- API/auth settings
- alert sinks

Rules:

- secrets never appear in normal config files
- configuration is versioned and auditable
- invalid combinations fail at startup
- production/live mode is not a valid V1 configuration value
- config reloads must be atomic
- risky changes require explicit confirmation and audit

## 30.1 Configuration layers

Configuration has four distinct layers:

1. **Compiled safety policy:** allowed modes, public/private client separation, authenticated host/route/method allowlists, prohibited request fields, spot-only serializer rules, and absence of a production broker. Environment variables cannot weaken this layer.
2. **Deployment environment:** non-secret wiring documented in `.env.example`, including ports, image versions, paths, feature profiles, public URLs, resource limits, and secret-file references.
3. **Versioned research and risk configuration:** reviewed YAML/JSON or database versions for strategies, risk, portfolio allocation, fees, latency, valuation, assets, datasets, and simulation. Each run references immutable hashes.
4. **Secrets:** file-mounted or secret-manager values never committed to Git or exposed through APIs, logs, metrics, support bundles, or generated Compose output.

Environment variables are not a convenient back door for changing strategy or risk behavior. Production-like deployments should point to versioned configuration files. Any environment-provided safety cap may only tighten the versioned policy; conflicting or looser values fail startup.

## 30.2 Execution-mode matrix

| Mode | Market data | Clock | Broker | Credentials | Source of truth | External side effects |
|---|---|---|---|---|---|---|
| `backtest` | approved historical dataset | deterministic logical | historical simulator | none | run journal | none |
| `replay` | recorded canonical events plus raw references | deterministic replay | replay simulator | none | run journal/checkpoint | none |
| `paper` | historical or synthetic configured feed, never live production | logical or controlled real-time | paper simulator | none | virtual journal | none |
| `shadow` | live production public data | live monotonic + UTC | shadow simulator | none | recoverable virtual journal | public reads only |
| `testnet` | Binance Spot Testnet public/private data | live monotonic + UTC | Binance Spot Testnet only | Binance Testnet key only | exchange test account plus local journal | virtual testnet orders only |
| `demo` | Bybit production public data plus demo private data | live monotonic + UTC | Bybit demo REST only | Bybit demo key only | exchange demo account plus local journal | virtual demo orders only |

Mode is fixed for a session. Results from different modes retain separate model namespaces and are not merged as if equally realistic. Binance Testnet order validation uses Testnet market/account data; production-public Binance shadow research is a separate session. Bybit demo follows its official split between production public data and demo private account/order endpoints.

Test/demo order submission requires all of:

- compiled adapter capability
- global sandbox integrations enabled
- exchange-specific integration enabled
- global order-submission gate enabled
- exchange-specific order-submission gate enabled
- successful startup and current reconciliation
- healthy fencing lease, persistence, books, clock, and risk state
- approved asset/instrument and filter-valid exact order
- short-lived, recently authenticated, audited manual arm
- configured per-order and daily sandbox caps

## 30.3 Configuration reload semantics

- Run identity, execution mode, dataset, code/build, strategy version, accounting policy, and random seed are immutable after a run starts.
- Risk limits may tighten for new and existing exposure according to policy. Loosening applies only to future decisions after approval and never retroactively changes a stored evaluation.
- Asset blocking and exchange/instrument disabling apply immediately to new entries and trigger a documented review of existing exposure; they do not silently fabricate an exit.
- Fee, instrument, latency, and valuation model updates apply atomically to new decisions. Existing orders retain submission-time versions while reconciliation records actual exchange facts.
- Queue sizes, storage paths, database settings, endpoint selections, and credential references are restart-only.
- Every reload validates the complete configuration graph, writes an audit diff, and swaps one immutable snapshot. Partial reload is forbidden.

## 30.4 Endpoint and credential policy

- Production-public endpoints may be configurable only within a code-owned hostname and route allowlist.
- Authenticated endpoints are closed enums compiled into the relevant test/demo broker. Arbitrary URL overrides are forbidden outside isolated emulator tests.
- No environment variable, config key, secret name, service, profile, or interface for production-order credentials exists.
- Credentials are scoped to exactly one exchange and environment. They are never shared with API, recorder, shadow, worker, Prometheus, Grafana, or edge containers.
- Credential files must be regular files, not world/group readable, and must not contain placeholders. Startup validates permissions where the operating system supports it.
- Bybit demo uses `category=spot`, `isLeverage=0`, an allowlisted request schema, REST order entry, and demo private streams. Binance uses only Spot Testnet authenticated endpoints.

## 30.5 Storage, retention, backup, and disk pressure

Before production-public recording begins, Phase A4 must measure expected bytes/day for each stream/depth and create a capacity plan covering at least the configured retention plus 30% headroom.

Initial defaults:

- Parquet + Zstandard level 3
- one-hour or 256 MiB maximum segment, whichever occurs first
- 30 days hot raw-data retention until measured
- 10 GiB minimum-free-space warning/decision threshold on a small server, increased when capacity planning requires it
- daily PostgreSQL backup, 14 daily restore points, and off-host encrypted copy before V1D readiness
- Prometheus retention 15 days with a configured size cap

At the high disk watermark, reject new backtest/export jobs and alert. At the critical watermark, stop starting new shadow decisions, safely finalize or quarantine recorder segments, preserve critical journal/audit writes, and enter `PAUSED` or `LOCKED` according to policy. Retention never deletes data referenced by a locked final test, incident, active replay, legal hold, or reproducibility bundle.

A Docker volume is not a backup. Backup jobs write to independent storage; restore is tested on a clean instance. Database and market-data manifests are backed up consistently enough to locate dataset files, while bulk raw files may use their own independently verified object/filesystem backup policy.

## 30.6 Required owner-supplied deployment values

The project can scaffold safe placeholders, but deployment cannot be considered ready until the owner supplies or approves:

- public application domain/base URL and TLS/ACME contact email
- unique instance ID and truthful collector/server region label
- bootstrap administrator email and a securely generated Argon2id password hash
- PostgreSQL owner, migrator, runtime, and read-only credentials
- independent session-signing, CSRF, Grafana-admin, and optional webhook secrets
- optional Binance Spot Testnet credentials
- optional Bybit demo-account credentials
- server CPU, RAM, disk path/capacity, expected retention, and backup destination
- alert webhook/email destination if external alerting is enabled
- approved-asset decisions and reasons beyond the defaults
- an independent USDT/USD reporting source if USD display/depeg monitoring is enabled
- acceptance of or changes to the conservative risk caps before sandbox order tests

All missing values and safe defaults are documented in `.env.example`. Secret values belong in `.secrets/` or a real secret manager, never directly in the committed environment example.

---

# 31. Cumulative release program and development phases

V1A through V1D form one complete mature program. Releases are cumulative verification and deployment gates; they do not remove final scope. Development remains incremental so unsafe assumptions are discovered before later strategies, authenticated sandboxes, or UI workflows depend on them.

No release may weaken a previously passing invariant. A later release re-runs all applicable earlier safety, determinism, accounting, recovery, and real-money-lock gates.

## 31.1 Program-wide phase rules

Every phase has:

- a named owner role
- linked requirement IDs and threat-model items
- entry dependencies
- deliverables
- automated and manual acceptance evidence
- migrations and rollback/forward-compatibility notes
- operational metrics and alerts
- documentation and runbook updates
- an explicit statement of remaining limitations

The implementation owner cannot waive a failed security, accounting, deterministic-replay, or production-order-lock gate. Waivers for non-safety items require owner approval, expiry, rationale, and a tracked remediation issue.

Release rollout sequence is always:

```text
offline emulator and fixtures
-> deterministic backtest/replay
-> record-only production public data
-> live public-data shadow
-> fault and soak qualification
-> manually armed authenticated testnet/demo canaries
```

## 31.2 Release V1A — Safe deterministic research core

V1A produces a deployable, observable research platform with Binance public data, exact accounting, deterministic replay, one complete strategy, and live shadow operation. It contains no authenticated exchange credentials or external order side effects.

### Phase A0: Scope traceability and safety architecture

Owner: product, architecture, and security.

Deliver:

- requirement-ID scheme and verification matrix
- execution-mode matrix and compiled endpoint policy
- initial threat model and trust boundaries
- real-money-lock test plan
- normative process topology, single-writer/fencing design, and startup/shutdown state machine
- initial SLOs, RPO/RTO, data classification, and risk-policy review

Acceptance:

- every non-negotiable safety rule maps to a test or reviewed evidence artifact
- no unresolved design question permits production orders, ambiguous ownership, unbalanced accounting, or nondeterministic replay

### Phase A1: Repository, toolchain, Compose, and CI governance

Owner: platform engineering.

Deliver:

- monorepo structure, README, contribution guide, coding standards, ADR template, implementation tracker, and phase checklist
- exact Go, Node, pnpm, PostgreSQL, and major dependency pins
- minimal Go/React health applications
- image-based `docker-compose.yml`, `.env.example`, profiles, health checks, volumes, networks, and deployment documentation
- CI skeleton with formatting, lint, tests, security scans, SBOM, secret scan, and prohibited-capability checks

Acceptance:

- backend and frontend health applications start locally
- Compose configuration renders with safe placeholders and exposes no production-private service or setting
- CI builds the skeleton and rejects prohibited modes/endpoints

### Phase A2: Financial domain and typed configuration

Owner: domain engineering.

Deliver:

- fixed-point financial types and checked arithmetic
- canonical IDs, assets, instruments, metadata versions, clocks, typed errors, and immutable configuration snapshots
- configuration schema, version hashes, environment matrix, and prohibited-combination validation

Acceptance:

- arithmetic property/fuzz tests pass
- no authoritative financial calculation uses binary floating point
- invalid products, modes, URLs, placeholder secrets, unsafe percentages, or missing units fail closed

### Phase A3: Deterministic runtime and concurrency model

Owner: runtime/platform engineering.

Deliver:

- canonical event envelope and ingestion ordinal
- deterministic scheduler, keyed randomness, market-view version vectors, and clock abstraction
- bounded queues with event-class loss policies
- execution lease/fencing implementation and graceful lifecycle framework
- in-process event bus and durable command/inbox contracts

Acceptance:

- concurrency scheduling cannot change replay results
- overlapping engine startup cannot acquire the same execution ownership
- overload tests pause or invalidate safely without stale execution

### Phase A4: Transactional storage, journal, and data segments

Owner: storage and accounting.

Deliver:

- PostgreSQL roles, migrations, repositories, immutable journal, reservations, inbox/outbox, and audit foundation
- exact multi-commodity balancing constraints, cost-basis policy, suspense workflow, and rebuildable projections
- raw plus normalized Parquet schemas, crash-safe segment finalization, manifests, compatibility readers, retention, backup, and restore foundation

Acceptance:

- every journal transaction balances per asset
- double spending and negative available balances are rejected under concurrency
- kill-point tests recover segments, outbox items, journals, and reservations without silent loss
- a clean backup restores into a verifiable instance

### Phase A5: Security and observability foundation

Owner: security and SRE.

Deliver:

- structured redacted logs, bounded-cardinality metrics, liveness/readiness, audit events, and core alerts
- non-root/read-only container baseline, least-privilege roles, dependency/image scanning, and secret-file handling
- initial Prometheus/Grafana provisioning and operational dashboards

Acceptance:

- secrets/signatures never appear in logs, metrics, APIs, or support bundles
- critical persistence, fencing, disk, clock, queue, and book failures alert and fail closed

### Phase A6: Exchange contracts and deterministic conformance emulator

Owner: exchange platform.

Deliver:

- narrow public market-data, metadata, history, account, broker, and reconciliation interfaces
- environment/version-aware capabilities, error taxonomy, retry policy, and shared rate budgets
- deterministic REST/WebSocket exchange emulator and golden payload framework

Acceptance:

- simulator proves the contracts
- emulator exercises gaps, reconnects, rate limits, filter changes, duplicates, partial fills, unknown states, and resets deterministically
- unsupported functions fail with typed errors

### Phase A7: Binance production-public market data and recorder

Owner: Binance adapter team.

Deliver:

- public metadata, server-time synchronization, trades, candles, streams, snapshot/delta books, and recorder integration
- reconnect, scheduled connection renewal, sequence-gap handling, and raw/canonical archival

Acceptance:

- approved books meet the V1A 72-hour soak and resynchronization objectives
- recorded events, lifecycle, snapshots, and gaps produce complete verifiable manifests
- the adapter contains no credentials or order capability

### Phase A8: Backtest, replay, simulation, and durable order aggregates

Owner: execution and research platform.

Deliver:

- explicit backtest engine, deterministic replay clock/scheduler, paper/shadow simulation brokers, latency/fill models, shared liquidity scheduler, and virtual journal
- canonical order reducer, multi-leg plan/saga, checkpoints, fault injection, and restart restoration

Acceptance:

- ten identical runs produce identical events, balances, decisions, and checksum
- partial/failed fills, cancel races, fees, dust, and recovery balance exactly
- confidence tiers and non-comparable model namespaces are enforced

### Phase A9: Portfolio, reservations, risk, and recovery

Owner: portfolio and risk engineering.

Deliver:

- strategy/exchange/asset ownership initialization
- exclusive reservations, allocator, risk hierarchy, intent-action matrix, circuit breakers, inventory bands, and risk-reducing recovery
- startup locked recovery and current-state reconciliation for virtual modes

Acceptance:

- concurrent candidates cannot double-own capital or displayed combined-portfolio liquidity
- all initial risk caps, hysteresis, pause/lock, and recovery permissions pass model tests
- unresolved critical state quarantines exposure and blocks entries

### Phase A10: Trend strategy and research validation

Owner: strategy and research.

Deliver:

- exact indicators, rules, stressed sizing, marketable-limit simulation, exits, benchmarks, strategy documentation, and registered validation plan
- backtest, replay, and shadow evidence with confidence intervals and stress tests

Acceptance:

- no look-ahead or signal-close fills
- deterministic decisions across all supported modes
- untouched-test and parameter-stability evidence is available without claiming production profitability

### Phase A11: Minimal research API, operational console, and live shadow sessions

Owner: API and frontend.

Deliver:

- versioned API/OpenAPI, authentication bootstrap, durable job lifecycle, live stream revisions/resume, and audit
- minimal Command Center, Binance health, portfolio/journal, risk, trend backtest/replay, incident, and shadow-session workflows
- live production-public shadow engine with no private credentials

Acceptance:

- the complete trend workflow runs from configured experiment through backtest, replay, live shadow, report, and incident replay
- UI recovers snapshot/stream state after disconnect and clearly labels all values virtual
- stale, error, paused, locked, and empty states are accessible and tested

### V1A release gate

- Production broker, withdrawal, margin, leverage, and production-private credential paths are absent from code, config, images, and Compose.
- Binance public books, recorder, and replay meet numeric SLOs.
- Journal, reservation, fencing, restart, and deterministic-replay invariants pass.
- Trend runs end-to-end in backtest, replay, and live shadow.
- Core alerts and a clean backup/restore drill pass.

## 31.3 Release V1B — Multi-exchange strategy research

V1B adds Bybit public data, every required strategy family, cross-market qualification, inventory economics, statistical promotion evidence, and complete multi-exchange shadow workflows. It still has no authenticated exchange credentials or external orders.

### Phase B1: Bybit production-public market data and recorder

Owner: Bybit adapter team.

Deliver the same public metadata, book, trade, ticker, candle, health, lifecycle, and recorder contract as Binance, including snapshot replacement and sequence semantics.

Acceptance: the common conformance, soak, reconnect, manifest, and exchange-independence suites pass.

### Phase B2: Dataset and cross-market-view qualification

Owner: market data and research.

Deliver cross-exchange as-of joins, version vectors, clock-uncertainty intervals, maximum-skew enforcement, completeness manifests, collector-region metadata, and dataset quality gates.

Acceptance: cross-exchange decisions reproduce exact coherent input views and Tier A datasets contain no hidden gaps.

### Phase B3: Mean-reversion strategy

Owner: strategy and research.

Deliver exact z-score/regime formulas, stressed sizing, bounded holding/cooldown, exits, tests, documentation, and registered validation evidence.

Acceptance: no averaging down, deterministic decisions, downtrend/adverse-ordering stress tests, and regime reports pass.

### Phase B4: Triangular arbitrage

Owner: strategy and execution.

Deliver exact exhaustive cycle enumeration, multi-size fixed-point depth validation, sequential latency/fill simulation, dust/fee-asset treatment, plan/saga recovery, and opportunity lifetime reporting.

Acceptance: no false profit from rounding, invalid filters, reused liquidity, partial cycles, or stranded assets.

### Phase B5: Cross-exchange arbitrage

Owner: strategy and execution.

Deliver coherent two-book comparison, concurrent leg scheduling, exclusive two-sided reservations, marginal inventory/rebalancing economics, one-leg recovery, and P&L separation.

Acceptance: both directions, partial/unknown legs, inventory depletion, natural reversal, and closed-inventory-cycle economics pass.

### Phase B6: Inventory and rebalancing optimizer

Owner: portfolio and research.

Deliver the asset@exchange graph, natural-reversal preference, versioned advisory trade/transfer facts, cost/risk routes, provenance/freshness, and manual checklist.

Acceptance: no withdrawal/transfer execution path exists and stale or incompatible network facts cannot produce an unqualified recommendation.

### Phase B7: Multi-strategy research validation and promotion evidence

Owner: research and QA.

Deliver experiment registry, locked final tests, walk-forward analysis, block-bootstrap intervals, multiple-testing controls, deflated/probabilistic Sharpe, capacity curves, regime breakdowns, and champion/challenger reports.

Acceptance: each maturity promotion has preregistered measurable evidence; low-confidence or demo/test results cannot qualify a strategy.

### Phase B8: Multi-exchange shadow API and UI

Owner: API and frontend.

Deliver the opportunity scanner, strategy comparison, inventory, rebalancing, quality/confidence displays, multi-leg timelines, recovery analysis, and replay fault controls.

Acceptance: every strategy runs through the same production code in backtest, replay, and shadow as applicable, with separated experiments and no misleading combined balance.

### V1B release gate

- Both public adapters and recorders meet freshness, completeness, and soak SLOs.
- All strategies, failure/recovery paths, budgets, and inventory ownership are deterministic and exactly accounted.
- Arbitrage, inventory, fees, latency, recovery, and rebalancing economics remain separated.
- Statistical validation reports are complete and make no claim based on sandbox liquidity or fills.

## 31.4 Release V1C — Authenticated sandbox execution

V1C adds production-grade authenticated integration discipline, but only for Binance Spot Testnet and Bybit demo virtual accounts. Sandbox submission remains default-off, manually armed, tightly capped, and incapable of targeting production.

### Phase C1: Credential and endpoint security boundary

Owner: security and exchange platform.

Deliver separate public/authenticated transports, compiled allowlists, signer denial policy, least-privilege credential validation, serialized-field allowlists, and negative outbound-request tests.

Acceptance: every signed request is proven to target only the exact permitted test/demo host, route, method, spot category, and non-leveraged field set.

### Phase C2: Authenticated control plane

Owner: API and security.

Deliver sessions, CSRF, Origin validation, authorization, session revocation, recent reauthentication/TOTP, audited manual arming, idempotent admin commands, and secure secret rotation workflows.

Acceptance: unauthorized, stale-session, CSRF, replayed-command, and privilege-escalation tests pass; secrets never reach the browser.

### Phase C3: Account reconciliation and crash-recovery harness

Owner: accounting and execution.

Deliver adapter-neutral account snapshots, order/fill deduplication, suspense classification, startup-locked recovery, fencing, submission outbox/inbox, and process kill-point harness.

Acceptance: termination at every submission boundary produces no duplicate order, lost fill, negative balance, or unsafe reservation release.

### Phase C4: Binance Spot Testnet integration

Owner: Binance adapter team.

Deliver testnet signing, order place/cancel/query, user-data events, exact filters, unknown-order recovery, periodic-reset handling, and reconciliation.

Acceptance: canonical states and races pass; resets become explicit external adjustments; startup reconciliation and duplicate prevention pass.

### Phase C5: Bybit demo integration

Owner: Bybit adapter team.

Deliver demo REST orders, private events, account reconciliation, `category=spot`, `isLeverage=0`, field allowlists, and unsupported-capability handling.

Acceptance: WebSocket order entry and prohibited fields are never attempted; asynchronous/duplicate events reduce idempotently; demo balances reconcile.

### Phase C6: Sandbox console and soak qualification

Owner: frontend, QA, and SRE.

Deliver test/demo orders, fills, unknown states, reconciliation/suspense, arm expiry, risk controls, account-reset incidents, and chaos/soak dashboards.

Acceptance: long-running sandbox canaries meet caps and SLOs; UI cannot arm production or bypass reconciliation/risk; all actions are auditable.

### V1C release gate

- Outbound-request capture proves production-private submission impossible.
- Every order state, duplicate, late fill, cancel/fill race, ambiguous timeout, reset, and crash boundary is exercised.
- Startup remains locked until reconciliation succeeds.
- Credentials, signatures, private payloads, and session material do not leak.
- Sandbox results remain isolated from strategy-profitability evidence.

## 31.5 Release V1D — Complete product and operational readiness

V1D completes the full API, React console, labs, reporting, incident response, data lifecycle, security, and single-server operational maturity described throughout this document.

### Phase D1: Complete versioned API and live updates

Owner: API engineering.

Deliver every Section 24 resource, generated OpenAPI/types, pagination/filtering, idempotency, revisions, resume cursors, quotas, exports, and administrative authorization.

Acceptance: API contract, recovery, authorization, compatibility, load, and secret-leak tests pass.

### Phase D2: Complete React Command Center and monitoring

Owner: frontend engineering.

Deliver the complete Section 25 navigation and operational pages except the specialized labs completed in D3, using accessible responsive components and explicit confidence/state labels.

Acceptance: WCAG 2.2 AA critical workflows, browser matrix, responsive layout, stale/error/loading/reconnect states, and end-to-end workflows pass.

### Phase D3: Complete Backtest, Replay, and Shadow Labs

Owner: frontend and research platform.

Deliver run creation/lifecycle, progress, comparison, parameter diffs, reproducibility bundles, replay controls/faults, shadow comparison, and export.

Acceptance: runs can be created, paused/canceled where supported, monitored, reproduced, compared, opened, and exported without losing immutable identity.

### Phase D4: Reports, incidents, audit, and alert delivery

Owner: reporting, security, and SRE.

Deliver scheduled/on-demand reports, incident lifecycle, tamper-evident audit review, alert routing, acknowledgement, replay links, and evidence bundles.

Acceptance: incidents link to complete replay inputs when available, alerts meet delivery SLOs, and reports preserve confidence/valuation/model provenance.

### Phase D5: Operational hardening and data lifecycle

Owner: SRE and storage.

Deliver hardened image/digest deployment, edge TLS, backup/off-host retention/restore drills, schema upgrades, raw-data lifecycle, disk-pressure automation, load/race/chaos tests, runbooks, rollback/forward-fix procedures, and seven-day readiness soak.

Acceptance: numeric SLOs, resource bounds, RPO/RTO, disaster restore, graceful lifecycle, and incident rollback criteria pass on the recorded reference server.

### Phase D6: V1 readiness and safety certification

Owner: QA, security, product, and independent reviewer where available.

Deliver traceability closure, security review, safety manifest, complete documentation, reproducible example bundle, limitation register, and signed release evidence.

Acceptance: every final criterion in Section 35 passes; high-severity security/accounting/safety findings are closed; a clean build independently proves production-order submission impossible.

### V1D release gate

- All V1A-V1D phase gates and original final acceptance criteria pass.
- Every required strategy, API, UI, report, runbook, test/demo integration, and failure mode is implemented and verified.
- Numeric SLOs and restore objectives pass on the declared reference server.
- The platform is mature for continuous production-public research and controlled sandbox validation, while real-money trading remains impossible.

## 31.6 Original phase-to-release preservation map

The original phase requirements are preserved below as a detailed deliverable catalogue. They no longer define execution order; the cumulative phases above are normative.

| Original phase | New owning phase(s) |
|---|---|
| 0 Repository and governance | A1 |
| 1 Domain foundation | A2, A3 |
| 2 Storage foundation | A4 |
| 3 Exchange framework | A6 |
| 4 Binance public market data | A7 |
| 5 Bybit public market data | B1 |
| 6 Replay and simulation broker | A8, C3 |
| 7 Portfolio and risk | A9 |
| 8 Trend strategy | A10 |
| 9 Mean-reversion strategy | B3 |
| 10 Triangular arbitrage | B4 |
| 11 Cross-exchange arbitrage | B5 |
| 12 Rebalancing optimizer | B6 |
| 13 Binance Spot Testnet | C4 |
| 14 Bybit demo integration | C5 |
| 15 Backend API and live updates | A11, C2, D1 |
| 16 React command center and monitoring | A11, B8, C6, D2 |
| 17 Backtest, replay, and shadow labs | A11, B8, D3 |
| 18 Reports, incidents, and operational hardening | A5, C3, D4, D5 |
| 19 V1 readiness review | D6 |

## 31.7 Preserved original deliverable catalogue

The following detailed requirements remain binding under the mapped phases. Where wording here conflicts with a more precise normative rule or release phase above, the safer and more precise rule governs.

### Original Phase 0: Repository and governance

Deliver:

- monorepo structure
- README
- contribution guide
- coding standards
- implementation status file
- ADR template
- Makefile/task commands
- CI skeleton
- local Docker Compose

Acceptance:

- backend and frontend start with health pages
- CI runs formatting, lint, and placeholder tests
- documentation structure exists

### Original Phase 1: Domain foundation

Deliver:

- fixed-point financial types
- canonical IDs
- assets/instruments
- clocks
- typed errors
- configuration schema
- event envelope

Acceptance:

- arithmetic property tests pass
- no financial float use in domain packages
- config rejects prohibited modes/products

### Original Phase 2: Storage foundation

Deliver:

- PostgreSQL migrations
- repositories
- append-only raw segment writer
- Parquet schemas
- checksums
- retention configuration

Acceptance:

- data segments can be written/read deterministically
- migration tests pass
- ledger schema supports balancing

### Original Phase 3: Exchange framework

Deliver:

- adapter contracts
- capability model
- normalization layer
- exchange health state machine
- rate-limit abstraction
- fixture framework

Acceptance:

- simulator adapter proves the contract
- unsupported capabilities return typed errors

### Original Phase 4: Binance public market data

Deliver:

- instrument metadata
- time sync
- public streams
- snapshot/delta books
- trades/candles
- recorder integration

Acceptance:

- BTC/ETH books meet the V1A 72-hour soak, freshness, and resynchronization objectives
- reconnect and sequence-gap tests pass
- recorded data replays correctly

### Original Phase 5: Bybit public market data

Deliver:

- instrument metadata
- sequence-aware books
- trades/tickers/candles
- recorder integration

Acceptance:

- same canonical tests pass for Binance and Bybit
- strategies receive exchange-independent data

### Original Phase 6: Replay and simulation broker

Deliver:

- deterministic replay clock
- paper broker
- latency model
- fill model
- order state machine
- virtual ledger

Acceptance:

- golden replay is deterministic
- partial/failed fills balance correctly
- restart restores virtual state

### Original Phase 7: Portfolio and risk

Deliver:

- strategy sub-portfolios
- reservations
- allocator
- risk states
- circuit breakers
- inventory bands

Acceptance:

- double spending impossible in concurrency tests
- risk rules reject invalid candidates
- pauses propagate safely

### Original Phase 8: Trend strategy

Deliver:

- indicators
- baseline rules
- sizing
- exits
- tests
- strategy documentation

Acceptance:

- no look-ahead in backtests
- strategy produces deterministic decisions
- metrics and benchmark reports available

### Original Phase 9: Mean-reversion strategy

Deliver:

- regime filter
- z-score model
- bounded risk/holding period
- tests and documentation

Acceptance:

- no averaging down
- downtrend safety tests
- regime-specific reports

### Original Phase 10: Triangular arbitrage

Deliver:

- conversion graph
- candidate discovery
- exact depth validation
- multi-size simulation
- recovery simulation

Acceptance:

- fee/rounding tests
- no false profit from invalid quantities
- candidate lifetime reporting

### Original Phase 11: Cross-exchange arbitrage

Deliver:

- synchronized comparison
- two-leg simulation
- inventory-aware controls
- one-leg recovery
- P&L separation

Acceptance:

- both directions tested
- one-leg scenarios handled
- inventory constraints enforced

### Original Phase 12: Rebalancing optimizer

Deliver:

- asset@exchange graph
- trade and transfer-edge model
- advisory route output
- network warnings

Acceptance:

- no withdrawal execution path
- recommendations include full cost/risk breakdown

### Original Phase 13: Binance Spot Testnet

Deliver:

- authenticated testnet client
- order placement/cancel/query
- user-data events
- unknown-order recovery
- reconciliation

Acceptance:

- all canonical order states exercised
- duplicate prevention verified
- startup reconciliation passes

### Original Phase 14: Bybit demo integration

Deliver:

- demo authenticated REST operations
- demo private stream support
- capability restrictions
- reconciliation

Acceptance:

- unsupported WebSocket order entry is not attempted
- demo orders reconcile
- spot category enforcement tests pass

### Original Phase 15: Backend API and live updates

Deliver:

- versioned REST API
- OpenAPI
- auth/session
- live event stream
- pagination/filtering

Acceptance:

- no secret leakage
- UI can recover after stream reconnect
- admin actions are audited

### Original Phase 16: React command center and monitoring

Deliver the Section 25 operational pages assigned to A11, B8, C6, and D2. Specialized functional Backtest, Replay, and Shadow Labs are completed in D3; earlier phases may provide clearly labelled shells or vertical subsets only when their underlying workflow is already functional.

Acceptance:

- responsive and accessible
- clear environment labels
- stale/error/loading states
- realistic fixture/demo data
- end-to-end critical workflows pass

### Original Phase 17: Backtest, replay, and shadow labs

Deliver:

- run configuration UI
- progress
- reports
- reproducibility details
- replay controls
- shadow session comparison

Acceptance:

- runs can be created, monitored, opened, and exported
- identical replay produces identical checksum

### Original Phase 18: Reports, incidents, and operational hardening

Deliver:

- reports
- incident center
- alert sinks
- runbooks
- backup/restore
- chaos testing
- load testing

Acceptance:

- incident can link to replay data
- backup restore tested
- long-running shadow session passes the applicable 72-hour or seven-day release soak and numeric service objectives

### Original Phase 19: V1 readiness review

Required evidence:

- all real-money paths absent/blocked
- all phase acceptance tests pass
- security review completed
- race tests pass
- static analysis clean
- frontend type/lint/build/tests clean
- documentation complete
- known limitations recorded
- reproducible backtest and replay examples included

---

# 32. CI/CD quality gates

Backend gates:

- `gofmt`
- `go vet`
- `staticcheck`
- unit/integration tests
- `go test -race`
- fuzz smoke tests
- vulnerability scan
- build all commands

Frontend gates:

- formatter
- ESLint
- TypeScript strict check
- unit/component tests
- production build
- accessibility smoke tests

Repository gates:

- migration validation
- OpenAPI generation consistency
- generated-code consistency
- documentation links
- prohibited keyword/config scan for live, margin, futures, withdrawals where appropriate
- secret scanning
- requirement-to-test traceability consistency
- deterministic replay compatibility fixtures from supported prior schemas
- Docker Compose render validation for every supported profile combination
- container build, SBOM, vulnerability scan, non-root/read-only smoke test, and immutable image digest output
- outbound signed-request capture proving exact test/demo destinations and prohibited-field absence
- build-symbol/package scan proving no production broker, withdrawal, margin, leverage, or production-private signer path is linked

No merge when required gates fail.

---

# 33. Documentation deliverables

Maintain:

- `README.md`: setup and project overview
- `.env.example`: complete non-secret deployment inputs, safe defaults, and owner-supplied placeholders
- `docker-compose.yml`: image-based single-server deployment contract with safe profiles
- `deploy/README.md`: server preparation, secrets, profiles, backup, and sandbox enablement
- `docs/glossary.md`: crypto and system terminology
- `docs/architecture/system-overview.md`
- `docs/architecture/hot-path.md`
- `docs/architecture/data-storage.md`
- `docs/architecture/concurrency.md`
- `docs/strategies/*.md`
- `docs/exchange-adapters/binance.md`
- `docs/exchange-adapters/bybit.md`
- `docs/security/threat-model.md`
- `docs/operations/runbook.md`
- `docs/operations/incident-response.md`
- `docs/operations/backup-restore.md`
- `docs/api/`
- `docs/implementation-status.md`
- `docs/requirements/traceability.md`
- `docs/configuration/execution-modes.md`
- `docs/configuration/environment.md`
- `docs/accounting/journal-and-valuation.md`
- `docs/research/validation-policy.md`
- `docs/deployment/single-server-compose.md`
- `docs/deployment/tls-and-secrets.md`
- ADRs for major decisions

Every feature documentation must explain:

- purpose
- inputs
- outputs
- business rules
- formulas
- failure modes
- metrics
- configuration
- tests
- known limitations

---

# 34. Definition of done for any feature

A feature is done only when:

- domain behavior is implemented
- tests pass
- error and edge cases are handled
- metrics and logs exist
- configuration is documented
- API/UI behavior is complete where applicable
- audit behavior is included
- documentation is updated
- no prohibited V1 capability is introduced
- code remains modular and reviewable
- linked requirement IDs and acceptance evidence are updated
- migration, rollback/forward-fix, restart, and configuration-version behavior is tested where applicable
- security, metrics-cardinality, accessibility, and SLO effects are reviewed
- no result overstates its simulation confidence or research maturity

---

# 35. V1D final acceptance criteria

V1 is complete when all of the following are true:

1. Binance and Bybit public BTC/ETH spot books are maintained reliably.
2. Raw market data is recorded and deterministically replayable.
3. Trend, mean reversion, triangular arbitrage, cross-exchange arbitrage, cash/risk regime, and rebalancing recommendation logic all run.
4. All strategies support backtest/replay/shadow as applicable.
5. Binance Testnet and Bybit demo order plumbing are validated.
6. Central allocator and risk engine control every simulated/demo order.
7. Virtual accounting balances exactly.
8. One-leg arbitrage failures are simulated and recovered.
9. Inventory P&L is separated from arbitrage P&L.
10. React provides all required command, strategy, opportunity, backtest, replay, shadow, portfolio, risk, exchange, asset, rebalancing, health, incident, audit, and reporting screens.
11. The platform survives reconnects, sequence gaps, slow storage, restarts, and injected failures safely.
12. Backtests and replays are reproducible.
13. Production real-money trading is impossible in the V1 build.
14. Documentation and runbooks are complete.
15. All quality gates pass.
16. Every critical requirement maps to current evidence and no expired waiver affects safety, accounting, determinism, or production-order lockout.
17. The seven-day declared-load readiness soak meets the numeric service objectives.
18. Database restore, market-data manifest recovery, and clean-server Compose deployment are demonstrated from documented artifacts.
19. Kill-point tests across every sandbox submission boundary cause no duplicate order, lost fill, or unsafe reservation release.
20. Signed-request capture and clean-build inspection independently prove production-private order submission impossible.
21. Every strategy result shows its mode, data-confidence tier, valuation basis, fee/latency/fill models, sample size, uncertainty, and maturity state.
22. Platform readiness and strategy viability are reported separately. A trustworthy negative strategy result is an acceptable research outcome and is never hidden.

---

# 36. Known limitations of V1

- Only Binance and Bybit are implemented.
- Only USDT, BTC, and ETH are approved by default.
- Real-money trading is intentionally unavailable.
- Withdrawals and automatic transfers are unavailable.
- Demo/testnet liquidity does not represent production liquidity.
- Reliable arbitrage research requires collecting sufficient synchronized order-book history.
- Maker queue simulations are approximate.
- Public order books cannot prove hypothetical fills, hidden liquidity, queue position, market impact, or production order latency.
- Spot-only cross-exchange arbitrage retains inventory price risk.
- USDT is not risk-free USD; USD display and depeg controls depend on a separate configured reference.
- Single-server Compose is not high availability; execution fencing prevents dual ownership but does not eliminate server downtime.
- Binance Testnet may reset account/order state, and demo/testnet behavior is integration evidence only.
- The platform does not guarantee profit.
- Asset-screening decisions are external inputs, not automated religious rulings.

---

# 37. Future work after V1

Possible later phases, requiring separate approval:

- Kraken adapter
- KuCoin adapter
- Gate.io adapter
- additional approved assets
- USDC as a separate settlement asset
- multi-region latency probes
- warm standby with leader election and fencing
- advanced maker simulation
- production live spot pilot
- manual approval workflow for live orders
- optional Rust optimization for a proven bottleneck

No future item should weaken V1 safety or auditability.

The production live spot pilot and any production-order capability are explicitly **not authorized by this specification**. They require a separate threat model, specification, approval, credential architecture, build capability, deployment manifest, and readiness review. No placeholder production broker or environment toggle may be added preemptively.

---

# 38. Official implementation references

Codex must verify current behavior against official documentation before implementing exchange-specific logic.

- Binance Spot API documentation: https://developers.binance.com/docs/binance-spot-api-docs/
- Binance Spot Testnet general information: https://developers.binance.com/en/docs/products/spot/testnet/general-info
- Binance Spot WebSocket API: https://developers.binance.com/docs/binance-spot-api-docs/websocket-api
- Binance Spot market streams: https://developers.binance.com/en/docs/catalog/core-trading-spot-trading/api/ws-streams/~
- Bybit V5 API documentation: https://bybit-exchange.github.io/docs/
- Bybit demo trading service: https://bybit-exchange.github.io/docs/v5/demo
- Bybit instrument information: https://bybit-exchange.github.io/docs/v5/market/instrument
- Bybit order-book information: https://bybit-exchange.github.io/docs/v5/market/orderbook
- Bybit create-order semantics, including spot leverage field and asynchronous acknowledgement: https://bybit-exchange.github.io/docs/v5/order/create-order
- Bybit private order stream and duplicate/race notes: https://bybit-exchange.github.io/docs/v5/websocket/private/order
- Go concurrency tour: https://go.dev/tour/concurrency
- Go pipelines and cancellation: https://go.dev/blog/pipelines
- Go context: https://go.dev/blog/context
- Go supported release history: https://go.dev/doc/devel/release
- Node.js release/LTS schedule: https://nodejs.org/en/about/previous-releases
- PostgreSQL versioning and supported releases: https://www.postgresql.org/support/versioning/
- PostgreSQL official container image and PostgreSQL 18+ data-volume layout: https://hub.docker.com/_/postgres
- React versions: https://react.dev/versions
- Vite release announcements: https://vite.dev/blog
- Docker Compose specification and profiles: https://docs.docker.com/reference/compose-file/

When official documentation conflicts with this document on a low-level API detail, follow the current official API behavior and record the difference in an ADR. Do not change product scope, safety boundaries, strategy rules, or accounting principles without explicit approval.

---

# 39. Initial Codex command

After this specification is added to the repository, Codex should begin with **Phase A0 and then Phase A1 only**. A0 produces the traceability/safety architecture; after its gate passes, A1 produces:

1. the complete repository skeleton
2. project README
3. coding standards
4. ADR template
5. implementation status tracker
6. local/server-oriented image-based Docker Compose with PostgreSQL, safe profiles, `.env.example`, and deployment documentation
7. minimal Go and React applications with health/status pages
8. CI quality-gate skeleton
9. a phase-by-phase issue/checklist file derived from this specification

Codex must not jump directly into strategy implementation before the safety architecture, deterministic runtime, exact journal/storage, adapter contracts/emulator, recorder, replay/simulation, allocator, and risk foundations exist. V1A begins with public-data research only; authenticated test/demo integrations do not begin until V1C.
