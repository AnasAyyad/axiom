# V1A phase checklist

Work proceeds A0 through A11; a later phase does not waive an earlier gate.
A0 gate items and the A1 entry prerequisite are checked because the dated A0
review evidence is registered. Requirement details and stable IDs are in
[the canonical matrix](../requirements/traceability.md); evidence is registered
in [V1A readiness](v1a-readiness.md). Clause-level plan coverage is in
[the source crosswalk](../requirements/source-coverage.md).

## A0 — scope and safety architecture

- [x] Verify all `AX-V1A-A00-*` requirements.
- [x] Confirm A0 rows require design/target/test-plan review only; all measured
      runtime proof is owned by A1–A11 or `RG` IDs.
- [x] Approve mode/endpoint policy, threat model, real-money-lock plan, topology,
      lifecycle, fencing, recovery, data policy, SLO/RPO/RTO, and risk review.
- [x] Close every ambiguity affecting production orders, ownership, accounting,
      replay, credentials, or failure behavior.
- [x] Record independent no-order-path safety sign-off.

## A1 — repository, toolchain, Compose, and CI

- [x] Confirm A0 is verified.
- [ ] Verify all `AX-V1A-A01-*` requirements.
- [ ] Start all minimal processes and health applications locally.
- [ ] Render every Compose profile and prove no authenticated/real-trading path.
- [ ] Pass positive CI and every seeded-negative governance fixture.

## A2 — financial domain and configuration

- [ ] Confirm A1 is verified.
- [ ] Verify all `AX-V1A-A02-*` requirements.
- [ ] Pass arithmetic/rounding property and fuzz tests and financial-float scans.
- [ ] Pass the full fail-closed configuration matrix and reproduction golden.

## A3 — deterministic runtime and fencing

- [ ] Confirm A2 is verified.
- [ ] Verify all `AX-V1A-A03-*` requirements.
- [ ] Prove schedule-independent results and exclusive fenced ownership.
- [ ] Pass overload, lease-loss, race, stress, leak, and shutdown tests.

## A4 — storage, journal, Parquet, and recovery

- [ ] Confirm A3 is verified.
- [ ] Verify all `AX-V1A-A04-*` requirements.
- [ ] Prove exact per-asset journal balance and exclusive reservations.
- [ ] Pass migration, segment, kill-point, projection-rebuild, and idempotency tests.
- [ ] Complete a clean, timed, verifiable backup/restore drill.

## A5 — security and observability

- [ ] Confirm A4 is verified.
- [ ] Verify all `AX-V1A-A05-*` requirements.
- [ ] Pass redaction canaries, metric-cardinality, alert/fail-closed, and health tests.
- [ ] Validate dashboards, external alert sink, hardened image, scans, and runbooks.

## A6 — exchange contracts and emulator

- [ ] Confirm A5 is verified.
- [ ] Verify all `AX-V1A-A06-*` requirements.
- [ ] Pass shared contract/capability/error/rate/retry suites.
- [ ] Reproduce every emulator fault scenario deterministically.
- [ ] Prove no signer, credentials, private route, or order implementation exists.

## A7 — Binance public data and recorder

- [ ] Confirm A6 is verified.
- [ ] Verify all `AX-V1A-A07-*` requirements.
- [ ] Pass public adapter sequence, reconnect, time, malformed/stale, and manifest tests.
- [ ] Complete the 72-hour declared-load soak with bounded memory and all incidents.
- [ ] Prove the process and image are public-only and credential-free.

## A8 — backtest, replay, simulation, and orders

- [ ] Confirm A7 is verified.
- [ ] Verify all `AX-V1A-A08-*` requirements.
- [ ] Produce ten byte-identical canonical run results.
- [ ] Pass no-look-ahead, fill/fee/dust/recovery, liquidity, and namespace tests.
- [ ] Pass every checkpoint/fault/kill-point restart case without duplication/loss.

## A9 — portfolio, risk, reconciliation, and recovery

- [ ] Confirm A8 is verified.
- [ ] Verify all `AX-V1A-A09-*` requirements.
- [ ] Prove the exact 500-USDT Trend initialization and exclusive ownership.
- [ ] Pass all risk thresholds, state/intent, circuit-breaker, and recovery models.
- [ ] Prove unresolved state blocks entries and restart state is identical.

## A10 — Trend strategy

- [ ] Confirm A9 is verified.
- [ ] Verify all `AX-V1A-A10-*` requirements.
- [ ] Match independent indicator/strategy goldens and prove no look-ahead.
- [ ] Prove cross-mode deterministic decisions.
- [ ] Complete registered, reproducible validation without a profitability claim.

## A11 — API, authentication, UI, and shadow workflow

- [ ] Confirm A10 is verified.
- [ ] Verify all `AX-V1A-A11-*` requirements.
- [ ] Pass OpenAPI, endpoint, auth, CSRF, authorization, idempotency, job, and SSE tests.
- [ ] Pass UI state, virtual-label, WCAG 2.2 AA, and responsive checks.
- [ ] Complete the full clean-state Playwright workflow through incident replay.
- [ ] Prove API/UI cannot enable real trading or bypass allocation/risk.

## Release decision

- [ ] Confirm A0–A11 are verified in order.
- [ ] Verify every `AX-V1A-RG-*` requirement.
- [ ] Record candidate identity, immutable evidence, approvers, limitations, and risks.
- [ ] Approve V1A release only after every item in the readiness release gate passes.
