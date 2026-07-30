# V1C PR2 source coverage

The product specification and approved C1-C6 delivery plan are authoritative.
PR2 extends the merged C1-C3 foundation with C4/C5 and its runtime/canary
boundary. Requirement IDs resolve to
[`v1c-pr2-traceability.md`](v1c-pr2-traceability.md).

| Source area | PR2 requirement IDs | Current disposition | External or later gate |
|---|---|---|---|
| V1 safety: virtual Spot only, fail closed, no production-private or executable prohibited capability; ADR-0022 records the exact Bybit Demo provider-key bundle exception | `AX-V1C-C04-001`, `AX-V1C-C05-001`, `AX-V1C-C05-002`, `AX-V1C-C05-005`, `AX-V1C-PR2-001` | implementation updated; affected qualification pending | owner/security acceptance |
| Binance Spot Testnet completeness, private stream, ambiguity recovery, rate capacity, and reset handling | `AX-V1C-C04-001` through `006` | implemented and deterministic gates passed | independent manually armed Binance canary |
| Bybit Demo completeness, asynchronous acknowledgement, private topics, and public/private host separation | `AX-V1C-C05-001` through `005` | implemented and deterministic gates passed | independent manually armed Bybit canary |
| Separate credential-owning engines, DB roles, leases, proxies, networks, locked startup, and recovery | `AX-V1C-PR2-001` through `003` | implemented and locally qualified | image-backed runtime qualification |
| Full-pipeline canary, controlled restart, duplicate prevention, and immutable evidence | `AX-V1C-PR2-004` through `006` | implementation and local tests complete | both real canaries and sealed evidence pending |
| Clean install and exact B8 upgrade | `AX-V1C-PR2-007` | PostgreSQL 18.4 qualification passed | repeat on final source candidate if it changes |
| C6 HTTP API, console, bounded resumable SSE, and 72-hour qualification | none in PR2 | intentionally absent | PR3 after PR2 canaries pass |

PR1 C1-C3 requirements remain cumulative and are re-exercised by
`v1c-pr2-local-qualify`. A7/B1/B2 evidence is historical context only.
