# Axiom research environment

This directory is a locked, cold-path Python 3.12.3 environment for independent
indicator checks and report validation. It is not copied into the production
image, is not mounted into `engine-shadow`, and cannot authorize a decision.
The Go Trend and Mean Reversion implementations remain authoritative. Trend
uses its original report contract; B3 uses the separate
`mean-reversion-report.v1` contract so later evidence cannot silently weaken or
reinterpret Trend evidence.

Reusable logic belongs in `src/axiom_research/` and is covered by `tests/`.
Notebooks are presentation-only and must import the tested modules rather than
reimplement strategy rules.

Run the dependency-free validation with:

```bash
PYTHONPATH=research/src python3 -m unittest discover -s research/tests
```
