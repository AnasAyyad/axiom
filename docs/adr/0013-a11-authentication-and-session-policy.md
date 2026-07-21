# ADR-0013: A11 local authentication and opaque sessions

- **Status:** Accepted
- **Date:** 2026-07-16
- **Scope:** V1A private research console

## Context

V1A needs a single-owner console without adding external identity infrastructure
or any exchange credential path. Password verification, bootstrap, browser
sessions, authorization, CSRF, and administrative command evidence must fail
closed and remain independently replaceable by OIDC later.

## Decision

- The first owner is created only when the user table is empty and only from a
  file-backed email plus a precomputed Argon2id PHC hash. Plaintext bootstrap
  passwords are never accepted by the runtime.
- The current Argon2id profile is 64 MiB memory, three iterations, parallelism
  one, a fresh 16-byte salt, and 32-byte output. Verification accepts no profile
  below 19 MiB, two iterations, and parallelism one. A successful login
  automatically upgrades any accepted obsolete profile.
- Login failures use the same external response for unknown user, bad password,
  locked user, and malformed hash. Five failures for the durable normalized
  email/source scope in 15 minutes rate-limit subsequent attempts.
- Sessions use 32 random bytes in an opaque token. Only SHA-256 token hashes are
  stored. Absolute lifetime is 12 hours, idle lifetime is 30 minutes, and at
  most five active sessions are retained per user.
- Session cookies are host-only, `HttpOnly`, `SameSite=Strict`, and `Secure`
  outside the explicit local environment. Privilege changes revoke active
  sessions. High-risk recovery requires authentication within ten minutes.
- Mutations require an exact allowlisted Origin and a separately signed CSRF
  token bound to the stored session. Authorization checks explicit permissions,
  not UI visibility or role-name shortcuts.
- Authentication and signing inputs are mounted only into the API process. No
  exchange credential, exchange signer, private client, or broker is introduced.

## Consequences

An empty installation remains unready until valid bootstrap and signing files
exist. Existing installations ignore bootstrap inputs but still require CSRF
and signing keys. Password hashing deliberately consumes memory and CPU; the
profile must remain below one second on the declared server and is re-measured
when hardware or cryptography dependencies change.

The opaque-session repository is local and intentionally small. The service
contract keeps identity and authorization separate enough to add OIDC without
changing durable command permissions or the browser's CSRF/Origin boundary.

## Rejected alternatives

- Plaintext bootstrap password environment values: process inspection and
  diagnostics can expose them.
- Browser-stored bearer tokens: script access expands session-theft impact.
- Role-name checks in handlers: permission changes become inconsistent and
  difficult to audit.
- Disabling CSRF because the console is private: DNS rebinding and cross-site
  requests remain relevant outside an isolated developer host.

## Validation

- Benchmark current and minimum accepted Argon2id profiles and assert current
  hashing remains under one second on the declared profile.
- Exercise empty/existing bootstrap, transaction races, generic failures,
  durable rate limiting, session caps, idle/absolute expiry, revocation,
  privilege rotation, cookie flags, Origin, CSRF, and recent authentication.
- Inspect database rows, logs, errors, streams, image layers, and Compose mounts
  for plaintext credentials or session tokens.
- Verify engine, recorder, worker, and observability containers receive none of
  the authentication files.

## Revisit when

OIDC is introduced, the declared server changes materially, or reviewed password
storage guidance raises the accepted minimum. A changed current hash profile
requires automatic migration on successful login and updated qualification.
