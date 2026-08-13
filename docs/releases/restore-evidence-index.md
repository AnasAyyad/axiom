# Restore evidence index

Current-candidate status: **Blocked**. No accepted declared-server restore
verdict is present in this repository.

| Evidence | Repository implementation | Required accepted artifact | Current status |
|---|---|---|---|
| PostgreSQL backup | `internal/backup`, `cmd/storage-backup`, backup image | encrypted/authenticated backup identity on independent mount | implementation tests available; formal artifact missing |
| PostgreSQL clean restore | restore command and authenticated no-replace verdict | exact source/image/config/backup hashes, integrity checks, duration under four hours | formal artifact missing |
| Market-data recovery | D5 confined inventory/checksum verifier | segment inventory hash, manifest recovery result, no ambiguous path | formal artifact missing |
| Schema upgrade | migrations and D4-to-D5 upgrade test | declared server/database identity and exact migration digest | current server artifact missing |
| Clean-server Compose | image-based profiles and render checks | server declaration, immutable image digests, TLS/secrets validation, health/SLO results | current server artifact missing |

Local tests and disposable PostgreSQL runs verify implementation; they do not
replace the declared-server evidence required by Section 35 criterion 18.
