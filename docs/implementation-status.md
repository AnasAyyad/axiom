# Axiom V1A implementation status

This tracker records implemented behavior and verified evidence. A phase is marked complete only after every acceptance criterion in the authoritative specification and the V1A implementation plan has current evidence.

| Phase | Status                                                                                              | Current slice                                                                                                                           | Evidence                                                                                                                                                                        |
| ----- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A0    | Complete                                                                                            | Scope traceability, safety architecture, threat model, topology, lifecycle, and readiness policy                                        | `docs/releases/evidence/a0-review.md`                                                                                                                                           |
| A1    | Complete                                                                                            | Repository, toolchain, application skeleton, Compose, and CI                                                                            | Local validation, owner-verified hosted CI/supply-chain evidence, and clean-machine setup/governance walkthrough pass                                                           |
| A2    | Complete                                                                                            | Fixed-point finance, canonical domain types, and immutable fail-closed configuration                                                    | `docs/releases/evidence/a2-local-validation.md`; external integration remains owner-managed                                                                                     |
| A3    | Complete                                                                                            | Deterministic runtime, bounded concurrency, and fencing                                                                                 | `docs/releases/evidence/a3-local-validation.md`; PostgreSQL durability remains A4 work                                                                                          |
| A4    | Complete                                                                                            | PostgreSQL, journal, generated repositories, Parquet/Zstd, and recovery                                                                 | `docs/releases/evidence/a4-local-progress.md`; clean PG18 and timed restore qualification passed                                                                                |
| A5    | Complete                                                                                            | Redacted logs/traces, bounded metrics, authenticated health, durable alerts, rules, and dashboards                                      | `docs/releases/evidence/a5-local-progress.md`; Docker, scans, alert SLO, and tabletop qualification passed                                                                      |
| A6    | Complete                                                                                            | Public exchange contracts, capability boundary, deterministic controls, emulator, and fixtures                                          | `docs/releases/evidence/a6-local-validation.md`; cumulative verification and binary absence gate passed                                                                         |
| A7    | Accepted — owner waiver                                                                             | Binance public adapter, synchronized books, operational recorder, and completed 72-hour qualification                                  | `docs/releases/evidence/a7-owner-waiver-2026-07-26.md`; terminal result remains `qualified:false`; waiver is availability/resynchronization-only and time-bounded                |
| A8    | Implemented and locally validated — own formal acceptance pending                                   | Backtesting, replay, simulation, durable orders, persistence, and local dataset qualification                                           | `docs/releases/evidence/a8-local-validation.md`; implementation is merged into `main`                                                                                           |
| A9    | Implemented and locally validated — formal A8/A9 acceptance pending                                 | Portfolio allocation, risk, reconciliation, and recovery                                                                                | `docs/releases/evidence/a9-local-validation.md`; implementation is merged into `main`                                                                                           |
| A10   | Implemented and locally validated — formal A8-A10 acceptance pending                                | Trend strategy, exact sizing/exits, shared simulated pipeline, immutable research governance, and reporting                             | `docs/releases/evidence/a10-local-validation.md`; implementation is merged into `main`                                                                                          |
| A11   | Implemented and locally qualified — formal A8-A11 acceptance pending                                | Versioned API/authentication, durable worker/replay controls, production-public shadow runtime, resumable SSE, and routed React console | `docs/releases/evidence/a11-local-validation.md`; implementation is merged into `main`, and clean PostgreSQL/browser/verify/image/Compose gates passed                           |

## Absolute V1A boundary

V1A is public-data research and simulation software only. It contains no authenticated exchange transport, signing implementation, private endpoint, production broker, withdrawal or transfer operation, or execution mode capable of external order side effects. The only V1A execution modes are `backtest`, `replay`, `paper`, and `shadow`; `testnet`, `demo`, and `live` are rejected.

## Current limitations

- The A1-A6 foundations and A7 production-public collector/recorder
  implementation exist. A7 is accepted under the repository owner's documented,
  time-bounded non-safety availability/resynchronization waiver; the completed
  machine result remains `qualified:false`. This acceptance does not pre-check
  or formally accept A8-A11.
- Immutable-candidate local A1 validation is recorded in
  `docs/releases/evidence/a1-local-validation.md`. Owner-verified hosted CI and
  retained supply-chain evidence for commit
  `5ce09c3611e05a8fa5d0f1afc4706e17698b2d90` are recorded in
  `docs/releases/evidence/a1-hosted-ci.md`; the completed setup/governance
  walkthrough is recorded in
  `docs/releases/evidence/a1-clean-machine-walkthrough.md`.
- A8 has local implementation evidence but its own formal acceptance remains pending.
- A9 has local implementation evidence but formal A8/A9 acceptance remains pending.
- A10 has local implementation evidence but is not formally complete because
  formal A8/A9/A10 acceptance remains pending. No final-test strategy result
  was consumed for implementation evidence.
- A11 has current local implementation evidence for clean PostgreSQL 18 setup,
  desktop/mobile fixtures, the unmocked browser workflow, full cumulative
  verification, live vulnerability lookup, exact-identity image inspection,
  and image-backed Compose smoke. This closes the local implementation gate,
  not formal phase acceptance; A11 remains blocked by formal A8/A9/A10
  acceptance.
- The clean backup/restore drill and A8-A11 formal acceptances remain open release evidence.

V1B planning and implementation status is tracked separately in
[V1B implementation status](releases/v1b-implementation-status.md). V1B work
does not change the open V1A evidence gates above.
