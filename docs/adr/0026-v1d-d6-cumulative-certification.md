# ADR-0026: Exact signed cumulative V1 certification

- **Status:** Accepted
- **Date:** 2026-08-04
- **Scope:** V1D D6 release evidence

## Context

D6 must distinguish implementation, local checks, hosted CI, formal
qualification, and final acceptance across years of phase evidence. Prose,
mutable paths, older SHAs, smoke tests, or a later aggregate cannot prove an
independent prerequisite.

## Decision

Final certification consumes a separately trusted, Ed25519-authenticated,
current, exact-source candidate. It requires one formal passed verdict for every
A0-A11, B1-B8, C1-C6, and D1-D5 gate; seven independent reviews; all 22 Section
35 criteria; and an exact immutable artifact set covering source, binaries,
images, SBOMs, generated contracts, configuration, migrations, UI, Compose,
request capture, and scans.

Every safety assertion must be true and signed destinations must equal the
compiled Binance Spot Testnet and Bybit Demo set. Missing, expired, failed,
wrong-SHA, dirty, mutable, unsigned, tampered, duplicate, or unresolved
critical/high evidence rejects. The final command is default-off, exact-build
bound, and writes one signed no-replace verdict. Certification is never
profitability evidence.

## Consequences

Older good evidence requires an explicit independent current binding; this is
more operator work but prevents accidental promotion. Local/CI implementation
can finish while the release remains visibly blocked. Reviewer trust and
release keys are external inputs and are not shipped by the repository.

## Rejected alternatives

- One repository boolean: cannot authenticate scope, age, or source identity.
- Treating CI as qualification: omits duration, server, owner, and independent
  evidence.
- Letting D5 replace B2/C6: violates independent qualification boundaries.
- Shipping example reviewer keys: could turn templates into apparent evidence.

## Validation

Tests cover success, default-off, missing evidence, wrong revision, tampering,
expiry, duplicate identities, mutable/unsigned artifacts, open high findings,
signature verification, exact destinations, and no-replace verdict output.

## Revisit when

The specification adds a phase, review area, artifact identity, signing policy,
or Section 35 criterion. Reconsideration requires a superseding ADR and cannot
weaken production-order lockout.
