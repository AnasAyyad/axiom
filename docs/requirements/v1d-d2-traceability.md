# V1D D2 traceability

Status distinguishes implementation and local browser qualification from
merged or formally accepted release evidence.

| ID | Requirement | Implementation | Verification |
|---|---|---|---|
| AX-V1D-D02-001 | Group authenticated navigation into Home, Activity, Strategies, Run Lab, Risk & Controls, and Operations with role-aware links | D2 shell navigation model and permission projection | role matrix, direct-route, keyboard, and E2E tests |
| AX-V1D-D02-002 | Keep environment, mode, exchange, engine, risk, freshness, live state, and `REAL TRADING DISABLED` visible on every route | persistent D2 safety header | route matrix, responsive, and accessibility tests |
| AX-V1D-D02-003 | Present plain status, reason, impact, and action before expandable IDs and redacted evidence | shared reason, status, and evidence components | component, unknown-reason, and screen-reader tests |
| AX-V1D-D02-004 | Provide filtered Decisions & Orders and restricted System Events with correlation navigation and audited export controls | activity feature and D1 activity/export client | filter, permission, detail-chain, export, and E2E tests |
| AX-V1D-D02-005 | Show complete strategy purpose, version, maturity, modes, confidence, viability, readiness, configured/runtime state, blockers, decisions, provenance, and authorized controls | Strategy Center feature and exact-revision command panels | role, reauthentication, validation, conflict, and fail-closed tests |
| AX-V1D-D02-006 | Provide Qualification Center preflight, start, progress, abort, evidence, and verdict workflows with owner/operator separation | qualification feature and D1 command client | role, reauthentication, abort, recovery, and E2E tests |
| AX-V1D-D02-007 | Expose all required operational screens and only approved test/run launchers; arbitrary browser command execution is absent | grouped operational hubs, existing A11/B8/C6 pages, approved Run Lab | route coverage and prohibited-surface tests |
| AX-V1D-D02-008 | Render loading, empty, stale, reconnecting, partial, permission, validation, and server-error states | shared query-state and page-state boundaries | unit, mocked-server, offline, and E2E tests |
| AX-V1D-D02-009 | Meet WCAG 2.2 AA critical workflows and desktop/tablet/mobile browser compatibility without horizontal page overflow | accessible primitives, responsive CSS, Playwright projects | axe, keyboard, labels, browser matrix, and viewport tests |
| AX-V1D-D02-010 | Preserve all V1 safety and secret boundaries and separate profitability claims from readiness/qualification evidence | persistent safety copy, capability guards, server-redacted artifacts | secret, prohibited-capability, copy, and browser-network tests |
