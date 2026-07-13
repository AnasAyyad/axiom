# V1A source-clause coverage

## Purpose

This crosswalk inventories the explicit normative clauses in the attached V1A
implementation plan, the Plan A0 reference set (Spec Sections 1–4, 6, and 27),
and Spec Section 31.2, and maps each V1A-applicable clause to stable IDs in
[the canonical matrix](traceability.md). The matrix owns requirement wording,
owner, target, verification, evidence location, and status. This file owns
source coverage only.

Source-clause IDs are permanent audit locators. They do not replace `AX-V1A-*`
requirement IDs. A clause may map to multiple requirements when implementation
and release verification are intentionally separate. Broad section references
are not accepted as proof of coverage.

## Plan §1 — release outcome and prohibitions

| Source clause | Atomic source obligation | Requirement ID |
|---|---|---|
| P§1/OUT-01 | Consume Binance public Spot data without credentials | AX-V1A-RG-FUN-002 |
| P§1/OUT-02 | Maintain validated local books and completed candles | AX-V1A-RG-FUN-003 |
| P§1/OUT-03 | Record raw and normalized market data | AX-V1A-RG-FUN-004 |
| P§1/OUT-04 | Run deterministic backtests and replay | AX-V1A-RG-FUN-005 |
| P§1/OUT-05 | Run Trend in live shadow | AX-V1A-RG-FUN-006 |
| P§1/OUT-06 | Simulate orders, fills, costs, latency, partial fills, and liquidity | AX-V1A-RG-FUN-007 |
| P§1/OUT-07 | Allocate virtual capital and enforce central risk | AX-V1A-RG-FUN-008 |
| P§1/OUT-08 | Maintain an auditable balanced journal | AX-V1A-RG-FUN-009 |
| P§1/OUT-09 | Recover safely from crashes and unsafe state | AX-V1A-RG-OPS-003 |
| P§1/OUT-10 | Expose versioned API and React administration UI | AX-V1A-RG-FUN-010 |
| P§1/OUT-11 | Provide metrics, dashboards, logs, alerts, backups, and operations docs | AX-V1A-RG-OPS-004 |
| P§1/NO-01 | Accept no authenticated exchange credential | AX-V1A-RG-SAF-006 |
| P§1/NO-02 | Submit, cancel, or query no external exchange order | AX-V1A-RG-SAF-006 |
| P§1/NO-03 | Support no testnet/demo/real or prohibited financial capability | AX-V1A-RG-SAF-007 |
| P§1/NO-04 | Contain no production-order endpoint or hidden switch | AX-V1A-RG-SAF-008 |
| P§1/GATE-01 | Require every named V1A release criterion | AX-V1A-RG-QLT-008 |

## Plan §2 — locked technical decisions

| Source clause | Atomic source obligation | Requirement ID |
|---|---|---|
| P§2/TD-01 | Go 1.26 modular-monolith backend | AX-V1A-A01-FUN-004 |
| P§2/TD-02 | React 19, strict TypeScript, Vite, pnpm | AX-V1A-A11-FUN-043 |
| P§2/TD-03 | PostgreSQL 18, pgx, SQL migrations, sqlc | AX-V1A-A04-OPS-003 |
| P§2/TD-04 | Project-owned decimal wrappers over apd | AX-V1A-A02-FUN-007 |
| P§2/TD-05 | Parquet with Zstd historical storage | AX-V1A-A04-FUN-022 |
| P§2/TD-06 | net/http, OpenAPI-first, generated Go/TypeScript types | AX-V1A-A11-FUN-044 |
| P§2/TD-07 | Resumable SSE; REST snapshots authoritative | AX-V1A-A11-FUN-051 |
| P§2/TD-08 | Docker Compose local infrastructure | AX-V1A-A01-OPS-007 |
| P§2/TD-09 | Exact forms: `/app/platform api`; `/app/platform trader --mode shadow`; `/app/platform recorder`; `/app/platform worker`; `/app/platform admin migrate`; `/app/platform healthcheck` | AX-V1A-A01-FUN-002 |
| P§2/TD-10 | Multi-stage non-root/read-only image with embedded React | AX-V1A-A01-OPS-008 |
| P§2/TD-11 | Vite development proxy to the Go API | AX-V1A-A11-OPS-001 |
| P§2/TD-12 | PostgreSQL coordination; no Redis | AX-V1A-A03-OPS-003 |
| P§2/TD-13 | slog structured JSON with mandatory redaction | AX-V1A-A05-FUN-006 |
| P§2/TD-14 | Prometheus metrics | AX-V1A-A05-FUN-007 |
| P§2/TD-15 | Grafana enabled after A5 | AX-V1A-A05-FUN-008 |
| P§2/TD-16 | Nonblocking OpenTelemetry-compatible tracing | AX-V1A-A05-FUN-009 |
| P§2/TD-17 | TanStack Query server state | AX-V1A-A11-FUN-045 |
| P§2/TD-18 | React Router and TanStack Table | AX-V1A-A11-FUN-046 |
| P§2/TD-19 | Zod client-boundary validation | AX-V1A-A11-FUN-047 |
| P§2/TD-20 | Project-owned wrappers around Radix primitives | AX-V1A-A11-FUN-048 |
| P§2/TD-21 | CSS Modules and shared project tokens | AX-V1A-A11-FUN-049 |
| P§2/TD-22 | ECharts behind project-owned adapters | AX-V1A-A11-FUN-050 |
| P§2/TD-23 | Backend authoritative for finance and risk | AX-V1A-A11-SAF-007 |

## Plan §3 — repository and file rules

| Source clause | Atomic source obligation | Requirement ID |
|---|---|---|
| P§3/ORG-01 | Create only the V1A monorepo/package structure | AX-V1A-A01-FUN-001 |
| P§3/FILE-01 | Production source targets fewer than 300 lines | AX-V1A-A01-QLT-010 |
| P§3/FILE-02 | Files above 400 lines need split or justification | AX-V1A-A01-QLT-010 |
| P§3/FILE-03 | Files above 500 lines are prohibited except named exceptions | AX-V1A-A01-QLT-010 |
| P§3/FILE-04 | Functions normally remain below 50 lines | AX-V1A-A01-QLT-015 |
| P§3/FILE-05 | React components normally remain below 250 lines | AX-V1A-A01-QLT-016 |
| P§3/FILE-06 | Focused packages and doc.go where needed | AX-V1A-A01-QLT-017 |
| P§3/FILE-07 | Meaningful comments for every exported Go identifier | AX-V1A-A01-QLT-018 |
| P§3/FILE-08 | Document formulas, sequencing, rounding, ownership, and recovery invariants | AX-V1A-A01-QLT-013 |
| P§3/FILE-09 | No generic dumping-ground package | AX-V1A-A01-QLT-019 |
| P§3/FILE-10 | Interfaces live near consumers | AX-V1A-A01-QLT-020 |
| P§3/FILE-11 | Generated SQL/API code is isolated and never hand-edited | AX-V1A-A01-QLT-021 |
| P§3/FILE-12 | Tests live beside owners or in dedicated cross-system suites | AX-V1A-A01-QLT-022 |

## Plan §4 — interfaces, flow, and determinism

| Source clause | Atomic source obligation | Requirement ID |
|---|---|---|
| P§4/IF-01 | Clock | AX-V1A-A03-FUN-008 |
| P§4/IF-02 | Scheduler | AX-V1A-A03-FUN-009 |
| P§4/IF-03 | MarketDataSource | AX-V1A-A06-FUN-007 |
| P§4/IF-04 | MarketViewProvider | AX-V1A-A07-FUN-007 |
| P§4/IF-05 | Strategy | AX-V1A-A10-FUN-012 |
| P§4/IF-06 | Allocator | AX-V1A-A09-FUN-007 |
| P§4/IF-07 | RiskEngine | AX-V1A-A09-FUN-008 |
| P§4/IF-08 | ExecutionPlanner | AX-V1A-A08-FUN-010 |
| P§4/IF-09 | Broker | AX-V1A-A08-FUN-011 |
| P§4/IF-10 | OrderReducer | AX-V1A-A08-FUN-012 |
| P§4/IF-11 | Journal | AX-V1A-A04-FUN-019 |
| P§4/IF-12 | Reconciler | AX-V1A-A09-FUN-009 |
| P§4/IF-13 | Recorder | AX-V1A-A07-FUN-008 |
| P§4/IF-14 | RunRepository | AX-V1A-A04-FUN-020 |
| P§4/IF-15 | LeaseRepository | AX-V1A-A03-FUN-010 |
| P§4/IF-16 | AuditWriter | AX-V1A-A04-FUN-021 |
| P§4/IF-17 | Strategies cannot own broker/balance/journal or bypass allocator/risk | AX-V1A-A10-SAF-005 |
| P§4/FLOW-01 | Preserve the documented V1A hot-path sequence | AX-V1A-A03-FUN-011 |
| P§4/FLOW-02 | Keep database/HTTP/report/UI work outside the decision hot path | AX-V1A-A03-SAF-006 |
| P§4/EV-01 | Canonical event envelope contains every named identity/time/hash field | AX-V1A-A03-FUN-001 |
| P§4/FLOW-03 | Critical persistence boundaries use bounded queues and explicit overload policies | AX-V1A-A03-SAF-007 |
| P§4/ORDER-01 | Scheduler tuple is exactly replay/scheduled time, exchange event time, source sequence, ingest ordinal, stable event ID; it does not replace Spec replay order | AX-V1A-A03-FUN-002; AX-V1A-A03-FUN-012 |
| P§4/RNG-01 | Start from a configured root seed and key sub-seeds by stable run/model/order/event identities | AX-V1A-A03-FUN-004 |

## Plan §5 — persistent aggregates and invariants

| Source clause | Atomic source obligation | Requirement ID |
|---|---|---|
| P§5/AG-01 | Users, password hashes, sessions, roles | AX-V1A-A04-FUN-001 |
| P§5/AG-02 | Configuration versions/hashes/activation | AX-V1A-A04-FUN-002 |
| P§5/AG-03 | Assets, instruments, exchange metadata versions | AX-V1A-A04-FUN-003 |
| P§5/AG-04 | Segments, manifests, gaps, validation, quality incidents | AX-V1A-A04-FUN-004 |
| P§5/AG-05 | Strategy versions/parameters/experiments/promotion | AX-V1A-A04-FUN-005 |
| P§5/AG-06 | Portfolios/accounts/balances/positions/ownership | AX-V1A-A04-FUN-006 |
| P§5/AG-07 | Reservation lifecycle | AX-V1A-A04-FUN-007 |
| P§5/AG-08 | Opportunities/decisions/inputs/explanations/risk | AX-V1A-A04-FUN-008 |
| P§5/AG-09 | Plans/orders/events/fills/fees/recovery | AX-V1A-A04-FUN-009 |
| P§5/AG-10 | Journal transactions and entries | AX-V1A-A04-FUN-010 |
| P§5/AG-11 | Backtest/replay/shadow runs/checkpoints/results | AX-V1A-A04-FUN-011 |
| P§5/AG-12 | Fee/latency/spread/slippage models | AX-V1A-A04-FUN-012 |
| P§5/AG-13 | Commands/jobs/leases/fences/inbox/outbox | AX-V1A-A04-FUN-013 |
| P§5/AG-14 | Incidents/alerts/audit/acknowledgements | AX-V1A-A04-FUN-014 |
| P§5/INV-01 | Unique client order IDs | AX-V1A-A04-SAF-007 |
| P§5/INV-02 | Unique inbox/idempotency keys | AX-V1A-A04-SAF-008 |
| P§5/INV-03 | No negative available/reserved balance | AX-V1A-A04-SAF-009 |
| P§5/INV-04 | Reservation cannot exceed owned availability | AX-V1A-A04-SAF-010 |
| P§5/INV-05 | Journal balances per asset/rule | AX-V1A-A04-SAF-011 |
| P§5/INV-06 | Approved order-state transitions only | AX-V1A-A04-SAF-012 |
| P§5/INV-07 | Used configuration/strategy versions immutable | AX-V1A-A04-SAF-013 |
| P§5/INV-08 | Fencing tokens increase only | AX-V1A-A04-SAF-014 |
| P§5/INV-09 | One active lease per resource | AX-V1A-A04-SAF-015 |
| P§5/INV-10 | Persistent timestamps use UTC | AX-V1A-A04-SAF-016 |
| P§5/INV-11 | No binary floating-point financial column | AX-V1A-A02-SAF-001 |

