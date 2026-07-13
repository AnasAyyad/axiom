# V1A readiness and release evidence

## Current decision

**V1A is not ready for release.** A0 is verified and A1 is in progress; A1–A11
and every release gate remain unverified. This document is an evidence index,
not evidence by itself. An unchecked item must not be inferred from source
layout, planned work, or a passing narrower test.

Normative requirement details live in
[requirements/traceability.md](../requirements/traceability.md). The execution
plan crosswalk is [requirements/source-coverage.md](../requirements/source-coverage.md),
the execution checklist is [v1a-phase-checklist.md](v1a-phase-checklist.md), and
current phase status is [implementation-status.md](../implementation-status.md).

## Release identity

| Field                              | Value   |
| ---------------------------------- | ------- |
| Candidate version                  | `0.1.0-a1` local A1 candidate |
| Source commit                      | `30889cdf55c01258559531f474b3ea40df8382fa` |
| Clean-tree proof                   | Local fresh-clone proof recorded; hosted proof pending |
| Build/toolchain identity           | Local A1 identity recorded; hosted proof pending |
| Image digest and SBOM              | Local image digest recorded; CI SBOM pending |
| Configuration/safety-manifest hash | Pending |
| Dataset/soak manifest              | Pending |
| Reference machine/load profile     | Pending |
| Qualification start/end UTC        | Pending |
| Product approver                   | Pending |
| Security approver                  | Pending |
| QA approver                        | Pending |
| SRE approver                       | Pending |

## Evidence rules

- Evidence must identify the source commit, exact command or procedure, UTC
  time, configuration/dataset hashes, tool versions, result, and retained
  artifact location.
- The pre-implementation A0 gate is the sole exception to the source-commit
  field: this repository has no baseline `HEAD`, so its review uses a dated,
  scoped SHA-256 manifest and records that limitation. A1 and every later gate
  require a committed source identity; the A0 exception cannot certify a build
  or release candidate.
- A summary, screenshot, source search, or narrow test cannot prove a broader
  gate. Long-running, restore, network, accessibility, and fault gates require
  their own primary evidence.
- `Implemented` does not mean `Verified`. A phase remains open until every owned
  requirement is `Verified`, dependencies are verified, limitations are current,
  and the gate review is signed.
- A0 verifies architecture, target, test-plan, ownership, and review artifacts.
  A0 never waits for a later binary, runtime metric, soak, restore drill, or UI
  test; those use the owning A1–A11 or release-gate requirement ID.
- Safety, accounting, deterministic replay, fencing/ownership, and fail-closed
  gates cannot be waived. A permitted non-safety waiver requires approver,
  rationale, expiry, and a tracked remediation item.
- Evidence containing credentials, session material, private payloads, database
  dumps, or market recordings must not be copied into this file.

## Phase summary

| Phase | Owner                           | Entry dependency                      | Gate status | Evidence bundle                    |
| ----- | ------------------------------- | ------------------------------------- | ----------- | ---------------------------------- |
| A0    | Product, Architecture, Security | Authoritative spec and plan available | Verified    | [A0 review](evidence/a0-review.md) |
| A1    | Platform Engineering            | A0 verified                           | In progress | Pending                            |
| A2    | Domain Engineering              | A1 verified                           | Not started | Pending                            |
| A3    | Runtime/Platform Engineering    | A2 verified                           | Not started | Pending                            |
| A4    | Storage/Accounting              | A3 verified                           | Not started | Pending                            |
| A5    | Security/SRE                    | A4 verified                           | Not started | Pending                            |
| A6    | Exchange Platform               | A5 verified                           | Not started | Pending                            |
| A7    | Binance Adapter Team            | A6 verified                           | Not started | Pending                            |
| A8    | Execution/Research Platform     | A7 verified                           | Not started | Pending                            |
| A9    | Portfolio/Risk Engineering      | A8 verified                           | Not started | Pending                            |
| A10   | Strategy/Research               | A9 verified                           | Not started | Pending                            |
| A11   | API/Frontend/Security           | A10 verified                          | Not started | Pending                            |

## A0

Requirements: all `AX-V1A-A00-*` IDs.

- [x] The clause-level source crosswalk proves complete Plan and Spec §31.2 V1A
      coverage with no coarse unmapped source item.
- [x] Mode/endpoint/credential/capability matrices receive security review.
- [x] Threat model and trust-boundary review closes every high-risk path.
- [x] Real-money-lock test plan covers source through runtime network behavior.
- [x] Topology, lifecycle, single-writer/fencing, recovery, and readiness designs
      are reviewed with no unresolved safety ambiguity.
- [x] Data classification, retention/deletion, backup, RPO/RTO, SLO, and risk
      policy reviews are approved.
