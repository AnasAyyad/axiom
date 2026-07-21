# V1B source-clause coverage

This crosswalk maps the approved V1B implementation plan and Specification
Section 31.3 to stable IDs in [the V1B matrix](v1b-traceability.md). Detailed
acceptance wording remains in the matrix.

| Source clause | Obligation | Requirement ID |
|---|---|---|
| Plan/setup-01 | Reconcile V1A merged status and preserve formal A7 dependency | AX-V1B-RG-QLT-001 |
| Plan/setup-02 | Create V1B requirements, checklist, readiness, ownership, migrations, limitations, and evidence locations | AX-V1B-RG-QLT-001 |
| Plan/B1-01; Spec 31.3/B1 | Credential-free Bybit public REST and WebSocket surface | AX-V1B-B01-FUN-001; AX-V1B-B01-SAF-001 |
| Plan/B1-02 | Common ticker/lifecycle contracts and exchange-neutral recording | AX-V1B-B01-FUN-002; AX-V1B-B01-FUN-004 |
| Plan/B1-03 | Snapshot replacement, delta insert/update/delete, and update ID 1 reset | AX-V1B-B01-FUN-003 |
| Plan/B1-04 | Raw-before-canonical and lifecycle/clock/gap evidence | AX-V1B-B01-FUN-004 |
| Plan/B1-05 | Three approved pairs, depth 1000, and 15m/1h/4h configuration | AX-V1B-B01-FUN-005 |
| Plan/B1-06 | Binance/Bybit/emulator conformance and deterministic faults | AX-V1B-B01-QLT-001 |
| Plan/B1-07 | Malformed, enum, reconnect, heartbeat, throttle, queue, linkage, recovery, endpoint, and credential qualification | AX-V1B-B01-QLT-001; AX-V1B-B01-SAF-001 |
| Plan/B1-08 | Short public validation and isolated 72-hour soak | AX-V1B-B01-OPS-001 |
| Plan/B2 | Coherent versioned views and Tier-A multi-exchange datasets | AX-V1B-B02-FUN-001; AX-V1B-B02-QLT-001 |
| Plan/B3 | Mean-reversion implementation and safety invariants | AX-V1B-B03-FUN-001; AX-V1B-B03-SAF-001 |
| Plan/B4 | Exact triangular evaluation, allocation, simulation, and recovery | AX-V1B-B04-FUN-001; AX-V1B-B04-SAF-001 |
| Plan/B5 | Cross-exchange arbitrage, owned inventory, and separated economics | AX-V1B-B05-FUN-001; AX-V1B-B05-SAF-001 |
| Plan/B6 | Deterministic advisory rebalancing without execution capability | AX-V1B-B06-FUN-001; AX-V1B-B06-SAF-001 |
| Plan/B7 | Honest multi-strategy statistics and audited promotion | AX-V1B-B07-FUN-001; AX-V1B-B07-SAF-001 |
| Plan/B8 | Generic API/console/SSE and simulation-only workflows | AX-V1B-B08-FUN-001; AX-V1B-B08-SAF-001 |
| Plan/release | Sequential gates, cumulative CI, database qualification, performance, rollout, and final safety rerun | AX-V1B-RG-QLT-001 |
