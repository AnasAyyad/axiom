# B1 local validation evidence

## Status

Implemented, not verified. This file is the evidence index for B1 implementation and local
qualification. It does not claim formal B1 verification or replace the required
short production-public validation and continuous 72-hour soak.

## Baseline

- Source: `70ba3f74addee3d19ef529434122dfabd357d3c5`
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0
- Gate: full pre-change `make verify` passed after ignored local SDK and Vite
  caches were kept outside the source-scanning tree.

## B1 implementation evidence

Recorded at `2026-07-21T12:48:50Z` from the current uncommitted B1 worktree:

- Reviewed configuration SHA-256:
  `8a5ada09d2e689d33f92f567d569ddc74cd6aae24bce55e8805958a77cf0685a`.
- `make b1-model-qualify`: passed outside the filesystem sandbox because the
  deterministic emulator requires a loopback listener.
- `make b1-adapter-qualify`: passed, including a three-second fuzz run with
  86,592 executions in the retained terminal result.
- `make b1-security-qualify`: passed the B1 boundary, secret, prohibited
  capability, scanner self-tests, and A6/A7 binary dependency gates.
- `AXIOM_B1_LIVE_PUBLIC=1 make b1-live-qualify`: passed against Bybit public
  server time, metadata, depth 1,000, ticker, and one-hour candles.
- `make verify`: passed cumulatively with Go 1.26.5, Node 24.18.0, and pnpm
  11.12.0, including formatting, documentation, generated contracts, lint,
  Go/frontend tests, race tests, fuzz smoke, builds, Compose rendering,
  security scans, binary boundaries, and `govulncheck`.
- `git diff --check`: passed.

The PostgreSQL migration catalog and static B1 migration assertions passed as
part of `make verify`. The dedicated `b1-postgres-qualify` integration target
was not run because no isolated `*_b1_test` DSN was available.

## Formal evidence still required

- Acceptance and retention of the short production-public Bybit validation.
- Isolated PostgreSQL 18 clean install and V1A-to-B1 upgrade proof.
- Continuous 72-hour declared-load Binance/Bybit recording soak.
- Exact source/configuration/image/dataset identities and retained artifacts.
- Product, Security, QA, and SRE acceptance after V1A closes.
