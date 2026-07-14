# A0 architecture and safety review

## Decision

**A0 verified on 2026-07-12 at 11:25:26 UTC.** The requirements catalogue,
safety boundary, threat model, mode/endpoint/asset policies, topology,
ownership/fencing, lifecycle, recovery, data policy, service objectives, risk
baseline, ADR set, and real-money-lock proof plan are complete enough to admit
A1. This is architecture and static-configuration evidence only; it is not a
V1A release certification and proves no later runtime, image, soak, restore,
browser, accounting, or deterministic-result gate.

## Authority and identity

| Item | SHA-256 / identity |
|---|---|
| `AGENTS.md` | `42a12c05cd5182ba15bf147c4674ab122cfca007dc3802227778123088a639bd` |
| `crypto_bot_v1_codex_spec.md` | `b11a4aa93a342e1d5e46b80468385446726b7c7d546a5271076ea9e026e2ed08` |
| User-supplied V1A implementation plan | `18f874df82e7bed46f16658f11f024402f0f46e6b822ae7033e2e3b3def8f2e4` |
| A0 scoped worktree manifest | `9de43eadce6a3f1b8fa5448153f94dd120d284123c481ab86e297fea26724cb8` |
| Source commit | None: the supplied repository has no baseline `HEAD` |

The scoped manifest is the SHA-256 of the sorted `sha256sum` stream for all
regular files under `AGENTS.md`, `README.md`, the specification,
`.env.example`, `.gitignore`, `docker-compose.yml`, `deploy/`, `monitoring/`,
`scripts/`, and `docs/`, excluding `docs/releases/evidence/` to avoid a
self-referential hash. It contains no local `.env`, secret file, database dump,
market recording, generated artifact, or attachment. The plan hash above binds
the external user-supplied attachment separately.

The no-`HEAD` condition is a bounded A0 exception recorded in
[V1A readiness](../v1a-readiness.md#evidence-rules). A1 and every later phase
must use a committed source identity; this manifest cannot identify a build or
release candidate.

## Reviewers and scope

- Primary conformance review: root Codex implementation agent.
- Independent no-order-path/static review: Codex `a0_safety_audit` reviewer.
- Independent architecture consistency review: Codex `a0_docs_audit` reviewer.
- Traceability correction received a separate structural audit before the
  primary verifier ran the retained checks below.
- No external human security audit is claimed.

The independent reviews found no authenticated exchange client, credential
mount, signer, private route, external-order process, or allowed prohibited
product. Four architecture/deployment findings were closed before this
decision: startup ordering now matches the fenced normative sequence; database
role secrets no longer enter `psql` arguments; research configuration is no
longer overridable through deployment environment keys; and the premature
plaintext backup command is absent until A4 supplies encrypted backup/restore
implementation.

## Retained verification

| Check | Exact command or procedure | Result |
|---|---|---|
| Requirements structure and plan endpoints | `/tmp/axiom-node-v24.18.0/bin/node scripts/check-a0-traceability.mjs /home/anas/.codex/attachments/7085c3d9-bb74-4587-8af7-85d8e499faf1/pasted-text-1.txt` | Passed: 381 unique rows, 37 verified A0 rows, 10 retired IDs with successors, total reverse coverage, and 30 exact A11 endpoints |
| Documentation links | `/tmp/axiom-node-v24.18.0/bin/node scripts/check-doc-links.mjs` | Passed after this evidence file was registered: all local paths and anchors resolve |
| Static prohibited-capability scan | `bash scripts/check-prohibited-capabilities.sh` | Passed |
| Scanner seeded-negative suite | `bash scripts/test-check-prohibited-capabilities.sh` | Passed; representative forbidden modes, routes, keys, products, and capability flags are rejected |
| Compose syntax and all active profile combinations | Render `docker compose --env-file .env.example config --quiet` for all 32 subsets of `app`, `record`, `workers`, `observability`, and `edge`, then render `--profile '*'` | Passed with Docker Compose 5.1.4; services are only PostgreSQL, migrator, API, shadow engine, recorder, worker, Prometheus, Grafana, and Caddy |
| PostgreSQL role initialization shell | `sh -n deploy/postgres/init/001-create-roles.sh` | Passed |
| PostgreSQL role initialization behavior | Run `postgres:18.4-alpine` with its data directory on tmpfs, mount the initialization script and four generated placeholder secret files, wait for initialization, and query `pg_authid` for the three non-owner roles and non-null password hashes | Passed: migrator, runtime, and read-only roles were created and password hashes were set without secret values in argv or ordinary environment variables |
| Deployment research-input ownership | Search Compose, `.env.example`, and `deploy/` for environment definitions of settlement asset, approved assets, instruments, initial equity, USD reporting, and USDT/USD provider | Passed: no deployment override exists; immutable versioned research configuration owns those inputs |
| Secret delivery/static backup posture | Inspect deployment files for `PGPASSWORD`, password-valued `psql --set`, command substitution into password environment variables, and runnable plaintext backup commands | Passed: none remain; A4 owns encrypted backup implementation and restore evidence |

Tool identities used for the retained static checks were GNU bash 5.2.21,
GNU coreutils `sha256sum` 9.4, Docker Compose 5.1.4, Node.js 24.18.0, and the
pinned PostgreSQL 18.4 Alpine image.

## Architecture conclusions

- V1A accepts only `backtest`, `replay`, `paper`, and `shadow`; it rejects
  `testnet`, `demo`, `live`, credentials, private endpoints, and external order
  side effects.
- The only exchange boundary is the compiled Binance production-public Spot
  REST/WebSocket allowlist for BTC-USDT and ETH-USDT market data.
- Strategy candidates must remain inside the allocator → risk → simulation →
  virtual journal path. Strategies and the browser own no broker, balance,
  reservation, or journal writer.
- Recorded replay order is exactly recorded logical time plus unique
  `ingest_ordinal`; the plan's five-field tuple is limited to deterministic
  derived/synthetic scheduler work as recorded in ADR-0008.
- Every protected virtual asset unit has one owner or exclusive reservation;
  journal transactions balance per asset; unknown or non-durable state fails
  closed.
- Shadow startup performs bounded local preflight, acquires fenced ownership,
  enters `LOCKED`, validates the complete safety/configuration manifest, loads
  immutable configuration, recovers and reconciles, synchronizes public market
  data, and becomes `READY + PAUSED`. Activation remains separate and audited.
- The reviewed risk baseline starts paused, never auto-unpauses, preserves all
  specification limits, and records the explicit V1A exchange-exposure
  assumption in ADR-0007.

## Remaining limitations and next gate

A1 starts from documentation and deployment contracts; no Go module, platform
binary, React application, OpenAPI generation, application image, or CI run has
yet been verified. The 72-hour public-data soak, deterministic ten-run proof,
database migrations and accounting constraints, encrypted backup/clean restore,
binary/image/browser/network inspection, accessibility testing, and full release
evidence remain assigned to A1–A11 or the cumulative release gate. Any later
implementation that diverges from this safety architecture reopens A0.
