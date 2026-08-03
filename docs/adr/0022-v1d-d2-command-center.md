# ADR-0022: V1D D2 command-center information architecture

- **Status:** Accepted
- **Date:** 2026-08-03
- **Scope:** D2 React shell, operational pages, controls, and browser recovery

## Context

A11, B8, C6, and D1 provide working vertical UI slices and the complete typed
control-plane API. Their flat navigation and phase-specific pages do not yet
form one understandable role-aware product. D2 must unify those slices without
duplicating command execution, hiding evidence, or suggesting that simulation
or sandbox results prove profitability.

## Decision

The authenticated shell has six product groups: Home, Activity, Strategies,
Run Lab, Risk & Controls, and Operations. A persistent safety header contains
the always-visible execution boundary and current system, exchange, engine,
risk, freshness, and stream state. Links are permission-aware; direct routes
still rely on server authorization and render a permission-denied state.

Operational pages use progressive disclosure. Human-readable state, reason,
impact, and recommended action are primary. Correlation IDs, revisions,
source identities, redacted attributes, and evidence links remain on the same
page in expandable detail. Decisions and orders are separated from restricted
system events but retain correlation navigation.

Standard controls follow the existing accessible Radix/shadcn interaction
patterns: labelled fields, bounded tables, state badges, keyboard-operable
tabs, and modal confirmation for consequential commands. The existing CSS
variable system remains authoritative; D2 does not add a second utility-CSS
stack. Dense operational tables and grouped sidebar patterns are informed by
the reviewed 21st.dev references but implemented with Axiom tokens and safety
language.

The Strategy Center renders summary and D1 detail together. Configuration
enablement is an owner-only, password/TOTP reauthenticated action bound to the
exact revision. Runtime pause/resume is a separate operator control and resume
remains fail-closed when any prerequisite is blocked. Qualification start uses
the same exact-revision high-risk grant; operators may monitor or abort.

Run Lab links only to approved product workflows. It has no free-form command,
script, or test-name execution surface. Specialized lab lifecycle and result
work remains D3.

## Consequences

- Existing A11/B8/C6 routes remain compatible while the product navigation is
  understandable to researcher, operator, auditor, and owner roles.
- REST snapshots remain authoritative and SSE reconnect invalidates/refetches
  active queries, including D1 resources.
- Technical evidence stays available without dominating the default workflow.
- D2 introduces no broker, credential, production-private, transfer,
  withdrawal, leverage, derivative, staking, lending, borrowing, or shorting
  capability.

## Validation

- Strict TypeScript, lint, unit/component, and production build checks.
- Role/navigation and command-boundary component tests.
- D1 response validation and reconnect recovery tests.
- Playwright critical workflows in Chromium, Firefox, and WebKit at desktop,
  tablet, and mobile viewports.
- axe WCAG 2.2 AA checks, keyboard traversal, labelled-control checks, and
  horizontal-overflow assertions.
- Repository secret and prohibited-capability scans.
