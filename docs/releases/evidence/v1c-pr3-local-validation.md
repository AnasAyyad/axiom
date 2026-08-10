# V1C PR3 local validation

## Candidate identity

| Field | Value |
|---|---|
| Refreshed merged-main baseline | `8902b3b794ed344a131822b34fa8bb81cedaa35e` |
| Branch | `v1c-c6-console-soak` |
| Qualified implementation commit | `b5ac868ec38d9204afc6f9fd4db6673aee10e852` |
| Evidence directory | `.local/v1c-pr3-image-evidence-clean-b5ac868-20260730t163401z` (ignored, local) |
| Configuration schema | `axiom.config.v1c.1`; integrations and submission default off |
| Migration | forward-only `000024` |
| Formal C6 run | Not run; pending |
| Authenticated exchange orders during PR3 | None |

## Current decision

**Implementation and non-soak qualification are complete; the formal C6 soak
and acceptance are pending.**

The implementation, cumulative C1-C6 non-soak phase gates, PostgreSQL 18.4
clean-install and exact B8-upgrade qualification, complete repository
verification, and exact-source clean image and supply-chain qualification
pass. No formal C6 result, owner acceptance, or security acceptance is
claimed.

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

The following implementation and aggregate checks passed on 2026-07-30. The
complete aggregate was rerun to exit zero on the qualified implementation
commit:

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
make image-reproducibility \
  IMAGE=axiom:v1c-pr3-clean-b5ac868 \
  REBUILD_IMAGE=axiom:v1c-pr3-clean-b5ac868-rebuild \
  VERSION=v1c-pr3-clean \
  COMMIT=b5ac868ec38d9204afc6f9fd4db6673aee10e852 \
  BUILT_AT=2026-07-30T16:34:01Z DIRTY=false
scripts/inspect-image.sh axiom:v1c-pr3-clean-b5ac868
GO=/tmp/axiom-go-1.26.5 scripts/check-sandbox-security-boundary.sh
make compose-smoke \
  IMAGE=axiom:v1c-pr3-clean-b5ac868 \
  GO=/tmp/axiom-go-1.26.5
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

## Clean artifact record

| Artifact field | Value |
|---|---|
| Image tag | `axiom:v1c-pr3-clean-b5ac868` |
| Image ID | `sha256:67e2175ce5324a69f5bf15c21247b8ed0e03c07712a0b0f25a16ccc012364151` |
| Image size | `11,325,382` bytes |
| Runtime user / entrypoint | `10001:70` / `["/app/platform"]` |
| Reproducibility fingerprint | `sha256:9b3507a205070fef6c2005ee4e9a1f42ed6be3411eee128a948f5c0748cb175d` |
| Executable SHA-256 | `289c02eca2c430969baf9e2ca919ca42ab58842280c72ae730f6321542b99362` |
| Exported image tar SHA-256 | `0fcf20b28df94eb03cc8804cfa277a098c6f07f790bf11cabb9f88ded8715916` |
| Trivy | `0.72.0`; vulnerability database updated `2026-07-30T12:58:40Z` |
| SPDX | `SPDX-2.3`; 47 packages |

The image rebuild produced the same complete runtime descriptor and passed
fixed entrypoint, non-root, shell-absence, read-only runtime, and
credential-like environment-key checks. The exact extracted executable
contains the qualified commit, version, build time, and approved Testnet/Demo
hosts. Test-only and production-private order destinations are absent. The
image-backed application and observability Compose smoke passed.

Trivy scanned the frozen image tar and an exact `git archive` of the qualified
commit offline, using the current isolated database and checks cache. The
image has zero HIGH/CRITICAL vulnerabilities, secrets, or misconfigurations.
The raw source report has one HIGH advisory:
`GHSA-qwww-vcr4-c8h2` in `react-router@7.18.0`. It affects only unstable React
Server Components APIs, which this application does not import or use. The
advisory's fixed `8.3.0` line is not published through the compatible
`react-router-dom` package line. A local evidence-only disposition is scoped
to that exact advisory, `pnpm-lock.yaml`, and package URL, and expires on
2026-08-30. The raw report remains alongside the disposition-filtered report;
no repository-wide ignore was added. The filtered source gate has zero
HIGH/CRITICAL vulnerabilities, secrets, or misconfigurations.

The Go dependency update in the qualified commit resolves
`GHSA-r277-6w6q-xmqw` by upgrading `github.com/getkin/kin-openapi` to
`v0.144.0`. `govulncheck` reports zero called vulnerabilities.

## Explicit exclusions

- No 72-hour qualification was started.
- No Binance Testnet or Bybit Demo authenticated order was submitted.
- Existing PR2 canary evidence and `.secrets/` were not read, modified,
  replaced, or copied.
- No formal V1C owner or security acceptance is claimed.
