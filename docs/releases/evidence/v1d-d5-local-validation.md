# V1D D5 local-validation record

Source SHA: `93dc3edf74ead553af75a589cd50eeb4735f2db5`
Evidence class: implementation and non-formal checks

The D5-base hosted CI run `30904448621` passed PostgreSQL clean-install and
D4-upgrade, pressure, backup, lifecycle, chaos, and short deterministic smoke
jobs. Its sole failed job was the original backup-image supply-chain scan.

During D6 repair, the backup runtime was rebuilt from source into a deterministic
scratch root filesystem. PostgreSQL 18.4 `pg_dump`, `pg_restore`, and `psql`
version smoke passed; clean no-cache filesystem/configuration comparison passed;
Trivy 0.70.0 vulnerability, secret, misconfiguration, and license scanning at
HIGH/CRITICAL returned zero findings. These D6-worktree checks repair the
repository gate but are local and do not retroactively make hosted run
`30904448621` green.

The D5 seven-day run was not started, and its absence is an explicit Section 35
blocker.
