## Purpose

This file is a short operating guide for coding agents working in the Axiom repository.

It is not the product specification.

Keep this file concise. Do not duplicate large sections of the main spec here.

---

<!-- codebase-memory-mcp:start -->

# Codebase Knowledge Graph (codebase-memory-mcp)

This project uses codebase-memory-mcp to maintain a knowledge graph of the codebase.
ALWAYS prefer MCP graph tools if presenr over rg/grep/glob/file-search for code discovery.

## Priority Order

1. `search_graph` — find functions, classes, routes, variables by pattern
2. `trace_path` — trace who calls a function or what it calls
3. `get_code_snippet` — read specific function/class source code
4. `query_graph` — run Cypher queries for complex patterns
5. `get_architecture` — high-level project summary

## When to fall back to rg/grep/glob

- Searching for string literals, error messages, config values
- Searching non-code files (Dockerfiles, shell scripts, configs)
- When MCP tools return insufficient results

## Examples

- Find a handler: `search_graph(name_pattern=".*OrderHandler.*")`
- Who calls it: `trace_path(function_name="OrderHandler", direction="inbound")`
- Read source: `get_code_snippet(qualified_name="pkg/orders.OrderHandler")`
<!-- codebase-memory-mcp:end -->

## Source of truth

Read documents in this order before making substantive changes:

1. `AGENTS.md`
2. `crypto_bot_v1_codex_spec.md`
3. `deploy/README.md` when working on Docker, deployment, secrets, operations, or profiles
4. Relevant files under `deploy/` and `monitoring/` for the area you are changing
5. Nearby tests and implementation files when application code exists

`crypto_bot_v1_codex_spec.md` is the authoritative product and architecture specification.

Do not silently weaken, reinterpret, or omit a requirement from the spec.

If the spec and the code disagree, treat the spec as authoritative unless the user explicitly asks to change the spec.

If a task requires a design choice the spec does not define:

1. Choose the safest reasonable design.
2. State the assumption clearly in your final summary.
3. Add tests if code exists.
4. Update supporting docs when those docs exist in the repo.

Do not reference files or directories that are not present in the repository.

---

## Current repository state

At the time of writing, this repository contains:

- The main V1 product and release spec in `crypto_bot_v1_codex_spec.md`
- Docker Compose deployment and infrastructure configuration
- Deployment notes under `deploy/`
- Monitoring configuration under `monitoring/`

It does not currently contain the full application implementation.

Do not invent placeholder application behavior and present it as operational.

When working on Docker or deployment files, assume the compose stack is deployment-oriented and image-based unless the repository later adds real build artifacts and runnable services.

---

## Non-negotiable product boundaries

These rules come from the main spec and must remain true in every V1 release.

- V1 must never place real-money production orders.
- V1 is spot-only.
- V1 must fail closed.
- Do not add any hidden or undocumented bypass for restricted capabilities.
- Do not implement or enable withdrawals, transfers, margin, futures, perpetuals, options, leverage, borrowing, lending, staking, or short selling in V1.
- Do not allow selling an asset the relevant virtual or demo portfolio does not own.
- Do not treat testnet or demo trading as proof of profitability.

If a change would make production order submission possible, stop and raise it immediately.

---

## Engineering rules

- Follow the phased V1A, V1B, V1C, and V1D delivery model from the spec.
- Do not collapse phased release gates into a single “everything now” implementation plan.
- Build a modular monolith first.
- Keep strategy, allocation, risk, accounting, reconciliation, and execution boundaries explicit.
- Design exchange integrations through shared adapter contracts.
- Keep exchange-specific details at the adapter boundary.
- Do not use binary floating-point for prices, balances, quantities, fees, P&L, allocations, or risk limits.
- Prefer explicit, test-covered rounding rules.
- Preserve determinism, auditability, and reproducibility.
- Keep the hot path in process; do not insert unnecessary HTTP or RPC boundaries.

---

## Security and secrets

- Never commit secrets.
- Never print secrets.
- Never place secrets in fixtures, screenshots, logs, traces, or error messages.
- Use placeholder values only in committed examples.
- Treat secret files, database dumps, exported account data, and local market recordings as sensitive.

If you discover a secret in tracked files:

1. Stop.
2. Remove the exposure from the working tree if the task allows it.
3. Report it clearly.
4. Do not repeat or display the secret.
5. Recommend credential rotation.

---

## Docker and operations guidance

- Use Docker Compose for local infrastructure and single-server shadow or sandbox deployment work, consistent with the main spec.
- Prefer minimal, deployment-safe changes.
- Keep public ports bound narrowly unless the deployment design explicitly requires broader exposure.
- Respect profile-gated services in the compose stack.
- Do not claim the application stack is runnable if the required image, binary, or source code is not present.

When editing deployment files, also check `deploy/README.md` for the intended operating model.

---

## Working style

Before editing:

1. Restate the task briefly.
2. Inspect only the files needed for the current change.
3. Prefer the smallest safe implementation.
4. Check whether the requested behavior is already constrained by the spec.

While editing:

- Keep changes narrow and local.
- Do not refactor unrelated code.
- Do not invent extra architecture without evidence.
- Do not add new dependencies unless clearly justified.
- Keep documentation aligned with implementation.

After editing:

1. Run the narrowest relevant validation.
2. Fix only issues related to the task.
3. Report what changed, what was validated, and any remaining limitations.

---

## Validation expectations

Use the smallest relevant checks available for the change:

- Targeted tests
- Targeted type checks
- Targeted linting
- Targeted build or config validation

For documentation-only changes, verify path correctness and internal consistency.

Do not claim a check passed unless you actually ran it successfully.

---

## Definition of done

A task is complete only when:

- The requested change is implemented or the blocker is explained.
- Relevant validations were run when available.
- Safety constraints remain intact.
- Documentation still matches repository reality.
- No forbidden capability was introduced.
- Remaining limitations or assumptions are stated honestly.

---
