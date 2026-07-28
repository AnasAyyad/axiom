# V1C PR1 source coverage

The authoritative requirements are the repository product specification and
the approved C1-C6 delivery plan. PR1 freezes only the C1-C3 foundation.
Requirement IDs resolve to
[`v1c-pr1-traceability.md`](v1c-pr1-traceability.md).

| Source area | PR1 requirement IDs | PR1 disposition | Deferred boundary |
|---|---|---|---|
| V1 safety contract: virtual spot-only, no production-private path, no prohibited capability, fail closed | `AX-V1C-C01-001` through `009` | implemented and locally qualified | formal security/owner acceptance on a frozen clean candidate |
| C1 authenticated boundary, credentials, endpoint/proxy policy, request evidence, identity checks, and emulation | `AX-V1C-C01-002` through `009` | implemented and locally qualified | complete Binance/Bybit operation adapters and real canaries in C4/C5 |
| C2 existing authentication controls, TOTP, one-use authorization, RBAC, audit, session control, and rotation | `AX-V1C-C02-001` through `008` | implemented and locally qualified | C6 HTTP endpoints and operator workflows |
| C3 durable approval/dispatch, caps, leases, inbox/reducer, paired legs, reset/unknown recovery, and locked startup | `AX-V1C-C03-001` through `014` | implemented and locally qualified | authenticated C4/C5 adapter operation completeness and canaries |
| Clean install, exact B8 upgrade, source/binary/image/Compose/security evidence | `AX-V1C-C03-014` plus C1 evidence rows | local source and dirty-image gates passed | rebuild and repeat artifact gates from a committed `DIRTY=false` candidate |
| Console, bounded resumable SSE, sandbox workflows, and console qualification | none in PR1 | intentionally absent | C6 |
| Concurrent 72-hour V1C qualification | none in PR1 | intentionally absent | C6 after C4/C5 canaries |

Historical A/B qualification is context only. It cannot qualify C1, C2, C3,
or any later V1C phase.
