# A4 local validation

**Recorded:** 2026-07-14
**Status:** A4 phase acceptance verified locally

## Implemented locally

- Exact immutable multi-commodity journal validation, canonical projection
  rebuild, exact one-time reversal validation, weighted-average cost basis with
  residual-safe partial sales, sanitized fail-closed mismatch handling, and
  concurrent fenced/quarantined reservations.
- Embedded checksummed forward-only PostgreSQL migrations covering the required
  relational model, exact financial domains, immutable history, uniqueness,
  state-transition, sealed-journal, reservation, durable-coordination, and
  fencing constraints. The DSN-gated qualification harness covers role
  isolation, run/order/reservation lifecycle, transactional inbox/outbox/journal
  and balance recovery, idempotent retry, and coordination state guards.
- Closed least-privilege runtime/recorder/read-only grant matrices and expanded
  reviewed sqlc query inputs for accounting, datasets, runs, inbox/outbox,
  cursors, jobs, and leases. `sqlc v1.31.1` generated the repository `pgx/v5`
  package cleanly and its compile-only tests pass.
- Crash-safe segment finalization/recovery, compatibility/checksum reader,
  deny-by-default retention planning, versioned raw/canonical schema contracts,
  ADR-0008-compatible logical-time/ordinal dataset reading, and integer-only
  capacity planning. Finalization rejects a codec writer whose canonical
  ordered-content hash differs from the immutable segment specification. The
  concrete `parquet-go v0.30.1` writer/reader emits exact integer, UTC-nanosecond,
  byte-array, and fixed-digest columns with single-worker Zstandard level 3;
  validates physical/logical schemas and compression metadata; and recomputes a
  versioned, framed ordered-content hash before exposing records.
- Framed AES-256-GCM database artifacts, authenticated manifests, clean-target
  restore command, WAL/tool/time metadata, pre-inventory `pg_restore --list`
  validation with invalid-archive quarantine, independent Compose storage, and
  authenticated crash-resumable pruning with a hard 14-generation floor. Restore
  success is gated on an actually clean target plus schema, journal, spot
  ownership, and reservation-projection integrity queries.

## Commands executed successfully

```text
go test ./...
go test -race ./...
make format-check
make docs-check
make lint
make test
make fuzz-smoke
make build
make compose-validate
make security-static
git diff --check
make a4-sqlc
AXIOM_A4_TEST_DSN=<isolated PostgreSQL 18.4 *_a4_test DSN> make a4-postgres-qualify
AXIOM_A4_RESTORE_DSN=<clean restored *_a4_test DSN> go test ./internal/storage/postgres -run '^TestA4PostgresRestoredRoleQualification$' -count=1 -v
go test ./internal/storage/segments
go test -race ./internal/storage/segments
go test ./internal/storage/segments -run '^$' -bench '^BenchmarkCanonicalParquetRoundTrip$' -benchmem -count=5
```

The Compose validation rendered all 128 active profile combinations and proved
reserved later-release profiles inert. Focused tests include per-asset journal
properties, high-contention reservation rejection, every segment finalization
kill boundary, corrupt/incompatible dataset rejection, backup mutation and
truncation rejection, authenticated pruning/recovery, schema invariants, and
capacity overflow/weak-policy rejection.

The focused Parquet benchmark includes cloning/validation, ordered hashing,
Zstd writing, and reading of 4,096 canonical rows. Five local samples ranged
from 17.10 ms to 24.51 ms per operation (approximately 167k–240k rows/second),
with the exact raw output retained in the work log. This is codec qualification,
not recorder-derived bytes/day capacity evidence.

`make verify` was also run through every offline target. Those targets passed;
the final network-backed `govulncheck` target could not fetch
`https://vuln.go.dev/index/modules.json.gz` because this sandbox denies network
DNS/socket access. No vulnerability-scan pass is claimed.

## PostgreSQL and recovery qualification

The destructive gate ran on a clean `postgres:18.4-alpine` database. It applied
all three migrations, proved a second migration pass was empty, regenerated and
compiled every reviewed `sqlc` query, and passed role-negative, lifecycle,
journal, reversal, sealing, concurrent reservation, transactional rollback,
idempotent retry, inbox/outbox, cursor, job, and lease assertions in 3.11 seconds.

The first real gate run exposed a clock-dependent test selection: a fixture with
a future fixed timestamp could be selected instead of the concurrent USDT
reservation. The assertion now selects the intended asset and advances from the
stored timestamp. The clean rerun passed.

The hardened backup image was built from the reviewed Dockerfile. Its first real
database attempt exposed that internal `psql` probes omitted the configured
database, so the database-scoped pgpass entry could not match. The command now
passes `--dbname`, has a regression test, and was rebuilt before evidence was
accepted.

The corrected non-root, read-only, capability-dropped job created and
authenticated a 176,781-byte AES-256-GCM restore point in 1.02 seconds. A second
clean PostgreSQL 18.4 instance accepted the atomic single-transaction restore
in 1.22 seconds. Post-restore integrity and role-matrix qualification passed.
Independent SHA-256 projections over schema checksums, balances, sealed
journals, ledger rows, dataset manifests, segment manifests, and replay hashes
were identical on source and restore:

```text
fbef59bb8757782d948976a3cb0b670270045c5a948666933dbb5da1fa6a4a47
```

## Deferred release evidence

The A4 acceptance bullets are satisfied. Production-public recording still
requires recorder-derived bytes/day measurements and owner-approved server
capacity before it begins. Daily scheduling, protected off-host replication,
and a 24-hour operational RPO remain deployment/V1D evidence; this one-shot
local drill does not claim them. The dependency vulnerability scan also remains
unclaimed for the network reason above.

## Safety result

No authenticated exchange transport, signer, private exchange route, external
order submission, withdrawal, transfer, margin, futures, leverage, borrowing,
lending, staking, short-selling, or `live` execution capability was introduced.
