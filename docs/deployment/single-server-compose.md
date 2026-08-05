# Single-server Compose deployment

The canonical operating detail is in `deploy/README.md`; this document defines
the architecture contract.

The base project starts PostgreSQL only. Image-backed application, record,
worker, observability, edge, backup, restore, sandbox-foundation, sandbox, and
formal observer functions are profile gated. Services use numeric non-root
users, read-only root filesystems, dropped capabilities, bounded resources,
explicit health checks, least-privilege database roles, narrow networks, and
operator-supplied bind mounts. Server images must use immutable digests.

Public application roles receive no exchange credentials. Each authenticated
Testnet/Demo engine owns only its exchange credentials and database lease, has
no direct external network, and can reach only its credential-free egress proxy.
The proxies enforce compiled exact host/IP policy and receive no credentials.
There is no production-private destination or broker.

Validate all profile combinations with `make compose-validate`. Upgrade by
backing up, validating disk/clock/identity, applying the one-shot migration, and
checking readiness before writers. Use forward fixes for schema changes; a
rollback image must remain schema compatible. The independent backup filesystem
and clean restore paths are outside Compose volume ownership.

Single-server operation is not high availability. Formal claims require a
completed server declaration, immutable render/config/image hashes, clean
deploy/restore evidence, numeric SLO results, and independent review.
