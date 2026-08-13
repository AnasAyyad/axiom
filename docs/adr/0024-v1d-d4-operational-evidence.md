# ADR-0024: V1D D4 operational evidence model

- **Status:** Accepted
- **Date:** 2026-08-04
- **Scope:** Reports, incidents, alerts, audit review, and evidence holds

## Context

D1 introduced authenticated, revisioned commands, immutable activity, and the
seven-day artifact lifecycle. D4 must use those authorities to make operational
work complete without adding a second command path, exposing private payloads,
or treating generated reports as release verdicts.

## Decision

D4 keeps all mutations behind the existing origin, CSRF, idempotency,
permission, expected-revision, reason, and high-risk reauthentication controls.
It adds read-optimized projections, while authoritative command, alert, audit,
job, incident, and artifact records remain durable in PostgreSQL.

Report schedules are strict UTC hourly, daily, or weekly definitions. A due
window deterministically creates at most one durable report job. Report
snapshots preserve their source revision and provenance before generation;
success stores hash-sealed JSON, and generation errors become a sanitized
terminal failure on both report and job.

Incident events form an immutable per-incident hash-linked timeline. Replay
links are accepted only for complete ordinal windows in a qualified
`decision_inputs` dataset. Resolution requires evidence. Incident and
reproduction holds validate their referenced source and continue to block
artifact deletion until a separately authorized release.

Alert routes expose only stable labels and enabled state. The webhook route is
disabled in storage until the runtime has an actual configured sink; endpoints
and credentials never enter the route projection. Delivery attempts keep
actual start/completion time and latency. Audit review verifies the immutable
sidecar sequence against the authoritative audit hashes and reports an explicit
valid or broken verdict.

## Consequences

- Reports distinguish strategy viability from platform readiness and do not
  prove profitability or enable real trading.
- Evidence bundles use the existing redacted TXT, CSV, JSON, and JSONL artifact
  service with a seven-day default expiry and audited access.
- Operators can manage incident, alert, and schedule workflows only within
  their granted permissions; owner reauthentication remains required for
  applying evidence holds.
- D4 owns no exchange client, credential, signed-request, or order-submission
  capability.
- D4 does not replace B2, C6, D5, or final D6 certification evidence.

## Validation

- OpenAPI/generated-type parity and route authorization tests.
- Clean PostgreSQL 18 install and exact D1-to-D4 upgrade qualification.
- Schedule boundary, report success/failure/provenance, alert delivery,
  incident replay, evidence hold, and audit tamper tests.
- Frontend typecheck, lint, unit, build, and Playwright operational workflows.
- Redaction, secret, outbound-boundary, and prohibited-capability scans.
