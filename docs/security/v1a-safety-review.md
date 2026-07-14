# V1A initial safety review

## Review metadata

| Field | Value |
|---|---|
| Review status | **PASSED for the A0 architecture gate; not a V1A release certification** |
| Review date | 2026-07-12 UTC |
| Reviewer | Independent Codex A0 safety audit (`a0_safety_audit`) plus primary conformance review; no claim of an external human audit |
| Scope reviewed | A0 safety architecture, current deployment/configuration surface, Compose renders, and static scanner behavior; no application binary, release image, or runtime traffic exists yet |
| Intended V1A capability | Binance production-public Spot reads plus internal backtest, replay, paper, and shadow simulation only |
| Authority | User-supplied specification and end-to-end V1A plan; Product owner Anas Abu-Sulik |
| Re-review basis | Every later phase, material safety-policy change, and the final immutable release candidate |

## Decision

The A0 architecture and current deployment contract contain no allowed path
from a strategy, browser, configuration value, or service to an exchange order
endpoint. A prior draft contained later-release service and credential
placeholders; those findings are closed in the current worktree rather than
accepted as disabled capabilities.

Review evidence on 2026-07-12:

- all 32 combinations of the `app`, `record`, `workers`, `observability`, and
  `edge` profiles render and list only PostgreSQL plus the V1A API, shadow
  engine, recorder, worker, migrator, monitoring, and edge roles;
- the premature plaintext database-backup command is absent; A4 owns the
  encrypted backup image, independent key/storage contract, and restore proof;
- `.env.example` selects the closed `market-data-only-v1` endpoint set and
  contains no exchange URL, credential key, private route, or external-order
  enablement setting;
- Compose, deployment notes, and Prometheus contain no authenticated exchange
  service, credential mount, private endpoint, or later-release executable
  profile;
- `scripts/check-prohibited-capabilities.sh` passes the worktree and its seeded-
  negative self-test proves representative violations fail with redacted
  diagnostics; and
- mode, endpoint, topology, lifecycle, threat, secret, and risk policies agree
  that V1A contains simulators and public Binance reads only.

This is design/static evidence only. The final release must repeat the test plan
against an identified clean commit and additionally inspect generated contracts,
the linked binary, container image, browser bundle, database constraints, and
captured outbound traffic. Those later obligations do not reopen the resolved
A0 design question unless implementation diverges from this architecture.

## Trust-path review

The architecture under review permits only this exchange boundary:

```text
Binance public allowlisted HTTPS/WSS host
-> credential-free public client
-> raw recorder
-> validation and normalization
-> local market views
```

The required decision path remains internal:

```text
strategy opportunity
-> allocator
-> risk engine
-> simulation execution planner
-> in-process simulated broker
-> virtual journal
```

The design requires the simulated broker to have no authenticated transport.
API commands are durable administrative requests, not exchange requests.
Browser clients cannot supply an exchange endpoint, credential, execution mode,
or serialized order payload. These are design requirements pending
implementation and negative-test evidence, not claims about a runnable system.

## Ownership and accounting conclusions

- The design assigns one database lease and increasing fencing token to each
  execution resource.
- Strategies may not own brokers, balances, or journal writers.
- Exclusive reservations must mediate virtual ownership before risk or
  simulation.
- Journal transactions must balance independently by asset; mismatches create
  incidents and pause the affected scope.
- Unknown, stale, gapped, unreconciled, or non-durable state must fail closed.
- Initial risk state, hierarchy, permissions, hysteresis, and manual recovery
  are reviewed separately in the
  [V1A risk-policy review](risk-policy-review.md).

## Later evidence gates

Implementation tests, binary and image inspection, database constraints,
outbound capture, deterministic replay, soak evidence, and an independent
clean-build review remain mandatory before V1A release. Passing A0 does not
claim any of those later gates.

Any implementation that introduces an authenticated exchange constructor, signing primitive, credential field, private endpoint, external order method, or later-release execution service invalidates this review and must stop immediately.
