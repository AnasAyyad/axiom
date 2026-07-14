# V1A accounting and reservation rules

**Status:** Normative A4 contract; domain conformance model implemented,
PostgreSQL integration in progress

## Source of truth and arithmetic

The immutable multi-commodity journal is the accounting source of truth for
backtest, replay, paper, and shadow. Every quantity is an exact project-owned
decimal value; prices, balances, quantities, fees, P&L, reservations, and
valuation never use binary floating point.

Each journal transaction balances debits and credits independently for every
asset. A USDT debit cannot be offset with a numerically equal BTC credit. Lines
always contain a positive quantity plus an explicit debit or credit direction.
Historical headers and lines are append-only. Correction uses a linked reversal
or compensating transaction and never edits prior history.

A journal header starts unsealed inside its creation transaction. Its exact
lines may be inserted only while that transaction remains unsealed; the deferred
per-asset constraint seals the header at commit. Later line insertion and every
header/line update or deletion are rejected, including a later balanced append.

A linked reversal must reference an existing transaction, may occur only once,
and must contain the exact multiset of original account/asset/quantity/metadata
lines with debit and credit directions exchanged. A merely balanced but
economically unrelated transaction cannot claim reversal identity. The same
rule is enforced by the in-process journal and a deferred PostgreSQL commit
constraint; other corrections are explicitly typed compensating transactions.

## Chart of accounts

The closed A4 account classes are:

- external/equity;
- available asset and reserved asset;
- strategy inventory and exchange inventory;
- trade cost/proceeds;
- fee expense;
- spread, slippage, and latency attribution;
- realized and unrealized P&L;
- inventory valuation;
- rebalancing expense and recovery loss;
- rounding/dust; and
- reconciliation suspense.

The asset and account owner remain separate key dimensions. Projection rebuilds
retain debit and credit totals per exact account/asset key, sort canonical keys,
and hash the canonical result. A materialized balance, position, cost-basis, or
P&L projection is disposable unless it reproduces this journal-derived result.

## Posting rules

A virtual buy posts quote-asset cost/proceeds against the owned quote account
and base-asset inventory against the external/equity counter-account. A sell is
the inverse and posts realized P&L separately under the weighted-average
cost-basis policy. Fees use their actual or versioned simulated fee asset and a
separate fee-expense line. Spread, slippage, latency, recovery, rebalancing, and
dust never collapse into fee or realized-P&L accounts.

Third-asset fees and rebates require balanced lines in that third asset. An
unknown difference enters reconciliation suspense, links an incident, and
blocks affected entries until an evidence-backed compensating adjustment
resolves it.

The guarded journal boundary rejects every invalid append and sends only
transaction/run/portfolio identity plus a stable failure code to the required
pause-and-incident handler; it does not copy quantities or caller payloads into
error evidence. If incident persistence or pausing fails, the append still fails
closed with `journal_failure_handler_failed`. Durable PostgreSQL coupling to the
A5 incident implementation remains an integration gate.

## Reservations and ownership

A reservation atomically moves a positive exact amount from available to
reserved under one virtual account, asset, revision, and fencing token.
Concurrent requests serialize at the balance row; the update succeeds only when
owned availability is sufficient. This makes over-reservation and negative
available/reserved balances impossible rather than detectable after commit.

Only an active reservation at its expected revision and fence may transition
once to consumed, released, expired, or quarantined. Consumption reduces the
reserved projection. Release or safe expiry returns it to availability. Active,
unknown-order, cancel-pending, or recovery-required ownership is not expired or
released; those later order-aware decisions remain fail closed. Quarantine closes
the reservation lifecycle revision but leaves the uncertain quantity reserved;
ordinary release/consume calls cannot reopen it. Only a later reconciled,
audited adjustment may resolve that ownership.

## Transaction boundary

Where one simulated fill changes order state, reservation ownership, journal,
balance/position projections, inbox identity, audit evidence, and outbox event,
all facts commit in one PostgreSQL transaction. The transaction uses row locks
or revision/fencing predicates and acknowledges only after commit. A retry is
idempotent by stable order/fill/inbox/transaction identity.

The deferred PostgreSQL journal constraint rejects a transaction whose stored
lines do not balance per asset. Database constraints also enforce exact numeric
columns, nonnegative balances, unique client/fill/inbox/run identities,
monotonic lease epochs, legal order/reservation transitions, and immutable
history.

## Cost basis and P&L

V1A uses versioned weighted-average cost by virtual account, strategy inventory,
and asset. A buy increases quantity and adds exact acquisition cost including
the policy-selected fee treatment. A partial sell removes cost at the prior
weighted average; realized P&L is exact proceeds less removed cost and separately
attributed fees/spread/slippage/latency. Remaining quantity keeps the same unit
cost except for explicit inventory adjustments. Quantization residual remains in
the lot's exact total cost rather than silently re-averaging the unit cost, and
the final sale removes that residual exactly. Zero remaining quantity resets the
carried unit cost to exact zero.

Unrealized P&L is a rebuildable valuation fact tied to a specific eligible
market-view and valuation policy version. It never mutates realized history.
USDT is the V1A functional reporting asset; USD valuation remains disabled.

## Evidence and current limitation

`internal/accounting` currently proves per-asset journal balance, immutable
defensive history, canonical projection rebuild, revision/fence reservation
lifecycle, and concurrent double-spend rejection. The embedded A4 migrations
define the corresponding relational constraints and append-only triggers.

A4 is not complete until generated-query repositories, clean PostgreSQL role and
constraint integration tests, actual Parquet/Zstd encoding/decoding, kill-point
transaction tests, encrypted independent backup, and clean timed restore have
all passed. The current memory models and crash-safe file protocol are not
substitutes for those durable gates.
