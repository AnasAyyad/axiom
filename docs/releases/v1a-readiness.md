# V1A readiness and release evidence

## Current decision

**V1A is not ready for release.** A0-A6 are verified; A7-A11 and every release
gate remain unverified. This document is an evidence index, not evidence
by itself. An unchecked item must not be inferred from source layout, planned
work, or a passing narrower test.

Normative requirement details live in
[requirements/traceability.md](../requirements/traceability.md). The execution
plan crosswalk is [requirements/source-coverage.md](../requirements/source-coverage.md),
the execution checklist is [v1a-phase-checklist.md](v1a-phase-checklist.md), and
current phase status is [implementation-status.md](../implementation-status.md).

## Release identity

| Field                              | Value                                                                                                             |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Candidate version                  | `0.1.0-a6` local phase candidate                                                                                  |
| Source commit                      | `6bbda3029031c8226bfaaba3478ecba958176a2a` plus the following evidence-only commit                                |
| Clean-tree proof                   | Local predecessor fresh-checkout proof plus owner-verified clean hosted run for current commit                    |
| Build/toolchain identity           | Local predecessor identity plus owner-verified hosted run `#4` for current commit                                 |
| Image digest and SBOM              | Local predecessor digests; current hosted image/SBOM artifacts owner-verified, hashes not independently inspected |
| Configuration/safety-manifest hash | Pending                                                                                                           |
| Dataset/soak manifest              | Pending                                                                                                           |
| Reference machine/load profile     | Pending                                                                                                           |
| Qualification start/end UTC        | Pending                                                                                                           |
| Product approver                   | Pending                                                                                                           |
| Security approver                  | Pending                                                                                                           |
| QA approver                        | Pending                                                                                                           |
| SRE approver                       | Pending                                                                                                           |

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

| Phase | Owner                           | Entry dependency                      | Gate status | Evidence bundle                                                                                                                                                                  |
| ----- | ------------------------------- | ------------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A0    | Product, Architecture, Security | Authoritative spec and plan available | Verified    | [A0 review](evidence/a0-review.md)                                                                                                                                               |
| A1    | Platform Engineering            | A0 verified                           | Verified    | [Local validation](evidence/a1-local-validation.md); [owner-verified hosted CI](evidence/a1-hosted-ci.md); [clean-machine walkthrough](evidence/a1-clean-machine-walkthrough.md) |
| A2    | Domain Engineering              | A1 verified                           | Verified    | [Local validation](evidence/a2-local-validation.md)                                                                                                                              |
| A3    | Runtime/Platform Engineering    | A2 verified                           | Verified    | [Local validation](evidence/a3-local-validation.md)                                                                                                                              |
| A4    | Storage/Accounting              | A3 verified                           | Verified    | [Local validation](evidence/a4-local-progress.md)                                                                                                                                |
| A5    | Security/SRE                    | A4 verified                           | Verified    | [Completion evidence](evidence/a5-local-progress.md)                                                                                                                             |
| A6    | Exchange Platform               | A5 verified                           | Verified    | [Local validation](evidence/a6-local-validation.md)                                                                                                                              |
| A7    | Binance Adapter Team            | A6 verified                           | Not started | Pending                                                                                                                                                                          |
| A8    | Execution/Research Platform     | A7 verified                           | Implemented | [Local validation](evidence/a8-local-validation.md); merged into `main`, formal entry gate remains open                                                                          |
| A9    | Portfolio/Risk Engineering      | A8 verified                           | Implemented | [Local validation](evidence/a9-local-validation.md); merged into `main`, formal entry gate remains open                                                                          |
| A10   | Strategy/Research               | A9 verified                           | Implemented | [Local validation](evidence/a10-local-validation.md); merged into `main`, formal entry gate remains open                                                                         |
| A11   | API/Frontend/Security           | A10 verified                          | Implemented | [Local validation](evidence/a11-local-validation.md); merged into `main`, formal entry gate remains open                                                                         |

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

Requirements: every phase-local `AX-V1A-A01-*` ID. Entry: A0 verified. The
program-wide documentation row `AX-V1A-A01-QLT-013` remains continuously owned
through later phases and the V1A release gate; it is not waived by this phase
decision.

