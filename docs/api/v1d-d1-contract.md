# V1D D1 API contract

`api/openapi.yaml` is authoritative. D1 extends the existing compatible
`/api/v1` surface; it does not remove A11, B8, or C6 aliases.

## Resource groups

- assets and strategy detail/version/configuration/runtime state
- scoped risk controls
- decisions and orders activity, orders, and fills
- alerts, reports, exports, incidents, and audit evidence
- configuration revisions
- backtest, replay, shadow, qualification, and drill lifecycle
- user role administration
- durable command lookup

Collections use a maximum `page_size` of 200, opaque signed cursors,
deterministic descending time/revision/identity ordering, and explicit UTC
`from`/`to` bounds where time applies. Invalid, tampered, or expired cursors
fail closed. Financial values and revisions remain decimal strings.

## Command envelope

Every mutation requires `Idempotency-Key`, an expected revision, and an audit
reason. A successful request returns a durable command ID and lifecycle state.
High-risk commands also carry a one-use password/TOTP authorization token for
the exact purpose, session, source, expected revision, and reason.

## Activity and reasons

Activity has `decisions_orders` and `system_events` views. Each row has a source
identity/revision, stable reason code, safe summary/explanation/action, severity,
UTC time, correlation ID, and links to authoritative resources. Technical
details contain allowlisted fields only.

## Export artifacts

TXT, CSV, JSON, and JSONL artifacts contain redacted projections only. Each
artifact is linked to the guarded durable-job lifecycle and exposes its job ID,
SHA-256 hash, creation/expiry time, hold state, and audit identity. Downloads
and deletion are audited. Deletion is blocked by an active incident or
reproducibility hold.

## Safety boundary

The API has no production-private endpoint, arbitrary URL, raw log, credential,
signature, transfer, withdrawal, leverage, derivative, short, or production
order operation. Real trading remains impossible.
