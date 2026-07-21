# A8 local implementation validation

**Date:** 2026-07-16

**Candidate branch:** `a8-backtest-replay`

**Sequencing:** owner-authorized speculative local implementation while the
A7 formal 72-hour qualification remains pending. This evidence does not satisfy
the A7 prerequisite and does not authorize merge or formal A8 acceptance.

## Outcome

A8 is implemented and locally validated. Formal acceptance remains blocked by
A7. The implementation adds bounded verified dataset admission, immutable run
and model identities, shared-mode replay/backtest processing, deterministic
replay controls and faults, exact simulation models, shared liquidity,
no-look-ahead fills, the canonical order reducer and saga, balanced fill/fee/
rebate/dust/recovery journal postings, durable checkpoints, forward-only
PostgreSQL schema/sqlc repositories, and an atomic event/fill/journal/checkpoint/
outbox boundary.

The credential-free offline worker framework accepts only backtest and replay
sources. Its operational pipeline fails closed until real A9 allocation/risk
and A10 strategy implementations are supplied; A8 tests use explicit test-only
processors. No API or UI launch surface was added.

## Retained local checks

- `make verify` passed with Go 1.26.5, Node 24.18.0, pnpm 11.12.0, all backend
  and frontend tests, race tests, fuzz smoke, build, all 128 Compose profile
  combinations, source/binary capability gates, and vulnerability scanning.
- The image-backed application/recorder/worker/observability Compose smoke
  passed using the A8 candidate image.
- `make a8-postgres-qualify` passed on isolated PostgreSQL 18.4. The test proves
  forward migration, generated query compilation, canonical order transition,
  fill deduplication, balanced journal sealing, checkpoint/outbox persistence,
  atomic rollback, and restored projection state.
- Ten synthetic runs produced byte-identical canonical events, decisions,
  orders, balances, metrics, and result hashes.
- Adversarial model tests cover post-latency market state, rejection of the
  signal state, side-safe filters/rounding, unowned sells, partial/missed fills,
  fees, maker cancellation, shared-liquidity exhaustion, independent
  namespaces, order duplicates/stale/conflicts, late cancel/fill races, saga
  recovery, and defensive checkpoint restore.
- Deterministic fault tests cover disconnect, sequence gap, latency, rejection,
  partial fill, cancel/fill race, unknown state, storage failure, and restart at
  the selected event.

## Ignored local recording qualification

Every admission ran the repository's complete cumulative-manifest and
`recorder.VerifyDataset` checks. Recordings were opened read-only; no payload,
recording path, or artifact was exported or committed.

- Revision 43 was identified by its selected manifest hash, revision, A7 source
  commit, schema, parser, normalizer, and segment hashes. Ten clean streaming
  passes each processed 1,083,956 linked records with identical canonical
  checksums. The reader retained at most one segment pair and maximum-speed
  replay was faster than the recording's logical duration.
- Revision 62 was separately verified and streamed for 1,240,370 linked
  records.
- Revision 84 exposed 22 low-density segment pairs, was downgraded to Tier C,
  and was rejected for decision-grade use.
- Revisions 43 and 62 remain Tier B partial engineering evidence. They are not
  Tier A, formal release, or profitability evidence.

## Safety and remaining gate

The candidate remains spot-only simulation. It adds no credentials, signing,
private exchange routes, external order transport, withdrawals, transfers,
margin, derivatives, leverage, borrowing, lending, staking, short selling, or
sale of unowned inventory.

A8 must remain unmerged. After A7 is formally accepted, the candidate must be
rebased onto accepted `main`, rerun against the final A7 artifact, and pass the
formal A8 acceptance workflow. All phase prerequisite and completion checkboxes
remain unchecked until then.
