# D6 independent review records

Each template is a deliberately unsigned, expired `PENDING` record. A release
reviewer replaces every placeholder, references immutable artifacts from the
exact safety manifest, records every finding, and signs the canonical record
with a key present in the separately controlled trust store.

Findings require severity, owner, evidence, remediation, closure state,
reviewer, timestamp, and exact source SHA. Critical or high security, safety,
accounting, or reconciliation findings must be closed with an immutable
closure-evidence digest before certification. The repository does not supply
reviewer keys and these templates are not acceptance evidence.
