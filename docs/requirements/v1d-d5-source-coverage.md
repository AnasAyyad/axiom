# V1D D5 source coverage

The authoritative D5 scope is covered by these source-owned boundaries:

- Runtime safety: current storage pressure, durable transitions, recorder
  finalization, shadow entry disablement, and heavy-job rejection.
- Lifecycle: 30-day raw-data floor, verified-backup requirement, active
  reference/hold exclusions, and seven-day generated-artifact expiry.
- Recovery: encrypted authenticated backup, independent mount proof, clean
  target restore, database integrity checks, confined market-data catalogue/file
  checksum recovery, and authenticated restore verdict.
- Deployment: non-root read-only services, resource limits, edge TLS, image
  digest identity, SBOM, vulnerability/secret/misconfiguration scanning,
  schema upgrade, rollback, and forward-fix procedures.
- Qualification: exact declared load, version-controlled drills, continuous
  hash-chained samples, terminal failure reasons, immutable evidence, and an
  authenticated seven-day verdict. A separate preflight command validates the
  exact contract, measured preflight, and one fresh live sample without
  creating evidence or starting the formal clock.

B2, C6, and D5 are separate verdicts. A D5 sample may exercise market data and
sandbox recovery, but cannot replace or relabel either dedicated qualification.
Local D5 smoke evidence is non-qualifying and does not select or approve the
reference server.
