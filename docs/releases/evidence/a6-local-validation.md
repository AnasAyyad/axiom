# A6 local phase-gate validation

**Date:** 2026-07-14

**Candidate branch:** `a6-candidate`

**Implementation commit:** `6bbda3029031c8226bfaaba3478ecba958176a2a`

**Parent:** `f8e3e0c4a343c1459b8f40ee90d5a6331a4b23ef` (A1-A5 merged to `origin/main`)

**Profile:** Linux amd64 WSL2, Go 1.26.5, Node 24.18.0, pnpm 11.12.0

## Outcome

All thirteen `AX-V1A-A06-*` requirements are verified for the A6 contract and
local-emulator scope. A6 provides narrow public market-data interfaces,
environment/version-aware capabilities, a complete typed error taxonomy,
operation-specific deterministic retry, a shared weighted rate budget, strict
Binance-style normalization, sanitized fixtures, and a real local REST/WebSocket
conformance emulator.

V1A exposes no callable authenticated exchange operation. Account, submission,
cancellation, identifier, and reconciliation features are descriptive
capability entries fixed to `unsupported`; requiring them returns a typed error.
This is the ADR-0006-compatible interpretation recorded in ADR-0010. It does not
defer a hidden implementation or add a configuration switch.

## Requirement proof map

| Area | Requirements | Evidence |
|---|---|---|
| Narrow contracts and DTO boundary | FUN-001, FUN-007, SAF-001 | Compile-time adapter assertions; package/import boundary scan; platform dependency scan; emulator adapter has only public methods |
| Versioned constrained capabilities | FUN-002, QLT-002 | Descriptor validation/serialization tests; exhaustive supported/unsupported Binance table; defensive-copy test |
| Canonical normalization and fixtures | FUN-003, FUN-006 | Exact-decimal golden fixtures; raw SHA-256/native fact assertions; unknown-status preservation plus typed rejection; schema/malformed/trailing-data tests; fixture JSON/redaction scanner |
| Error and retry policy | FUN-004, SAF-002 | Complete typed-error table; stable `errors.Is` behavior; bounded cancelable retries; official retry delay; deterministic keyed jitter; ambiguous-submission reconciliation classification |
| Shared rate budget | OPS-001 | Integer virtual-time refill, recovery reserve, impossible weight, regression, and 100-way contention tests |
| Deterministic emulator | FUN-005, QLT-001 | Exact GET matching, loopback REST/WebSocket sessions, reconnect generations, full fault matrix, repeat and golden transcript hash |
| V1A binary safety | SAF-003 | Prohibited-capability positive/negative scanner; platform dependency/build/binary scan; no emulator or WebSocket test dependency linked |

## Emulator fault evidence

The retained scenario exercises snapshots, streams, disconnect/reconnect,
duplicate/gapped/regressing/malformed/stale frames, slow response, throttling,
retry delay, filter/schema changes, asynchronous acknowledgement, partial and
late fill facts, ambiguous state, reset, and reconciliation snapshots.

Two clean executions produced the same canonical transcript hash, which is also
locked as a golden test value:

```text
e838f0921541040852261302b4749a26da850530dc1eb8d8693df0d4825c52dd
```

These later-state facts are inert local conformance frames. They are not an
authenticated client, an exchange request, or evidence of sandbox behavior.

## Commands and results

The final candidate tree passed:

```text
go test ./internal/exchanges/...
go test -race ./internal/exchanges/...
go test ./internal/exchanges/binance -run '^$' -fuzz '^FuzzNormalizePublicPayload$' -fuzztime 3s
go vet ./internal/exchanges/...
go tool staticcheck ./internal/exchanges/...
go run scripts/check_go_policy.go
scripts/check-file-policy.sh
node scripts/check-a6-exchange-boundary.mjs
scripts/check-prohibited-capabilities.sh
scripts/check-a6-binary-boundary.sh
go mod verify
git diff --check
make verify
```

The focused fuzz run completed 67,886 executions without failure before the
cumulative run. The cumulative verification passed exact preflight and format,
generated contracts, all documentation and A0-A6 boundary checks, Go/frontend
lint, all backend/frontend unit tests, the full Go race suite, all five fuzz
smoke targets, frontend/backend builds, 128 active Compose profile renders,
secret/prohibited-capability positive and seeded-negative tests, the A6 binary
boundary, and `govulncheck` with no vulnerabilities found. The expected jsdom
canvas diagnostic remained non-fatal; both frontend tests passed.

The first cumulative attempt stopped because the explicit validation PATH
omitted the repository-bundled `rg`; no project check had run. A later attempt
correctly rejected denied identifier literals in the new boundary scanner
itself. The scanner was rewritten without adding an exclusion, the primary
negative test passed, and the complete cumulative gate was rerun successfully.

## Safety result and limitations

- The platform binary dependency graph has 439 packages and contains neither
  the emulator package nor its WebSocket test dependency.
- Binary and source scans find no signer, exchange credential input, private
  route, authenticated client, callable external-order method, or later-release
  sandbox environment.
- The emulator binds only to an ephemeral IPv4 loopback listener and its adapter
  constructor accepts a server value, not an arbitrary URL.
- Production-public endpoint enforcement, DNS/redirect policy, reconnect,
  sequence-aware books, recording/manifests, and the 72-hour public-data soak
  remain A7 work. A6 does not claim those capabilities.
- Emulator behavior is deterministic contract evidence, not exchange
  availability, integration performance, fill realism, or profitability
  evidence.