- [x] Required ADR decisions are recorded and linked.
- [x] Independent reviewer signs the no-exchange-order-path conclusion.

Evidence register: [A0 architecture and safety review](evidence/a0-review.md).

## A1

Requirements: all `AX-V1A-A01-*` IDs. Entry: A0 verified.

- [ ] Clean builds prove the pinned Go/React toolchains and platform commands.
- [ ] Backend/frontend health applications start and dependency-aware readiness
      behaves truthfully.
- [ ] Every Compose profile renders with safe placeholders and no authenticated
      or real-trading service, setting, mount, or port.
- [ ] CI positive run and seeded-negative fixtures prove every governance gate.
- [ ] Runtime image is minimal, non-root, and contains the intended embedded UI.
- [ ] Setup/governance documentation passes a clean-machine walkthrough.

Evidence register: [immutable-candidate local A1 validation](evidence/a1-local-validation.md).
This is local evidence only; hosted CI, retained supply-chain artifacts, a
clean-machine walkthrough, and the reproducibility disposition remain open, so
the A1 gate remains open.

## A2

Requirements: all `AX-V1A-A02-*` IDs. Entry: A1 verified.

- [ ] Decimal/type API, arithmetic, serialization, overflow, precision, and
      rounding unit/property/fuzz suites pass.
- [ ] AST and schema scans prove no authoritative financial float use.
- [ ] Full configuration negative matrix fails closed before side effects.
- [ ] Immutable snapshot/hash golden reproduces the exact effective config.
- [ ] Configuration reference matches schema units, ranges, defaults, and safety
      semantics.

Evidence register: pending.

## A3

Requirements: all `AX-V1A-A03-*` IDs. Entry: A2 verified.

- [ ] Scheduler/concurrency permutations produce identical canonical results.
- [ ] Overlapping engine, stale-fence, lease-loss, and database-failure tests
      prove exclusive ownership and fail-closed mutation.
- [ ] Queue saturation loses no critical event and creates no stale decision.
- [ ] Race/stress/leak/load tests and graceful lifecycle timing pass.
- [ ] Architecture/recovery documents match the tested implementation.

Evidence register: pending.

## A4

Requirements: all `AX-V1A-A04-*` IDs. Entry: A3 verified.

- [ ] Migration, role, constraint, repository, locking, and generated-query gates
      pass on clean PostgreSQL.
- [ ] Every journal posting balances per commodity; projections rebuild exactly.
- [ ] High-contention reservations reject double spending and negative balances.
- [ ] Segment kill-point and compatibility tests detect/quarantine every unsafe
      file/manifest state.
- [ ] Inbox/outbox/job/journal/reservation kill-point matrix has no loss or
      duplicate effect.
- [ ] A timed clean restore reproduces balances, manifests, and replay hashes.

Evidence register: pending.

## A5

Requirements: all `AX-V1A-A05-*` IDs. Entry: A4 verified.

- [ ] Secret canaries are absent from logs, metrics, traces, APIs, audit, errors,
      and support artifacts.
- [ ] Metrics pass schema/unit/bounded-cardinality review and dashboards/rules
      validate against them.
- [ ] Persistence, fence, disk, clock, queue, book, reconciliation, and accounting
      faults alert and fail closed within SLO.
- [ ] Prometheus/Grafana profiles, in-app alerts, external sink, deduplication, and
      acknowledgement work end to end.
- [ ] Container hardening and supply-chain scan/SBOM evidence pass.
- [ ] Operations runbooks pass tabletop exercises.

Evidence register: pending.

## A6

Requirements: all `AX-V1A-A06-*` IDs. Entry: A5 verified.

- [ ] Shared contract/capability/error/retry/rate-budget suites pass.
- [ ] Emulator scenarios for gaps, reconnects, limits, filters, duplicates,
      partial/late fills, unknown state, reset, and reconciliation are repeatable.
- [ ] Unsupported functions return typed errors.
- [ ] Compile/build inspection proves no V1A signer, credential, private route, or
      authenticated order implementation.

Evidence register: pending.

## A7

Requirements: all `AX-V1A-A07-*` IDs. Entry: A6 verified.

- [ ] Binance fixture/emulator/public integration tests pass sequence bridging,
      gaps, reconnect, malformed/stale data, clock, and renewal scenarios.
- [ ] Manifest validator and replay prove raw/canonical linkage and explicit gaps.
- [ ] Continuous 72-hour declared-load soak meets freshness, resynchronization,
      latency, and bounded-memory SLOs with every incident recorded.
- [ ] Request/environment/image inspection proves public-only operation with no
      credentials or order capability.

Evidence register: pending; the 72-hour soak has not been run.

## A8

