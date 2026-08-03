# V1D D3 traceability

Status distinguishes implementation and local verification from merge and
formal cumulative acceptance.

| ID | Requirement | Implementation | Verification |
|---|---|---|---|
| AX-V1D-D03-001 | Create approved backtest and replay runs without implying unsupported free-form execution inputs | guided immutable-identity form and existing typed job requests | request-validation, UI, and E2E tests |
| AX-V1D-D03-002 | Reopen, monitor, pause/resume/cancel where supported, and reproduce with exact revisions and durable identity | job projection capabilities plus D1 lab commands/history | transition, conflict, restart, race, and workflow tests |
| AX-V1D-D03-003 | Compare exact input, model, provenance, confidence, maturity, and result differences | two-resource comparison projection | pure comparison and browser tests |
| AX-V1D-D03-004 | Produce safe reproduction evidence and audited TXT/CSV/JSON/JSONL artifacts | run-manifest bundle and enriched D1 lab export | manifest/hash, redaction, and export tests |
| AX-V1D-D03-005 | Control replay speed, pause/resume/step, ordinal navigation, incident windows, checkpoints, and approved deterministic faults | replay contract, controller, fault scheduler, and D3 replay UI | deterministic replay/fault and E2E tests |
| AX-V1D-D03-006 | List and inspect public-data virtual shadow sessions, decisions, fills, inventory, P&L, risk, data health, and comparison | bounded shadow list and enriched session projection | storage projection, lifecycle, comparison, and E2E tests |
| AX-V1D-D03-007 | Separate strategy viability from platform readiness and state that lab/demo evidence does not prove profitability | result language and persistent lab safety note | content and browser tests |
| AX-V1D-D03-008 | Preserve V1 no-production-order, spot-only, owned-inventory, fail-closed, secret, and redaction boundaries | closed API inputs and existing execution/storage boundaries | source-boundary, secret, and prohibited-capability scans |
