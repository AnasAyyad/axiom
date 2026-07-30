# V1C PR3 source coverage

The product specification and approved C1-C6 delivery plan are authoritative.
Requirement IDs resolve to
[`v1c-pr3-traceability.md`](v1c-pr3-traceability.md).

| Source area | PR3 requirement IDs | Current disposition | External or later gate |
|---|---|---|---|
| V1 safety: virtual Spot only, default off, fixed test/demo environments, closed production/private and prohibited capabilities | `AX-V1C-C06-004`, `005`, `011`, `AX-V1C-PR3-002` | implemented; non-soak source, compiled binary, and all 1,024 Compose gates pass | clean image boundary and formal owner/security acceptance |
| Redacted API projections and administrative control plane | `AX-V1C-C06-001` through `007` | implemented; targeted and complete aggregate API/auth/storage tests pass | clean artifact and owner/security acceptance |
| Accessible responsive sandbox console and guarded controls | `AX-V1C-C06-008` | implemented; React/axe, desktop/mobile C6 fixture, and complete unmocked browser workflows pass | clean artifact |
| Bounded metrics, alerts, dashboard, and incident response | `AX-V1C-C06-009` | implemented; bounded label tests and complete repository verification pass | image-backed review |
| Default-off formal runner, redacted immutable evidence, dedicated role, and smoke denial | `AX-V1C-C06-010`, `011`, `AX-V1C-PR3-001` | implemented; deterministic unit/storage gates pass | later exact 72-hour run and evidence review |
| Deterministic chaos/failure coverage | `AX-V1C-C06-012` | implemented; the closed C6 set, cumulative emulator/reducer/recovery suites, race, fuzz, and aggregate pass | none |
| Clean installation, exact B8 upgrade, cumulative C1-C5, Compose, and clean artifact gates | `AX-V1C-PR3-001`, `002` | database, cumulative, Compose, and repository gates pass; exact-source artifact pending | none after successful artifact record |
| Formal C6 verdict and V1C decision | `AX-V1C-PR3-003` | intentionally not claimed | exact-source 72-hour run, evidence review, owner/security acceptance |

PR1 and PR2 requirements remain cumulative and are re-exercised by
`v1c-pr3-local-qualify`. Existing canaries are prerequisites and historical
evidence only; PR3 does not rerun or replace them.
