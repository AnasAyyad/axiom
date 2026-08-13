# V1D D2 readiness

## Current decision

**D2 implementation and local critical-workflow qualification are complete on
the feature branch; merge, hosted CI, and cumulative acceptance are pending.**

The branch turns the merged D1 API into one understandable command center with
six role-aware navigation groups, a persistent safety header, correlated
activity and evidence, separate strategy configuration/runtime control,
approved qualification and Run Lab workflows, scoped risk controls, and the
required operational pages. Existing A11, B8, and C6 routes remain available.

This is not formal V1D acceptance. D3 specialized labs, D4 reporting and
incident operations, D5 hardening and seven-day readiness soak, and D6 safety
certification remain later sequential phases. B2, C6, and D5 retain separate
verdicts; a D2 browser pass cannot replace any of them.

## Safety decision

- `REAL TRADING DISABLED` and shadow/virtual state remain visible on every
  authenticated route.
- The browser launches only registered product workflows and has no arbitrary
  command, script, or unit-test executor.
- D1 response validation, server authorization, idempotency, exact revisions,
  reasons, and purpose-bound high-risk grants remain authoritative.
- Strategy enablement is separate from runtime pause/resume; blocked
  prerequisites disable resume in the UI and remain server-enforced.
- Activity and exports expose redacted server projections and audited
  artifacts, never private payloads, headers, signatures, credentials, or
  arbitrary logs.
- Historical, replay, shadow, demo, and testnet outcomes are explicitly not
  profitability evidence.

## Browser decision

The D2 critical workflow passes Chromium desktop, Chromium tablet, Chromium
mobile, Firefox desktop, and WebKit desktop. The complete existing A11, B8,
C6, and D2 suite also passes 16 cases across the non-WebKit matrix. An older
A11-only WebKit cleanup step is flaky in the isolated browser image; D2's
WebKit workflow itself passes and hosted CI now owns the pinned five-project
D2 matrix.

## Evidence

Commands and limitations are recorded in
[D2 local validation](evidence/v1d-d2-local-validation.md). Requirement
coverage is recorded in
[D2 traceability](../requirements/v1d-d2-traceability.md) and
[D2 source coverage](../requirements/v1d-d2-source-coverage.md).
