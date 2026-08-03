# V1D D1 traceability

Status distinguishes implementation and local qualification from merged or
formally accepted release evidence.

| ID | Requirement | Implementation | Verification |
|---|---|---|---|
| AX-V1D-D01-001 | Complete compatible `/api/v1` resources for assets, strategies, scoped risk, activity, orders/fills, alerts, reports, exports, incidents, configuration, labs, qualifications, and roles | OpenAPI D1 paths, generated Go/TypeScript types, console D1 handlers and PostgreSQL service | contract parity, handler, validation, and compatibility tests |
| AX-V1D-D01-002 | Every collection is bounded, cursor-paginated, deterministically ordered, and supports applicable time and stable filters | common D1 list query/cursor contract and storage queries | invalid-bound, cursor, ordering, filter, and load tests |
| AX-V1D-D01-003 | Every mutation is Origin/CSRF protected, idempotent, revision checked, reason bound, audited, and returns a durable command | existing middleware plus D1 command service | replay, conflict, revision, audit, and failure-path tests |
| AX-V1D-D01-004 | Researcher, operator, auditor, and owner permissions are explicit; viewer is deprecated read-only compatibility | migration `000025`, authentication projection, role administration API | complete authorization-matrix tests |
| AX-V1D-D01-005 | High-risk owner changes require password/TOTP reauthentication and one-use exact-purpose/revision/reason proof | generalized V1C authorization grant and D1 handlers | bad password/TOTP, expiry, replay, binding, role, and revision tests |
| AX-V1D-D01-006 | Strategy configured state is separate from runtime state and enable/resume fails closed on any unmet prerequisite | D1 strategy control state and command validation | prerequisite matrix, idempotency, conflict, and audit tests |
| AX-V1D-D01-007 | Immutable activity projection is linked to authoritative sources, idempotent by source identity/revision, and deterministically backfilled | migration `000025`, activity projection trigger/backfill, D1 reads | clean/upgrade, duplicate, revision, ordering, and linkage tests |
| AX-V1D-D01-008 | Versioned reason catalogue provides plain explanations/actions and a safe unknown fallback | D1 reason catalogue and presentation service | catalogue uniqueness, fallback, sanitization, and compatibility tests |
| AX-V1D-D01-009 | TXT/CSV/JSON/JSONL artifacts are redacted, hash-sealed, audited, seven-day retained, and hold aware | D1 export artifact store and endpoints | format, redaction, hash, retention, hold, deletion, and quota tests |
| AX-V1D-D01-010 | SSE adds typed permission-filtered D1 events and explicitly requires a fresh snapshot after retention expiry | existing stream store plus D1 stream vocabulary/filter | reconnect, gap, expired cursor, quota, and permission tests |
| AX-V1D-D01-011 | API/activity/export surfaces never expose secrets, signatures, auth headers, arbitrary logs, or private exchange payloads | allowlisted projection/export encoders and closed API services | negative fixtures, secret scan, request capture, and source boundary checks |
| AX-V1D-D01-012 | D1 adds no production broker, production-private route, transfer, withdrawal, leverage, derivatives, short, or unowned sell capability | compiled policy, OpenAPI, service boundaries, and prohibited-capability checker | source/binary/image absence and negative API tests |
