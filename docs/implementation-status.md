# Axiom V1A implementation status

This tracker records implemented behavior and verified evidence. A phase is marked complete only after every acceptance criterion in the authoritative specification and the V1A implementation plan has current evidence.

| Phase | Status      | Current slice                                                                                    | Evidence                                                                                                     |
| ----- | ----------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| A0    | Complete    | Scope traceability, safety architecture, threat model, topology, lifecycle, and readiness policy | `docs/releases/evidence/a0-review.md`                                                                        |
| A1    | Complete    | Repository, toolchain, application skeleton, Compose, and CI                                     | Local validation, owner-verified hosted CI/supply-chain evidence, and clean-machine setup/governance walkthrough pass |
| A2    | Complete    | Fixed-point finance, canonical domain types, and immutable fail-closed configuration              | `docs/releases/evidence/a2-local-validation.md`; external integration remains owner-managed                    |
| A3    | Complete    | Deterministic runtime, bounded concurrency, and fencing                                          | `docs/releases/evidence/a3-local-validation.md`; PostgreSQL durability remains A4 work                         |
| A4    | Complete    | PostgreSQL, journal, generated repositories, Parquet/Zstd, and recovery                          | `docs/releases/evidence/a4-local-progress.md`; clean PG18 and timed restore qualification passed                |
| A5    | Complete    | Redacted logs/traces, bounded metrics, authenticated health, durable alerts, rules, and dashboards | `docs/releases/evidence/a5-local-progress.md`; Docker, scans, alert SLO, and tabletop qualification passed    |
| A6    | Not started | Exchange contracts and deterministic emulator                                                     | A5 verified; implementation has not started                                                                  |
| A7    | Not started | Binance public adapter and recorder                                                              | Pending A6 gate                                                                                              |
| A8    | Not started | Backtesting, replay, simulation, and durable orders                                              | Pending A7 gate                                                                                              |
| A9    | Not started | Portfolio allocation, risk, reconciliation, and recovery                                         | Pending A8 gate                                                                                              |
| A10   | Not started | Trend Following strategy                                                                         | Pending A9 gate                                                                                              |
| A11   | Not started | Versioned API, authentication, React UI, and live shadow workflow                                | Pending A10 gate                                                                                             |

## Absolute V1A boundary

V1A is public-data research and simulation software only. It contains no authenticated exchange transport, signing implementation, private endpoint, production broker, withdrawal or transfer operation, or execution mode capable of external order side effects. The only V1A execution modes are `backtest`, `replay`, `paper`, and `shadow`; `testnet`, `demo`, and `live` are rejected.

## Current limitations

- The A1 application skeleton, A2 financial domain/configuration boundary, and
  A3 deterministic runtime foundation exist, but market-data, strategy,
  simulation, accounting, and risk engines remain owned by later phases.
- Immutable-candidate local A1 validation is recorded in
  `docs/releases/evidence/a1-local-validation.md`. Owner-verified hosted CI and
  retained supply-chain evidence for commit
  `5ce09c3611e05a8fa5d0f1afc4706e17698b2d90` are recorded in
  `docs/releases/evidence/a1-hosted-ci.md`; the completed setup/governance
  walkthrough is recorded in
  `docs/releases/evidence/a1-clean-machine-walkthrough.md`.
- No phase is complete until its tests and evidence have actually been produced.
- The 72-hour Binance public-data soak and clean backup/restore drill are release evidence, not documentation-only checkboxes.
