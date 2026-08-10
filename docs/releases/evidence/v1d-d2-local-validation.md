# V1D D2 local validation

## Candidate identity

| Field | Value |
|---|---|
| Merged D1 baseline | `4cf0b14` |
| Branch | `v1d-d2-command-center` |
| Product contract | `crypto_bot_v1_codex_spec.md`, Phase D2 |
| API contract | merged D1 `/api/v1` OpenAPI and generated clients |
| Formal C6 run | Separate and not run by D2 |
| Formal D5 run | Separate and not implemented by D2 |
| Production-private orders | Impossible and not attempted |

## Current decision

**D2 implementation and local critical-workflow qualification pass. Merge,
hosted CI, cumulative earlier-gate acceptance, and D3-D6 remain pending.**

## Validation record

The following checks passed on 2026-08-03 with Node 24.18.0, pnpm 11.12.0,
and the pinned Go 1.26.5 toolchain where applicable:

```text
make d2-contract-qualify
corepack pnpm --filter @axiom/web typecheck
corepack pnpm --filter @axiom/web lint
corepack pnpm --filter @axiom/web test
corepack pnpm --filter @axiom/web build
node scripts/check-owner-experience-boundary.mjs
scripts/check-file-policy.sh
scripts/check-secret-patterns.sh
scripts/test-check-secret-patterns.sh
scripts/check-prohibited-capabilities.sh
scripts/test-check-prohibited-capabilities.sh
make d2-security-qualify
make docs-check
make format-check contracts-check
```

Frontend unit qualification contains 23 passing tests, including D1 response
validation, the exact legacy trend-route precedence, navigation roles, required
state presentation, React components, and axe accessibility checks.

The D2 browser workflow passed:

```text
chromium-desktop: pass
chromium-tablet: pass
chromium-mobile: pass
firefox-desktop: pass
webkit-desktop: pass in mcr.microsoft.com/playwright:v1.61.1-noble
```

Each D2 case verifies the six navigation groups, persistent safety state,
activity detail and artifact download, restricted system events, fail-closed
strategy resume, owner-only formal qualification preflight, approved-only Run
Lab, no arbitrary command textbox, serious/critical WCAG 2.2 AA findings, and
horizontal page overflow. The complete existing suite also passed 16 tests in
Chromium desktop/tablet/mobile and Firefox desktop.

The cumulative repository gate was also completed with the same source tree.
The initial `make verify` passed preflight, formatting, generated-contract,
documentation, lint, backend/frontend test, and Go race-test stages before the
terminal was interrupted during fuzzing. The remaining stages were then run
without changing source code:

```text
make fuzz-smoke build compose-validate security-static vulnerability
```

Fuzz smoke tests, the embedded frontend/platform build, all 1,024 active
Compose profile combinations, inert reserved profiles, static secret and
prohibited-capability scans, compiled A6/A7/V1C safety boundaries, and the Go
dependency vulnerability scan passed. The vulnerability scan reported zero
reachable vulnerabilities. Compose validation was run outside the restricted
filesystem/network sandbox because it requires access to the local Docker
daemon. The vulnerability scanner and current database were downloaded through
the approved network boundary.

Targeted strategy timing tests passed unchanged on two stable CPUs against the
25 ms p99 threshold:

```text
mean-reversion: 11.335056 ms p99
trend:           4.647904 ms p99
triangular:     12.065724 ms p99
```

An earlier parallel attempt exceeded that timing threshold under shared host
scheduling pressure; no threshold was weakened. The stable-CPU rerun above is
the recorded local timing result.

## Tooling limitations

The host lacks WebKit native libraries and cannot use `sudo`; the exact pinned
official Playwright image supplied those libraries without modifying the host.
The D2 WebKit workflow passed there. In a full four-workflow WebKit regression,
the three B8/C6/D2 cases passed while an older A11 shadow cleanup step timed out;
this is not represented as a complete WebKit regression pass.

The shadcn post-build audit helper timed out in its approval service twice, and
the Playwright MCP browser could not start because its separate Chrome channel
is absent. These unavailable advisory helpers are not counted as validation;
the executable Playwright 1.61.1 matrix above is the browser evidence.

## Explicit exclusions

- No D3 specialized lab lifecycle is claimed.
- No D4 scheduled-report or complete incident/alert delivery engine is claimed.
- No D5 reference-server hardening or seven-day readiness soak is claimed.
- No D6 final V1 safety certification is claimed.
- D2 does not replace B2, C6, or D5 evidence and does not prove profitability.
