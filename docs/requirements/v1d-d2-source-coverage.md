# V1D D2 source coverage

The product specification and approved V1D delivery plan are authoritative.
Requirement IDs resolve to
[`v1d-d2-traceability.md`](v1d-d2-traceability.md).

| Source area | D2 requirement IDs | D2 disposition | Later gate |
|---|---|---|---|
| Section 25 shell, header, Command Center, and operational pages | `AX-V1D-D02-001` through `003`, `007` | implemented and locally browser-qualified on the D2 branch | merge and cumulative acceptance |
| D1 activity, reason, export, and live-update consumption | `AX-V1D-D02-004`, `008` | implemented through validated D1 REST/SSE clients | D4 evidence bundles and D5 soak |
| Strategy configuration/runtime control separation | `AX-V1D-D02-005` | exact-revision and role-aware UI over D1 commands | owner/security review |
| Approved qualification and drill control | `AX-V1D-D02-006`, `007` | D2 monitoring/start/abort UI; specialized lab completion remains D3 | C6 formal run and D5 readiness run |
| Accessibility, browser, and responsive behavior | `AX-V1D-D02-008`, `009` | component and Playwright gates on D2 branch | D3-D4 regression and D6 release suite |
| Safety, redaction, and non-profitability posture | `AX-V1D-D02-010` | retained in shell, pages, commands, and downloads | D5 clean-image and D6 independent inspection |

D2 does not complete D3's specialized lab lifecycle, D4's scheduled reporting
and delivery engine, D5's seven-day soak, or D6 certification. C6 and B2 retain
their separate formal evidence gates.
