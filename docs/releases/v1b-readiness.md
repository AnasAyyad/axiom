# V1B readiness and release evidence

## Current decision

**V1B is not ready for release.** B1 is locally verified for every non-soak
gate and is merged into `main`. Formal B1 acceptance is held only for A7/V1A
closure, the explicitly deferred B1 72-hour soak, and approver acceptance. B2
is locally verified for every implemented non-soak gate, including a passing
short live coherent view and retained Tier A evidence. Formal B2 acceptance is
held by predecessor acceptance, its explicitly deferred 72-hour qualification,
and approvers. B3-B8 are not implemented.

## Release identity

| Field | Value |
|---|---|
| Pre-V1B baseline | `70ba3f74addee3d19ef529434122dfabd357d3c5` |
| B1 final source | `f4675667b939a346af3319c622ce2b31b6d495c1` |
| Merged B1 main | `91d8bab54216210f2ef54dc20fed716ccf22c831`; post-merge CI run `29893542073` succeeded |
| V1A accepted evidence | Pending A7 and dependent gates |
| B1 configuration hash | `8a5ada09d2e689d33f92f567d569ddc74cd6aae24bce55e8805958a77cf0685a` |
| Short Bybit dataset manifest | `004ab342a3bc2e51661a1aaeba2a8401616fd6aa953aee3494a68d842d18c5e1`; combined soak manifest deferred |
| B1 image digest | `sha256:246dc0cf2e7773ef19e801dca546dbcefa8f3b9d66ed4589814278d8468d24e5` |
| B1 SPDX SBOM | 45 packages; SHA-256 `028e502ad8e2c8afbf94f2c00349ec6786a71fef7255859b4a1a41a66fd172a3` |
| Short public validation | REST, WebSocket, and recorder manifest passed locally on 2026-07-21 |
| B2 short public dataset | Passing Tier A identity `379202ad9d16491ee60e252ae7aa47f09e9977dcf67bae60be0a1a290ce97e11`; 30 linked public records; archive SHA-256 `cca98c02255c2da4b0f1d16be101ffa337f8df85a219212472f4911ca104f445` |
| B2 short coherent view | Passed in Southeast Asia: Binance 59.569181 ms and Bybit 40.927081 ms; identity `4c80fb5ddd1eb210c01d295001ecf643bc0649784446568dcd447ab07e8ec825` |
| B1 72-hour soak | Deferred by owner; not run and not claimed |
| B2 72-hour qualification | Deferred by owner; not run and not claimed |
| Product / Security / QA / SRE approvers | Pending |

## Phase gates

| Phase | Entry dependency | Implementation | Verification | Evidence |
|---|---|---|---|---|
| B1 | A6 verified; owner-authorized overlap with open A7 | Complete | Locally verified; formal hold: A7, 72-hour soak, approvers | [B1 local validation](evidence/b1-local-validation.md) |
| B2 | B1 completion merged and local verification retained | Complete | Locally verified; formal hold: predecessor, 72-hour qualification, approvers | [B2 local validation](evidence/b2-local-validation.md) |
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
- B1 PostgreSQL, short-live, image, security, and cumulative gates are locally
  verified; this does not represent or replace the deferred 72-hour soak.
- B2 began from merged B1 `main` at `91d8bab54216210f2ef54dc20fed716ccf22c831`;
  every implemented non-soak gate is locally verified, but the deferred
  72-hour qualification and formal acceptance remain open.
- B3-B8 remain unimplemented.
- V1B has no authenticated exchange transport, private endpoint, external
  order, withdrawal, transfer, testnet, demo, or live execution capability.