## Specification clauses referenced by Plan A0

The Plan makes Spec Sections 1–4, 6, and 27 direct A0 inputs. The tables below
are the clause-level audit; a section or phase prefix is never treated as
coverage. A cumulative-program clause that belongs after V1A is retained with
an explicit deferred disposition so V1A does not accidentally implement a later
authenticated or multi-exchange capability.

### Spec §1 — product vision

| Source clause | Atomic source obligation | Requirement ID(s) / V1A disposition |
|---|---|---|
| S1/VIS-01 | Collect and normalize live and historical market data | AX-V1A-A06-FUN-003; AX-V1A-A07-FUN-001 |
| S1/VIS-02 | Maintain reliable local order books | AX-V1A-A07-FUN-003; AX-V1A-A07-FUN-004 |
| S1/VIS-03 | Backtest and deterministically replay production strategy logic | AX-V1A-A08-FUN-002; AX-V1A-A08-QLT-001 |
| S1/VIS-04 | Run strategies concurrently on live public data without real orders | AX-V1A-A03-OPS-001; AX-V1A-A10-FUN-012; V1A implements Trend only under S31.2/A10 |
| S1/VIS-05 | Simulate fees, spread/slippage, latency, partial fills, liquidity, and failures | AX-V1A-A08-FUN-004; AX-V1A-A08-FUN-005; AX-V1A-A08-FUN-008 |
| S1/VIS-06 | Validate authenticated integration only in official demo/test environments | Deferred to V1C; AX-V1A-A00-SAF-001 and AX-V1A-A00-SAF-002 make it unavailable in V1A |
| S1/VIS-07 | Allocate virtual capital across owners/exchanges | AX-V1A-A09-FUN-002; AX-V1A-A09-FUN-003 |
| S1/VIS-08 | Enforce centralized risk controls | AX-V1A-A09-SAF-001; AX-V1A-A09-SAF-002 |
| S1/VIS-09 | Reconcile simulated or sandbox orders and balances | AX-V1A-A09-FUN-006; sandbox reconciliation is deferred to V1C |
| S1/VIS-10 | Explain every decision and rejection | AX-V1A-A10-FUN-009 |
| S1/VIS-11 | Provide the professional React research/operations console | AX-V1A-A11-FUN-034; AX-V1A-A11-FUN-035; AX-V1A-A11-FUN-036; AX-V1A-A11-FUN-037; AX-V1A-A11-FUN-038; AX-V1A-A11-FUN-039; AX-V1A-A11-FUN-040; AX-V1A-A11-FUN-041; AX-V1A-A11-FUN-042 |
| S1/VIS-12 | Permit future adapters without strategy/risk changes | AX-V1A-A06-FUN-001 |
| S1/VIS-13 | Treat the product as a research framework, never a profit guarantee | AX-V1A-RG-SAF-005 |

### Spec §2 — non-negotiable boundaries

| Source clause | Atomic source obligation | Requirement ID(s) / V1A disposition |
|---|---|---|
| S2.1/SPOT-OWNED | Model or place only spot sells backed by owned inventory | AX-V1A-A00-SAF-006; AX-V1A-A09-FUN-002; AX-V1A-A09-SAF-001 |
| S2.1/SPOT-PROHIBITED | Forbid futures, perpetuals, options, margin, leverage, borrowing, lending/earn, staking, shorts/unowned sales, leveraged tokens, tokenized derivatives, withdrawals, transfers, and real-money orders | AX-V1A-A00-SAF-004; AX-V1A-RG-SAF-007 |
| S2.1/SPOT-CAPABILITY | Adapters declare product categories and reject every category except spot | AX-V1A-A06-FUN-002; AX-V1A-A06-SAF-001 |
| S2.2/REGISTRY | Treat screening as a configurable eligibility registry—not a religious ruling—with exactly `approved`, `scan_only`, `blocked`, and `pending_review` | AX-V1A-A00-SAF-012; AX-V1A-A02-FUN-008 |
| S2.2/ENFORCEMENT | Permit only `approved` assets in every order-capable or fill-simulating mode | AX-V1A-A00-SAF-013; AX-V1A-A09-SAF-008 |
| S2.2/SCAN-ONLY | Permit `scan_only` observation but never execution or executable inventory | AX-V1A-A00-SAF-013; AX-V1A-A09-SAF-008 |
| S2.2/DEFAULTS | Seed USDT, BTC, and ETH as the default approved assets | AX-V1A-A00-SAF-012; AX-V1A-A02-FUN-008 |
| S2.2/AUDIT | Record actor, time, and reason for every asset-status change | AX-V1A-A00-SAF-014; AX-V1A-A04-FUN-023 |
| S2.3/LOCK-CREDENTIALS | Require no production trading credentials | AX-V1A-A00-SAF-002; AX-V1A-RG-SAF-006 |
| S2.3/LOCK-CONFIG | Exclude production-private endpoints from the configuration schema | AX-V1A-A00-SAF-003; AX-V1A-A02-SAF-003 |
| S2.3/LOCK-IMPLEMENTATION | Keep real-order implementation absent from every V1A build | AX-V1A-A00-SAF-002; AX-V1A-A06-SAF-003; AX-V1A-RG-SAF-008 |
| S2.3/LOCK-BACKEND | Reject `execution_mode=live` at the backend boundary | AX-V1A-A00-SAF-001; AX-V1A-A02-SAF-004 |
| S2.3/LOCK-DATABASE | Make `live` an invalid V1 database value | AX-V1A-A00-SAF-003; AX-V1A-A04-SAF-013 |
| S2.3/LOCK-FRONTEND | Provide no frontend control that enables live trading | AX-V1A-A11-SAF-005; AX-V1A-A11-SAF-006 |
| S2.3/LOCK-LABELS | Label every balance/trade by virtual mode/environment | AX-V1A-A11-SAF-005 |
| S2.3/LOCK-PROHIBITED-KEYS | Fail startup when prohibited withdrawal/margin/production capability keys are detected | AX-V1A-A00-SAF-004; AX-V1A-A02-SAF-004 |
| S2.3/LOCK-CLIENT-SEPARATION | Separate production-public clients from any future authenticated sandbox client by type, constructor, and transport | AX-V1A-A00-SAF-002; AX-V1A-A06-SAF-001 |
| S2.3/LOCK-AUTH-ALLOWLIST | A future credential-bearing client may use only code-owned sandbox hosts/routes; V1A contains no such client | AX-V1A-A00-SAF-002; AX-V1A-A00-SAF-005 |
| S2.3/LOCK-PUBLIC-UNSIGNED | Public clients cannot receive credentials or sign | AX-V1A-A06-SAF-001 |
| S2.3/LOCK-SIGNED-DENIAL | Reject any out-of-policy signed request before I/O; V1A removes the signer entirely | AX-V1A-A00-SAF-002; AX-V1A-A00-SAF-005; AX-V1A-A00-SAF-007 |
| S2.3/LOCK-BYBIT | Force future Bybit demo spot/non-leverage fields; Bybit demo is unavailable in V1A | Deferred to V1C; AX-V1A-A00-SAF-001 and AX-V1A-A00-SAF-004 prohibit the path in V1A |
| S2.3/ASSET-RECHECK | Recheck asset approval at the final submission/execution boundary | AX-V1A-A00-SAF-013; AX-V1A-A09-SAF-008 |
| S2.3/LOCK-NETWORK-PROOF | Capture outbound requests and independently prove signed production-order traffic impossible | AX-V1A-A00-SAF-003; AX-V1A-A00-SAF-007; AX-V1A-RG-SAF-002 |
| S2.3/LOCK-SANDBOX-GATES | Keep sandbox integrations/submission default-off behind separate gates and a short-lived audited arm | Deferred to V1C; V1A rejects both modes through AX-V1A-A00-SAF-001 |
| S2.3/MODES-ALLOWED | Full V1 defines backtest, replay, shadow, paper, testnet, and demo with fixed semantics | AX-V1A-A00-SAF-001 records the V1A subset; testnet/demo are explicitly deferred |
| S2.3/MODE-LIVE | `live` is forbidden in every release | AX-V1A-A00-SAF-001; AX-V1A-RG-SAF-008 |

### Spec §3 — core decisions and topology

