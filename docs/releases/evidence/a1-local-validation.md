# A1 provisional local validation

This record captures diagnostic progress for Phase A1. It is not A1 gate
evidence and does not mark A1 verified.

## Validation identity

| Field           | Value                                                                        |
| --------------- | ---------------------------------------------------------------------------- |
| Validation time | 2026-07-13T11:21:59Z                                                         |
| Source commit   | Unavailable: this working tree has no `HEAD`                                 |
| Working tree    | All repository files are untracked                                           |
| Go              | 1.26.5 from the previously provisioned `/tmp/axiom-go1.26.5` toolchain       |
| Node            | 24.18.0 from the previously provisioned `/tmp/axiom-node-v24.18.0` toolchain |
| pnpm            | 11.12.0 through Corepack                                                     |

The missing source commit means these results cannot identify an immutable
candidate and therefore cannot satisfy the A1 evidence rules.

## Passing local checks

The following repository-local gate completed successfully with the pinned
toolchains and existing dependency caches:

```text
make preflight format-check contracts-check lint test test-race fuzz-smoke build compose-validate security-static
```

Observed results:

- toolchain preflight passed;
- formatting, generated OpenAPI consistency, Go vet/staticcheck, frontend lint,
  Go source policy, and file/package policy passed;
- all Go tests and both frontend component/accessibility tests passed;
- the Go race suite passed;
- the execution-mode fuzz smoke ran for three seconds and passed;
- frontend type checking/build, embedded-asset generation, and platform build
  passed;
- all 32 active Compose profile combinations rendered safely;
- reserved `testnet` and `demo` profile placeholders remained inert; and
- secret and prohibited-capability scans plus their seeded negative self-tests
  passed.

Documentation-link validation passed for all 45 Markdown files. The full A0
traceability check against the owner-supplied V1A plan also passed with 381
unique requirements, 37 verified A0 rows, 10 retired IDs, complete reverse
coverage, and 30 exact A11 endpoints.

The pinned `govulncheck` completed against `vuln.go.dev` and reported no known
vulnerabilities. The production Dockerfile built successfully as
`axiom:a1-local`; its local image identity is
`sha256:c955b03d69e61912195dd69f16d4a8689189d5ae1b2239307dc3ed997119a8e5`.
Image inspection proved numeric non-root user `10001:70`, the exact platform
entrypoint, no shell, read-only execution, no credential-like environment key,
and a working embedded binary. An ephemeral `postgres:18.4-alpine` instance then
passed the migration, API/worker/recorder/shadow process, dependency-up/down
readiness, forbidden-mode, exact-command, embedded-UI, and graceful-shutdown
smoke suite. The test database container was removed afterward.

## Open A1 evidence

- GitHub Actions cannot be executed from this uncommitted local tree. There is
  no positive CI run, dependency review, full-history secret scan, SBOM,
  container scan, image digest, or retained CI artifact.
- A clean-machine setup walkthrough has not been executed.

Phase A1 remains **In progress** until those checks run against an immutable
source commit and every A1 acceptance item has direct evidence.
