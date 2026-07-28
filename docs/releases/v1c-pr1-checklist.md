# V1C PR1 C1-C3 checklist

## C1

- [x] Freeze neutral contracts and default-off V1C policy.
- [x] Add file-only credentials, fixed signers, endpoint matrix, redacted
  evidence, closed proxies, and deterministic authenticated emulators.
- [x] Add startup identity and permission validation.
- [x] Pass C1 source, binary, endpoint, secret, and Compose-render capture gates.
- [x] Pass image-backed Compose smoke on a truthful clean candidate.

## C2

- [x] Add TOTP, replay prevention, one-use authorizations, sandbox RBAC,
  high-risk audit schema, revoke-all foundation, and rotation state machine.
- [x] Pass clean PostgreSQL and exact B8 upgrade qualification.
- [x] Pass the complete C2 authentication/race gate.

## C3

- [x] Add asynchronous dispatch, atomic caps/reservations/outbox, normalized
  inbox, fencing, unknown quarantine, ordered startup, and kill points.
- [x] Add migration `000022`.
- [x] Pass PostgreSQL, property/model, race, replay, duplicate, timeout, and
  exhaustive kill-point qualification.

## PR decision

- [x] `c1-security-qualify` passes.
- [x] `c2-auth-qualify` passes.
- [x] `c3-recovery-qualify` passes.
- [x] `v1c-pr1-local-qualify` and cumulative `make verify` pass.
- [x] Dirty local image reproducibility, minimal non-root/read-only inspection,
  SPDX, and current-database Trivy gates pass.
- [x] Confirm dirty-image smoke fails closed at build admission before engine
  readiness.
- [x] Freeze a committed candidate and pass image-backed Compose smoke.
- [ ] Security and owner acceptance recorded.