| Source clause | Atomic source obligation | Requirement ID(s) / V1A disposition |
|---|---|---|
| S3.1/EXTENSIBILITY | Prove exchange extensibility with a common conformance suite | AX-V1A-A06-FUN-001; AX-V1A-A06-QLT-001 |
| S3.1/V1-ADAPTERS | Full V1 implements Binance Spot and Bybit Spot only | V1A implements Binance public only through AX-V1A-A07-FUN-001; Bybit begins in V1B |
| S3.1/CORE-INDEPENDENCE | Keep domain, strategy, risk, simulation, recorder, and UI free of exchange-native models | AX-V1A-A06-FUN-001 |
| S3.2/BE-GO | Use the exact supported Go 1.26 patch consistently; upgrades require ADR and replay evidence | AX-V1A-A01-OPS-001 |
| S3.2/BE-STDLIB | Prefer the Go standard library and add a router only for demonstrated value | AX-V1A-A01-FUN-004 |
| S3.2/BE-CONCURRENCY | Use bounded goroutines and typed channels | AX-V1A-A03-OPS-001; AX-V1A-A03-SAF-007 |
| S3.2/BE-CONTEXT | Propagate cancellation/deadlines/shutdown with `context.Context` | AX-V1A-A01-OPS-002 |
| S3.2/BE-LIBRARIES | Use slog, pgx/v5, sqlc, and versioned migrations | AX-V1A-A05-FUN-006; AX-V1A-A04-OPS-003 |
| S3.2/BE-DECIMAL | Wrap the reviewed decimal implementation behind project financial types | AX-V1A-A02-FUN-001; AX-V1A-A02-FUN-007 |
| S3.2/BE-PARQUET | Select Parquet only with A4 compatibility/performance evidence | AX-V1A-A04-FUN-017; AX-V1A-A04-FUN-022 |
| S3.2/BE-TELEMETRY | Use Prometheus and optional nonblocking OpenTelemetry-compatible tracing | AX-V1A-A05-FUN-007; AX-V1A-A05-FUN-009 |
| S3.2/RESEARCH-PYTHON | Limit Python to offline research/report work; all authoritative strategy decisions remain Go and Python never enters the shadow hot path | AX-V1A-A10-SAF-004 |
| S3.2/RESEARCH-REPRO | Pin a supported Python and lock research dependencies; keep reusable logic in tested modules | AX-V1A-A01-OPS-001; AX-V1A-A10-SAF-004 |
| S3.2/RESEARCH-TOOLS | Prefer Polars/PyArrow and use statistical libraries/Hypothesis where their ecosystems add value | AX-V1A-A10-FUN-010; AX-V1A-A10-QLT-001 |
| S3.2/FE-TOOLCHAIN | Use pinned React 19.2, strict TypeScript, Vite 8, Node 24 LTS, and pnpm/Corepack | AX-V1A-A01-OPS-001; AX-V1A-A11-FUN-043 |
| S3.2/FE-ARCH | Use feature-based frontend architecture | AX-V1A-A01-FUN-001; AX-V1A-A11-FUN-034 |
| S3.2/FE-DATA | Use TanStack Query/Table, React Router, Zod, and generated OpenAPI types at their defined boundaries | AX-V1A-A11-FUN-045; AX-V1A-A11-FUN-046; AX-V1A-A11-FUN-047; AX-V1A-A11-QLT-001 |
| S3.2/FE-VISUAL | Put ECharts behind project adapters and accessible primitives behind project-owned wrappers | AX-V1A-A11-FUN-048; AX-V1A-A11-FUN-050 |
| S3.2/FE-TEST | Use Vitest, RTL, Playwright, and axe for the named test layers | AX-V1A-A11-QLT-003; AX-V1A-A11-QLT-004; AX-V1A-A11-QLT-005 |
| S3.2/FE-LIVE | Use a browser live-update channel with REST recovery | AX-V1A-A11-FUN-051; AX-V1A-A11-QLT-002 |
| S3.2/STORAGE-POSTGRES | Use PostgreSQL 18 for transactional/audit/research metadata | AX-V1A-A04-OPS-003; AX-V1A-A04-FUN-001; AX-V1A-A04-FUN-014 |
| S3.2/STORAGE-FILES | Use compressed append-only files for high-rate raw market events | AX-V1A-A04-FUN-017; AX-V1A-A04-FUN-022 |
| S3.2/STORAGE-BOUNDARY | Do not put every raw depth event in PostgreSQL; store segment metadata/checksums there | AX-V1A-A04-FUN-004; AX-V1A-A04-FUN-017 |
| S3.2/INFRA-COMPOSE | Use Docker/Compose locally and for the supported single-server profile | AX-V1A-A01-OPS-004; AX-V1A-A01-OPS-007 |
| S3.2/INFRA-OBS | Use Prometheus-compatible metrics, Grafana where useful, JSON logs, and CI | AX-V1A-A05-FUN-001; AX-V1A-A05-FUN-004; AX-V1A-A01-QLT-002 |
| S3.2/INFRA-EDGE | Use a reviewed HTTPS reverse proxy for a server deployment | AX-V1A-A01-OPS-004; implementation is later operational hardening |
| S3.2/INFRA-NO-BROKERS | Do not add Redis/Kafka/NATS/Kubernetes/service mesh without measured need; default to PostgreSQL jobs and in-process bus | AX-V1A-A03-OPS-003 |
| S3.3/MODULAR-MONOLITH | Use a modular monolith for V1 | AX-V1A-A00-OPS-001; AX-V1A-A01-FUN-004 |
| S3.3/HOT-PATH | Keep strategy, allocator, risk, and execution in one process without internal HTTP/JSON/database round trips | AX-V1A-A03-SAF-006 |
| S3.3/PROCESS-EXCEPTIONS | Permit API, recorder, offline workers, and reports as separate processes without fragmenting business logic | AX-V1A-A00-OPS-001 |
| S3.4/PROC-API | API owns browser/auth/admin intake/fan-out, has no exchange credentials, and cannot call an order endpoint | AX-V1A-A00-OPS-001; AX-V1A-A11-SAF-004 |
| S3.4/PROC-SHADOW | Shadow engine owns the public-data hot path and virtual execution with no private credentials | AX-V1A-A00-OPS-001; AX-V1A-A11-SAF-004 |
| S3.4/PROC-RECORDER | Recorder owns public recording and has no private credentials | AX-V1A-A00-OPS-001; AX-V1A-A07-FUN-008 |
| S3.4/PROC-AUTH-ENGINES | Authenticated Binance/Bybit engines are profile-gated single owners | Deferred to V1C; AX-V1A-A00-SAF-001 rejects them in V1A |
| S3.4/PROC-WORKER | Worker is credential-free and reads datasets without mutating source data | AX-V1A-A00-OPS-001; AX-V1A-A11-FUN-032 |
| S3.4/PROC-MIGRATE | Migrator is one-shot and uses a distinct least-privilege role | AX-V1A-A00-OPS-001; AX-V1A-A04-SAF-001 |
| S3.4/FENCING | Enforce one database-backed fenced owner and lock on lease/durability/fence loss | AX-V1A-A00-SAF-009; AX-V1A-A03-SAF-003; AX-V1A-A03-SAF-004 |
| S3.4/COMMANDS | Administrative commands use a durable idempotent inbox, never an unaudited in-memory shortcut | AX-V1A-A03-FUN-007 |
| S3.4/OUTBOX | Commit business events to a durable outbox; notifications are wake hints and API resumes by revision | AX-V1A-A03-FUN-007; AX-V1A-A11-QLT-002 |
| S3.4/RECORDERS | Live engines preserve exact decision inputs independently of the broad recorder and pause if evidence cannot be retained | AX-V1A-A07-FUN-006; AX-V1A-A07-FUN-008; AX-V1A-A03-SAF-007 |
| S3.4/STARTUP | Order startup as fence, locked, safety/config, recovery, reconciliation, market sync, ready, then authorized arm/activation | AX-V1A-A00-OPS-002 |
| S3.4/SHUTDOWN | Stop entries, checkpoint, resolve/quarantine, release reservations, flush audit, then release the fence | AX-V1A-A00-OPS-003 |
| S3.4/HEALTH | Separate readiness/liveness and avoid stale-exchange restart loops | AX-V1A-A00-OPS-004; AX-V1A-A05-OPS-001 |

### Spec §4 — virtual capital, ownership, and valuation

| Source clause | Atomic source obligation | Requirement ID(s) / V1A disposition |
|---|---|---|
| S4/CAPITAL | Default combined capital is exactly 500.00 USDT and USDT is the risk/trading numeraire | AX-V1A-A09-FUN-001 |
| S4/USD-LABEL | Do not display `$500` without a versioned quality-checked same-time USDT/USD mark | AX-V1A-A09-SAF-007 |
| S4/OWNERSHIP | Every asset unit has exactly one portfolio/sub-ledger owner or exclusive reservation; budgets are NAV ceilings, not overlapping claims | AX-V1A-A00-SAF-015; AX-V1A-A09-FUN-002; AX-V1A-A09-FUN-003 |
| S4.1/EXCHANGE-ALLOCATION | Full combined portfolio allocates 250 USDT value per exchange | Deferred to the multi-exchange V1B portfolio; V1A uses P§10/AS-06 through AX-V1A-A09-FUN-001 |
| S4.2/CROSSARB-BENCHMARK | The independent cross-exchange benchmark uses the 60/25/15 inventory allocation and records exact initialization facts | Deferred to V1B; V1A has no cross-exchange strategy or Bybit account |
| S4.2/PNL-SEPARATION | Distinguish stable balance, inventory, reservations, available, inventory P&L, and trading P&L | AX-V1A-A09-FUN-002; AX-V1A-A04-FUN-015; AX-V1A-A04-FUN-016 |
| S4.3/STRATEGY-BUDGETS | Full combined portfolio uses the specified strategy/reserve budgets and ownership matrix | Deferred to V1B; V1A has one isolated Trend portfolio under AX-V1A-A09-FUN-001 |
| S4.3/NO-IMPLICIT-TRANSFER | Reserve all required assets atomically; global reserve and ownership transfers require explicit immutable policy/journal actions | AX-V1A-A09-FUN-003; AX-V1A-A04-FUN-010 |
| S4.3/EXPERIMENT-ISOLATION | Independent and combined experiments use distinct accounts/constraints and are never aggregated | AX-V1A-A08-SAF-002; AX-V1A-A09-FUN-001 |
| S4.4/PORTFOLIOS | Support separate named benchmark portfolios and configurable capital levels | Later portfolio families are deferred to V1B; V1A Trend-only seed is AX-V1A-A09-FUN-001 |
| S4.4/INITIALIZATION | Any nondefault initialization is versioned with ownership, quantities, marks, and rationale | AX-V1A-A09-FUN-001; AX-V1A-A04-FUN-002 |
| S4.5/NUMERAIRE | USDT is ledger numeraire; USD is reporting-only and needs an independent quality-qualified source | AX-V1A-A09-SAF-007 |
| S4.5/LIQUIDATION-MARK | Mark volatile inventory with conservative executable liquidation value; midpoint is secondary only | AX-V1A-A09-FUN-002; AX-V1A-A04-FUN-016 |
| S4.5/COST-BASIS | Use versioned weighted-average cost basis initially and never silently change history | AX-V1A-A04-FUN-016; AX-V1A-A04-SAF-013 |
| S4.5/ATTRIBUTION | Keep realized/unrealized/inventory P&L, fees, slippage, latency, recovery, rebalancing, and USDT/USD effects separate | AX-V1A-A04-FUN-015; AX-V1A-A04-FUN-016 |
| S4.5/DEPEG | Missing/stale USD reference fails USD reporting closed; depeg policy is configurable | AX-V1A-A09-SAF-007; AX-V1A-A09-SAF-002 |
| S4.5/DAILY-LOSS | Measure both UTC-day and rolling-24-hour loss and enforce the stricter value | AX-V1A-A09-SAF-002 |

### Spec §6 — core domain model

| Source clause | Atomic source obligation | Requirement ID(s) / V1A disposition |
|---|---|---|
| S6.1/NO-FLOAT | Never use binary floating point for authoritative financial values or decisions | AX-V1A-A02-SAF-001 |
| S6.1/TYPES | Provide Price, Quantity, Money, Rate, Percent, Fee, and Notional domain types | AX-V1A-A02-FUN-003 |
| S6.1/ARITHMETIC | Provide explicit scale, overflow checking, deterministic/exchange rounding, exact serialization, properties, and benchmarks | AX-V1A-A02-FUN-002; AX-V1A-A02-QLT-001 |
| S6.1/DISPLAY-FLOAT | Convert to floating point only for nonauthoritative display/statistics after exact inputs are fixed | AX-V1A-A02-SAF-001; AX-V1A-A11-SAF-007 |
| S6.1/TRIANGULAR-EXACT | Exhaustively enumerate the approved triangular cycle with exact fixed-point conversions | Deferred to V1B/B4; no triangular implementation exists in V1A |
| S6.2/IDENTIFIERS | Use the complete canonical typed-ID set rather than untyped domain strings | AX-V1A-A02-FUN-004 |
| S6.3/INSTRUMENT | Canonical spot instruments carry exchange/native/canonical identity, status, filters, order types/TIF, fees, and metadata time | AX-V1A-A02-FUN-005 |
| S6.3/METADATA-VERSION | Refresh/version metadata and use period-correct data or record an approximation | AX-V1A-A02-FUN-005; AX-V1A-A04-FUN-003 |
| S6.3/FILTERS | Version side/order/account/environment filter facts and revalidate the exact rounded request at the final broker boundary | AX-V1A-A08-FUN-005; AX-V1A-A09-SAF-008 |
| S6.3/FEES | Treat metadata fees as estimates; version models and reconcile actual fee asset/facts | AX-V1A-A04-FUN-012; AX-V1A-A08-FUN-004; AX-V1A-A08-FUN-005 |
| S6.4/EVENT-TIMES | Record exchange, receive, processing, connection, and sequence time/identity fields | AX-V1A-A03-FUN-001; AX-V1A-A07-FUN-005 |
| S6.4/UTC | Persist wall-clock timestamps in UTC | AX-V1A-A04-SAF-016 |
| S6.4/TIME-HEALTH | Monitor clock drift, event age, cross-exchange uncertainty, and reconnect gaps | AX-V1A-A07-FUN-002; AX-V1A-A07-OPS-001 |
| S6.4/MONOTONIC | Use monotonic time for duration/age/deadline and never let a wall correction reorder ingestion or make age negative | AX-V1A-A02-FUN-005; AX-V1A-A03-FUN-008 |
| S6.5/ENVELOPE | Every decision-relevant event has the complete immutable envelope | AX-V1A-A03-FUN-001 |
| S6.5/INGEST | Assign session-local `ingest_ordinal` before concurrent fan-out as the dataset total-order tie breaker | AX-V1A-A03-FUN-002; AX-V1A-A03-FUN-012 |
| S6.5/REPLAY-ORDER | Replay sorts by recorded logical time then `ingest_ordinal`; exchange sequence validates one book and never defines cross-exchange order | AX-V1A-A03-FUN-012 |
| S6.5/SCHEDULER-DISTINCTION | The Plan's five-field scheduler tuple remains deterministic but cannot override the authoritative replay order | AX-V1A-A03-FUN-002; AX-V1A-A03-FUN-012 |
| S6.5/MARKET-VECTOR | Store the exact multi-market version vector and deterministic as-of/skew policy | AX-V1A-A03-FUN-005 |
| S6.5/RANDOM | Use deterministic keyed streams derived from stable run/strategy/decision/order/leg/model identity | AX-V1A-A03-FUN-004 |

### Spec §27 — observability, alerting, and objectives

