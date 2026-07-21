# V1B readiness and release evidence

## Current decision

**V1B is not ready for release.** B1 implementation is complete; B2-B8
are planned. Formal V1B verification is blocked until V1A closes, regardless of
local V1B implementation progress.

## Release identity

| Field | Value |
|---|---|
| Pre-V1B baseline | `70ba3f74addee3d19ef529434122dfabd357d3c5` |
| B1 source commit | Pending |
| V1A accepted evidence | Pending A7 and dependent gates |
| B1 configuration hash | `8a5ada09d2e689d33f92f567d569ddc74cd6aae24bce55e8805958a77cf0685a` |
| Binance/Bybit dataset manifests | Pending |
| B1 image digest and SBOM | Pending |
| Short public validation | Passed locally at `2026-07-21T12:48:50Z`; acceptance pending |
| B1 72-hour soak | Pending |
| Product / Security / QA / SRE approvers | Pending |

## Phase gates

| Phase | Entry dependency | Implementation | Verification | Evidence |
|---|---|---|---|---|
| B1 | A6 verified; owner-authorized overlap with open A7 | Implemented | Pending live/PG evidence | [B1 local validation](evidence/b1-local-validation.md) |
| B2 | B1 verified | Planned | Not started | Pending |
| B3 | B2 verified | Planned | Not started | Pending |
| B4 | B3 verified | Planned | Not started | Pending |
| B5 | B4 verified | Planned | Not started | Pending |
| B6 | B5 verified | Planned | Not started | Pending |
| B7 | B6 verified | Planned | Not started | Pending |
| B8 | B7 verified | Planned | Not started | Pending |

## Evidence rules

- Every result identifies source, configuration, toolchain, command, UTC time,
  database/dataset identity, outcome, and retained artifact location.
- Local unit or integration results can prove implementation, never a 72-hour
  live gate or formal release acceptance.
- Public validation captures no credentials and stores no sensitive headers.
- Historical V1A manifests and checksums are immutable and are never rewritten
  into a V1B schema.
- Corrections use a new evidence revision with an explicit supersession link.

## Known limitations

- A7 formal qualification and dependent V1A acceptance remain open.
- B1 cannot be marked verified until its PostgreSQL clean/upgrade gate, short
  public validation, and continuous 72-hour declared-load soak are accepted.
- B2-B8 source is outside the current goal and must remain unimplemented here.
- V1B has no authenticated exchange transport, private endpoint, external
  order, withdrawal, transfer, testnet, demo, or live execution capability.
