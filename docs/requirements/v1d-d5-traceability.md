# V1D D5 operational-readiness traceability

Local implementation evidence is not the formal D5 verdict. The final gate
requires an approved reference server and one uninterrupted authenticated
seven-day run against the exact release artifacts.

| Requirement | Implementation | Verification |
|---|---|---|
| AX-V1D-D05-001 current fresh high/critical pressure and immutable history | `internal/storage/pressure`, migration `000027`, `operational_readiness_storage_pressure.go`; two-minute freshness gates | pressure unit tests; D5 PostgreSQL qualification |
| AX-V1D-D05-002 high rejects heavy jobs | `operational_readiness_pressure_gates.go`, job/export/report/shadow creation gates | D5 PostgreSQL pressure lifecycle |
| AX-V1D-D05-003 critical pauses recording and shadow entries | recorder pressure monitor; shadow posture and activation clauses | bootstrap unit/race tests; critical-watermark formal drill |
| AX-V1D-D05-004 protected retention | segment `PlanDeletion`; `D5LifecycleWorker`; artifact holds | segment retention and D1 hold tests; D5 lifecycle tests |
| AX-V1D-D05-005 independent encrypted remote backup | backup location proof; encrypted authenticated artifacts; bind-mounted backup profile | backup unit tests; formal mount preflight |
| AX-V1D-D05-006 authenticated timed clean restore and market-data manifest recovery | `restore_evidence.go`; `market_recovery.go`; clean-target restore path | backup confinement/checksum tests; D5 clean-restore drill |
| AX-V1D-D05-007 hardened exact deployment | non-root/read-only Compose services, edge TLS, pinned-digest preflight, SBOM and security scans | image and D5 hardening CI gates |
| AX-V1D-D05-008 exact seven-day runner | `internal/qualification/operationalreadiness`, `cmd/operational-readiness`; bounded acquisition recovery; per-service memory windows; typed observer status and hash-chained lifecycle | non-qualifying preflight check; retry deadline/source-cause tests; memory-window tests; runner smoke, failure, no-replace, signature, and formal server evidence |
| AX-V1D-D05-009 approved fault schedule and declared load | `deploy/config/operational-readiness-fault-schedule-v1.json`; operational-readiness test manifest; versioned start/drill/status/event controllers under `deploy/operational-readiness` | checked-in manifest contract test; controller syntax/boundary check; immediate failed/late-drill evaluation and formal fault evidence |
| AX-V1D-D05-010 independent prior verdicts | runner/test manifest and release docs preserve B2 and C6 identities | D5 boundary check; D6 cumulative gate review |

The 5 GiB initial critical reserve is the safest explicit value selected for
the previously unspecified critical watermark. A measured capacity plan may
raise either watermark, but configuration validation cannot lower the 10 GiB
high or 1 GiB absolute critical floors, and critical must remain below high.