| Source clause | Atomic source obligation | Requirement ID(s) / V1A disposition |
|---|---|---|
| S27.1/LOG-FIELDS | Structured logs include timestamp, level, service, correlation, exchange, instrument, strategy, decision/order ID, and event code where applicable | AX-V1A-A05-FUN-001 |
| S27.1/LOG-STRUCTURE | Do not rely on free-form log text alone | AX-V1A-A05-FUN-001 |
| S27.2/METRIC-WS-RATE | WebSocket messages per second | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-DECODE | Decode failures | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-GAPS | Sequence gaps | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-RECONNECT | Reconnects | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-BOOK-AGE | Book age | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-QUEUE | Queue depth and dropped events | AX-V1A-A05-FUN-002; AX-V1A-A03-OPS-002 |
| S27.2/METRIC-STRATEGY | Strategy evaluations and candidates | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-REJECTIONS | Rejection reason counts | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-RISK | Risk-check duration | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-SIMULATION | Execution-simulation duration | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-LATENCY | REST latency and WebSocket lag | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-SANDBOX | Test/demo acknowledgements | Deferred to V1C; V1A has no authenticated order metric |
| S27.2/METRIC-RECONCILIATION | Reconciliation mismatch count | AX-V1A-A05-FUN-002 |
| S27.2/METRIC-PNL | Virtual P&L and drawdown | AX-V1A-A05-FUN-002 |
| S27.3/LEVELS | Alerts have info, warning, and critical levels | AX-V1A-A05-FUN-003 |
| S27.3/CRITICAL | Critical examples cover unresolved state, reconciliation, critical database failure, gaps, risk lock, clock drift, and capability change | AX-V1A-A05-SAF-002; AX-V1A-A09-SAF-005 |
| S27.3/SINK | Alert sink interface supports in-app plus a configurable webhook/email-compatible sink | AX-V1A-A05-FUN-003 |
| S27.4/PROFILE | Record the reference server profile before certification | AX-V1A-A00-QLT-003 |
| S27.4/OBJ-STALE | Zero decisions from books beyond age/skew | AX-V1A-A00-QLT-004; AX-V1A-RG-SAF-003 |
| S27.4/OBJ-GAP | Zero gaps left valid without generation invalidation | AX-V1A-A00-QLT-004; AX-V1A-A07-SAF-002 |
| S27.4/OBJ-DUPLICATE-ORDER | Zero restart/retry duplicate sandbox orders | AX-V1A-A00-QLT-005; measured sandbox proof is deferred to V1C |
| S27.4/OBJ-FILL | Zero lost or double-posted fills in fault tests | AX-V1A-A00-QLT-005; AX-V1A-A08-QLT-003 |
| S27.4/OBJ-JOURNAL | Zero unbalanced committed journal transactions | AX-V1A-A00-QLT-005; AX-V1A-A04-QLT-001 |
| S27.4/OBJ-REPLAY | Zero mismatch across ten identical deterministic runs | AX-V1A-A00-QLT-006; AX-V1A-A08-QLT-001 |
| S27.4/OBJ-BOOK-P99 | Decode/sequence/book-update p99 at declared load is at most 10 ms | AX-V1A-A00-QLT-007; AX-V1A-A07-QLT-005 |
| S27.4/OBJ-DECISION-P99 | Strategy/allocator/risk p99 at declared load is at most 25 ms | AX-V1A-A00-QLT-007; AX-V1A-A10-QLT-006 |
| S27.4/OBJ-RESYNC | Gap-to-healthy p95 is at most 15 seconds while REST is available | AX-V1A-A00-QLT-008; AX-V1A-A07-QLT-006 |
| S27.4/OBJ-ALERT-INAPP | Critical in-app alert creation is at most five seconds | AX-V1A-A00-QLT-009; AX-V1A-A05-QLT-004 |
| S27.4/OBJ-ALERT-EXTERNAL | External delivery p95 is at most 60 seconds while the sink is available | AX-V1A-A00-QLT-009; AX-V1A-A05-QLT-004 |
| S27.4/OBJ-SHUTDOWN | Graceful shutdown is at most 60 seconds | AX-V1A-A00-QLT-010; AX-V1A-A03-QLT-006 |
| S27.4/OBJ-SHADOW-RTO | Shadow restart/recovery readiness is at most five minutes | AX-V1A-A00-QLT-010; AX-V1A-A03-QLT-006 |
| S27.4/OBJ-SANDBOX-RTO | Sandbox restart/reconciliation readiness is at most ten minutes | Deferred to V1C; sandbox engines are unavailable in V1A |
| S27.4/OBJ-CRITICAL-RPO | Critical database RPO is zero after acknowledged commit | AX-V1A-A00-QLT-011; AX-V1A-A04-QLT-006 |
| S27.4/OBJ-RECORDER-RPO | Recorder RPO is within flush interval or an explicit dataset gap | AX-V1A-A00-QLT-011; AX-V1A-A04-QLT-006 |
| S27.4/OBJ-V1A-SOAK | V1A public-data soak is at least 72 continuous hours | AX-V1A-A00-QLT-012; AX-V1A-A07-QLT-002 |
| S27.4/OBJ-V1D-SOAK | V1D readiness soak is at least seven days | Deferred to V1D/D5; it is not a V1A completion claim |
| S27.4/OBJ-MEMORY | Sustained memory remains bounded with no positive leak trend after warm-up | AX-V1A-A00-QLT-012; AX-V1A-A03-QLT-004 |
| S27.4/OBJ-BACKUP | Initial backup cadence/RPO is daily/at most 24 hours | AX-V1A-A00-QLT-013; AX-V1A-A04-QLT-007 |
| S27.4/OBJ-RESTORE | Tested restore RTO is at most four hours | AX-V1A-A00-QLT-013; AX-V1A-A04-QLT-007 |
| S27.4/OBJ-ACCESSIBILITY | Critical workflows meet WCAG 2.2 AA | AX-V1A-A00-QLT-014; AX-V1A-A11-QLT-004 |
| S27.4/METRIC-LABELS | Keep metric labels bounded and exclude IDs, paths, URLs, and arbitrary error text | AX-V1A-A05-FUN-002 |

## Plan §6 and Spec §31.2 — phase deliverables and acceptance

The following prefix table is navigation only. It is not coverage evidence.
Every Spec §31.2 deliverable, acceptance bullet, and V1A gate clause is
enumerated immediately after it; Plan §6 is enumerated in the following two
indexes.

