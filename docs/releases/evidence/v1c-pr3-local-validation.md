# V1C PR3 local validation

## Candidate identity

| Field | Value |
|---|---|
| Refreshed merged-main baseline | `8902b3b794ed344a131822b34fa8bb81cedaa35e` |
| Branch | `v1c-c6-console-soak` |
| Implementation commit | Pending final freeze |
| Final pushed commit | Pending evidence commit |
| Configuration schema | `axiom.config.v1c.1`; integrations and submission default off |
| Migration | forward-only `000024` |
| Formal C6 run | Not run; pending |
| Authenticated exchange orders during PR3 | None |

## Current decision

The implementation, cumulative C1-C6 non-soak phase gates, PostgreSQL 18.4
clean-install and exact B8-upgrade qualification, and complete repository
verification pass. The exact-source clean image and supply-chain qualification
remain pending until the implementation commit is frozen. No formal C6 result,
owner acceptance, or security acceptance is claimed.

## Implemented evidence boundary

The API returns only bounded, redacted account, engine, arm, cap, order, fill,
reconciliation, reset, and qualification projections. Every administrative
mutation uses the existing session, RBAC, Origin/CSRF, idempotency, revision,
authorization, and audit boundaries. Arm and unlock additionally consume a
recent password/TOTP, one-use, purpose- and request-bound authorization.

The manual C6 observer owns no exchange credentials or exchange client. It
reads redacted database facts from a dedicated role, records immutable samples
and chaos evidence, seals one no-overwrite terminal JSON file, and can qualify
only a clean exact 259,200-second formal run. Smoke evidence is explicitly
non-qualified and `profitability_evidence=false`.

## Validation record

The following implementation and aggregate checks passed on 2026-07-30:

```text
make preflight
make contracts
make verify GO=/tmp/axiom-go-1.26.5 GOFMT=/tmp/axiom-gofmt-1.26.5
AXIOM_V1C_TEST_DSN=<dedicated-clean-pg18.4-dsn> \
AXIOM_V1C_UPGRADE_TEST_DSN=<dedicated-b8-upgrade-pg18.4-dsn> \
GOFMT=/tmp/axiom-gofmt-1.26.5 \
make v1c-pr3-local-qualify GO=/tmp/axiom-go-1.26.5-pg
corepack pnpm --filter @axiom/web test:e2e --grep 'C6 sandbox'
make a11-e2e-qualify
```

The aggregate exercised C1-C5, C6 API/frontend/security/chaos/smoke, generated
OpenAPI consistency, backend and frontend tests, React/axe accessibility,
desktop/mobile Playwright, race and fuzz tests, strict TypeScript, frontend
build, source/file policies, all 1,024 Compose profile combinations, secret and
prohibited-capability scanner self-tests, compiled boundaries, and live
`govulncheck`. The vulnerability result contained zero called
vulnerabilities.

The PostgreSQL qualification used dedicated databases
`axiom_pr3_final_clean_v1c_test` and
`axiom_pr3_final_upgrade_v1c_test`. Both passed against PostgreSQL 18.4. The
aggregate initially exposed that `c6-security-qualify` inherited the parent
V1C DSNs and populated the dedicated databases before their clean-state gate.
The target now explicitly clears both DSNs, a targeted run proved both
databases remained at zero public tables, the two databases were recreated,
and the entire aggregate then passed in one uninterrupted rerun.

The complete unmocked A11 browser workflow also passed against a clean
integrated environment, including authenticated login, backtest, replay
pause/step/resume, recovery, incident evidence, reconnect, public shadow,
responsive/keyboard behavior, and logout. The disposable harness mounted the
host CA bundle read-only because its Debian fixture image does not carry one;
the production Dockerfile already includes the CA bundle.

The final clean-image, reproducibility, non-root/read-only, compiled absence,
image-backed Compose, SPDX, and Trivy results and identities will be appended
after the implementation commit is frozen.

## Explicit exclusions

- No 72-hour qualification was started.
- No Binance Testnet or Bybit Demo authenticated order was submitted.
- Existing PR2 canary evidence and `.secrets/` were not read, modified,
  replaced, or copied.
- No formal V1C owner or security acceptance is claimed.
