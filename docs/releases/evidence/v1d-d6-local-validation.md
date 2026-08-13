# V1D D6 local validation

Date: 2026-08-04
Candidate source base: `93dc3edf74ead553af75a589cd50eeb4735f2db5`
Working branch: `v1d-d6-certification`
Evidence class: local, non-formal, non-soak
Disposition: **NOT CERTIFIED**

## Identity and toolchain

The D6 implementation is an uncommitted working tree on the source base above.
It is not an immutable release candidate. The base SHA identifies the parent,
not the uncommitted D6 content.

| Tool | Exact version |
|---|---|
| Go | `go1.26.5 linux/amd64` |
| Node.js | `v24.18.0` |
| pnpm | `11.12.0` |
| Docker client/server | `29.6.2` / `29.6.2` |
| Trivy | `0.70.0`, database v2 updated `2026-08-04T13:27:17Z` |
| PostgreSQL | `18.4` disposable test server and backup/restore clients |
| Playwright | `1.61.1`, official `mcr.microsoft.com/playwright:v1.61.1-noble` image |

No B2 or C6 72-hour runner, D5 seven-day runner, or soak-named Make target was
invoked. No formal enablement variable, exchange credential, signed external
request, or order submission was used.

## Hosted failure reproduction and repair

Hosted run `30904448621` at the base SHA passed every job except its backup
image Trivy step. Re-querying the job confirmed it scanned `axiom-backup:ci` as
Alpine 3.24.1. A local scan of that frozen pre-fix image reproduced:

- `c-ares` `1.34.6-r0`: `CVE-2026-33630` HIGH, fixed in `1.34.8-r0`;
- the Go 1.24.6 `gosu` binary: `CVE-2025-61726`, `CVE-2025-61729`,
  `CVE-2026-25679`, `CVE-2026-27145`, `CVE-2026-32280`,
  `CVE-2026-32281`, `CVE-2026-32283`, `CVE-2026-33811`,
  `CVE-2026-33814`, `CVE-2026-39820`, `CVE-2026-39822`,
  `CVE-2026-39836`, `CVE-2026-42499`, and `CVE-2026-42504` HIGH,
  plus `CVE-2025-68121` CRITICAL; and
- 30 HIGH license-policy findings from the general Alpine runtime package set.

The repair does not ignore findings or lower thresholds. The final backup
stage is a deterministic scratch filesystem containing the Axiom backup
binary, PostgreSQL 18.4 `pg_dump`, `pg_restore`, and `psql`, their exact shared
libraries, CA roots, source identity, a reviewed runtime component manifest,
and notices. It removes Alpine's package manager, shell, server, and `gosu`.

The dependency update uses the normal pnpm lockfile path. `react-router` 8.3.0
is the direct runtime dependency. `undici` 7.29.0 is transitive through
jsdom/Vitest, `postcss` 8.5.23 through Vite, and `brace-expansion` 5.0.9 through
minimatch/ESLint; exact workspace overrides constrain those three transitive
graphs. `pnpm audit --audit-level moderate` returned no known vulnerabilities.
GitHub still reports alerts 3 and 5-12 against the unmodified remote base;
their patched versions exist only in this unpushed worktree.

## Commands that passed

The following commands, or the same targets with ephemeral PostgreSQL DSNs,
were run successfully. Password-bearing DSN values are intentionally not
retained.

