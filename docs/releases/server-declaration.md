# Reference server declaration

Status: **UNDECLARED / NOT ACCEPTED**.

The owner must complete and independently review this record before any formal
D5 run. Never place credentials or secret-file contents here.

| Field | Required value |
|---|---|
| Declaration ID | `CHANGE_ME` |
| Owner and independent reviewer | `CHANGE_ME` |
| Physical/virtual provider and region | `CHANGE_ME` |
| CPU model/count and memory | `CHANGE_ME` |
| PostgreSQL, market-data, staging, and remote-backup filesystem identities | `CHANGE_ME` |
| Network/TLS termination and firewall policy hashes | `CHANGE_ME` |
| OS/kernel/container runtime/Compose versions | `CHANGE_ME` |
| Exact source SHA and clean state | `CHANGE_ME` |
| Application and backup image digests | `CHANGE_ME` |
| SBOM, configuration, Compose render, dataset, and D5 manifest hashes | `CHANGE_ME` |
| Numeric load/SLO/RPO/RTO declaration | `CHANGE_ME` |
| Clock source and measured offset | `CHANGE_ME` |
| Signed approval timestamp and expiry | `CHANGE_ME` |

Changing any identity after preflight invalidates the declaration and requires a
new run; failed formal evidence is terminal and cannot be restarted or relabeled.