- [x] Clean builds prove the pinned Go/React toolchains and platform commands.
- [x] Backend/frontend health applications start and dependency-aware readiness
      behaves truthfully.
- [x] Every Compose profile renders with safe placeholders and no authenticated
      or real-trading service, setting, mount, or port.
- [x] CI positive run and seeded-negative fixtures prove every governance gate.
- [x] Runtime image is minimal, non-root, and contains the intended embedded UI.
- [x] Setup/governance documentation passes a clean-machine walkthrough.

Evidence register: [immutable-candidate local A1 validation](evidence/a1-local-validation.md)
and [owner-verified hosted CI](evidence/a1-hosted-ci.md). Hosted run `#4` and
its retained supply-chain artifacts are recorded for the current commit. The
[clean-machine setup/governance walkthrough](evidence/a1-clean-machine-walkthrough.md)
also passes for that candidate. A1 is verified.

## A2

Requirements: all `AX-V1A-A02-*` IDs. Entry: A1 verified.

- [x] Decimal/type API, arithmetic, serialization, overflow, precision, and
      rounding unit/property/fuzz suites pass.
- [x] AST and schema scans prove no authoritative financial float use.
- [x] Full configuration negative matrix fails closed before side effects.
- [x] Immutable snapshot/hash golden reproduces the exact effective config.
- [x] Configuration reference matches schema units, ranges, defaults, and safety
      semantics.

Evidence register: [A2 local phase-gate validation](evidence/a2-local-validation.md).
A2 is verified locally; pushes and hosted CI are owner-managed and are not
claimed by this record.

## A3

Requirements: all `AX-V1A-A03-*` IDs. Entry: A2 verified.

- [x] Scheduler/concurrency permutations produce identical canonical results.
- [x] Overlapping engine, stale-fence, lease-loss, and database-failure tests
      prove exclusive ownership and fail-closed mutation.
- [x] Queue saturation loses no critical event and creates no stale decision.
- [x] Race/stress/leak/load tests and graceful lifecycle timing pass.
- [x] Architecture/recovery documents match the tested implementation.

Evidence register: [A3 local phase-gate validation](evidence/a3-local-validation.md).
A3 is verified for its runtime-contract scope. PostgreSQL durability remains an
A4 gate, and the complete integrated shadow recovery profile is requalified at
the V1A release gate.

## A4

Requirements: all `AX-V1A-A04-*` IDs. Entry: A3 verified.

- [x] Migration, role, constraint, repository, locking, and generated-query gates
      pass on clean PostgreSQL.
- [x] Every journal posting balances per commodity; projections rebuild exactly.
- [x] High-contention reservations reject double spending and negative balances.
- [x] Segment kill-point and compatibility tests detect/quarantine every unsafe
      file/manifest state.
- [x] Inbox/outbox/job/journal/reservation kill-point matrix has no loss or
      duplicate effect.
- [x] A timed clean restore reproduces balances, manifests, and replay hashes.

Validation record: [A4 local validation](evidence/a4-local-progress.md).
Recorder-derived capacity and daily/off-host operational evidence remain later
deployment/release gates and do not weaken this local A4 acceptance.

## A5

Requirements: all `AX-V1A-A05-*` IDs. Entry: A4 verified.

- [x] Secret canaries are absent from logs, metrics, traces, APIs, audit, errors,
      and support artifacts.
- [x] Metrics pass schema/unit/bounded-cardinality review and dashboards/rules
      validate against them.
- [x] Persistence, fence, disk, clock, queue, book, reconciliation, and accounting
      faults alert and fail closed within SLO.
- [x] Prometheus/Grafana profiles, in-app alerts, external sink, deduplication, and
      acknowledgement work end to end.
- [x] Container hardening and supply-chain scan/SBOM evidence pass.
- [x] Operations runbooks pass tabletop exercises.

Evidence register: [A5 completion evidence](evidence/a5-local-progress.md) and
[operations tabletop](evidence/a5-tabletop.md). Docker, supply-chain,
exact-tree PostgreSQL, alert-objective, and tabletop qualification passed.

