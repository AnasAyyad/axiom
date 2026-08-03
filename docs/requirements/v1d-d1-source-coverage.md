# V1D D1 source coverage

The product specification and approved V1D delivery plan are authoritative.
Requirement IDs resolve to
[`v1d-d1-traceability.md`](v1d-d1-traceability.md).

| Source area | D1 requirement IDs | D1 disposition | Later gate |
|---|---|---|---|
| Complete Section 24 resource and collection contract | `AX-V1D-D01-001`, `002` | implemented and locally qualified on the D1 branch | merge and cumulative acceptance |
| Durable mutations, role matrix, and high-risk reauthentication | `AX-V1D-D01-003` through `006` | implemented and locally qualified on the D1 branch | owner/security review |
| Activity projection and reason catalogue | `AX-V1D-D01-007`, `008` | implemented with deterministic upgrade backfill | production-volume observation in D5 |
| General exports, retention, and holds | `AX-V1D-D01-009` | implemented; seven-day policy automated | D4 evidence-bundle integration and D5 lifecycle soak |
| Typed resumable permission-filtered stream | `AX-V1D-D01-010` | implemented on the existing SSE boundary | D2 browser reconnect workflows |
| Secret and prohibited-capability boundary | `AX-V1D-D01-011`, `012` | closed and locally scanned | clean-image inspection in D5/D6 |

C6 remains a separate formal 72-hour sandbox qualification. D1 may read its
evidence but neither runs nor replaces it. D5's seven-day readiness verdict and
B2's market-data verdict also remain separate.
