# V1A execution modes

## Status and authority

This document is the normative V1A configuration policy derived from
`crypto_bot_v1_codex_spec.md`. It describes behavior that the V1A implementation
must enforce. It is not evidence that the application binary, database
constraints, API, UI, or deployment image already enforce it.

V1A is a research and simulation release. It has no authenticated exchange
client and no external order side effect. A mode is enabled only after its
implementation, negative tests, and release evidence pass; documentation alone
does not enable a mode.

## Mode matrix

| Mode | V1A status | Market data | Clock | Broker | Credentials | Authoritative state | Permitted external side effects |
|---|---|---|---|---|---|---|---|
| `backtest` | Enabled by policy | Approved historical dataset | Deterministic logical clock | Historical simulator | None | Immutable run manifest and virtual journal | None |
| `replay` | Enabled by policy | Recorded canonical events with raw references | Deterministic replay clock | Replay simulator | None | Run journal and durable checkpoint | None |
| `paper` | Enabled by policy | Historical or synthetic configured feed; never a live production feed | Logical or controlled real time | Paper simulator | None | Virtual journal | None |
| `shadow` | Enabled by policy | Live Binance production-public Spot data | Monotonic live clock plus UTC wall time | Shadow simulator | None | Recoverable virtual journal | Public reads only |
| `testnet` | Unavailable and rejected in V1A | Not applicable | Not applicable | Absent | Not accepted | Not applicable | None |
| `demo` | Unavailable and rejected in V1A | Not applicable | Not applicable | Absent | Not accepted | Not applicable | None |
| `live` | Prohibited in every V1 release | Not applicable | Not applicable | Must be absent | Must not exist | Not applicable | None |

“Enabled by policy” means the mode is in V1A scope. It does not claim the mode
currently runs. Binance Spot Testnet and Bybit demo first become eligible for
implementation in V1C after separate credential, endpoint, reconciliation,
fencing, and manual-arm gates. `live` is not a future V1 mode.

## Common invariants

Every accepted V1A session must satisfy all of these rules:

- The mode is explicit, validated, and immutable after the session starts.
- Unknown, empty, differently cased, or aliased mode values are rejected.
- No environment variable, database value, API request, UI action, feature
  flag, Compose profile, or generated contract can add or reinterpret a mode.
- The selected adapter, clock, broker, journal namespace, dataset, and model
  namespace must match the matrix. A mismatch fails startup.
- All values, orders, fills, balances, and reports carry their mode and remain
  visibly labelled virtual, backtest, replay, paper, or shadow.
- Results from different modes remain in separate evidence/model namespaces.
- Strategy code can emit candidates only. Allocation, risk, simulation,
  accounting, and reconciliation remain mandatory boundaries in every mode.
- Initial execution state is `PAUSED`. Startup health and recovery may make a
  process ready, but never activate strategy decisions automatically.
- Missing, stale, contradictory, or unverifiable safety inputs reject new work.
- No mode may sell inventory that its relevant virtual portfolio does not own.
- No mode may model margin, futures, perpetuals, options, leverage, borrowing,
  lending, staking, withdrawal, transfer execution, or short selling.

## Mode-specific contract

### Backtest

Backtest reads an approved, immutable historical dataset and advances only by a
deterministic scheduler. The run identity fixes the dataset hashes, build,
configuration, strategy, risk and accounting policies, seed, models, and
starting balances. It cannot open an outbound exchange connection. Ten runs
with the same identity must produce identical canonical events and result hash.

### Replay

Replay consumes recorded event envelopes in canonical order. Pause, resume,
speed, seek, single-step, and fault injection may change how quickly the user
observes a run, but not event order or deterministic results. Replay cannot
silently substitute current exchange data or configuration for recorded input.

### Paper

Paper uses historical or synthetic configured input and simulated execution. It
never consumes a live production feed. A real-time scheduler does not turn
paper into shadow; the data-source contract remains decisive.

### Shadow

Shadow may make only the public reads in `endpoint-policy.md`. It performs the
same strategy, allocation, risk, simulation, accounting, and recovery flow as
other virtual modes but never constructs or sends an exchange order. The
process accepts no exchange API key, secret, signature material, listen key, or
private-account payload. A stale, gapped, crossed, or otherwise unhealthy book
pauses affected decisions and triggers resynchronization.

## Required rejection behavior

Configuration parsing and every later boundary must reject:

- `testnet` or `demo` in a V1A build; V1A deployment profiles and secret files
  with those capabilities must not exist;
- `live`, `production`, `prod`, `real`, or any other real-order synonym;
- any exchange credential reference in a V1A application process;
- any broker or adapter capability for authenticated account, private stream,
  order, cancellation, transfer, or withdrawal operations;
- a shadow configuration that selects synthetic/historical data without being
  explicitly changed to the appropriate mode;
- a paper configuration that selects a live production feed;
- a run whose stored mode is outside the compiled V1A enum.

Rejection occurs before process readiness and before outbound network I/O. The
error uses a stable, non-secret reason code and emits an audit event where the
audit store is safely available. A safety rejection cannot be overridden.

## Enforcement layers and evidence

V1A implementation must provide independent controls and evidence:

| Layer | Required control | Gate evidence |
|---|---|---|
| Compile/build | Only V1A mode enum and simulator broker implementations are linked; no production or authenticated broker | Package/symbol scan and clean-build review |
| Configuration | Schema rejects prohibited modes, credentials, products, and conflicting source/broker combinations | Unit, table, property, and startup tests |
| Persistence | Database constraints accept only V1A modes for V1A run records | Migration and constraint integration tests |
| API | Contracts accept only enabled V1A mode values and expose no external-order operation | OpenAPI review and negative HTTP tests |
| UI | Persistent `REAL TRADING DISABLED` banner and no real/test/demo activation control | Component, accessibility, and E2E evidence |
| Deployment | V1A profiles mount no exchange credentials and start no authenticated engine | Compose render and secret-mount inspection |
| Network | Shadow outbound traffic matches the compiled public-only endpoint policy | Captured-request and release-image egress test |

Until each evidence item exists and passes, the corresponding row is a planned
control, not an implemented guarantee.

## Ownership and change control

- Product and security own the allowed-mode policy.
- Platform engineering owns schema, build, and deployment enforcement.
- Storage owns mode constraints and immutable run identity.
- API/frontend own contract and user-visible labelling.
- QA owns negative-mode and clean-build evidence.

Adding a mode, changing a data-source/broker pairing, or weakening a rejection
is a safety-policy change. It requires specification approval, threat-model and
ADR updates, traceability, negative tests, and a new release review. No V1
change may authorize `live`.