## A6

Requirements: all `AX-V1A-A06-*` IDs. Entry: A5 verified.

- [x] Shared contract/capability/error/retry/rate-budget suites pass.
- [x] Emulator scenarios for gaps, reconnects, limits, filters, duplicates,
      partial/late fills, unknown state, reset, and reconciliation are repeatable.
- [x] Unsupported functions return typed errors.
- [x] Compile/build inspection proves no V1A signer, credential, private route, or
      authenticated order implementation.

Evidence register: [A6 local phase-gate validation](evidence/a6-local-validation.md).

## A7

Requirements: all `AX-V1A-A07-*` IDs. Entry: A6 verified.

- [x] Binance fixture/emulator/public integration tests pass sequence bridging,
      gaps, reconnect, malformed/stale data, clock, and renewal scenarios.
- [x] Manifest validator and bounded replay prove raw/canonical linkage and explicit gaps.
- [ ] Continuous 72-hour declared-load soak meets freshness, resynchronization,
      latency, and bounded-memory SLOs with every incident recorded.
- [ ] Request/environment/image inspection proves public-only operation with no
      credentials or order capability.

Evidence register: [A7 local validation](evidence/a7-local-validation.md); the
72-hour soak is pending and therefore A7 is not verified.

## A8

Requirements: all `AX-V1A-A08-*` IDs. Entry: A7 verified.

- [ ] Ten clean identical runs produce byte-identical events, decisions, balances,
      orders, and canonical result hashes.
- [ ] Temporal adversarial fixtures prove no look-ahead or impossible fill.
- [ ] Fill/fee/partial/cancel/dust/recovery scenarios balance exactly.
- [ ] Combined liquidity cannot be reused and confidence namespaces cannot mix.
- [ ] Kill-point/fault matrix resumes without duplicate order/fill or lost state.

Evidence register: [A8 local validation](evidence/a8-local-validation.md).

## A9

Requirements: all `AX-V1A-A09-*` IDs. Entry: A8 verified.

- [ ] Exact 500-USDT Trend portfolio initialization and journal proof pass.
- [ ] Ownership/reservation/liquidity concurrency tests prove no double ownership.
- [ ] Every risk threshold and missing/stale input boundary test passes.
- [ ] Risk-state/intent, hysteresis, circuit-breaker, quarantine, and recovery
      model tests pass with audit/alert assertions.
- [ ] Startup/reconciliation/restart tests reproduce exact portfolio and journal
      state and block entries on unresolved state.

Evidence register: [A9 local validation](evidence/a9-local-validation.md).

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

Evidence register: [A10 local validation](evidence/a10-local-validation.md).
Formal acceptance remains pending; the unchecked items above are not
pre-approved by local implementation evidence.

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

Evidence register: [A11 local implementation evidence](evidence/a11-local-validation.md).

All six A11 behaviors above have passing local implementation evidence from a
clean PostgreSQL 18 and unmocked browser qualification plus full repository,
image, and Compose gates. The checkboxes remain open because the normative A10
entry gate and prerequisite-ordered formal A7-A10 acceptance are not verified;
local qualification does not pre-approve formal A11 acceptance.

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

- The repository contains verified A1-A6 foundations, the implemented A7
  production-public Binance collector/recorder, and locally implemented A8-A11
  candidates. A7 remains gated on continuous soak and final inspection; formal
  acceptance of the stacked A8-A11 candidates remains prerequisite-ordered.
- A1 has a committed source identity, owner-verified hosted CI/supply-chain
  artifacts, local immutable-candidate evidence, and a completed clean-machine
  setup/governance walkthrough.
- The A0 review is an independent Codex architecture/static audit, not an
  external human security assessment and not runtime or release certification.
- A7 targeted unit, emulator, race, public-network, recorder-role, and short
  qualification evidence exists. A11 has current PostgreSQL 18,
  desktop/mobile Playwright, image, and image-backed Compose evidence; it is
  local implementation evidence, not formal phase acceptance.
- The 72-hour Binance soak and clean backup/restore drill have not been executed.
- A passing documentation review alone cannot advance A1–A11 or the release gate.
