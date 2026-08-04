# V1D D4 traceability

Status distinguishes implementation and local verification from merge and
formal cumulative acceptance.

| ID | Requirement | Implementation | Verification |
|---|---|---|---|
| AX-V1D-D04-001 | Create all approved report types on demand and from deterministic UTC schedules | durable report definitions, schedules, jobs, worker, and provenance projection | schedule-boundary, deduplication, worker, API, and browser tests |
| AX-V1D-D04-002 | Preserve mode, confidence, valuation/model provenance, maturity, source identity, generation time, revision, and content hash | immutable report projection and safe export record | provenance completeness, hash, revision-conflict, and redaction tests |
| AX-V1D-D04-003 | Operate incidents with owner, severity, immutable timeline, related alerts/activity, remediation, and resolution evidence | revisioned incident commands and hash-linked incident events | transition, authorization, linkage, resolution-precondition, and browser tests |
| AX-V1D-D04-004 | Link complete replay inputs when qualified data exists and expose the missing-input condition plainly | explicit incident replay-input projection with qualified-data fallback | incident-to-replay linkage and missing-input tests |
| AX-V1D-D04-005 | Route, acknowledge, escalate, retry, and test alerts without exposing route secrets | sanitized route/delivery projection and immutable delivery attempts | delivery SLO, retry/deduplication, test-delivery, and secret-scan tests |
| AX-V1D-D04-006 | Review authentication, controls, exports, configuration, qualifications, incidents, alerts, and evidence access with tamper detection | unified redacted audit projection and hash-link verification verdict | category-coverage and broken-chain tests |
| AX-V1D-D04-007 | Create downloadable incident evidence bundles through the seven-day audited artifact lifecycle | incident bundle command over D1 hash-sealed TXT/CSV/JSON/JSONL artifacts | format, hash, download-audit, expiry, and redaction tests |
| AX-V1D-D04-008 | Prevent deletion of incident/reproduction evidence while an active, validated hold exists | reference validation and storage-boundary hold enforcement | invalid-reference, held-delete, expiry, and retention-enforcement tests |
| AX-V1D-D04-009 | Preserve V1 production-order lockout, spot-only, owned-inventory, fail-closed, and secret boundaries | closed D4 inputs and read-only/reporting operational services | prohibited-capability, authorization, secret, and outbound-boundary scans |
