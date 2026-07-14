# V1A asset registry policy

## Purpose and boundary

Axiom records owner-supplied asset eligibility; it does not make or present religious rulings. Asset status is a versioned safety input enforced independently of instrument metadata, strategy configuration, and market-data availability.

The only statuses are:

| Status | Observation | Simulated entry/fill | Existing exposure |
|---|---|---|---|
| `approved` | Allowed | Allowed after every other allocator/risk check | Managed normally |
| `scan_only` | Allowed | Rejected | Review; no fabricated exit |
| `blocked` | Optional diagnostics only | Rejected | Pause entries and require documented risk review |
| `pending_review` | Allowed only when the configured universe permits observation | Rejected | Quarantine from executable use |

Unknown, missing, expired, contradictory, or unversioned status fails closed as non-executable. A high liquidity or quality score never overrides status.

## V1A initialization

The initial registry version contains exactly these approved assets:

- `USDT`, the virtual settlement and functional reporting asset;
- `BTC`; and
- `ETH`.

The executable V1A instrument universe is Binance Spot `BTC-USDT` and `ETH-USDT`. A valid instrument requires both assets to be `approved`, product category `spot`, current eligible metadata, and the owning virtual portfolio's allocator/risk approval. A simulated sell is additionally capped by owned available inventory.

## Versioning and changes

Each immutable registry version stores the canonical asset ID, status, effective UTC time, actor, reason, approval identity, prior version, configuration hash, and monotonic audit revision. Used versions are never edited or deleted; correction creates a new version and audit event.

A complete registry snapshot is part of every run and decision identity. Reload validates the whole registry and swaps one immutable snapshot for new decisions. It never changes the rules recorded on an existing order or historical result.

A move away from `approved` blocks new entries immediately and triggers review of existing virtual exposure. It does not silently liquidate, transfer, relabel, or manufacture inventory. Restoring `approved` status requires an authenticated, authorized, reasoned, audited change plus current risk and data checks; it never auto-resumes a paused strategy.

## Ownership and evidence

Product owns the supplied status and rationale; Security owns authorization/audit integrity; Risk owns immediate entry enforcement and exposure review; Storage owns immutable versions; API/UI must show status and history without implying a ruling.

Required evidence includes schema-enum and unknown-status rejection tests, immutable version/audit tests, asset-plus-instrument enforcement at allocation and immediately before simulated fill, reload/restart tests, owned-inventory sell tests, and UI/API history and labelling checks. These are later implementation gates; this file defines the A0 contract.
