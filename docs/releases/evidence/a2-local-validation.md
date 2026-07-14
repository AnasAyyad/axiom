# A2 local phase-gate validation

This record identifies the local A2 financial-domain and configuration gate.
It does not claim a hosted CI run or remote integration; those actions are
owner-managed.

## Candidate and environment

The candidate is the local `a2-candidate` commit containing this record, based
on verified A1 completion commit
`9b7952d95f621485c95eafb57165d272100b799c`. Git identifies the complete tree;
no push or hosted-CI claim is part of this local record.

Validation used Linux `6.6.87.2-microsoft-standard-WSL2` on x86-64, an 11th Gen
Intel Core i7-1185G7 with four cores/eight logical CPUs, 15.5 GiB RAM, and Go
1.26.5 linux/amd64.

## Focused acceptance results

The following focused checks passed on 2026-07-14:

- all domain, configuration, bootstrap, security, and policy unit tests;
- the repository-wide Go race suite;
- `go vet` and Staticcheck;
- the financial-float/prohibited-capability scan and Go API leak policy;
- execution-mode, strict-configuration, and financial parser fuzz smoke tests,
  each for three seconds;
- strict deployment JSON to code-default equivalence and configuration-reference
  consistency; and
- canonical snapshot reproduction, defensive immutability, concurrent atomic
  reads, safe reload, secret-reference, and startup-before-resource negative
  tests.

The three fuzz targets executed 156,900, 10,884, and 155,270 inputs
respectively during their recorded smoke runs. Fuzz counts are scheduling and
machine dependent; passing invariants, not a fixed count, are the gate.

## Decimal benchmark

`make benchmark-a2` ran `BenchmarkFinancialArithmetic` five times with memory
reporting. Exact price-times-quantity calculation and half-even notional
quantization measured 328.6–717.2 ns/op, with a median 362.2 ns/op, 96 B/op,
and three allocations/op. The first run was the slow outlier. This is a local
regression baseline, not a throughput or profitability claim and not approval
to replace the locked `cockroachdb/apd` engine.

## Complete repository and image gate

The final complete `make verify` passed in one run with the exact pinned Go,
Node, Corepack, and pnpm toolchains. It covered formatting, generated contracts,
documentation and traceability, configuration/reference consistency, Go vet,
Staticcheck, frontend lint/tests/build, all Go tests, the full race suite, all
three fuzz targets, backend build, every Compose profile combination, seeded
secret/prohibited-capability negative tests, and `govulncheck`. The vulnerability
database refresh required allowed network access; the final run reported no
known vulnerabilities.

The local `axiom:a2-local` image then built successfully and passed the minimal
non-root/read-only inspection. Image ID
`sha256:0d2120d223f19a3e9952966bf2f2799edd0efe2756a9a10a38a19aa9023c344a`
contained `/etc/axiom/platform.json` byte-for-byte equal to the reviewed source
graph; both files hashed to
`sha256:5c67991d0f31bfe4a6d86044865a6c8e0fdd9519f4b5dfaeee9e5f345668f3ce`.

All 17 `AX-V1A-A02-*` requirements point to this evidence and are verified.
The durable database repository for snapshot history remains correctly owned by
A4; A2 proves the immutable in-process representation, exact serialization,
hash reproduction, and PostgreSQL numeric codec boundary.
