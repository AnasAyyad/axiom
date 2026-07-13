# Glossary

| Term | Meaning in Axiom |
|---|---|
| A1 skeleton role | A process whose command, lifecycle, database readiness, and health boundary exist, while its later-phase business capability remains unavailable. |
| Backtest | Deterministic strategy evaluation over an approved immutable historical dataset with simulated execution. |
| Fencing token | Monotonically increasing ownership epoch that rejects writes from stale process owners. |
| Ingest ordinal | Session-local unique ordinal assigned before concurrent fan-out; with recorded logical time it defines dataset replay order. |
| Liveness | Whether the process loop can respond. It does not assert safe dependencies or strategy eligibility. |
| Paper | Credential-free simulation over historical or synthetic configured data; it never uses a live production feed. |
| Readiness | Whether the process's declared dependencies and invariants are currently safe. Readiness does not activate strategy entries. |
| Shadow | Live Binance production-public Spot input with internal simulated execution and a virtual journal; no exchange order is sent. |
| Virtual | A balance, order, fill, position, or result that exists only inside a research/simulation environment. |
| V1A real-money lock | The compile/config/database/API/UI/image/network proof that authenticated exchange and external-order capability is absent. |
