# A1 owner-verified hosted CI evidence

This record captures owner-confirmed GitHub Actions evidence for the current A1
candidate. The repository and workflow run are private from this unauthenticated
workspace, so the facts below are recorded as owner-verified evidence rather
than independently inspected GitHub logs or artifacts.

## Evidence identity

| Field | Value |
| --- | --- |
| Owner confirmation received | 2026-07-13 |
| Candidate version | `0.1.0-a1` |
| Source commit | `5ce09c3611e05a8fa5d0f1afc4706e17698b2d90` |
| Branch | `a1-candidate` |
| Workflow | `Axiom CI` |
| Run number | `#4` |
| Run ID | `29257258436` |
| Run URL | <https://github.com/AnasAyyad/axiom/actions/runs/29257258436> |
| Overall result | Owner confirmed all five jobs green |

The local checkout confirms that both `HEAD` and `origin/a1-candidate` point to
the source commit above. The owner confirmed the hosted result and successful
artifact downloads for that same candidate.

## Owner-confirmed passing jobs

1. `Format, lint, test, contracts, and safety`
2. `PostgreSQL readiness and process smoke`
3. `Dependency review`
4. `Full-history secret scan`
5. `Image, clean deployment, SBOM, license, and vulnerability gates`

The committed workflow shows that these jobs collectively run the repository
quality gate, process/readiness smoke, pull-request dependency review,
full-history Gitleaks scan, immutable image build and inspection, runtime-payload
reproducibility comparison, image-backed Compose smoke, SPDX generation, and
Trivy vulnerability, secret, misconfiguration, and license scanning.

## Owner-confirmed retained artifacts

- `a1-supply-chain-evidence`
  - `image-reproducibility.txt`
  - `axiom.spdx.json`
  - `trivy.sarif`
  - `image-digest.txt`
- `axiom_ci.spdx.json`

The owner confirmed that both artifacts exist and download successfully. Their
contents were not independently downloaded or inspected in this workspace, so
this record does not claim artifact hashes or findings beyond the green job
result and the filenames above.

No Gitleaks license or personal token is required for this personal repository;
the owner confirmed the committed workflow completed the full-history scan with
the repository-provided GitHub token.

## Gate assessment

This evidence closes the previously missing hosted positive CI, dependency
review, full-history secret scan, SBOM, image/license/vulnerability scan, and
retained supply-chain artifact obligations for commit
`5ce09c3611e05a8fa5d0f1afc4706e17698b2d90`.

The separate setup and governance-document walkthrough is now recorded in
[a1-clean-machine-walkthrough.md](a1-clean-machine-walkthrough.md). No exchange
credentials or trading API keys were required or requested.
