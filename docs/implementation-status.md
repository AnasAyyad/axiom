# Axiom implementation status

## Complete owner console program

**Status:** In progress — implementation branch only; no formal qualification
or certification is implied.

**Branch:** `product/complete-owner-console` (local, intentionally does not
track `origin/main`)

**Starting SHA:** `a1bb6a89931cb6fcdd89dd842a4d16fe63cd18aa`

This program replaces delivery-stage terminology and the multi-role console
with a semantic, exactly-one-owner product model. It will add a server-validated
unified run catalogue, strategy registry, deterministic guided demonstrations,
and owner-facing workflows for historical, replay, public-data shadow, and
explicitly armed Testnet/Demo sessions. It preserves every existing safety
boundary: production-private orders remain structurally impossible; sandbox
orders remain spot-only, capped, reconciled, and require a short-lived owner
arm; and rebalancing remains advisory only.

Implementation, local validation, review, merge, formal qualification, and
release certification are reported separately. In particular, this program
does not start, replace, or certify B2, C6, D5, or D6 evidence.

### Implemented owner-console slices

- One active owner is enforced by the current authentication path and a
  fail-closed database migration; legacy authorization evidence remains
  historical only.
- The server supplies a semantic strategy/run catalogue and a unified,
  read-only run history over durable backtest, replay, and public-data shadow
  records. Trend Following and Mean Reversion now share the credential-free
  durable backtest/replay worker through a semantic runtime registry; all
  other strategy/mode combinations remain explicit blockers until their real
  shared runtime is installed.
- Each listed run has one semantic detail route with immutable timeline,
  decision, planned-order, simulated-execution, latest portfolio, risk
  availability, and safe reproducibility-evidence projections. A missing
  snapshot or run-scoped risk record is presented as `not_recorded`; the
  product never substitutes current global state or fabricates evidence.
- The run detail page exposes only lifecycle controls that the current durable
  run can accept: replay pause, resume, and one-event step, plus safe public
  shadow stop. The server supplies the allowed actions in every run projection,
  and each uses the existing revision-checked, audited command path;
  unsupported run types expose no control.
- The protected Data Catalogue lists registered immutable manifests, coverage,
  gaps, quality tier, and hashes. It accepts no browser upload or raw storage
  path.
- The sandbox qualification API now uses semantic public contract names for
  sandbox status, chaos, and service-level objectives. Its retained evidence
  window and endpoint are unchanged.
- The owner UI includes those catalogue/history projections and explicitly
  marks guided demonstrations unavailable until deterministic immutable bundles
  are actually installed.

Remaining implementation includes shared materialization for the remaining
strategy families and live shadows, deterministic demonstration bundles,
actual run-scoped risk/P&L projections, automated armed Testnet/Demo strategy
sessions, and the full historical terminology cleanup. These items are not
represented as operational capability today.

## V1D D6 repository certification

The `v1d-d6-certification` worktree adds cumulative exact-identity evidence,
independent-review, Section 35, safety-manifest, and signed no-replace release
verdict enforcement. Missing, expired, failed, dirty, wrong-SHA, mutable,
unsigned, tampered, duplicate, or high-severity-open evidence fails closed. The
final command remains default-off.

Repository implementation and all available dirty-worktree non-soak gates are
locally verified. A clean exact-SHA integrated workflow and hosted CI remain
pending because the D6 content is not committed. Final V1 certification is
also blocked by B2 and C6 72-hour verdicts, D5's seven-day verdict, earlier open
owner/security/cumulative acceptances, a declared-server restore, current
exact-candidate artifacts, all seven independent reviews, and signed formal
Section 35 evidence. State: **NOT CERTIFIED**.

## V1D D5 operational hardening

D5 implementation is on `main` at
`93dc3edf74ead553af75a589cd50eeb4735f2db5`. Storage-pressure fail-closed
automation, lifecycle/holds, independent encrypted backup and recovery proof,
hardened deployment, deterministic faults, and a default-off seven-day runner
exist. The formal seven-day run was not started. The D6 worktree repairs the
backup-image supply-chain failure from hosted run `30904448621`; new hosted D6
evidence is still pending.

## V1D D3-D4 application completion

D3 merged through PR #35 and supplies functional backtest, replay, and shadow
labs with immutable inputs, reproduction/comparison, controls, redacted export,
and responsive browser coverage. D4 merged through PR #36 and supplies reports,
incidents, alerts, tamper-evident audit review, evidence holds, and operational
bundles. Both are implemented; current cumulative formal acceptance remains
separate.

## V1D D2 React command center

D2 merged through PR #34. It implements the six-group role-aware product
navigation, persistent safety header, Decisions & Orders and restricted System
Events, Strategy Center, Qualification Center, approved Run Lab, scoped risk
controls, and the required operational resource screens over the merged D1
control-plane API. High-risk actions retain exact-revision password/TOTP
reauthorization, and the UI adds no arbitrary command or production-private
execution surface.

