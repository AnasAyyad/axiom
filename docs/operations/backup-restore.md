# Backup and restore

## Contract

PostgreSQL backups are encrypted and authenticated, retain at least 14 daily
restore points, and are copied to an operator-managed mounted filesystem that
is distinct from the server root, PostgreSQL, market data, and local staging.
A Compose-managed volume is not remote backup evidence. Raw market recovery
uses a separately restored clean tree plus manifest and file checksums.

Inputs are the exact backup image, source/configuration identities, backup-only
database credentials, base64-encoded 32-byte encryption key file, independent
remote path, staging path, clean restore database, and clean market-data restore
path. Outputs are immutable encrypted generations and an authenticated,
no-replace restore-evidence record.

## Backup procedure

1. Verify mount identities and free space. Reject root mounts, same-filesystem
   aliases, symlink ambiguity, and unavailable paths.
2. Run the image-backed backup profile with the least-privilege backup role.
3. Verify authentication, ciphertext identity, retention count, and remote copy
   before treating a generation as restorable.
4. Record exact image/source/configuration/database identities, UTC timestamps,
   generation hash, and operator. Never log credentials or encryption material.

## Restore drill

1. Declare an empty disposable database and clean recovered-market-data path;
   never target active paths.
2. Select one immutable generation, authenticate/decrypt it, restore with the
   PostgreSQL 18.4 tools, and run integrity, migration-head, row/invariant, and
   application readiness checks.
3. Verify recovered ready-segment inventory, manifest references, file bounds,
   and every checksum. Reject missing, extra, ambiguous, escaping, or changed
   paths.
4. Produce the no-replace authenticated verdict with RPO/RTO measurements. The
   D5 objective requires a successful clean restore within four hours.

Failures are terminal for that evidence identity. Do not overwrite a verdict,
reuse a dirty target, skip market recovery when required, lower retention, or
claim a local test as formal server evidence. Tests live in `internal/backup`,
`cmd/storage-backup`, D5 PostgreSQL gates, and Compose boundary checks. Current
accepted-server evidence remains listed as missing in the restore index.