Requirements: all `AX-V1A-A08-*` IDs. Entry: A7 verified.

- [ ] Ten clean identical runs produce byte-identical events, decisions, balances,
      orders, and canonical result hashes.
- [ ] Temporal adversarial fixtures prove no look-ahead or impossible fill.
- [ ] Fill/fee/partial/cancel/dust/recovery scenarios balance exactly.
- [ ] Combined liquidity cannot be reused and confidence namespaces cannot mix.
- [ ] Kill-point/fault matrix resumes without duplicate order/fill or lost state.

Evidence register: pending.

## A9

Requirements: all `AX-V1A-A09-*` IDs. Entry: A8 verified.

- [ ] Exact 500-USDT Trend portfolio initialization and journal proof pass.
- [ ] Ownership/reservation/liquidity concurrency tests prove no double ownership.
- [ ] Every risk threshold and missing/stale input boundary test passes.
- [ ] Risk-state/intent, hysteresis, circuit-breaker, quarantine, and recovery
      model tests pass with audit/alert assertions.
- [ ] Startup/reconciliation/restart tests reproduce exact portfolio and journal
      state and block entries on unresolved state.

Evidence register: pending.

## A10

Requirements: all `AX-V1A-A10-*` IDs. Entry: A9 verified.

- [ ] Independent EMA/ATR/Trend golden datasets match documented precision.
- [ ] No-look-ahead, incomplete-candle, cooldown, ownership, sizing, stop, gap,
      rounding, and marketable-limit tests pass.
- [ ] Equivalent backtest/replay/paper/shadow inputs produce identical decisions.
- [ ] Registered research plan protects the untouched test and records all model
      and parameter-search provenance.
- [ ] Backtest/replay/shadow, stress, confidence-interval, stability, benchmark,
      and uncertainty evidence is reproducible and makes no profitability claim.

Evidence register: pending.

## A11

Requirements: all `AX-V1A-A11-*` IDs. Entry: A10 verified.

- [ ] OpenAPI generation and every listed endpoint contract pass.
- [ ] Authentication, session, CSRF/Origin, authorization, idempotency, pagination,
      durable job, audit, redaction, and quota tests pass.
- [ ] SSE snapshot/resume/race/duplicate/retention browser and server tests pass.
- [ ] Every required screen passes state-matrix, virtual-label, and responsive
      accessibility checks.
- [ ] Full Playwright workflow reaches health, backtest, replay, shadow, detailed
      evidence, and incident reproduction from clean state.
- [ ] Dependency/route/bundle/network inspection proves API/UI cannot enable real
      trading or bypass backend allocation/risk.

Evidence register: pending.

## V1A release gate

Requirements: all `AX-V1A-RG-*` IDs. Entry: A0–A11 verified.

- [ ] All phase requirements are `Verified`; evidence is current and no prohibited
      waiver or unresolved high-severity issue exists.
- [ ] Clean-build source/package/symbol/config/generated-contract/image/Compose/UI
      inspection proves every production/private/order/withdrawal/margin/leverage
      path absent.
- [ ] Captured release traffic proves Binance public-only destinations and zero
      signed/private/order requests.
- [ ] Binance books, recorder, replay, numeric SLOs, and 72-hour soak pass.
- [ ] No stale/gapped/degraded/invalid book can become a decision input.
- [ ] Journal, reservations, fencing, restart, reconciliation, and deterministic
      replay invariant suites pass, including ten identical hashes.
- [ ] Trend works end to end in backtest, replay, paper, and public-live shadow.
- [ ] Risk starts paused, fails closed, and cannot be bypassed.
- [ ] Core alerts/dashboards and clean backup/restore drill pass within SLO.
- [ ] `make verify`, long-running replay/load, crash/fencing, full Playwright,
      outbound-host inspection, and clean-tree checks pass.
- [ ] Documentation, limitations, assumptions, API, ADRs, runbooks, status, and
      traceability match the candidate.
- [ ] Release claims research/simulation only and separates platform readiness
      from strategy viability/profitability.

Release decision: **Not evaluated; all evidence pending.**

## Known current limitations

- The repository contains the A1 health/application skeleton only; all later
  V1A business capabilities remain unimplemented.
- A1 has no committed source identity or clean CI/supply-chain artifact run, so
  its gate remains unverified even though the local checks, image inspection,
  and Docker-backed runtime smoke pass.
- The A0 review is an independent Codex architecture/static audit, not an
  external human security assessment and not runtime or release certification.
- No targeted application, integration, race, fuzz, accessibility, recovery,
  network-capture, load, or end-to-end evidence exists here.
- The 72-hour Binance soak and clean backup/restore drill have not been executed.
- A passing documentation review alone cannot advance A1–A11 or the release gate.
