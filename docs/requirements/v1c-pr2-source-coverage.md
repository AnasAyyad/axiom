# V1C PR2 source coverage

The product specification and approved C1-C6 delivery plan are authoritative.
PR2 extends the merged C1-C3 foundation with C4/C5 and its runtime/canary
boundary. Requirement IDs resolve to
[`v1c-pr2-traceability.md`](v1c-pr2-traceability.md).

| Source area | PR2 requirement IDs | Current disposition | External or later gate |
|---|---|---|---|
| V1 safety: virtual Spot only, fail closed, no production-private or executable prohibited capability; ADR-0022 records the exact Bybit Demo provider-key bundle exception | `AX-V1C-C04-001`, `AX-V1C-C05-001`, `AX-V1C-C05-002`, `AX-V1C-C05-005`, `AX-V1C-PR2-001` | implementation, local security gates, live canaries, and exact-source clean artifact qualification passed | owner/security acceptance |
| Binance Spot Testnet completeness, private stream, ambiguity recovery, rate capacity, and reset handling | `AX-V1C-C04-001` through `006` | implemented; deterministic gates and the independent manually armed Binance canary passed | owner/security acceptance |
| Bybit Demo completeness, asynchronous acknowledgement, private topics, and public/private host separation | `AX-V1C-C05-001` through `005` | implemented; deterministic gates and the independent manually armed Bybit canary passed | owner/security acceptance |
| Separate credential-owning engines, DB roles, leases, proxies, networks, locked startup, and recovery | `AX-V1C-PR2-001` through `003` | implemented and locally qualified, including image-backed runtime and live failover recovery | owner/security acceptance |
| Full-pipeline canary, controlled restart, duplicate prevention, and immutable evidence | `AX-V1C-PR2-004` through `006` | implementation, local tests, both real canaries, and both sealed evidence records passed | owner/security acceptance |
| Clean install and exact B8 upgrade | `AX-V1C-PR2-007` | PostgreSQL 18.4 qualification passed on the final implementation source | owner/security acceptance |
| C6 HTTP API, console, bounded resumable SSE, and 72-hour qualification | none in PR2 | intentionally absent; PR2 canary prerequisite is satisfied | PR3; not started |

PR1 C1-C3 requirements remain cumulative and are re-exercised by
`v1c-pr2-local-qualify`. PR2 local qualification is complete; formal
owner/security acceptance remains open. A7/B1/B2 evidence is historical
context only.
