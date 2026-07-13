# ADR-0009: Compose file-secret consumer groups

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** A1 local/single-server Docker Compose secret delivery

## Context

Docker Compose implements a `file:` secret source as a bind mount. Its official
service reference states that UID, GID, and mode remapping are ignored for that
source type. An owner-only host file therefore cannot be read by both the pinned
PostgreSQL initializer and a distinct non-root Axiom process. Making the file
world-readable would contradict the secret policy.

V1A still requires file delivery instead of password values in environment
variables, command arguments, image layers, labels, or URLs. The A1 runtime is
`scratch`, has no root bootstrap helper, and must remain non-root from process
start.

## Decision

- Local file-backed Compose secrets use mode `0640` or `0440`, never an
  other-readable, writable-group, or executable mode.
- Database password files use the pinned `postgres:18.4-alpine` group GID `70`.
  The Axiom image runs as UID `10001`, primary GID `70`, and is granted only the
  database password file needed by its service.
- Grafana's password file uses its separately pinned consumer group. It is not
  mounted into an Axiom or PostgreSQL process.
- Each service receives secrets through an explicit Compose `secrets` grant.
  Sharing a group does not grant a file that is not mounted into the container.
- The runtime secret reader rejects symlinks, changed files, oversized or
  placeholder contents, group membership mismatches, group write/execute bits,
  and every other-access bit. It never reports the value or raw path.
- Production-like deployments should replace local file-backed delivery with a
  reviewed secret manager or platform secret that provides per-task ownership.
- An image change that alters a consumer UID/GID blocks deployment until the
  file ownership procedure, rendered mounts, and runtime read/rejection tests
  pass again.

## Consequences

The local Compose model remains usable by non-root containers without exposing
secret values through ordinary environment variables or making files
world-readable. The cost is an explicit numeric-GID coupling to pinned images
and operator provisioning that requires `chgrp` privileges.

This decision does not allow a shared secret directory mount. Files remain
individually granted, and later A11 authentication secrets are absent until the
authentication subsystem is implemented.

## Rejected alternatives

- Mode `0444`: every process in the container could read the secret.
- Password environment variables: process inspection and diagnostics can expose
  them, and the A0 policy forbids the path.
- Start Axiom as root and drop privileges after reading: this violates the
  non-root runtime invariant and expands the startup attack surface.
- A root shell init container that copies every secret: it adds a privileged
  secret concentrator and mutable staging volume before the need is proven.
- Duplicate password files per consumer: rotation can diverge and create an
  inconsistent credential boundary.

## Validation

- Render every Compose profile and verify per-service mounts and numeric users.
- Start the pinned PostgreSQL and Axiom images with `0640` generated placeholder
  files and prove only explicitly granted consumers can read them.
- Table-test `0400`, `0600`, `0440`, and `0640` acceptance for the owning or
  consumer group and rejection of symlinks, group writes, and other access.
- Inspect the image for numeric non-root user, no shell, and no secret content.
- Repeat the checks on every PostgreSQL, Grafana, or Axiom base-image change.

## Revisit when

Compose supports ownership remapping for host-file secrets, the deployment
adopts external/platform secrets, or a pinned consumer UID/GID changes. A
replacement requires a superseding ADR and updated threat/deployment evidence.
