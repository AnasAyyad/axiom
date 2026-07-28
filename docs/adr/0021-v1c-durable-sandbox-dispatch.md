# ADR 0021: durable asynchronous sandbox dispatch

Status: accepted for V1C PR1.

The synchronous simulated broker remains unchanged. Sandbox delivery uses a
separate asynchronous dispatcher. Approval atomically writes the plan,
reservations, deterministic client IDs, non-refundable daily-cap reservation,
and per-account outbox legs.

Each account engine claims only its leg under a fencing lease. A response or
private event enters the durable normalized inbox before canonical reduction.
Ambiguous requests become `UNKNOWN`; they retain open-order capacity and
reservations until deterministic query, history, fills, events, balances, and
reconciliation resolve or quarantine them.

This preserves deterministic simulation while adding crash-safe external
virtual-environment behavior without an in-process HTTP/RPC boundary.
