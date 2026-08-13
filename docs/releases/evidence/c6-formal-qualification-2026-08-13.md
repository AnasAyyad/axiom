# C6 formal qualification

**Decision date:** 2026-08-13

**Machine qualification state:** Passed

**Owner/security acceptance:** Accepted by explicit repository-owner instruction
after the completed technical and security evidence review

The immutable formal run is
`/srv/axiom-data/qualification/c6-b1f1039-20260810-r1` on
`axiom-server`. It ran from exact clean source
`b1f1039f6ed3e2b6bad78686b35101bebc8b3407` and terminated with
`state: PASSED`, `qualified: true`, and no qualification failures.

## Qualification result

- Continuous observed duration: 259,257 seconds (72 hours and 57 seconds).
- Required duration: 259,200 seconds.
- Samples: 4,295 consecutive ordinals with no gaps; maximum observed interval
  was 60.610862 seconds.
- Safety failures: zero duplicate creates, lost fills, double-posted fills,
  unresolved unknown orders, reconciliation mismatches, suspense items, lease
  failures, persistence failures, restart-safety failures, entry-safety
  failures, or production targets.
- Deterministic chaos evidence: 14 distinct required scenarios, all `PASSED`.
- Profitability evidence: false. C6 is an operational sandbox qualification,
  not evidence of strategy profitability or approval for real-money trading.

## Bounded recovery evidence

The run consumed one permitted incident for the Bybit Demo account and no
incident for the Binance Spot Testnet account.

- Source: `private_stream`.
- Typed failure kind: `transient_outage`.
- Sanitized cause code: `private_stream_receive_failed`.
- Detected: `2026-08-12T02:52:27.748130Z`.
- First clean check: `2026-08-12T02:52:29.187783Z`.
- Second clean check and recovery: `2026-08-12T02:52:59.497658Z`.
- Clean-check separation: 30.309875 seconds.
- Total recovery duration: 31.749528 seconds, within the two-minute deadline.
- The engine remained dispatch-disabled while `DEGRADED` and returned only to
  `READY_PAUSED`.
- No repeated, expired, or unrecoverable recovery event was recorded.

## Immutable artifact identity

- Terminal evidence path:
  `/srv/axiom-data/qualification/c6-b1f1039-20260810-r1/evidence/c6-terminal.json`
- Terminal evidence file SHA-256:
  `56151952ad14c71b95bbbc5a0a9d08b22990d0975beb7b777c76f52d989c1237`
- Terminal evidence internal SHA-256:
  `fb0d215ea5b6866580f6bc3979c6c18527572c152f6ab86e64a3e8f6d1c87dd4`
- Build manifest SHA-256:
  `99d5b16f5409a4ed6ccda38ec8dfe8ac02f99e069d82fc85604e54e1142a0d24`
- Configuration manifest SHA-256:
  `b179c486992f9dcdd9ccec7b0366de4022ec5c632787af7de053276295afc2d4`
- Launch manifest SHA-256:
  `d7ae9a91305465910243ecbd155fd1aadb88fefd69a279160b32e972a0108a61`
- Observer executable SHA-256:
  `718e859d879e0bd263279fb0574dd8e2f28b12c08d573ce2cb4b4a3ace907203`
- Chaos controller executable SHA-256:
  `e96e962547aad408063ef1709f187143ceb03012de26d87bf06af2e541e53056`
- Candidate image digest:
  `sha256:90c8c0b6f1c9996bc37c126f3d31fab23a7ac72bee7e70e6c29db2cb746e4ace`

The terminal evidence hash was independently recomputed from the sealed JSON
with its `evidence_hash` field cleared and matched the stored internal hash.
The terminal file remains create-once, mode `0440`; database sample, chaos,
failure, account, and recovery evidence remains protected by immutable-table
triggers and a constrained terminal run transition.

## Evidence review and validation

The 2026-08-13 technical evidence review confirmed:

- the server checkout and `origin/c6-operational-closure` both identified the
  qualified source commit before this documentation-only record;
- all deployed Axiom application containers were healthy on the exact
  qualified image digest;
- the observer exited with code zero after sealing the terminal evidence;
- its dedicated PostgreSQL role was non-superuser and limited to redacted
  operational reads, append-only qualification evidence, and the constrained
  terminal run transition;
- the observer had no exchange credentials or published ports and ran as a
  non-root user with a read-only root filesystem on the internal `axiom_core`
  network; and
- `make c6-security-qualify`, `make c6-api-qualify`,
  `make c6-chaos-qualify`, and `make c6-frontend-qualify` passed on the server
  checkout.

The raw terminal JSON, binaries, database records, and retained runtime
artifacts are intentionally not committed to Git. This record binds their
locations and hashes without copying sensitive or generated operational data
into the source repository.

## Acceptance record

- Repository owner: accepted by explicit repository-owner instruction.
- Security evidence review: accepted after the completed least-privilege,
  redaction, immutable-evidence, and runtime-isolation review recorded above.
- Acceptance date: 2026-08-13.