| Command | Result |
|---|---|
| `make preflight GO=/tmp/axiom-go1.26.5/go/bin/go` | Passed exact Go/Node/pnpm and required-tool checks |
| `make verify GO=/tmp/axiom-go1.26.5/go/bin/go NODE=node COREPACK=corepack` with writable Go/staticcheck caches and loopback enabled | Passed format, lint, vet, staticcheck, generated contracts, 167 documentation files, 381 requirements, all Go and frontend tests, race, fuzz-smoke, production builds, 1,024 Compose combinations, source/secret/prohibited-capability/binary scans, and `govulncheck` |
| `make d6-security-qualify GO=/tmp/axiom-go1.26.5/go/bin/go` | Passed certification model and race tests, D6 traceability, links, all static safety checks, adapter/request-capture tests, and egress-proxy tests |
| `make d6-final-certification` with no enablement variables | Rejected as required: `AXIOM_D6_FINAL_CERTIFICATION_ENABLED=1 is required` |
| D1 contract/API/security; D2 frontend/security; D3 contract/API/frontend/security; D4 contract/API/frontend/security targets | Passed |
| D5 model, backup, hardening, chaos, and security targets | Passed; no D5 soak or readiness target invoked |
| C1 security, C2 auth, C3 recovery, C4 Binance Testnet, C5 Bybit Demo, and C6 API/security/chaos constituent targets | Passed with deterministic fixtures/emulators; no C6 aggregate or soak target invoked |
| A4, A8-A11, B1-B8, V1C, D1, D3-D5 PostgreSQL 18 clean-install and supported-upgrade targets | Passed against dedicated disposable PostgreSQL 18 databases |
| `make d2-browser-qualify PNPM=/tmp/axiom-d6-pnpm` | Five projects passed: Chromium, Firefox, WebKit, Chromium tablet, Chromium mobile |
| `make d3-browser-qualify PNPM=/tmp/axiom-d6-pnpm` | Five projects passed after fixing stream readiness and deterministic online fixture setup |
| `make d4-browser-qualify PNPM=/tmp/axiom-d6-pnpm` | Five projects passed |
| `make c6-frontend-qualify PNPM=/tmp/axiom-d6-pnpm` | Frontend checks and all five C6 browser projects passed |
| `make image-reproducibility IMAGE=axiom:d6-final REBUILD_IMAGE=axiom:d6-final-rebuild VERSION=0.1.0-d6-local COMMIT=93dc3edf74ead553af75a589cd50eeb4735f2db5 BUILT_AT=2026-08-04-local-non-formal DIRTY=true` | Passed; runtime fingerprint `sha256:ac127264d98b93f661f1fca1e36c38eb8dede3338f26c55be4cb1f4cb8828d7d` |
| `make backup-image-reproducibility BACKUP_IMAGE=axiom-backup:d6-final` | Passed clean no-cache comparison; runtime fingerprint `sha256:e7cf7b18ced62fc0f671831711a3da8592c784108e1fa0d973b955abfb03867c` |
| Trivy `image` with `--scanners vuln,secret,misconfig,license --severity HIGH,CRITICAL --ignore-unfixed=false --exit-code 1` on both final images | Passed with zero vulnerabilities, secrets, failed misconfigurations, or license findings |
| Trivy `--format spdx-json` on both final images | Generated local application and backup SBOMs |

The local dirty image IDs were
`sha256:d611f0ae7e9bd02117f11077c9f83e67ab8e77cff2993d0bc269f5f78ab46f34`
and
`sha256:2570a405fabe66da7a3b229d4b4b507cbd4e005bb9a3d8af2234b62afdcd84c5`.
They are diagnostic identities only. Local artifact hashes were:

| Artifact | SHA-256 |
|---|---|
| application SPDX | `d18daa00eb5458028d0e1a9d5c85c9a2b110b2885b25c8ae55430a0273fd4642` |
| backup SPDX | `bba0c90a82537adcb577d131ebd7919d7c629e734c02d6d1a258a81699aec9b8` |
| application Trivy JSON | `ba4d60ebc18ca37baf89db9906c773dc8ac4d6ca48125c6b9424e09f582b783f` |
| backup Trivy JSON | `0cd59f13ad00da47074818eb5ec9946c3a6413501fdff7093659f13f7f853bbe` |

The JSON and SPDX files stayed under a disposable local directory and are not
formal retained evidence.

## Integrated workflow and clean-candidate limitation

The local integrated harness generated a 1,024-event recorder dataset, ran the
A11 PostgreSQL qualification, prepared real strategy/allocator/risk/simulator
evidence, authenticated through the real API, and created a backtest through
the browser. It used loopback services and a test-only local shadow lifecycle
driver; no production-private client or external order route was present.

Materialization then failed closed with
`offline_claim_materialization_failed` because the binary truthfully embedded
`Dirty=true`. The Compose image-backed smoke similarly brought up migrations,
API, worker, recorder, PostgreSQL, Prometheus, and Grafana but held
`engine-shadow` unhealthy with `a11_startup_recovery_build_invalid`. These are
the required clean-source controls. The tests were tightened so current job and
session metric cards cannot be confused with older successful report rows.
Neither control was bypassed, and the worktree was not falsely labeled clean.

The standard Compose smoke started only production-public market collection;
it had no exchange credential or private/order capability. The separate signed
request and outbound-host tests used deterministic local emulators and redacted
captures only.

## Failures and limitations

- The first final `make verify` attempts were interrupted by read-only host
  Go/staticcheck cache paths and sandbox-denied loopback listeners. Writable
  temporary caches and approved loopback access resolved those environmental
  failures; the unchanged verification then passed.
- The full authenticated materialization/shadow workflow and image-backed
  Compose smoke cannot pass against an uncommitted dirty source without
  defeating the required clean-build invariant. They must be rerun from the
  eventual clean exact-SHA candidate.
- Hosted CI has not run for the D6 worktree. Run `30904448621` remains D5-base
  evidence and cannot be relabeled D6 evidence.
- No clean exact-SHA D6 manifest, retained artifact set, signed independent
  review, signed safety manifest, declared-server restore evidence, or final
  verdict exists.
- Formal A/B/C/D prerequisite and cumulative acceptance remains incomplete,
  including B2's 72-hour qualification, C6's 72-hour qualification, and D5's
  seven-day declared-server readiness run.

This record proves repository implementation and substantial local non-soak
behavior only. It does not qualify profitability or certify V1.
