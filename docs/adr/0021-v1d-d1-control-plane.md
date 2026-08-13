# ADR-0021: V1D D1 control-plane contracts

- **Status:** Accepted
- **Date:** 2026-08-03
- **Scope:** D1 API, authorization, activity, exports, and live updates

## Context

A11, B8, and C6 already provide authenticated `/api/v1` routes, durable jobs
and commands, optimistic revisions, resumable server-sent events, and a
password/TOTP one-use grant. D1 must complete the product API without creating
a second command engine or exposing exchange credentials and private payloads.

## Decision

D1 extends the existing console and PostgreSQL boundaries. Authoritative domain
tables remain the source of truth. An immutable activity projection stores only
allowlisted identity, status, correlation, and explanatory fields and links
back to its source identity and revision. A versioned reason catalogue supplies
plain-language presentation; unknown reasons use a safe generic fallback.

The authorization matrix has four primary roles: `researcher`, `operator`,
`auditor`, and `owner`. Existing `owner` remains the administrator. Existing
`viewer` is retained only as a deprecated read-only compatibility mapping.
Handlers authorize explicit permissions rather than role-name shortcuts.

Every mutation keeps the existing Origin, CSRF, idempotency, revision, reason,
audit, and durable-command boundaries. High-risk changes additionally consume
a password/TOTP one-use grant bound to the session, source, purpose, expected
revision, and reason.
One owner approval is sufficient; D1 does not add a two-person workflow.

Exports are generalized into seven-day artifacts supporting TXT, CSV, JSON,
and JSONL. Content is constructed from redacted projections, hash-sealed, and
audited. Deletion removes content while retaining a tombstone. Incident and
reproducibility holds block deletion.

Strategy configured state and runtime state are separate resources. Enabling
or resuming fails closed unless the service reports all required readiness
prerequisites. No contract can select `live`, a production-private endpoint,
an unowned-asset sell, or any prohibited V1 capability.

## Consequences

- D2-D4 can consume one typed, permission-filtered API and event vocabulary.
- Historical and live activity share stable reason and correlation identities.
- Export retention and evidence holds have one audited lifecycle.
- The API process still owns no authenticated exchange transport.

## Validation

- OpenAPI and generated-type parity.
- Authorization-matrix and password/TOTP grant tests.
- Idempotency, revision-conflict, prerequisite, quota, and migration tests.
- Activity backfill/idempotency and reason-fallback tests.
- Cursor-expiry/SSE reconnect and permission-filter tests.
- Export format, redaction, hash, retention, hold, and deletion tests.
- Prohibited-capability and secret scans.
