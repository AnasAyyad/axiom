# B2 local validation evidence

## Status

B2 is locally verified for every implemented and non-soak gate on 2026-07-22.
Its model, deterministic replay, recorder, Tier A, PostgreSQL 18, cumulative
verification, and short production-public coherent-view qualifications passed.
The passing public run measured 59.569181 ms Binance and 40.927081 ms Bybit
clock uncertainty, below the immutable 100 ms B2 limit, and retained the exact
30-record Tier A dataset and coherent-view identity.

Formal phase acceptance remains on an explicit hold for predecessor acceptance,
the deferred continuous 72-hour B2 qualification, and Product, Security, QA,
and SRE approval. No 72-hour B2 run or formal acceptance is claimed. B3 source
must start only after this locally verified B2 completion is merged into `main`.

## Source and toolchain identity

- Branch: `b2-coherent-views`, based on merged B1 `main`
  `91d8bab54216210f2ef54dc20fed716ccf22c831`.
- Baseline post-merge CI run: `29893542073`, succeeded before B2 changes.
- Passing live evidence timestamp: `2026-07-22T12:10:49Z`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  PostgreSQL 18.4, Docker Engine 29.6.1.
- Passing live candidate: commit `2c8c8e9062954d2f2c9f42712e2f5369fec99ce7`
  plus `internal/qualification/b2_live_integration_test.go` SHA-256
  `4d6236625ff014049a865a633cfb4ae9fdce6a698054351449f91c0941283db0`.
- Passing live runner: GitHub Codespaces, Southeast Asia, Linux/amd64, two
  vCPUs, Go 1.26.5.

## Implemented authority and policy

- Shared monotonic ordering epoch and bounded midpoint clock estimator for
  Binance and Bybit, with a 30-second fusion window and fail-closed arithmetic.
- Immutable per-market versions with connection generation, book version,
  monotonic receive offset, exact UTC nanoseconds, ingest ordinal, clock offset
  and uncertainty, state hash, and collector instance/region.
- Versioned deterministic as-of policy
  `axiom.coherent-view-policy.v1`: maximum book age 250 ms, maximum inter-book
  skew 250 ms, and maximum clock uncertainty 100 ms. All boundaries are
  inclusive; one nanosecond over is rejected.
- Canonical `(exchange, base, quote)` member ordering and SHA-256 version-vector
  identity stable across permutations, restarts, and repeated runs.
- PostgreSQL migration 000014 with atomic complete-view commits, exact
  nanosecond interval checks, immutable members, decision references, frozen
  dataset evidence, Tier A child membership, per-exchange coverage, and closed
  role grants. Both clean installation and exact B1-to-B2 upgrade are covered.
- Backward-compatible V1 recorder manifests plus V2 candidate manifests and a
  verified multi-exchange Tier A aggregate. Tier A assignment requires child
  chain validation, raw/canonical linkage, replay hashing, and a combined
  no-hole ingest-ordinal proof.
- The live harness evaluates the coherent view immediately after publishing its
  market versions, before evidence finalization can add unrelated filesystem
  latency. It still persists Tier A evidence before reporting a join rejection,
  so qualification remains fail-closed and diagnosable.

## Passed qualification

- `make b2-model-qualify GO=.local/toolchains/go/bin/go`: passed shared clock,
  Binance/Bybit adapter, market-data bridge, coherent-view, restart,
  deterministic hash, recorder, Tier A, and qualification tests.
- `GOFLAGS=-p=1 make b2-model-qualify`: passed again on the exact Southeast
  Asia live candidate before the public run.
- `make b2-postgres-qualify ...`: passed clean install and migrations
  000001-000013 to 000014 upgrade against isolated PostgreSQL 18.4 databases
  `axiom_clean_b2_test` and `axiom_upgrade_b2_test`.
- `/home/anas/.local/bin/sqlc generate --file sqlc.yaml`: passed and retained
  current generated models.
