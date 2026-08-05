# Cumulative V1A-V1D evidence index

This index reports the strongest existing repository claim. It does not upgrade
local evidence to formal qualification or acceptance. Every final candidate
must rebind accepted evidence to the exact candidate SHA through independently
signed prerequisite verdicts.

| Gate | Repository evidence | Strongest current claim | Remaining blocker |
|---|---|---|---|
| A0-A7 | `docs/releases/evidence/a0-review.md` through A7 records | implemented; A0-A6 recorded complete; A7 owner waiver recorded | revalidation/current signed prerequisite needed; A7 machine verdict was `qualified:false` |
| A8 | `docs/releases/evidence/a8-local-validation.md` | implemented and locally verified | formal acceptance pending |
| A9 | `docs/releases/evidence/a9-local-validation.md` | implemented and locally verified | formal A8/A9 acceptance pending |
| A10 | `docs/releases/evidence/a10-local-validation.md` | implemented and locally verified | formal A8-A10 acceptance pending |
| A11 | `docs/releases/evidence/a11-local-validation.md` | implemented and locally qualified | formal A8-A11 cumulative acceptance pending |
| B1 | `docs/releases/evidence/b1-formal-qualification-2026-07-26.md` | formal record exists for an earlier exact source | current-candidate binding and cumulative acceptance needed |
| B2 | `docs/releases/evidence/b2-local-validation.md` | implemented and non-soak locally verified | independent 72-hour qualification not started |
| B3-B8 | `docs/releases/evidence/b3-local-validation.md` through `b8-local-validation.md` | implemented and locally verified | predecessor, owner, security, and cumulative V1B acceptance remain open |
| C1-C3 | `docs/releases/evidence/v1c-pr1-local-validation.md` | implemented and locally verified | current formal/cumulative V1C acceptance needed |
| C4-C5 | `docs/releases/evidence/v1c-pr2-local-validation.md` | implemented; deterministic and historical controlled integration evidence recorded | owner/security acceptance remains open; no new canary is part of D6 |
| C6 | `docs/releases/evidence/v1c-pr3-local-validation.md` | implemented and non-soak locally verified | independent 72-hour qualification and owner/security acceptance not started/completed |
| D1 | D1 requirements plus merge history | implemented and merged | current cumulative signed verdict missing |
| D2 | `docs/releases/evidence/v1d-d2-local-validation.md` | implemented, locally verified, and merged | current cumulative signed verdict missing |
| D3 | `docs/releases/evidence/v1d-d3-local-validation.md` | implemented, locally verified, and merged | current cumulative signed verdict missing |
| D4 | `docs/releases/evidence/v1d-d4-local-validation.md` | implemented and merged; base hosted gates recorded | current exact-candidate rerun and formal cumulative acceptance missing |
| D5 | `docs/releases/evidence/v1d-d5-local-validation.md` | implemented and merged; base non-soak hosted gates recorded | seven-day declared-server readiness run not started |
| D6 | `docs/releases/evidence/v1d-d6-local-validation.md` | repository implementation and available dirty-worktree non-soak gates locally verified | clean exact candidate/integrated rerun, hosted CI, reviews, all formal prerequisites, and certification missing |

The formal verifier requires exact signed verdicts for A0-A11, B1-B8, C1-C6,
and D1-D5. This is intentionally stricter than treating prose status as machine
acceptance. Expired waivers and older-SHA evidence never pass automatically.
