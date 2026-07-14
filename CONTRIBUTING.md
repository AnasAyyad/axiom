# Contributing to Axiom

Read `AGENTS.md`, the complete authoritative specification, accepted ADRs, the
current implementation tracker, and the owning phase requirements before a
substantive change. Work proceeds in phase order; a later feature cannot waive
an earlier gate.

## Safety first

Stop immediately if a change can introduce an authenticated exchange client,
signer, private endpoint, external-order side effect, prohibited product, or a
sale not backed by owned virtual/demo inventory. V1A accepts only `backtest`,
`replay`, `paper`, and public-data `shadow`; every other execution mode fails
closed.

Never commit or print secrets, local `.env` values, dumps, or market recordings.
Use generated non-secret values only in ephemeral tests. A scanner exception
must be narrower than the rejected construct and must include a negative test.

## Change workflow

1. Link the change to stable requirement IDs and its owning phase.
2. Update or add focused tests before implementation where practical.
3. Keep the smallest coherent vertical slice and explicit module boundaries.
4. Run the narrowest checks during development, then `make verify` before handoff.
5. Update contracts, generated files, docs, ADRs, runbooks, status, and evidence.
6. Do not mark a phase complete until every acceptance item has retained proof.

## Repository commands

`make help` lists the stable interface. Common targets are `preflight`, `deps`,
`contracts`, `format`, `lint`, `test`, `test-race`, `fuzz-smoke`, `build`,
`compose-validate`, `security-static`, `vulnerability`, and `verify`.

Use conventional, focused commits when a repository history exists. Do not
rewrite or discard another contributor's working-tree changes. Generated API
files are updated only through `make contracts`.