Strict TypeScript, lint, unit/axe, production build, generated-contract parity,
D2 boundary, file policy, secret, prohibited-capability, and five-project
browser gates are implemented. Formal earlier-gate acceptance and C6's separate
72-hour qualification remain pending. See
`docs/releases/v1d-d2-readiness.md`.

## V1D D1 control plane

D1 is merged into `main` at merge commit `4cf0b14`. It provides the compatible
OpenAPI contracts, durable commands, read projections, reason catalogue,
exports, qualifications, roles, and permission-filtered stream extensions
consumed by D2. D1 merge is implementation state, not cumulative V1D safety
acceptance.

## V1C PR3 C6 console and non-soak qualification

The `v1c-c6-console-soak` branch implements the C6 redacted sandbox
operations API and React console, purpose-bound administrative controls,
bounded observability, deterministic chaos coverage, and the default-off
72-hour qualification runner/evidence contract. The implementation extends the
existing V1C account, arm, allocator/risk/planner/dispatcher, order reducer,
reconciliation, reset-incident, and audit state. It does not add an API-side
exchange client or a parallel order state machine.

Focused API, authentication, storage, React/axe, desktop/mobile C6 Playwright,
and the complete unmocked A11 browser workflow pass. The cumulative C1-C6
non-soak aggregate, PostgreSQL 18.4 clean-install and exact B8-upgrade gates,
all 1,024 Compose renders, security boundaries, and complete `make verify`
pass. The exact-source reproducible image, non-root/read-only and compiled
absence checks, image-backed Compose smoke, SPDX SBOM, and current Trivy gates
also pass for implementation commit
`b5ac868ec38d9204afc6f9fd4db6673aee10e852`. Implementation and non-soak
qualification are complete. The formal 72-hour C6 soak and owner/security
acceptance are explicitly pending and are not run as part of PR3
implementation qualification. No additional authenticated exchange order was
placed or required for this branch.

## V1C PR2 C4-C5 adapters and engines

The `v1c-c4-c5-adapters` branch extends the merged C1-C3 foundation with
complete Binance Spot Testnet and Bybit Demo authenticated adapters, separate
credential-owning engines, migration `000023`, and a controlled full-pipeline
canary/restart evidence path.

C4/C5 deterministic gates, clean PostgreSQL 18.4 install, exact B8 upgrade,
the closed security boundary, all 1,024 Compose profile renders, the complete
PR2 aggregate, and exact-source clean-image artifact gates pass. Both required
operator-armed canaries passed on the same final dirty executable. The
matching live proxy-cut tests also proved fail-closed `DEGRADED` recovery,
private-stream backfill, fresh reconciliation, zero engine restarts, and clean
exit-zero shutdown. PR2 is merged into `main` at
`8902b3b794ed344a131822b34fa8bb81cedaa35e`; formal owner/security acceptance
remains pending. See
`docs/releases/v1c-pr2-readiness.md`.

## V1C PR1 C1-C3 foundation

The `v1c-c1-c3-foundation` branch adds the default-off
`axiom.config.v1c.1` policy, neutral sandbox contracts, closed authenticated
signers and proxies, TOTP/one-use authorization and rotation foundations,
durable sandbox dispatch/recovery models, and migrations `000021`/`000022`.

C1-C3 and the aggregate PR1 local qualification passed and merged. C4/C5 are
tracked above. C6 and the V1C 72-hour soak are not implemented or claimed here.
See `docs/releases/v1c-pr1-readiness.md`.

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

V1A application roles remain public-data research and simulation software
only. They receive no exchange credentials and expose no external order side
effects. The repository now also contains separately gated, default-off V1C
Testnet/Demo foundations. Production `live` execution and every prohibited
product or fund-management capability remain rejected.

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
- V1C PR2 C4/C5 deterministic, database, aggregate, and exact-source clean
  artifact gates pass locally. Both operator-armed exchange canaries passed on
  the same final dirty executable, and both engines passed live proxy-cut
  degradation/recovery without restarting. The PR2 branch is committed and
  merged into `main`; formal owner/security acceptance remains open. PR3 C6
  implementation is tracked above, while its 72-hour qualification remains
  pending.
- V1C PR3 C6 implementation and non-soak qualification pass on exact
  implementation commit
  `b5ac868ec38d9204afc6f9fd4db6673aee10e852`, including the clean artifact
  record. The formal 72-hour soak, sealed evidence review, and owner/security
  acceptance remain open.

V1B planning and implementation status is tracked separately in
[V1B implementation status](releases/v1b-implementation-status.md). V1B work
does not change the open V1A evidence gates above.
