# ADR-0027: Single-owner semantic runtime and unified runs

- **Status:** Accepted
- **Date:** 2026-08-05
- **Scope:** Owner console, authentication, run contracts, and compatibility migration

## Context

The prior console evolved through delivery phases and exposes role assignments,
phase-derived identifiers, and separate laboratory entry points. That obscures
the actual product workflow for its sole owner and makes normal use depend on
raw resource identifiers. The product now requires an exactly-one-owner model,
semantic current-state naming, and one server-validated run workflow without
weakening sandbox or production-order safety.

## Decision

New runtime code uses semantic names. Every authenticated session represents
the one owner; roles and permissions are not returned in session projections,
do not control navigation, and are not consulted for normal product actions.
Existing role records are historical evidence only. High-risk actions retain
their password/TOTP, one-use purpose, expected-revision, reason, audit, and
CSRF boundaries.

The database records the active owner separately from historical user and role
data. Upgrade fails before committing if more than one active user exists; it
never chooses or deletes an account. A clean installation has no owner until
the existing file-backed bootstrap creates one. The compatibility migration
adds semantic views for current configuration and activity resources without
rewriting immutable evidence.

All new product workflows use a run catalogue and a unified run resource. A
catalogue is server-authoritative: it returns only supported combinations and
plain blockers. A run selects immutable strategy, configuration, model,
portfolio, dataset/feed, and safety identities. Strategies cannot bypass the
shared allocator, risk, broker, order reducer, accounting, or reconciliation
pipeline. Sandbox runs retain the existing isolated engines, fixed spot-only
allowlists, caps, inventory checks, and explicit short-lived owner arm.

## Consequences

- Existing semantic URLs may redirect while old delivery labels disappear from
  normal UI and active contracts.
- Historical data and migration history retain old identifiers for verifiability
  but are excluded from current product projections.
- A database with multiple active accounts requires an explicit operator
  resolution before the product can start after upgrade.
- This decision neither starts nor substitutes B2, C6, D5, or D6 qualification.

## Rejected alternatives

- Retaining role-gated navigation: violates the exactly-one-owner product
  requirement and hides normal owner actions.
- Silently selecting the oldest owner during migration: destroys ambiguity
  evidence and can grant authority to the wrong account.
- A browser-assembled run request with raw IDs: bypasses server compatibility
  validation and makes reproducibility fragile.

## Validation

- Migration tests cover clean install, one active owner upgrade, and rejection
  of multiple active users.
- Authentication, API, and browser tests prove session responses and
  navigation contain no roles or permissions.
- Unified-run tests prove only catalogue combinations and installed runtimes
  are accepted. A strategy/mode without its real shared materializer remains
  blocked rather than being routed through another strategy implementation.
- Existing outbound signed-request capture and prohibited-capability checks
  continue to prove production-private submission is impossible.

## Revisit when

The product introduces an explicitly approved multi-owner tenancy model, an
external identity provider, or a new execution environment. Any change must
preserve the production-order lock and requires a superseding ADR.
