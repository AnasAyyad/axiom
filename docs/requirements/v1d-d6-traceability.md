# V1D D6 traceability

Status is deliberately layered: **Implemented**, **Locally verified**, **Hosted
CI verified**, **Formally qualified**, and **Formally accepted/certified** are
different claims. A later layer never implies an earlier missing formal verdict.

| ID | Requirement | Implementation | Verification and current status |
|---|---|---|---|
| AX-V1D-D06-001 | Close cumulative A0-D5 evidence without relabeling smoke or local results | `internal/certification` requires all 31 separately signed phase verdicts | model tests pass; formal verdict set is missing, so Blocked |
| AX-V1D-D06-002 | Bind source, binary, image, SBOM, contract, migration, UI, Compose, configuration, capture, and scan identities | exact 19-artifact safety-manifest set with SHA-256 digests and source SHA | success/wrong-SHA/mutable/duplicate tests; exact candidate artifacts pending |
| AX-V1D-D06-003 | Prove the complete V1 prohibited-capability boundary | typed safety assertions plus exact signed destination set | repository scans and capture tests are locally verifiable; independent signed manifest pending |
| AX-V1D-D06-004 | Reject missing, stale, failed, mutable, unsigned, wrong-SHA, duplicate, or tampered evidence | bounded strict JSON, UTC validity windows, Ed25519 signatures, no-replace verdict | adversarial certification tests |
| AX-V1D-D06-005 | Require independent review of seven named areas | structured signed records and separately supplied trust root | seven fail-closed templates exist; reviews remain Pending |
| AX-V1D-D06-006 | Block unresolved critical/high safety, security, accounting, or reconciliation findings | finding validation and closure-evidence digest rule | high-finding rejection test |
| AX-V1D-D06-007 | Map all 22 Section 35 criteria to implementation, tests, evidence, status, and blockers | `v1d-section-35-matrix.md` and typed candidate entries | boundary checker enforces 1-22 coverage; formal criteria remain Blocked |
| AX-V1D-D06-008 | Keep final certification default-off | `cmd/release-certify` and `make d6-final-certification` require explicit enablement and clean exact build | default-off test passes; command is expected to reject current state |
| AX-V1D-D06-009 | Issue only a signed, no-replace, short-lived final verdict | Ed25519 canonical evidence hash and `O_EXCL`/fsync writer | success and duplicate-write tests; no real verdict issued |
| AX-V1D-D06-010 | Complete Section 33 documentation and known limitations | canonical operation, configuration, accounting, research, and deployment documents | documentation checks are part of D6 gate |
| AX-V1D-D06-011 | Provide safe reproducible backtest/replay examples | redacted fixture-only example bundle with a SHA-256 inventory | deterministic bundle check; no secret or private request material |
| AX-V1D-D06-012 | Report an honest release-candidate state | D6 readiness and release-candidate records | **NOT CERTIFIED** while any prerequisite, review, or formal criterion is missing |
