# Axiom V1A implementation status

This tracker records implemented behavior and verified evidence. A phase is marked complete only after every acceptance criterion in the authoritative specification and the V1A implementation plan has current evidence.

| Phase | Status      | Current slice                                                                                    | Evidence                                                                                                     |
| ----- | ----------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| A0    | Complete    | Scope traceability, safety architecture, threat model, topology, lifecycle, and readiness policy | `docs/releases/evidence/a0-review.md`                                                                        |
| A1    | In progress | Repository, toolchain, application skeleton, Compose, and CI                                     | Immutable candidate passes clean local gates, reproducibility, and full image-backed Compose smoke; hosted CI evidence pending |
| A2    | Not started | Financial domain and configuration safety                                                        | Pending A1 gate                                                                                              |
| A3    | Not started | Deterministic runtime, bounded concurrency, and fencing                                          | Pending A2 gate                                                                                              |
| A4    | Not started | PostgreSQL, journal, repositories, Parquet, and recovery                                         | Pending A3 gate                                                                                              |
| A5    | Not started | Security, observability, monitoring, and alerts                                                  | Pending A4 gate                                                                                              |
| A6    | Not started | Exchange contracts and deterministic emulator                                                    | Pending A5 gate                                                                                              |
| A7    | Not started | Binance public adapter and recorder                                                              | Pending A6 gate                                                                                              |
| A8    | Not started | Backtesting, replay, simulation, and durable orders                                              | Pending A7 gate                                                                                              |
| A9    | Not started | Portfolio allocation, risk, reconciliation, and recovery                                         | Pending A8 gate                                                                                              |
| A10   | Not started | Trend Following strategy                                                                         | Pending A9 gate                                                                                              |
| A11   | Not started | Versioned API, authentication, React UI, and live shadow workflow                                | Pending A10 gate                                                                                             |

## Absolute V1A boundary

V1A is public-data research and simulation software only. It contains no authenticated exchange transport, signing implementation, private endpoint, production broker, withdrawal or transfer operation, or execution mode capable of external order side effects. The only V1A execution modes are `backtest`, `replay`, `paper`, and `shadow`; `testnet`, `demo`, and `live` are rejected.

## Current limitations

- The A1 application skeleton exists, but it has no business, market-data,
  strategy, simulation, accounting, or risk implementation from later phases.
- Immutable-candidate local A1 validation is recorded in
  `docs/releases/evidence/a1-local-validation.md`; it cannot verify A1 without
  clean hosted CI, the required retained supply-chain evidence, and a separate
  clean-machine walkthrough.
- No phase is complete until its tests and evidence have actually been produced.
- The 72-hour Binance public-data soak and clean backup/restore drill are release evidence, not documentation-only checkboxes.