- `make verify GO=.local/toolchains/go/bin/go NODE=.local/toolchains/node/bin/node
  COREPACK=.local/toolchains/node/bin/corepack`: passed preflight, formatting,
  generated contracts, documentation, lint, backend/frontend tests, the full
  Go race detector, fuzz smoke, builds, all 128 active Compose profile
  combinations, security and binary-boundary scans, and `govulncheck` with no
  called vulnerabilities.
- `git diff --check`: passed after the final source and evidence updates.

## Passing short production-public evidence

The passing run was public and record-only. It accepted no credentials and
exposed no private, order, withdrawal, transfer, margin, derivative, or
external execution surface.

- Command: `GOFLAGS=-p=1 AXIOM_B2_LIVE_PUBLIC=1 make b2-live-qualify`, with
  evidence root and collector region set explicitly.
- Collector region: `github-codespaces-southeast-asia`.
- Source root inside the retained archive:
  `/workspaces/axiom/.local/b2-live-codespace/evidence/b2-live-1784722249894421047`.
- Tier A manifest: `b2-live-tier-a.tier-a.json`.
- Tier A identity:
  `379202ad9d16491ee60e252ae7aa47f09e9977dcf67bae60be0a1a290ce97e11`.
- Aggregate: 30 linked public Binance/Bybit records; the recorder and Tier A
  manifests are retained in the verified archive.
- Clock uncertainty: Binance `59.569181 ms`; Bybit `40.927081 ms`; both pass
  the inclusive 100 ms maximum.
- Coherent-view identity:
  `4c80fb5ddd1eb210c01d295001ecf643bc0649784446568dcd447ab07e8ec825`.
- Result: `PASS`; process exit code `0`; test duration `1.20 s`.
- Owner-retained archive: `b2-codespace-evidence.tar.gz`, SHA-256
  `cca98c02255c2da4b0f1d16be101ffa337f8df85a219212472f4911ca104f445`.
  The source and downloaded checksums matched, and the downloaded archive
  passed `tar -tzf`. The raw public recording is sensitive operational evidence
  and is intentionally not committed.

## Superseded high-latency runner evidence

The earlier local run was also public and record-only. It remains useful
fail-closed evidence but is superseded as the short coherent-view gate result.

- Root:
  `.local/b2-live-20260722/b2-live-1784703984488961762`.
- Tier A manifest:
  `qualification/b2-live-tier-a.tier-a.json`.
- Tier A identity:
  `ced498d7465e7ab46e8eb9f58093c06b8490407904398ea526c42bd60c3fe4db`.
- Manifest-file SHA-256:
  `5b5fc8ec84ffceffdeb2fcb530d847c7398f9cbdf1e6b8d1c5e4d3f449f0091c`.
- Aggregate: 122 raw records, 122 linked canonical records, two exchanges,
  zero declared gaps, zero hidden gaps, complete, quality tier A.
- Binance member: 101 records; manifest
  `85e7ee29b37a934e8fbe6525a6ae08b52912bae9a786586a3998a03da5dbd167`;
  replay
  `c49debe22d615d6a06195fb73c80f05eacac4b8cd735255c8784f56edcd7afc8`.
- Bybit member: 21 records; manifest
  `f9a79c52ddd323c127091d81fe4f413ac42d109194aee0d5be79a53ae62e22a1`;
  replay
  `480d71220356dca1f8d90c0a63e2838dbb93c254a76334146e22b311f8ec3a64`.
- Coherent-view result: rejected with `coherent_view_rejected:uncertainty`.
  This was the expected safe outcome for measurements above 100 ms. The later
  Southeast Asia run above closed the short live coherent-view gate.

## Explicitly deferred formal gates

- The continuous B2 72-hour qualification remains explicitly deferred and was
  not run.
- A7/V1A and B1 formal predecessor acceptance remains pending.
- Product, Security, QA, and SRE formal acceptance remains pending after those
  predecessor and deferred phase gates close.