| Source phase | Owning requirement prefix | Coverage location |
|---|---|---|
| P§6/A0 and S31.2/A0 | AX-V1A-A00-* | [A0 matrix](traceability.md#a0--scope-traceability-safety-and-architecture) |
| P§6/A1 and S31.2/A1 | AX-V1A-A01-* | [A1 matrix](traceability.md#a1--repository-toolchain-application-skeleton-compose-and-ci) |
| P§6/A2 and S31.2/A2 | AX-V1A-A02-* | [A2 matrix](traceability.md#a2--financial-domain-and-configuration-safety) |
| P§6/A3 and S31.2/A3 | AX-V1A-A03-* | [A3 matrix](traceability.md#a3--deterministic-runtime-bounded-concurrency-and-fencing) |
| P§6/A4 and S31.2/A4 | AX-V1A-A04-* | [A4 matrix](traceability.md#a4--postgresql-journal-repositories-parquet-and-recovery) |
| P§6/A5 and S31.2/A5 | AX-V1A-A05-* | [A5 matrix](traceability.md#a5--security-observability-monitoring-and-alerts) |
| P§6/A6 and S31.2/A6 | AX-V1A-A06-* | [A6 matrix](traceability.md#a6--exchange-contracts-and-deterministic-emulator) |
| P§6/A7 and S31.2/A7 | AX-V1A-A07-* | [A7 matrix](traceability.md#a7--binance-public-adapter-and-recorder) |
| P§6/A8 and S31.2/A8 | AX-V1A-A08-* | [A8 matrix](traceability.md#a8--backtesting-replay-simulation-and-durable-orders) |
| P§6/A9 and S31.2/A9 | AX-V1A-A09-* | [A9 matrix](traceability.md#a9--portfolio-allocation-risk-reconciliation-and-recovery) |
| P§6/A10 and S31.2/A10 | AX-V1A-A10-* | [A10 matrix](traceability.md#a10--trend-following-strategy-and-research-validation) |
| P§6/A11 and S31.2/A11 | AX-V1A-A11-* | [A11 matrix](traceability.md#a11--versioned-api-authentication-react-ui-and-live-shadow-workflow) |

### Spec §31.2 atomic phase-clause index: A0–A5

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| S31.2/V1A-OUTCOME | V1A is a deployable observable Binance-public research platform with exact accounting, deterministic replay, Trend, shadow, no authenticated credentials, and no external order side effects | AX-V1A-RG-FUN-002; AX-V1A-RG-FUN-005; AX-V1A-RG-FUN-006; AX-V1A-RG-FUN-009; AX-V1A-RG-SAF-006 |
| S31.2/A0-D01 | Requirement-ID scheme and verification matrix | AX-V1A-A00-QLT-001; AX-V1A-A00-QLT-002 |
| S31.2/A0-D02 | Execution-mode matrix and compiled endpoint policy | AX-V1A-A00-SAF-001; AX-V1A-A00-SAF-003; AX-V1A-A00-SAF-005 |
| S31.2/A0-D03 | Initial threat model and trust boundaries | AX-V1A-A00-SAF-008 |
| S31.2/A0-D04 | Real-money-lock test plan | AX-V1A-A00-SAF-007 |
| S31.2/A0-D05 | Process topology, single-writer/fencing, and startup/shutdown state machine | AX-V1A-A00-OPS-001; AX-V1A-A00-SAF-009; AX-V1A-A00-OPS-002; AX-V1A-A00-OPS-003; AX-V1A-A00-OPS-004 |
| S31.2/A0-D06 | Initial SLOs, RPO/RTO, data classification, and risk-policy review | AX-V1A-A00-OPS-005; AX-V1A-A00-QLT-003; AX-V1A-A00-QLT-004; AX-V1A-A00-QLT-005; AX-V1A-A00-QLT-006; AX-V1A-A00-QLT-007; AX-V1A-A00-QLT-008; AX-V1A-A00-QLT-009; AX-V1A-A00-QLT-010; AX-V1A-A00-QLT-011; AX-V1A-A00-QLT-012; AX-V1A-A00-QLT-013; AX-V1A-A00-QLT-014 |
| S31.2/A0-A01 | Every non-negotiable safety rule maps to a test or reviewed evidence artifact | AX-V1A-A00-QLT-001; AX-V1A-A00-QLT-002; AX-V1A-A00-SAF-003; AX-V1A-A00-SAF-004; AX-V1A-A00-SAF-006; AX-V1A-A00-SAF-012; AX-V1A-A00-SAF-013; AX-V1A-A00-SAF-014; AX-V1A-A00-SAF-015 |
| S31.2/A0-A02 | No unresolved question permits production orders, ambiguous ownership, unbalanced accounting, or nondeterministic replay | AX-V1A-A00-SAF-010; AX-V1A-A00-SAF-011 |
| S31.2/A1-D01 | Monorepo governance documents, tracker, and phase checklist | AX-V1A-A01-FUN-001; AX-V1A-A01-QLT-005 |
| S31.2/A1-D02 | Exact Go/Node/pnpm/PostgreSQL/major dependency pins | AX-V1A-A01-OPS-001 |
| S31.2/A1-D03 | Minimal Go/React health applications | AX-V1A-A01-FUN-003 |
| S31.2/A1-D04 | Image-based Compose, safe profiles, health, volumes, networks, and deployment docs | AX-V1A-A01-OPS-003; AX-V1A-A01-OPS-004; AX-V1A-A01-SAF-001 |
| S31.2/A1-D05 | CI skeleton for quality, scans, SBOM, secrets, and prohibited capabilities | AX-V1A-A01-QLT-002; AX-V1A-A01-QLT-003; AX-V1A-A01-QLT-004; AX-V1A-A01-SAF-003 |
| S31.2/A1-A01 | Backend/frontend health applications start | AX-V1A-A01-QLT-006 |
| S31.2/A1-A02 | Compose renders safely with no production-private setting/service | AX-V1A-A01-QLT-008 |
| S31.2/A1-A03 | CI builds skeleton and rejects prohibited modes/endpoints | AX-V1A-A01-QLT-009 |
| S31.2/A2-D01 | Fixed-point types and checked arithmetic | AX-V1A-A02-FUN-001; AX-V1A-A02-FUN-002; AX-V1A-A02-FUN-003 |
| S31.2/A2-D02 | Canonical IDs, assets/instruments/metadata, clocks, errors, and immutable configuration snapshots | AX-V1A-A02-FUN-004; AX-V1A-A02-FUN-005; AX-V1A-A02-FUN-006; AX-V1A-A02-FUN-008 |
| S31.2/A2-D03 | Configuration schema/hashes/environment matrix/prohibited-combination validation | AX-V1A-A02-SAF-003; AX-V1A-A02-SAF-004; AX-V1A-A02-SAF-005 |
| S31.2/A2-A01 | Arithmetic property/fuzz tests pass | AX-V1A-A02-QLT-001 |
| S31.2/A2-A02 | No authoritative financial float use | AX-V1A-A02-SAF-001 |
| S31.2/A2-A03 | Invalid product/mode/URL/placeholder/percentage/unit fails closed | AX-V1A-A02-QLT-002 |
| S31.2/A3-D01 | Canonical event envelope and ingestion ordinal | AX-V1A-A03-FUN-001; AX-V1A-A03-FUN-002; AX-V1A-A03-FUN-012 |
| S31.2/A3-D02 | Deterministic scheduler, keyed randomness, versioned market views, and clock abstraction | AX-V1A-A03-FUN-003; AX-V1A-A03-FUN-004; AX-V1A-A03-FUN-005 |
| S31.2/A3-D03 | Bounded queues with event-class loss policies | AX-V1A-A03-OPS-001; AX-V1A-A03-SAF-001; AX-V1A-A03-SAF-002; AX-V1A-A03-SAF-007 |
| S31.2/A3-D04 | Execution lease/fencing and graceful lifecycle | AX-V1A-A03-SAF-003; AX-V1A-A03-SAF-004; AX-V1A-A03-SAF-005 |
| S31.2/A3-D05 | In-process bus and durable command/inbox contracts | AX-V1A-A03-FUN-007; AX-V1A-A03-OPS-003 |
| S31.2/A3-A01 | Scheduling cannot change replay results | AX-V1A-A03-QLT-001 |
| S31.2/A3-A02 | Overlapping engines cannot share execution ownership | AX-V1A-A03-QLT-002 |
| S31.2/A3-A03 | Overload pauses/invalidates safely without stale execution | AX-V1A-A03-QLT-003 |
| S31.2/A4-D01 | Roles, migrations, repositories, journal, reservations, inbox/outbox, and audit foundation | AX-V1A-A04-OPS-001; AX-V1A-A04-SAF-001; AX-V1A-A04-FUN-007; AX-V1A-A04-FUN-010; AX-V1A-A04-FUN-013; AX-V1A-A04-FUN-014; AX-V1A-A04-FUN-023 |
| S31.2/A4-D02 | Exact multi-commodity balance, cost basis, suspense, and rebuildable projections | AX-V1A-A04-FUN-015; AX-V1A-A04-FUN-016; AX-V1A-A04-SAF-011; AX-V1A-A04-SAF-017 |
| S31.2/A4-D03 | Raw/normalized Parquet, crash-safe finalization, manifests, retention, backup, and restore | AX-V1A-A04-FUN-017; AX-V1A-A04-FUN-018; AX-V1A-A04-SAF-006; AX-V1A-A04-OPS-002 |
| S31.2/A4-A01 | Every journal transaction balances per asset | AX-V1A-A04-QLT-001 |
| S31.2/A4-A02 | Concurrency rejects double spending and negative available balances | AX-V1A-A04-QLT-002; AX-V1A-A04-SAF-009; AX-V1A-A04-SAF-010 |
| S31.2/A4-A03 | Kill points recover segments/outbox/journal/reservations without silent loss | AX-V1A-A04-QLT-003; AX-V1A-A04-QLT-006 |
| S31.2/A4-A04 | Clean backup restores into a verifiable instance | AX-V1A-A04-QLT-004; AX-V1A-A04-QLT-007 |
| S31.2/A5-D01 | Redacted structured logs, bounded metrics, health, audit, and core alerts | AX-V1A-A05-FUN-001; AX-V1A-A05-SAF-001; AX-V1A-A05-FUN-002; AX-V1A-A05-OPS-001; AX-V1A-A05-FUN-003 |
| S31.2/A5-D02 | Non-root/read-only baseline, least privilege, scans, and secret-file handling | AX-V1A-A05-SAF-003; AX-V1A-A05-QLT-001 |
| S31.2/A5-D03 | Initial Prometheus/Grafana provisioning and dashboards | AX-V1A-A05-FUN-004 |
| S31.2/A5-A01 | Secrets/signatures never appear in outputs | AX-V1A-A05-QLT-002 |
| S31.2/A5-A02 | Persistence/fencing/disk/clock/queue/book failures alert and fail closed | AX-V1A-A05-SAF-002 |

### Spec §31.2 atomic phase-clause index: A6–A11 and release gate

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| S31.2/A6-D01 | Narrow public data, metadata, history, account, broker, and reconciliation interfaces | AX-V1A-A06-FUN-001; AX-V1A-A06-FUN-003 |
| S31.2/A6-D02 | Environment-aware capabilities, errors, retry, and shared rate budgets | AX-V1A-A06-FUN-002; AX-V1A-A06-FUN-004; AX-V1A-A06-SAF-002; AX-V1A-A06-OPS-001 |
| S31.2/A6-D03 | Deterministic REST/WebSocket emulator and golden fixtures | AX-V1A-A06-FUN-005; AX-V1A-A06-FUN-006 |
| S31.2/A6-A01 | Simulator/emulator proves contracts | AX-V1A-A06-QLT-001 |
| S31.2/A6-A02 | Emulator deterministically exercises all named exchange faults | AX-V1A-A06-QLT-001 |
| S31.2/A6-A03 | Unsupported functions fail with typed errors | AX-V1A-A06-QLT-002 |
| S31.2/A7-D01 | Binance public metadata/time/trades/candles/books/recorder integration | AX-V1A-A07-FUN-001; AX-V1A-A07-FUN-002; AX-V1A-A07-FUN-003; AX-V1A-A07-FUN-004; AX-V1A-A07-FUN-006 |
| S31.2/A7-D02 | Reconnect/renewal/gap handling and raw/canonical archival | AX-V1A-A07-OPS-001; AX-V1A-A07-SAF-002; AX-V1A-A07-FUN-005 |
| S31.2/A7-A01 | Approved books meet 72-hour soak and resync objectives | AX-V1A-A07-QLT-002; AX-V1A-A07-QLT-005; AX-V1A-A07-QLT-006 |
| S31.2/A7-A02 | Recorded events/lifecycle/snapshots/gaps form verifiable manifests | AX-V1A-A07-QLT-003 |
| S31.2/A7-A03 | Adapter has no credentials or order capability | AX-V1A-A07-SAF-004 |
| S31.2/A8-D01 | Backtest/replay clocks, simulators, models, shared liquidity, and virtual journal | AX-V1A-A08-FUN-001; AX-V1A-A08-FUN-002; AX-V1A-A08-FUN-004; AX-V1A-A08-FUN-005; AX-V1A-A08-SAF-002; AX-V1A-A08-FUN-009 |
| S31.2/A8-D02 | Canonical order reducer, saga, checkpoints, faults, and restart restore | AX-V1A-A08-FUN-006; AX-V1A-A08-FUN-007; AX-V1A-A08-FUN-008 |
| S31.2/A8-A01 | Ten identical runs match events/balances/decisions/checksum | AX-V1A-A08-QLT-001 |
| S31.2/A8-A02 | Partial/failed fills, races, fees, dust, recovery balance | AX-V1A-A08-QLT-002 |
| S31.2/A8-A03 | Confidence tiers and noncomparable model namespaces enforced | AX-V1A-A08-SAF-003 |
| S31.2/A9-D01 | Strategy/exchange/asset ownership initialization | AX-V1A-A09-FUN-001; AX-V1A-A09-FUN-002 |
| S31.2/A9-D02 | Reservations, allocator, risk hierarchy, intent matrix, breakers, inventory, recovery | AX-V1A-A09-FUN-003; AX-V1A-A09-FUN-004; AX-V1A-A09-FUN-005; AX-V1A-A09-SAF-002; AX-V1A-A09-SAF-003; AX-V1A-A09-SAF-004; AX-V1A-A09-SAF-005; AX-V1A-A09-SAF-008 |
| S31.2/A9-D03 | Startup-locked recovery and virtual reconciliation | AX-V1A-A09-FUN-006; AX-V1A-A09-SAF-006 |
| S31.2/A9-A01 | Concurrent candidates cannot double-own capital/liquidity | AX-V1A-A09-QLT-002 |
| S31.2/A9-A02 | Initial caps, hysteresis, pause/lock, recovery pass model tests | AX-V1A-A09-QLT-003 |
| S31.2/A9-A03 | Unresolved critical state quarantines and blocks entries | AX-V1A-A09-SAF-006 |
| S31.2/A10-D01 | Exact Trend indicators/rules/sizing/simulation/exits/benchmarks/docs/validation plan | AX-V1A-A10-FUN-001; AX-V1A-A10-FUN-002; AX-V1A-A10-FUN-003; AX-V1A-A10-FUN-004; AX-V1A-A10-FUN-005; AX-V1A-A10-FUN-006; AX-V1A-A10-FUN-007; AX-V1A-A10-FUN-010; AX-V1A-A10-FUN-011 |
| S31.2/A10-D02 | Backtest/replay/shadow evidence with intervals and stress tests | AX-V1A-A10-QLT-003; AX-V1A-A10-QLT-004; AX-V1A-A10-QLT-006 |
| S31.2/A10-A01 | No look-ahead or signal-close fills | AX-V1A-A10-QLT-002 |
| S31.2/A10-A02 | Decisions deterministic across supported modes | AX-V1A-A10-QLT-003 |
| S31.2/A10-A03 | Untouched-test/stability evidence makes no production-profitability claim | AX-V1A-A10-QLT-004; AX-V1A-A10-SAF-003 |
| S31.2/A11-D01 | Versioned OpenAPI/auth/jobs/live resume/audit | AX-V1A-A11-QLT-001; AX-V1A-A11-SAF-001; AX-V1A-A11-SAF-002; AX-V1A-A11-FUN-032; AX-V1A-A11-FUN-030 |
| S31.2/A11-D02 | Minimal Command Center, Binance, portfolio/journal, risk, Trend, labs, incident, and shadow workflows | AX-V1A-A11-FUN-034; AX-V1A-A11-FUN-035; AX-V1A-A11-FUN-036; AX-V1A-A11-FUN-037; AX-V1A-A11-FUN-038; AX-V1A-A11-FUN-039; AX-V1A-A11-FUN-040; AX-V1A-A11-FUN-041; AX-V1A-A11-FUN-042 |
| S31.2/A11-D03 | Live public-data shadow engine has no private credentials | AX-V1A-A11-SAF-004 |
| S31.2/A11-A01 | Complete configured experiment-to-incident-replay Trend workflow | AX-V1A-A11-QLT-005 |
| S31.2/A11-A02 | UI recovers stream state and visibly labels all values virtual | AX-V1A-A11-QLT-002; AX-V1A-A11-SAF-005 |
| S31.2/A11-A03 | Accessible stale/error/paused/locked/empty states | AX-V1A-A11-QLT-003; AX-V1A-A11-QLT-004 |
| S31.2/GATE-01 | Production broker/withdrawal/margin/leverage/private credential paths absent | AX-V1A-RG-SAF-001; AX-V1A-RG-SAF-006; AX-V1A-RG-SAF-007; AX-V1A-RG-SAF-008 |
| S31.2/GATE-02 | Binance books, recorder, replay meet numeric SLOs | AX-V1A-RG-QLT-002 |
| S31.2/GATE-03 | Journal/reservation/fencing/restart/replay invariants pass | AX-V1A-RG-QLT-003; AX-V1A-RG-QLT-004 |
| S31.2/GATE-04 | Trend runs end to end in backtest, replay, shadow | AX-V1A-RG-FUN-001 |
| S31.2/GATE-05 | Core alerts and clean backup/restore drill pass | AX-V1A-RG-OPS-001; AX-V1A-RG-OPS-002 |

### Plan §6 atomic phase-clause index: A0–A5

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| P§6/A0-D01 | Assign stable IDs to every V1A requirement | AX-V1A-A00-QLT-001 |
| P§6/A0-D02 | Maintain code/test/metric/evidence traceability | AX-V1A-A00-QLT-002 |
| P§6/A0-D03 | Define all seven mode dispositions | AX-V1A-A00-SAF-001 |
| P§6/A0-D04 | Define public-only endpoint policy | AX-V1A-A00-SAF-005 |
| P§6/A0-D05 | Threat model every named threat and boundary | AX-V1A-A00-SAF-008 |
| P§6/A0-D06 | Define topology, lifecycle, shutdown, ownership, recovery, readiness | AX-V1A-A00-OPS-001; AX-V1A-A00-SAF-009; AX-V1A-A00-OPS-002; AX-V1A-A00-OPS-003; AX-V1A-A00-OPS-004 |
| P§6/A0-D07 | Define classifications, RPO/RTO, retention, backup, deletion | AX-V1A-A00-OPS-005; AX-V1A-A00-OPS-006; AX-V1A-A00-QLT-003; AX-V1A-A00-QLT-013 |
| P§6/A0-D08 | Record six named ADRs | AX-V1A-A00-QLT-015 |
| P§6/A0-D09 | Maintain implementation status and readiness | AX-V1A-A00-QLT-016 |
| P§6/A0-A01 | Every V1A requirement has owner, test, evidence location | AX-V1A-A00-QLT-002 |
| P§6/A0-A02 | No unresolved safety/ownership/accounting/replay/failure ambiguity | AX-V1A-A00-SAF-010 |
| P§6/A0-A03 | Safety review confirms no order-endpoint path | AX-V1A-A00-SAF-011 |
| P§6/A1-D01 | Create Go/pnpm/React/OpenAPI/Makefile/package skeleton | AX-V1A-A01-FUN-001 |
| P§6/A1-D02 | Implement one platform binary and subcommands | AX-V1A-A01-FUN-002 |
| P§6/A1-D03 | Add live/ready/version/build endpoints | AX-V1A-A01-FUN-003 |
| P§6/A1-D04 | Add shared 60-second lifecycle manager | AX-V1A-A01-OPS-002 |
| P§6/A1-D05 | Create pinned reproducible multi-stage hardened image | AX-V1A-A01-OPS-003 |
| P§6/A1-D06 | Align Axiom Compose naming/services/networks/volumes | AX-V1A-A01-OPS-004 |
| P§6/A1-D07 | Keep testnet/demo unavailable and reject their modes | AX-V1A-A01-SAF-002 |
| P§6/A1-D08 | Add stable Make targets | AX-V1A-A01-QLT-001 |
| P§6/A1-D09 | Add complete CI governance/scanning skeleton | AX-V1A-A01-QLT-002; AX-V1A-A01-QLT-003; AX-V1A-A01-QLT-004; AX-V1A-A01-SAF-003 |
| P§6/A1-D10 | Add repository-local preflight | AX-V1A-A01-QLT-001 |
| P§6/A1-D11 | Document setup, secrets, migration, ownership, troubleshooting | AX-V1A-A01-OPS-005 |
| P§6/A1-A01 | API/worker/recorder/shadow processes build/start | AX-V1A-A01-QLT-006 |
| P§6/A1-A02 | Readiness reflects dependencies accurately | AX-V1A-A01-QLT-007 |
| P§6/A1-A03 | Compose cannot start authenticated/real trading | AX-V1A-A01-QLT-008 |
| P§6/A1-A04 | CI rejects every named unsafe/stale artifact | AX-V1A-A01-QLT-009 |
| P§6/A2-D01 | Implement apd wrappers and explicit contexts | AX-V1A-A02-FUN-001 |
| P§6/A2-D02 | Implement checked decimal operations/errors | AX-V1A-A02-FUN-002 |
| P§6/A2-D03 | Implement every distinct financial type | AX-V1A-A02-FUN-003 |
| P§6/A2-D04 | Implement explicit exchange rounding rules | AX-V1A-A02-SAF-002 |
| P§6/A2-D05 | Implement stable typed aggregate IDs | AX-V1A-A02-FUN-004 |
| P§6/A2-D06 | Implement versioned schemas and immutable snapshots | AX-V1A-A02-FUN-006 |
| P§6/A2-D07 | Calculate canonical configuration hashes | AX-V1A-A02-FUN-006; AX-V1A-A02-QLT-003 |
| P§6/A2-D08 | Validate units/percent/symbol/URL/mode/secret/model/capability | AX-V1A-A02-SAF-003 |
| P§6/A2-D09 | Refuse absolute safety flags | AX-V1A-A02-SAF-004 |
| P§6/A2-D10 | Refuse unknown/production hosts | AX-V1A-A02-SAF-004 |
| P§6/A2-D11 | Keep financial environment values decimal strings | AX-V1A-A02-SAF-005 |
| P§6/A2-A01 | Arithmetic/rounding unit/property/fuzz pass | AX-V1A-A02-QLT-001 |
| P§6/A2-A02 | No authoritative float use | AX-V1A-A02-SAF-001 |
| P§6/A2-A03 | Invalid safety configuration fails closed | AX-V1A-A02-QLT-002 |
| P§6/A2-A04 | Snapshots reproduce prior run config | AX-V1A-A02-QLT-003 |
| P§6/A3-D01 | Canonical envelope and ingest ordinal | AX-V1A-A03-FUN-001; AX-V1A-A03-FUN-002 |
| P§6/A3-D02 | Real and deterministic clocks | AX-V1A-A03-FUN-003; AX-V1A-A03-FUN-008 |
| P§6/A3-D03 | Deterministic scheduler/randomness/versioned views | AX-V1A-A03-FUN-004; AX-V1A-A03-FUN-005; AX-V1A-A03-FUN-009 |
| P§6/A3-D04 | Bounded partitioned event bus | AX-V1A-A03-OPS-001 |
| P§6/A3-D05 | Preserve every named partition ordering | AX-V1A-A03-OPS-001; AX-V1A-A03-FUN-002 |
| P§6/A3-D06 | Critical facts cannot drop | AX-V1A-A03-SAF-001 |
| P§6/A3-D07 | Market overload invalidates and pauses | AX-V1A-A03-SAF-002 |
| P§6/A3-D08 | UI/metric state may coalesce safely | AX-V1A-A03-FUN-006 |
| P§6/A3-D09 | Lease/renew/fence/loss/shutdown | AX-V1A-A03-SAF-003; AX-V1A-A03-SAF-004; AX-V1A-A03-FUN-010 |
| P§6/A3-D10 | Durable command/inbox/outbox | AX-V1A-A03-FUN-007; AX-V1A-A03-OPS-003 |
| P§6/A3-D11 | Startup paused; never auto-enable | AX-V1A-A03-SAF-005 |
| P§6/A3-D12 | Runtime overload/queue/lag/shutdown metrics | AX-V1A-A03-OPS-002 |
| P§6/A3-A01 | Scheduling cannot change results | AX-V1A-A03-QLT-001 |
| P§6/A3-A02 | Two engines cannot share ownership | AX-V1A-A03-QLT-002 |
| P§6/A3-A03 | Lease loss stops plan acceptance | AX-V1A-A03-SAF-004 |
| P§6/A3-A04 | Saturation loses no critical facts or stale execution | AX-V1A-A03-QLT-003 |
| P§6/A3-A05 | Race and stress tests pass | AX-V1A-A03-QLT-004 |
| P§6/A4-D01 | Append-only migrations and verification | AX-V1A-A04-OPS-001 |
| P§6/A4-D02 | Least-privilege database roles | AX-V1A-A04-SAF-001 |
| P§6/A4-D03 | Generate typed sqlc queries | AX-V1A-A04-OPS-001 |
| P§6/A4-D04 | Repositories and explicit locking/transactions | AX-V1A-A04-OPS-001 |
| P§6/A4-D05 | Balanced multi-commodity accounting and named accounts | AX-V1A-A04-FUN-015; AX-V1A-A04-FUN-019 |
| P§6/A4-D06 | Journal-backed rebuildable projections | AX-V1A-A04-FUN-016 |
| P§6/A4-D07 | Complete reservation lifecycle/recovery | AX-V1A-A04-FUN-007 |
| P§6/A4-D08 | Inbox/outbox idempotency and durable jobs | AX-V1A-A04-FUN-013 |
| P§6/A4-D09 | Crash-safe raw/normalized Parquet protocol | AX-V1A-A04-FUN-017; AX-V1A-A04-SAF-006 |
| P§6/A4-D10 | Manifest compatibility and deterministic readers | AX-V1A-A04-FUN-018 |
| P§6/A4-D11 | Backup/restore tooling and runbooks | AX-V1A-A04-OPS-002 |
| P§6/A4-A01 | Journal properties balance | AX-V1A-A04-QLT-001 |
| P§6/A4-A02 | Concurrent reservations cannot double-spend | AX-V1A-A04-QLT-002 |
| P§6/A4-A03 | Kill-point recovery loses nothing silently | AX-V1A-A04-QLT-003 |
| P§6/A4-A04 | Restore reproduces balances/replay hashes | AX-V1A-A04-QLT-004; AX-V1A-A04-QLT-007 |
| P§6/A4-A05 | Ledger mismatch incidents pause; no silent repair | AX-V1A-A04-SAF-011; AX-V1A-A04-SAF-017 |
| P§6/A5-D01 | Redacted structured JSON logging | AX-V1A-A05-FUN-001; AX-V1A-A05-SAF-001 |
| P§6/A5-D02 | Relevant correlation/domain IDs in logs | AX-V1A-A05-FUN-001 |
| P§6/A5-D03 | Bounded-label Prometheus metrics | AX-V1A-A05-FUN-002 |
| P§6/A5-D04 | Instrument every named health/business metric | AX-V1A-A05-FUN-002 |
| P§6/A5-D05 | Separate liveness/readiness/authenticated health | AX-V1A-A05-OPS-001 |
| P§6/A5-D06 | Core alerts and Grafana dashboards | AX-V1A-A05-FUN-003; AX-V1A-A05-FUN-004 |
| P§6/A5-D07 | Secret redaction tests at every output | AX-V1A-A05-SAF-001; AX-V1A-A05-QLT-002 |
| P§6/A5-D08 | Harden containers and Compose permissions | AX-V1A-A05-SAF-003 |
| P§6/A5-D09 | Vulnerability/license/SBOM/image scans | AX-V1A-A05-QLT-001 |
| P§6/A5-D10 | Alert deduplication and acknowledgement | AX-V1A-A05-FUN-003 |
| P§6/A5-A01 | Secret canaries absent | AX-V1A-A05-QLT-002 |
| P§6/A5-A02 | Named failures alert and fail closed | AX-V1A-A05-SAF-002 |
| P§6/A5-A03 | Monitoring profiles enabled after A5 | AX-V1A-A05-FUN-004 |
| P§6/A5-A04 | Panels/rules use documented bounded metrics | AX-V1A-A05-QLT-003 |

### Plan §6 atomic phase-clause index: A6–A11

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| P§6/A6-D01 | Capability-based public exchange contracts | AX-V1A-A06-FUN-001 |
| P§6/A6-D02 | Represent every named capability/constraint | AX-V1A-A06-FUN-002 |
| P§6/A6-D03 | Binance authenticated capabilities unsupported | AX-V1A-A06-SAF-001 |
| P§6/A6-D04 | Typed errors/retry/backoff/rate/cancellation | AX-V1A-A06-FUN-004; AX-V1A-A06-SAF-002; AX-V1A-A06-OPS-001 |
| P§6/A6-D05 | Deterministic REST/WebSocket emulator faults | AX-V1A-A06-FUN-005 |
| P§6/A6-D06 | Sanitized Binance golden fixtures | AX-V1A-A06-FUN-006 |
| P§6/A6-A01 | Contract tests pass against emulator | AX-V1A-A06-QLT-001 |
| P§6/A6-A02 | Faults are deterministic | AX-V1A-A06-QLT-001 |
| P§6/A6-A03 | Unsupported auth operations return typed errors | AX-V1A-A06-QLT-002 |
| P§6/A6-A04 | No auth endpoint/signer in V1A binary | AX-V1A-A06-SAF-003 |
| P§6/A7-D01 | Binance public metadata/candles/trades/depth | AX-V1A-A07-FUN-001 |
| P§6/A7-D02 | Validate public REST/WSS allowlist | AX-V1A-A07-SAF-001 |
| P§6/A7-D03 | Server-time and clock-drift health | AX-V1A-A07-FUN-002 |
| P§6/A7-D04 | Nine-step snapshot/delta synchronization | AX-V1A-A07-FUN-003; AX-V1A-A07-SAF-002 |
| P§6/A7-D05 | Preserve all four event/publication times | AX-V1A-A07-FUN-005 |
| P§6/A7-D06 | Record raw before normalized after | AX-V1A-A07-FUN-006; AX-V1A-A07-FUN-008 |
| P§6/A7-D07 | Reconnect/backoff/resubscribe/stale/resync | AX-V1A-A07-OPS-001 |
| P§6/A7-D08 | V1A universe BTC/USDT and ETH/USDT only | AX-V1A-A07-SAF-003 |
| P§6/A7-A01 | Adapter fault/integration coverage passes | AX-V1A-A07-QLT-001 |
| P§6/A7-A02 | 72-hour soak and bounded memory pass | AX-V1A-A07-QLT-002 |
| P§6/A7-A03 | Manifests validate and replay | AX-V1A-A07-QLT-003 |
| P§6/A7-A04 | No credentials or order capability | AX-V1A-A07-SAF-004 |
| P§6/A8-D01 | Deterministic readers and run manifests | AX-V1A-A08-FUN-001 |
| P§6/A8-D02 | Shared production packages across modes | AX-V1A-A08-FUN-002 |
| P§6/A8-D03 | Replay controls and incident reproduction | AX-V1A-A08-FUN-003 |
| P§6/A8-D04 | Versioned execution-cost/fill models | AX-V1A-A08-FUN-004 |
| P§6/A8-D05 | Simulated arrival uses future eligible state | AX-V1A-A08-SAF-001 |
| P§6/A8-D06 | Depth/partial/expiry/filter/rounding/dust model | AX-V1A-A08-FUN-005 |
| P§6/A8-D07 | Shared-liquidity consumption | AX-V1A-A08-SAF-002 |
| P§6/A8-D08 | Durable full order state machine | AX-V1A-A08-FUN-006; AX-V1A-A08-FUN-012 |
| P§6/A8-D09 | Sagas/checkpoints/restart/unknown recovery | AX-V1A-A08-FUN-007; AX-V1A-A08-FUN-009 |
| P§6/A8-D10 | Confidence/model namespaces cannot mix | AX-V1A-A08-SAF-003 |
| P§6/A8-A01 | Ten byte-identical run hashes | AX-V1A-A08-QLT-001 |
| P§6/A8-A02 | No future/impossible fill semantics | AX-V1A-A08-SAF-001 |
| P§6/A8-A03 | Fees/fills/dust/journal/recovery balance | AX-V1A-A08-QLT-002 |
| P§6/A8-A04 | Crash resumes without duplicate/lost state | AX-V1A-A08-QLT-003 |
| P§6/A8-A05 | Candle-only arbitrage evidence is insufficient | AX-V1A-A08-SAF-003 |
| P§6/A9-D01 | Seed Trend portfolio with 500 USDT and zero BTC/ETH | AX-V1A-A09-FUN-001 |
| P§6/A9-D02 | Ownership/reservations/balances/positions/exposure | AX-V1A-A09-FUN-002 |
| P§6/A9-D03 | Central allocator conflict/ownership resolution | AX-V1A-A09-FUN-003; AX-V1A-A09-FUN-007 |
| P§6/A9-D04 | Six-level hierarchical risk config | AX-V1A-A09-FUN-004 |
| P§6/A9-D05 | Seven named risk result actions | AX-V1A-A09-FUN-005; AX-V1A-A09-FUN-008 |
| P§6/A9-D06 | Enforce all V1A default limits | AX-V1A-A09-SAF-002 |
| P§6/A9-D07 | Hysteresis and manual paused/locked recovery | AX-V1A-A09-SAF-003; AX-V1A-A09-SAF-004 |
| P§6/A9-D08 | Reconcile every virtual projection/fact | AX-V1A-A09-FUN-006; AX-V1A-A09-FUN-009 |
| P§6/A9-D09 | Quarantine inconsistent state and incident | AX-V1A-A09-SAF-006 |
| P§6/A9-D10 | Startup paused/locked; separate auth activation | AX-V1A-A09-SAF-003 |
| P§6/A9-A01 | Every risk boundary value tested | AX-V1A-A09-QLT-001 |
| P§6/A9-A02 | Allocator/risk cannot be bypassed | AX-V1A-A09-SAF-001 |
| P§6/A9-A03 | No double-owned cash/inventory/liquidity | AX-V1A-A09-QLT-002 |
| P§6/A9-A04 | Reconciliation mismatch pauses scope | AX-V1A-A09-SAF-006 |
| P§6/A9-A05 | Restart reproduces portfolio/journal state | AX-V1A-A09-QLT-004 |
| P§6/A10-D01 | Versioned Trend configuration/validation | AX-V1A-A10-FUN-001 |
| P§6/A10-D02 | Completed 4-hour candles only | AX-V1A-A10-SAF-001 |
| P§6/A10-D03 | Exact EMA/breakout/ATR/stop/cooldown/position rules | AX-V1A-A10-FUN-002; AX-V1A-A10-FUN-003; AX-V1A-A10-FUN-004; AX-V1A-A10-FUN-005; AX-V1A-A10-SAF-002 |
| P§6/A10-D04 | Risk 0.5% equity before central caps | AX-V1A-A10-FUN-006 |
| P§6/A10-D05 | Marketable-limit validity/slippage simulation | AX-V1A-A10-FUN-007 |
| P§6/A10-D06 | Explicit warmup/missing/tie/rounding/gap/stop timing | AX-V1A-A10-FUN-008 |
| P§6/A10-D07 | Stable reasons and explanations | AX-V1A-A10-FUN-009 |
| P§6/A10-D08 | Record exact input/config/state/model versions | AX-V1A-A10-FUN-009 |
| P§6/A10-D09 | Benchmark/holding/failure/cost/stability reports | AX-V1A-A10-FUN-011 |
| P§6/A10-D10 | Python analysis-only; Go authoritative | AX-V1A-A10-SAF-004 |
| P§6/A10-A01 | Indicators match independent goldens | AX-V1A-A10-QLT-001 |
| P§6/A10-A02 | No look-ahead/incomplete candle | AX-V1A-A10-QLT-002 |
| P§6/A10-A03 | Equivalent cross-mode decisions | AX-V1A-A10-QLT-003 |
| P§6/A10-A04 | Untouched test separated | AX-V1A-A10-QLT-004 |
| P§6/A10-A05 | Reports show uncertainty, no profit claim | AX-V1A-A10-SAF-003 |
| P§6/A11-EP-01 | `GET /health/live` | AX-V1A-A11-FUN-001 |
| P§6/A11-EP-02 | `GET /health/ready` | AX-V1A-A11-FUN-002 |
| P§6/A11-EP-03 | `POST /api/v1/session/login` | AX-V1A-A11-FUN-003 |
| P§6/A11-EP-04 | `POST /api/v1/session/logout` | AX-V1A-A11-FUN-004 |
| P§6/A11-EP-05 | `GET /api/v1/session/me` | AX-V1A-A11-FUN-005 |
| P§6/A11-EP-06 | `GET /api/v1/system/status` | AX-V1A-A11-FUN-006 |
| P§6/A11-EP-07 | `GET /api/v1/exchanges/binance/health` | AX-V1A-A11-FUN-007 |
| P§6/A11-EP-08 | `GET /api/v1/exchanges/binance/instruments` | AX-V1A-A11-FUN-008 |
| P§6/A11-EP-09 | `GET /api/v1/portfolios` | AX-V1A-A11-FUN-009 |
| P§6/A11-EP-10 | `GET /api/v1/portfolios/{id}` | AX-V1A-A11-FUN-010 |
| P§6/A11-EP-11 | `GET /api/v1/portfolios/{id}/journal` | AX-V1A-A11-FUN-011 |
| P§6/A11-EP-12 | `GET /api/v1/risk/status` | AX-V1A-A11-FUN-012 |
| P§6/A11-EP-13 | `POST /api/v1/risk/pause` | AX-V1A-A11-FUN-013 |
| P§6/A11-EP-14 | `POST /api/v1/risk/resume` | AX-V1A-A11-FUN-014 |
| P§6/A11-EP-15 | `GET /api/v1/strategies/trend` | AX-V1A-A11-FUN-015 |
| P§6/A11-EP-16 | `GET /api/v1/strategies/trend/decisions` | AX-V1A-A11-FUN-016 |
| P§6/A11-EP-17 | `POST /api/v1/backtests` | AX-V1A-A11-FUN-017 |
| P§6/A11-EP-18 | `GET /api/v1/backtests/{id}` | AX-V1A-A11-FUN-018 |
| P§6/A11-EP-19 | `POST /api/v1/replays` | AX-V1A-A11-FUN-019 |
| P§6/A11-EP-20 | `GET /api/v1/replays/{id}` | AX-V1A-A11-FUN-020 |
| P§6/A11-EP-21 | `POST /api/v1/replays/{id}/pause` | AX-V1A-A11-FUN-021 |
| P§6/A11-EP-22 | `POST /api/v1/replays/{id}/resume` | AX-V1A-A11-FUN-022 |
| P§6/A11-EP-23 | `POST /api/v1/replays/{id}/step` | AX-V1A-A11-FUN-023 |
| P§6/A11-EP-24 | `POST /api/v1/shadow-sessions` | AX-V1A-A11-FUN-024 |
| P§6/A11-EP-25 | `POST /api/v1/shadow-sessions/{id}/stop` | AX-V1A-A11-FUN-025 |
| P§6/A11-EP-26 | `GET /api/v1/shadow-sessions/{id}` | AX-V1A-A11-FUN-026 |
| P§6/A11-EP-27 | `GET /api/v1/incidents` | AX-V1A-A11-FUN-027 |
| P§6/A11-EP-28 | `GET /api/v1/incidents/{id}` | AX-V1A-A11-FUN-028 |
| P§6/A11-EP-29 | `GET /api/v1/audit-events` | AX-V1A-A11-FUN-029 |
| P§6/A11-EP-30 | `GET /api/v1/stream` | AX-V1A-A11-FUN-030 |
| P§6/A11-D02 | Secure mutations/auth/session/cookies | AX-V1A-A11-SAF-001; AX-V1A-A11-SAF-002 |
| P§6/A11-D03 | Pagination, stable errors, generated types | AX-V1A-A11-FUN-031; AX-V1A-A11-QLT-001; AX-V1A-A11-SAF-003 |
| P§6/A11-D04 | Durable resumable SSE | AX-V1A-A11-FUN-030; AX-V1A-A11-QLT-002 |
| P§6/A11-D05 | Global disabled-mode banner | AX-V1A-A11-SAF-005 |
| P§6/A11-D06 | Command Center | AX-V1A-A11-FUN-034 |
| P§6/A11-D07 | Binance Connection | AX-V1A-A11-FUN-035 |
| P§6/A11-D08 | Portfolio/journal | AX-V1A-A11-FUN-036 |
| P§6/A11-D09 | Risk Center | AX-V1A-A11-FUN-037 |
| P§6/A11-D10 | Trend Strategy | AX-V1A-A11-FUN-038 |
| P§6/A11-D11 | Backtest Lab | AX-V1A-A11-FUN-039 |
| P§6/A11-D12 | Replay Lab | AX-V1A-A11-FUN-040 |
| P§6/A11-D13 | Shadow Trading Center | AX-V1A-A11-FUN-041 |
| P§6/A11-D14 | Incident and Audit Center | AX-V1A-A11-FUN-042 |
| P§6/A11-D15 | Every state/accessibility/table requirement | AX-V1A-A11-QLT-003; AX-V1A-A11-QLT-004 |
| P§6/A11-A01 | Full authenticated workflow | AX-V1A-A11-QLT-005 |
| P§6/A11-A02 | SSE disconnect/resume safe | AX-V1A-A11-QLT-006 |
| P§6/A11-A03 | All values visibly virtual/mode-labelled | AX-V1A-A11-SAF-005 |
| P§6/A11-A04 | No API/UI external trading action | AX-V1A-A11-SAF-006; AX-V1A-A11-SAF-007 |

## Plan §7 — test and release validation

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| P§7/BT-01 | Unit/table tests for every backend package | AX-V1A-A01-QLT-023 |
| P§7/BT-02 | Property/fuzz financial, journal, reservation, order, decoder tests | AX-V1A-A02-QLT-001; AX-V1A-A04-QLT-001; AX-V1A-A08-FUN-006 |
| P§7/BT-03 | Golden Binance, indicator, report, OpenAPI, and run tests | AX-V1A-A06-FUN-006; AX-V1A-A10-QLT-001; AX-V1A-A11-QLT-001; AX-V1A-A08-QLT-001 |
| P§7/BT-04 | PostgreSQL integration coverage | AX-V1A-A04-OPS-001; AX-V1A-A04-QLT-002; AX-V1A-A04-QLT-003 |
| P§7/BT-05 | Adapter contract tests against emulator | AX-V1A-A06-QLT-001 |
| P§7/BT-06 | Race tests for runtime ownership/queues/books/projections | AX-V1A-A03-QLT-004 |
| P§7/BT-07 | Failure injection across data/storage/crash/reconciliation | AX-V1A-A03-QLT-003; AX-V1A-A04-QLT-003; AX-V1A-A07-QLT-001; AX-V1A-A08-FUN-008; AX-V1A-A09-SAF-006 |
| P§7/BT-08 | Deterministic replay under concurrency changes | AX-V1A-A03-QLT-001; AX-V1A-A08-QLT-001 |
| P§7/BT-09 | HTTP auth/authz/validation/pagination/CSRF/idempotency/redaction | AX-V1A-A11-SAF-001; AX-V1A-A11-SAF-002; AX-V1A-A11-SAF-003; AX-V1A-A11-FUN-031 |
| P§7/FT-01 | Strict frontend type check | AX-V1A-A11-FUN-043 |
| P§7/FT-02 | Vitest/RTL component tests | AX-V1A-A11-QLT-003 |
| P§7/FT-03 | Axe accessibility checks | AX-V1A-A11-QLT-004 |
| P§7/FT-04 | Contract-driven mock API tests | AX-V1A-A11-QLT-001 |
| P§7/FT-05 | Full Playwright V1A workflow | AX-V1A-A11-QLT-005 |
| P§7/FT-06 | SSE fault/recovery tests | AX-V1A-A11-QLT-006 |
| P§7/FT-07 | Visual disabled-mode and virtual-label checks | AX-V1A-A11-SAF-005 |
| P§7/RV-01 | Complete `make verify` gate | AX-V1A-RG-QLT-005 |
| P§7/LR-01 | 72-hour Binance soak | AX-V1A-A07-QLT-002; AX-V1A-RG-QLT-002 |
| P§7/LR-02 | Sustained replay and bounded memory | AX-V1A-RG-QLT-006 |
| P§7/LR-03 | Backup and restore drill | AX-V1A-A04-QLT-007; AX-V1A-RG-OPS-001 |
| P§7/LR-04 | Crash/restart and lease-fencing drill | AX-V1A-A03-QLT-002; AX-V1A-A03-QLT-006; AX-V1A-RG-QLT-006 |
| P§7/LR-05 | Full Playwright workflow | AX-V1A-A11-QLT-005; AX-V1A-RG-QLT-006 |
| P§7/LR-06 | Release-image outbound-host inspection | AX-V1A-RG-SAF-002 |

## Plan §8 — documentation deliverables

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| P§8/DOC-01 | Root README | AX-V1A-A01-QLT-005 |
| P§8/DOC-02 | Complete safe environment example | AX-V1A-A02-QLT-004 |
| P§8/DOC-03 | Deployment README | AX-V1A-A01-OPS-005 |
| P§8/DOC-04 | Implementation status | AX-V1A-A00-QLT-016 |
| P§8/DOC-05 | Requirements traceability | AX-V1A-A00-QLT-001 |
| P§8/DOC-06 | V1A readiness evidence | AX-V1A-A00-QLT-016 |
| P§8/DOC-07 | Architecture documents | AX-V1A-A03-QLT-005 |
| P§8/DOC-08 | Trend strategy document | AX-V1A-A10-QLT-005 |
| P§8/DOC-09 | Binance adapter document | AX-V1A-A07-QLT-004 |
| P§8/DOC-10 | Accounting document | AX-V1A-A04-QLT-005 |
| P§8/DOC-11 | Configuration reference | AX-V1A-A02-QLT-004 |
| P§8/DOC-12 | Threat model and secret-handling guide | AX-V1A-A00-SAF-008 |
| P§8/DOC-13 | Operations runbooks | AX-V1A-A05-OPS-002 |
| P§8/DOC-14 | Generated API documentation | AX-V1A-A11-QLT-007 |
| P§8/DOC-15 | ADRs for durable decisions | AX-V1A-A00-QLT-015 |

## Plan §9 — release gate

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| P§9/G-01 | All A0–A11 acceptance passes | AX-V1A-RG-QLT-001 |
| P§9/G-02 | Every external-order/authenticated capability absent | AX-V1A-RG-SAF-001; AX-V1A-RG-SAF-006; AX-V1A-RG-SAF-007; AX-V1A-RG-SAF-008 |
| P§9/G-03 | Forbidden flags remain false/absent | AX-V1A-RG-SAF-001 |
| P§9/G-04 | Binance health and 72-hour soak pass | AX-V1A-RG-QLT-002 |
| P§9/G-05 | Unsafe data cannot reach a decision | AX-V1A-RG-SAF-003 |
| P§9/G-06 | Journal/reservation/fence/restart/replay/reconcile pass | AX-V1A-RG-QLT-003 |
| P§9/G-07 | Ten deterministic hashes match | AX-V1A-RG-QLT-004 |
| P§9/G-08 | Trend works in all V1A modes | AX-V1A-RG-FUN-001 |
| P§9/G-09 | Risk starts paused and cannot be bypassed | AX-V1A-RG-SAF-004 |
| P§9/G-10 | Backup/restore succeeds | AX-V1A-RG-OPS-001 |
| P§9/G-11 | Alerts and dashboards operational | AX-V1A-RG-OPS-002 |
| P§9/G-12 | Clean-checkout `make verify` passes | AX-V1A-RG-QLT-005 |
| P§9/G-13 | Git state contains only intentional tracked files | AX-V1A-RG-QLT-006 |
| P§9/G-14 | Documentation/status current | AX-V1A-RG-QLT-007 |
| P§9/G-15 | Remaining limitations explicit | AX-V1A-RG-QLT-007 |
| P§9/G-16 | Claims research/simulation, never profitability | AX-V1A-RG-SAF-005 |

## Plan §10 — implementation assumptions

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| P§10/AS-01 | Initial operation is local Docker Compose | AX-V1A-A01-OPS-004; AX-V1A-A01-OPS-005 |
| P§10/AS-02 | Docker access is required for Compose acceptance | AX-V1A-A01-OPS-005; AX-V1A-A01-QLT-008 |
| P§10/AS-03 | Environment-driven naming; no destructive volume rename | AX-V1A-A01-OPS-006 |
| P§10/AS-04 | V1A uses Binance public Spot data only | AX-V1A-A07-SAF-003; AX-V1A-RG-FUN-002 |
| P§10/AS-05 | Initial instruments are BTC/USDT and ETH/USDT | AX-V1A-A07-SAF-003 |
| P§10/AS-06 | One Trend-only 500-USDT Binance portfolio | AX-V1A-A09-FUN-001 |
| P§10/AS-07 | USDT reporting; USD disabled without reviewed source | AX-V1A-A09-SAF-007 |
| P§10/AS-08 | No exchange credential accepted in V1A | AX-V1A-A00-SAF-002; AX-V1A-RG-SAF-006 |
| P§10/AS-09 | Prometheus/Grafana begin in A5 | AX-V1A-A05-FUN-004 |
| P§10/AS-10 | Python is analysis-only; Go is authoritative | AX-V1A-A10-SAF-004 |

## Additional authoritative Spec clauses used by atomic successors

| Source clause | Atomic source obligation | Requirement ID(s) |
|---|---|---|
| S8.3/QUEUE-CRITICAL | Critical order/account/reservation/ledger/risk/admin facts use bounded durable capacity and fail closed rather than drop | AX-V1A-A03-SAF-007 |
| S19.7/DB-UNIQUE-EVENT | Deduplicate each exchange event identity/canonical payload identity | AX-V1A-A04-SAF-018 |
| S20.2/CORRECTION | Never edit journal history; use explicit compensating/external-adjustment transactions with incident attribution | AX-V1A-A04-SAF-017 |
| S23/ASSET-SCREENING | Persist asset screening versions as first-class relational/audit data | AX-V1A-A04-FUN-023 |
| S23/DB-UNIQUE-FILL | Database-enforce exchange fill identity uniqueness | AX-V1A-A04-SAF-018 |
| S23/DB-UNIQUE-RUN | Database-enforce immutable run ID uniqueness | AX-V1A-A04-SAF-019 |

## Retired and superseded matrix IDs

Retired IDs remain visible here so reverse coverage is total. They are not
implementation obligations and cannot satisfy a gate; every successor is an
active atomic row.

| Retired ID | Original source | Superseding ID(s) |
|---|---|---|
| AX-V1A-A01-FUN-005 | P§2/TD-09 | AX-V1A-A01-FUN-002 |
| AX-V1A-A01-QLT-011 | P§3/FILE-04; P§3/FILE-05 | AX-V1A-A01-QLT-015; AX-V1A-A01-QLT-016 |
| AX-V1A-A01-QLT-012 | P§3/FILE-06; P§3/FILE-07 | AX-V1A-A01-QLT-017; AX-V1A-A01-QLT-018 |
| AX-V1A-A01-QLT-014 | P§3/FILE-09; P§3/FILE-10; P§3/FILE-11; P§3/FILE-12; S3.1/CORE-INDEPENDENCE | AX-V1A-A01-QLT-019; AX-V1A-A01-QLT-020; AX-V1A-A01-QLT-021; AX-V1A-A01-QLT-022; AX-V1A-A06-FUN-001 |
| AX-V1A-A04-SAF-002 | P§5/INV-03; P§5/INV-04; P§6/A4-A02 | AX-V1A-A04-SAF-009; AX-V1A-A04-SAF-010; AX-V1A-A04-QLT-002 |
| AX-V1A-A04-SAF-003 | P§5/INV-05; P§6/A4-A05 | AX-V1A-A04-SAF-011; AX-V1A-A04-SAF-017 |
| AX-V1A-A04-SAF-004 | P§5/INV-01; P§5/INV-02; P§5/INV-08; P§5/INV-09; S19.7/DB-UNIQUE-EVENT; S23/DB-UNIQUE-FILL; S23/DB-UNIQUE-RUN | AX-V1A-A04-SAF-007; AX-V1A-A04-SAF-008; AX-V1A-A04-SAF-014; AX-V1A-A04-SAF-015; AX-V1A-A04-SAF-018; AX-V1A-A04-SAF-019 |
| AX-V1A-A04-SAF-005 | P§5/INV-06; P§5/INV-07; P§5/INV-10 | AX-V1A-A04-SAF-012; AX-V1A-A04-SAF-013; AX-V1A-A04-SAF-016 |
| AX-V1A-A05-FUN-005 | P§2/TD-16 | AX-V1A-A05-FUN-009 |
| AX-V1A-A11-FUN-033 | P§2/TD-02; P§2/TD-17; P§2/TD-18; P§2/TD-19; P§2/TD-20; P§2/TD-21; P§2/TD-22; P§2/TD-23 | AX-V1A-A11-FUN-043; AX-V1A-A11-FUN-044; AX-V1A-A11-FUN-045; AX-V1A-A11-FUN-046; AX-V1A-A11-FUN-047; AX-V1A-A11-FUN-048; AX-V1A-A11-FUN-049; AX-V1A-A11-FUN-050; AX-V1A-A11-SAF-007 |

## Coverage maintenance

1. A new or changed plan/spec clause first receives a stable source-clause ID.
2. Every V1A-applicable source clause maps to at least one non-retired
   `AX-V1A-*` ID. A cumulative-program clause owned by V1B–V1D has an explicit
   deferred disposition and may not be used to claim V1A completion.
3. A source clause whose implementation and measured acceptance occur in
   different phases maps to separate IDs rather than forcing an early phase to
   wait for later runtime evidence.
4. Phase A0 IDs verify definitions, reviews, threat/test architecture, and target
   approval only. Runtime measurements and implementation conformance remain in
   A1–A11 or `RG`.
