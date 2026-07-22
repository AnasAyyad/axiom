# Axiom V1B implementation status

This tracker separates implemented behavior, local verification, and formally
accepted evidence. V1B work may proceed while A7 qualification remains open,
but V1B cannot be released until V1A and the deferred phase soaks are accepted.

## Current program identity

| Field | Value |
|---|---|
| Program baseline | merged B2 `main` at `0c2fce26cae9e171d4e622c080aaf9af5cab018f` |
| Baseline gate | B2 merge PR #13 completed before B3 source changes; the B3 branch was created from the exact merged SHA |
| Active implementation phase | B3 locally verified for every specified non-soak gate; formal predecessor and approver holds remain |
| Later implementation phases | B4-B8 planned; no implementation claimed |
| External side effects | Impossible: public data and simulation only |

## Phase progress

| Phase | Owner | Migration | Status | Scope | Evidence |
|---|---|---:|---|---|---|
| B1 | Bybit Adapter / Exchange Platform | 000012-000013 forward fix | Locally verified; formal soak hold | Credential-free Bybit public adapter, common ticker/lifecycle contracts, three-instrument multi-exchange recording, PostgreSQL clean/upgrade, exact image, and short live qualification | [B1 local validation](evidence/b1-local-validation.md) |
| B2 | Market Data / Storage | 000014 | Locally verified; formal soak hold | Coherent cross-market views, clock uncertainty, Tier-A manifests, and deterministic as-of joins | [B2 local validation](evidence/b2-local-validation.md); short live coherent view passed at 59.569181 ms / 40.927081 ms |
| B3 | Strategy / Research | 000015 | Locally verified; formal predecessor/approver hold | Exact completed-candle mean reversion, shared allocator/risk/execution/accounting, immutable decision/research evidence, and clean image qualification | [B3 local validation](evidence/b3-local-validation.md) |
| B4 | Strategy / Execution | 000016 | Planned | Exact triangular arbitrage, atomic claims, sequential simulation, and recovery | Pending |
| B5 | Strategy / Portfolio | 000017 | Planned | Coherent cross-exchange arbitrage and inventory economics | Pending |
| B6 | Portfolio / Research | 000018 | Planned | Advisory-only rebalancing graph and immutable transfer facts | Pending |
| B7 | Research / Data Science | 000019 | Planned | Multi-strategy statistical validation and audited promotion evidence | Pending |
| B8 | API / Frontend / SRE | 000020 | Planned | Generic multi-exchange API, SSE, console, and operational workflows | Pending |

## Locked sequencing

Each phase is implemented on a sequential branch from the latest merged
predecessor. B3 started from merged B2 `main` at
`0c2fce26cae9e171d4e622c080aaf9af5cab018f` and is locally verified. B4 source
must start only from the B3 completion merged into `main`. A phase may be
`Locally verified` after every non-soak gate passes; accepted predecessor, soak
where specified, and approver evidence changes it to formally accepted.

## Program references

- [V1B requirements](../requirements/v1b-traceability.md)
- [V1B source coverage](../requirements/v1b-source-coverage.md)
- [V1B phase checklist](v1b-phase-checklist.md)
- [V1B readiness](v1b-readiness.md)
- [Authoritative specification](../../crypto_bot_v1_codex_spec.md)
