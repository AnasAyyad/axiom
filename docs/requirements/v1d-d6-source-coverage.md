# V1D D6 source coverage

D6 extends the existing repository gates; it does not create a second runtime,
execution engine, or qualification platform.

- Certification model: `internal/certification` owns strict inputs, exact
  identities, trust, signature, lifetime, review, Section 35, and final-verdict
  rules.
- Final command: `cmd/d6-certify` is default-off, exact-build bound, consumes
  externally prepared signed records, and writes one immutable verdict.
- Repository enforcement: `scripts/check-v1d-d6-boundary.mjs`, existing secret,
  prohibited-capability, binary, generated-contract, image, Compose, and
  documentation checks.
- Image supply chain: the application and backup Dockerfiles, image inspection,
  reproducibility, SBOM, license, secret, misconfiguration, and vulnerability
  scans.
- Signed-request proof: the existing closed Binance Spot Testnet and Bybit Demo
  adapters, egress policy, deterministic emulator, and redacted capture tests.
- Evidence: D1-D5 and A/B/C records remain independent inputs. D6 never changes
  an earlier verdict or converts non-soak smoke evidence into qualification.
- Operations and recovery: D5 pressure, backup, restore, lifecycle, fault, and
  readiness implementations remain authoritative; D6 only indexes and verifies
  their exact evidence.
- Documentation: all Section 33 canonical paths and cumulative evidence/status
  indexes are repository-owned inputs to the documentation gate.

No private-repository source is exported to an external indexing service. No
production credential, signer, broker, private endpoint, hidden route, dormant
toggle, or environment bypass is introduced by D6.
