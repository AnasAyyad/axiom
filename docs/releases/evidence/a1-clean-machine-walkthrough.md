# A1 clean-machine setup and governance walkthrough

This record closes the Phase A1 setup/governance walkthrough for the immutable
implementation candidate below. It combines the owner-verified clean GitHub
runner from Axiom CI run `#4` with a separate isolated-checkout operator and
governance walkthrough. No exchange credential or trading API key was used.

## Evidence identity

| Field | Value |
| --- | --- |
| Result | Passed |
| Walkthrough start | 2026-07-13T14:34:33Z |
| Walkthrough end | 2026-07-13T14:46:36Z |
| Candidate version | `0.1.0-a1` |
| Source commit | `5ce09c3611e05a8fa5d0f1afc4706e17698b2d90` |
| Checkout | Fresh local clone with no hardlinks, detached at the source commit |
| Checkout status | Empty before setup and after generated secret/local-data cleanup |
| Host | Linux 6.6.87.2-microsoft-standard-WSL2, x86_64 |
| Go | 1.26.5 |
| Node.js | 24.18.0 |
| pnpm | 11.12.0 through Corepack |
| Docker | 29.6.1 |
| Docker Compose | 5.1.4 |
| PostgreSQL | `postgres:18.4-alpine` |
| Walkthrough image | `axiom:a1-clean-5ce09c3` |
| Local image ID | `sha256:63038c9ed864fa73075ac9b642c8758c54adc7042f178ffa8eebe4ed94a2231b` |

The owner-confirmed hosted clean-run identity and retained artifacts are
recorded in [a1-hosted-ci.md](a1-hosted-ci.md). The hosted runner independently
covered clean dependency retrieval, the full repository gate, image build,
image-backed deployment, SBOM, license/vulnerability scanning, and retained
supply-chain artifacts for the same source commit.

## Setup and runtime results

The detached checkout completed the documented A1 path:

- exact toolchain discovery and `make help` command review;
- Corepack activation and exact pnpm selection;
- frozen lockfile installation into an empty checkout;
- `make generate` with a production frontend build and embedded assets;
- one complete `make verify` run with exit status zero, including formatting,
  generated contracts, documentation/traceability, static analysis, backend and
  frontend tests, race detection, fuzz smoke, production builds, all 32 active
  Compose combinations, inert later-release profiles, seeded secret/prohibited
  capability negatives, and `govulncheck` reporting no vulnerabilities;
- an exact-commit production image build;
- `make compose-smoke`, including clean PostgreSQL role initialization,
  least-privilege migration, healthy API, shadow engine, recorder, and worker;
  and
- a separate manual `app` profile startup using a fresh isolated Compose project
  and database volume.

The manual profile returned successful `/health/live` and `/health/ready`
responses. `/api/v1/system/status` reported `READY_PAUSED`,
`strategy_activation:unavailable`, and `real_trading_enabled:false`. The
embedded application shell and assets loaded, and the passing frontend
component/accessibility test asserted the visible `REAL TRADING DISABLED`
label.

The isolated containers, networks, generated volume, `.env`, generated secret
files, and local data directory were removed. A final checkout status was empty.

## Documentation deviation found and resolved

The original secret setup example used unprivileged `chgrp 70` and `chgrp 472`.
That fails when the host user does not belong to those container groups. The A1
completion change corrects the guide to use `sudo chgrp`, consistent with the
existing privileged writable-directory ownership step. The walkthrough applied
the same numeric ownership through the pinned container helper because this
managed environment does not provide non-interactive sudo; both the full
Compose smoke and the isolated manual startup then read the `0640` secrets
successfully.

The host also contained an unrelated pre-existing Compose volume named for the
default project. Attempting the default project failed closed because its old
database lacked the current migrator role; PostgreSQL skipped initialization,
the migration exited, and application services did not become ready. The volume
was preserved exactly as required by the deployment guide. Repeating the
walkthrough with an isolated project name created a genuinely fresh volume and
passed. This supplies direct evidence for the non-destructive existing-volume
policy.

The local dependency install reused the previously verified content-addressed
pnpm store because this managed sandbox blocks ordinary registry access. It
installed all 250 locked packages into the empty checkout without downloads.
Owner-verified hosted run `#4` independently covers clean network retrieval on a
fresh GitHub runner, so the local cache reuse is not used as the sole clean-host
dependency proof.

## Governance review

`AGENTS.md`, `README.md`, `CONTRIBUTING.md`, `docs/coding-standards.md`,
`docs/adr/template.md`, `docs/implementation-status.md`,
`docs/releases/v1a-phase-checklist.md`, and `deploy/README.md` were reviewed
against the observed commands and runtime.

They consistently require phase-ordered delivery, a modular-monolith boundary,
exact toolchains, immutable evidence, secret-safe operation, public-data-only
V1A behavior, and rejection of `testnet`, `demo`, and `live`. No authenticated
exchange transport, signer, private endpoint, credential input, withdrawal,
transfer, margin, futures, leverage, borrowing, lending, staking, short-selling,
or production-order capability was introduced or requested.

## Gate conclusion

Together with the local immutable-candidate record and owner-verified hosted CI,
this walkthrough supplies current evidence for every phase-local A1 requirement
and acceptance criterion. Phase A1 is verified. The program-wide documentation
obligation `AX-V1A-A01-QLT-013` remains continuously enforced through later
phases and the V1A release gate; A1 completion does not retire or waive it.

A2 must not begin until this A1 completion record is committed and the owner
merges the A1 branch into `main`.
