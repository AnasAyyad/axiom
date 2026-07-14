# Coding standards

## Structure and dependencies

- Keep business logic out of `cmd/`; the single platform entrypoint delegates to
  `internal/bootstrap`.
- Preserve the modular-monolith boundary. Strategy, allocation, risk,
  simulation, accounting, and reconciliation communicate in process through
  explicit owned interfaces, never internal HTTP/RPC.
- Define interfaces near consumers. Keep exchange DTOs at adapter boundaries.
- Do not create generic `utils`, `helpers`, or `common` packages.
- Prefer the standard library. A dependency needs a phase requirement, exact
  pin, lock/checksum, license/vulnerability review, and removal plan.

## Reviewability

- Production files target fewer than 300 lines; more than 400 requires a
  reviewed exception, and more than 500 is prohibited except generated code,
  migrations, static fixtures, or readable table tests.
- Functions normally stay below 50 lines. React components stay below 250.
- Every Go package has `doc.go`; exported Go identifiers have meaningful
  comments. Comments explain invariants, units, sequencing, failure behavior,
  or non-obvious rationale.
- Keep package tests beside behavior. Cross-system and browser tests live in
  dedicated integration/E2E locations.
- Generated code is marked, isolated in generated directories, and never
  hand-edited.

## Correctness and determinism

- Authoritative financial values never use `float32` or `float64`. A2 introduces
  the project decimal wrappers; earlier phases contain no financial math.
- Avoid map iteration, goroutine completion, ambient wall time, or process-global
  randomness as an authoritative ordering input.
- Bound goroutines, channels, queues, payloads, requests, and shutdown work.
- Propagate cancellation/deadlines with `context.Context`; graceful shutdown has
  one ceiling of 60 seconds.
- Missing, stale, contradictory, unknown, non-durable, or unversioned safety
  input fails closed with a stable redacted reason code.

## Frontend

- TypeScript stays strict with `noUncheckedIndexedAccess` and
  `exactOptionalPropertyTypes`.
- OpenAPI-generated types are primary; runtime responses are still validated at
  the untrusted boundary.
- React performs presentation and command intake only, never authoritative
  financial, allocation, or risk calculations.
- Critical states include loading, empty, degraded, forbidden, partial, and
  error behavior. Keyboard, focus, labels, contrast, and announcements target
  WCAG 2.2 AA.

## Verification

Run `make verify`. CI repeats formatting, generation, lint/static analysis,
unit/component/race/fuzz smoke tests, builds, Compose renders, secret/prohibited
capability scans, vulnerability/license checks, SBOM generation, and hardened
image inspection. A passing narrow test never certifies a broader release gate.
