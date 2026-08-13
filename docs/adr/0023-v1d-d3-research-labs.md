# ADR-0023: V1D D3 research-lab evidence and control model

- **Status:** Accepted
- **Date:** 2026-08-03
- **Scope:** Backtest, replay, and public-data shadow laboratories

## Context

The repository already has deterministic backtest and replay workers, replay
fault scheduling, durable job state machines, and a simulation-only shadow
runtime. The earlier console exposes these engines, but its forms show raw IDs
and its result pages do not preserve enough input/build provenance for guided
comparison and reproduction.

## Decision

D3 keeps execution inputs closed and immutable. Guided presets explain the
approved strategy and versioned model assumptions; the browser sends only the
configuration, dataset, research generation, strategy, seed, replay speed, and
optional incident window supported by the server contract. The advanced panel
does not add an alternate parameter or execution path.

Job projections include the original safe input manifest, state-dependent
lifecycle capabilities, and a reproduction bundle assembled from the durable
run manifest. Before a worker creates a run, the input identity remains
available and run-manifest fields are absent rather than fabricated. Generic
D1 exact-revision commands remain the authority for pause, resume, cancel, and
reproduce. Generic D1 export artifacts remain the only download path.

Shadow projections add deterministic history plus public-only decisions,
risk outcomes, virtual balances and positions, ledger attribution, and public
connection health. All numeric financial values remain decimal strings.

The UI follows the reviewed shadcn accessibility patterns for tabbed guided
workflows, progressive advanced disclosure, progress, and confirmations. It
uses existing Axiom tokens rather than adding a second component or styling
runtime.

## Consequences

- A comparison is an exact client projection of two authoritative resources;
  it never reruns or mutates either source.
- Reproduction creates a new durable job with the original request and payload
  hash while keeping the source job visible in audit evidence.
- Exported lab records contain safe identity and hash fields, not request
  payloads, private exchange data, credentials, headers, or signatures.
- D3 cannot prove profitability and does not replace B2, C6, or D5 evidence.

## Validation

- OpenAPI/generated-type parity and handler authorization tests.
- Lifecycle, manifest, comparison, export-redaction, restart, cancellation,
  quota, replay-fault, and shadow-projection tests.
- Strict frontend typecheck, lint, unit tests, build, and Playwright workflows
  across the supported desktop, tablet, and mobile matrix.
- Secret and prohibited-capability scans.
