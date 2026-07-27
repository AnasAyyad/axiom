# A7 acceptance — repository-owner waiver

**Decision date:** 2026-07-26

**Acceptance state:** Accepted — owner waiver

**Remediation ID:** `A7-RSK-2026-07-26-001`

**Expiry:** `2026-08-26T00:00:00Z`, or replacement by a passing exact-source
A7 run, whichever occurs first.

## Machine result retained without reinterpretation

The immutable formal run is `/srv/axiom-data/qualification/a7-da47d14-r1`
from exact source `da47d143ac26806eb8f318d8e141f396d5576fea`.
Its terminal machine result remains `qualified: false`. The exact terminal
failures remain:

- `BTCUSDT_slo_failed`
- `ETHUSDT_slo_failed`
- `ETHUSDT_ineligible`

The run completed 72 continuous hours. Dataset integrity, canonical replay,
cumulative manifests, bounded memory, public-only capability boundaries, and
the required duration passed. It verified 45,200,151 records across 1,359
segment pairs. The failed availability measurements remain visible:

- BTCUSDT: 36 resynchronization samples, 10 over 15 seconds, p95 bucket
  90 seconds, exact maximum 188.693531862 seconds.
- ETHUSDT: 31 resynchronization samples, 7 over 15 seconds, p95 bucket
  90 seconds, exact maximum 226.358140255 seconds.

The unchanged 15-second SLO is not weakened and the failed machine result is
not converted to `qualified: true`.

## Waiver scope and rationale

The repository owner supplied this decision on 2026-07-26. The waiver is
non-safety and is limited to public-market-data availability and
resynchronization timing. It does not waive data integrity, replay,
manifest/checksum, memory, public-only, no-order-path, credential, accounting,
risk, or real-money-lock requirements.

The retained lifecycle evidence attributes the long recovery tail to
availability/recovery behavior, while preserving objective attribution
separately from duration. Duration alone never assigns blame. The remediation
work is tracked under `A7-RSK-2026-07-26-001`; a passing exact-source rerun
supersedes this temporary acceptance.

## Immutable artifact identity

- Terminal evidence SHA-256:
  `5f94f8f5972f200af36ed98019c35373379fed02dcb8ccd27e557bb9e08d19a5`
- Rolling status SHA-256:
  `717d01588173dc7990567d352b476d55c7a9410467024f2ab3b08bef138feea3`
- Event journal SHA-256:
  `7c1a33bfda051fd1ae04c46d2bf0822eafca59bbfe30eea56aef37872fee2f53`
- Manifest hash:
  `a0168217c65f7b8e7120105cc955d1e244882be4306b281eaf4ff2b5c9b84204`
- Replay checksum:
  `6820097d931b677c8f1b7fe613a355b0cc3aaa076d8dce1407f8bde83573bf2e`
- Journal terminal hash:
  `4ca3a16479fd8095336925a8f4cccf65787bf6831833cbf220c32b1a9fce6eb4`

Terminal JSON, status, journal, service logs, manifests, Parquet data, and
checksums remain unchanged in the retained qualification directory.
