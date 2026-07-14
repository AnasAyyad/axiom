# A3 local phase-gate validation

**Date:** 2026-07-14
**Candidate branch:** `a3-candidate`
**Parent:** `fb543962a2972d5887314c257d200761c3fc4335` (verified A2)
**Profile:** local Linux amd64, 11th Gen Intel Core i7-1185G7, Go 1.26.5,
Node 24.18.0, Docker Compose 5.1.4

## Outcome

All 28 `AX-V1A-A03-*` requirements are verified for the A3 runtime-contract
scope. The implementation provides deterministic event/replay/scheduling
primitives, bounded in-process ownership queues, fenced mutation and messaging
contracts, fail-closed lifecycle/recovery gates, and their local conformance
tests. No authenticated exchange transport or external-order path was added.

The A3 memory lease and coordination repositories are deterministic conformance
models. They do not claim PostgreSQL durability. A4 owns PostgreSQL migrations,
generated queries, transactional repositories, and database constraint/fault
proof. The complete integrated shadow recovery profile must be timed again
after its A4-A11 dependencies exist and before V1A release certification.

## Requirement proof map

| Area | Requirements | Evidence |
|---|---|---|
| Canonical input and replay | FUN-001, FUN-002, FUN-012 | Envelope golden/negative tests, 1,000-way ordinal concurrency test, adversarial replay tests, replay fuzz target |
| Time, scheduling, and randomness | FUN-003, FUN-004, FUN-008, FUN-009 | Five-field ordering tests, optional-sentinel tests, pause/step/cancel/derived-work tests, keyed-stream reproducibility/separation/concurrency tests |
| Immutable decision inputs | FUN-005 | Market-view version, defensive-copy, complete-vector, canonical-hash tests |
| Bounded event flow | OPS-001, OPS-002, SAF-001, SAF-002, SAF-007, FUN-006 | Critical-loss, acknowledgement-boundary, market-generation invalidation/resync, projection coalescing, capacity/age/saturation and bounded-label metric tests |
| Ownership and fencing | SAF-003, SAF-004, FUN-010, QLT-002 | Concurrent overlapping owner, expiry/new epoch, stale token, storage loss, renewal/release, and atomic fenced-mutation tests |
| Durable coordination contract | FUN-007, OPS-003 | Command/inbox/outbox idempotency, conflict, revision cursor, snapshot/restart, storage-loss tests; dependency/Compose scan proves PostgreSQL-only coordination and no Redis |
| Safety state and recovery | SAF-005, QLT-006 | Startup `PAUSED`, recovery `LOCKED`, explicit activation, no auto-unlock, bounded shutdown/leak tests, ordered fourteen-gate readiness qualification |
| Composition boundary | FUN-011, SAF-006 | Exact twelve-stage causation test and source scanner rejecting HTTP/RPC, ambient time, shared randomness, unbounded channels, and second-broker dependencies |
| Determinism and quality | QLT-001, QLT-003, QLT-004, QLT-005 | Twenty 100-way schedule permutations, 64,000-event bounded-load test, race suite, fuzzing, benchmark, policy scans, and aligned architecture documents |

## Commands and measured results

The following checks completed successfully from the candidate working tree:

```text
go test ./internal/runtime
go test -race ./internal/runtime
go test ./...
go vet ./...
go run scripts/check_go_policy.go
scripts/check-file-policy.sh
node scripts/check-a3-runtime-boundary.mjs
```

The focused runtime package reported 83.0% statement coverage. The race suite
completed without a reported race. The declared-load test admitted and consumed
64,000 critical events while keeping queue depth at or below the configured 64.
The concurrency determinism test repeated 100 concurrent insertions twenty
times and obtained one canonical order hash.

The replay fuzz smoke ran for three seconds and completed 144,498 executions
without failure. The five-run scheduler benchmark reported:

```text
1427 ns/op  520 B/op  9 allocs/op
1211 ns/op  520 B/op  9 allocs/op
1188 ns/op  520 B/op  9 allocs/op
1133 ns/op  520 B/op  9 allocs/op
1209 ns/op  520 B/op  9 allocs/op
```

The median was 1,209 ns/op with 520 B/op and 9 allocations/op on the declared
local profile. This is runtime primitive evidence, not a later Trend or market
data latency claim.

The local recovery conformance profile completed all fourteen ordered gates in
1,037 ns and remained `LOCKED`; the lifecycle shutdown drill completed below
the configured one-second test limit with no owned worker leak. These qualify
the A3 framework against the five-minute/60-second bounds. They do not replace
the required later integrated restart, persistence, reconciliation, and soak
drills.

## Full repository gate

`make verify` passed once after the phase documents and traceability records
were finalized. It covered exact toolchain preflight, formatting, generated
contracts, documentation/traceability, Go and frontend lint, all unit and race
tests, all four fuzz-smoke targets, frontend typecheck/build, embedded assets,
all 32 active Compose profile combinations, secret/prohibited-capability
positive and seeded-negative scans, and `govulncheck` (no vulnerabilities
found). The expected jsdom canvas diagnostic remained non-fatal; both frontend
tests passed.

Final adversarial review then replaced ambiguous concatenated market-view keys
with length-prefixed exchange/base/quote identities and added the
`AB/CDE`-versus-`ABC/DE` collision fixture. After that narrow change, focused
unit and race suites, `go vet`, `staticcheck`, source policies, documentation,
the A3 boundary scan, and whitespace validation passed. A second
network-enabled `make verify` could not be authorized because the local
approval service reported its usage limit; no network workaround was used.

## Safety and limitations

- The repository still accepts only `backtest`, `replay`, `paper`, and `shadow`.
- A3 introduces no signer, credential-bearing exchange adapter, authenticated
  route, production broker, transfer, withdrawal, leverage, or short-selling
  capability.
- The pipeline is a composition contract. A4-A10 supply its storage,
  accounting, market-data, strategy, allocation, risk, and simulation handlers.
- PostgreSQL lease, inbox, outbox, command, and protected-write transactions are
  A4 work; the A3 interfaces require atomic resource-and-fence validation at the
  mutation boundary so A4 cannot substitute a check-then-write race.
- Pushes and hosted CI are intentionally not claimed; repository-owner
  integration is outside this local evidence record.
