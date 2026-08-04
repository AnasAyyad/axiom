# V1D D4 source coverage

The product specification and approved sequential V1D plan are authoritative.
Requirement IDs resolve to
[`v1d-d4-traceability.md`](v1d-d4-traceability.md).

| Source area | D4 requirement IDs | D4 disposition | Later gate |
|---|---|---|---|
| Scheduled/on-demand reports and provenance | `AX-V1D-D04-001`, `002` | durable UTC schedules and immutable report evidence | D5 declared-load soak and D6 review |
| Incident lifecycle, replay linkage, and evidence | `AX-V1D-D04-003`, `004`, `007` | revisioned operations and audited bundles | D5 incident drills and D6 evidence review |
| Alert routing, acknowledgement, escalation, delivery, and tests | `AX-V1D-D04-005` | sanitized durable route and attempt workflow | D5 delivery SLO qualification |
| Audit review and tamper detection | `AX-V1D-D04-006` | unified categories and explicit chain verdict | D6 independent security review |
| Artifact retention and holds | `AX-V1D-D04-007`, `008` | seven-day hash-sealed artifacts with validated holds | D5 lifecycle automation |
| V1 safety boundaries | `AX-V1D-D04-009` | no production-private order capability added | D6 independent safety proof |

D4 does not complete D5 hardening/seven-day soak or D6 certification. B2, C6,
and D5 remain separate formal evidence gates.
