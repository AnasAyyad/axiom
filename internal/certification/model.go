package certification

import "time"

// CandidateSchema and the related constants pin every accepted release certification document and release state.
const (
	// CandidateSchema is the only accepted cumulative candidate format.
	CandidateSchema = "axiom.v1.release-candidate.v1"
	// SafetyManifestSchema is the only accepted signed safety-manifest format.
	SafetyManifestSchema = "axiom.v1.safety-manifest.v1"
	// PrerequisiteSchema is the only accepted signed readiness-verdict format.
	PrerequisiteSchema = "axiom.readiness.prerequisite-verdict.v1"
	// ReviewSchema is the only accepted signed independent-review format.
	ReviewSchema = "axiom.v1.independent-review.v1"
	// TrustStoreSchema is the only accepted reviewer trust-root format.
	TrustStoreSchema = "axiom.v1.trusted-reviewers.v1"
	// ReleaseVerdictSchema is the only emitted final release-verdict format.
	ReleaseVerdictSchema = "axiom.v1.release-verdict.v1"
	// CertifiedReleaseState is the successful final release state.
	CertifiedReleaseState = "CERTIFIED"
)

// ReviewerIdentity is a non-secret review identity bound to a trusted key.
type ReviewerIdentity struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Independent bool   `json:"independent"`
}

// TrustedReviewer binds a reviewer identity to an externally provisioned key.
type TrustedReviewer struct {
	Reviewer       ReviewerIdentity `json:"reviewer"`
	PublicKey      string           `json:"public_key"`
	KeyFingerprint string           `json:"key_fingerprint"`
	ValidUntil     time.Time        `json:"valid_until"`
}

// TrustStore is the separately supplied release-review trust root.
type TrustStore struct {
	SchemaVersion string            `json:"schema_version"`
	Reviewers     []TrustedReviewer `json:"reviewers"`
}

// ArtifactIdentity binds every release input to the candidate source.
type ArtifactIdentity struct {
	Name              string `json:"name"`
	Kind              string `json:"kind"`
	Digest            string `json:"digest"`
	SourceSHA         string `json:"source_sha"`
	Immutable         bool   `json:"immutable"`
	SignatureVerified bool   `json:"signature_verified"`
}

// SafetyAssertions is the complete V1 prohibited-capability proof surface.
type SafetyAssertions struct {
	SpotOnly                    bool `json:"spot_only"`
	OwnedInventoryOnlySells     bool `json:"owned_inventory_only_sells"`
	CentralAllocatorAllOrders   bool `json:"central_allocator_all_orders"`
	CentralRiskAllOrders        bool `json:"central_risk_all_orders"`
	NoProductionPrivateSubmit   bool `json:"no_production_private_submission"`
	NoTransfers                 bool `json:"no_transfers"`
	NoWithdrawals               bool `json:"no_withdrawals"`
	NoMargin                    bool `json:"no_margin"`
	NoFutures                   bool `json:"no_futures"`
	NoPerpetuals                bool `json:"no_perpetuals"`
	NoOptions                   bool `json:"no_options"`
	NoLeverage                  bool `json:"no_leverage"`
	NoBorrowing                 bool `json:"no_borrowing"`
	NoLending                   bool `json:"no_lending"`
	NoStaking                   bool `json:"no_staking"`
	NoShortSelling              bool `json:"no_short_selling"`
	NoProductionBroker          bool `json:"no_production_broker"`
	NoProductionSigner          bool `json:"no_production_signer"`
	NoProductionCredentialInput bool `json:"no_production_credential_input"`
	NoProductionPrivateEndpoint bool `json:"no_production_private_endpoint"`
	NoHiddenRoute               bool `json:"no_hidden_route"`
	NoDormantToggle             bool `json:"no_dormant_toggle"`
	NoEnvironmentBypass         bool `json:"no_environment_bypass"`
}

// SignedDestination is one exact destination at which signing may occur.
type SignedDestination struct {
	Exchange  string `json:"exchange"`
	Transport string `json:"transport"`
	Host      string `json:"host"`
}

// SafetyManifest is the independently signed V1 build and capability proof.
type SafetyManifest struct {
	SchemaVersion         string              `json:"schema_version"`
	ManifestID            string              `json:"manifest_id"`
	SourceSHA             string              `json:"source_sha"`
	SourceDirty           bool                `json:"source_dirty"`
	Artifacts             []ArtifactIdentity  `json:"artifacts"`
	Assertions            SafetyAssertions    `json:"assertions"`
	SignedDestinations    []SignedDestination `json:"signed_destinations"`
	Reviewer              ReviewerIdentity    `json:"reviewer"`
	ReviewedAt            time.Time           `json:"reviewed_at"`
	ValidUntil            time.Time           `json:"valid_until"`
	EvidenceHash          string              `json:"evidence_hash"`
	SigningKeyFingerprint string              `json:"signing_key_fingerprint"`
	Signature             string              `json:"signature"`
}

// EvidenceReference points to a signed immutable artifact in the safety manifest.
type EvidenceReference struct {
	ArtifactName string `json:"artifact_name"`
	Digest       string `json:"digest"`
}

// PrerequisiteVerdict normalizes one independently signed readiness verdict.
type PrerequisiteVerdict struct {
	SchemaVersion         string              `json:"schema_version"`
	EvidenceID            string              `json:"evidence_id"`
	GateID                string              `json:"gate_id"`
	SourceSHA             string              `json:"source_sha"`
	State                 string              `json:"state"`
	Formal                bool                `json:"formal"`
	Qualified             bool                `json:"qualified"`
	ProfitabilityEvidence bool                `json:"profitability_evidence"`
	Evidence              []EvidenceReference `json:"evidence"`
	Reviewer              ReviewerIdentity    `json:"reviewer"`
	IssuedAt              time.Time           `json:"issued_at"`
	ValidUntil            time.Time           `json:"valid_until"`
	EvidenceHash          string              `json:"evidence_hash"`
	SigningKeyFingerprint string              `json:"signing_key_fingerprint"`
	Signature             string              `json:"signature"`
}

// Finding is one review issue with explicit ownership and closure evidence.
type Finding struct {
	ID                    string    `json:"id"`
	Severity              string    `json:"severity"`
	Owner                 string    `json:"owner"`
	Evidence              string    `json:"evidence"`
	Remediation           string    `json:"remediation"`
	ClosureStatus         string    `json:"closure_status"`
	ClosureEvidenceDigest string    `json:"closure_evidence_digest,omitempty"`
	ClosedAt              time.Time `json:"closed_at,omitempty"`
}

// ReviewRecord is one signed independent review for an exact candidate.
type ReviewRecord struct {
	SchemaVersion         string              `json:"schema_version"`
	ReviewID              string              `json:"review_id"`
	Area                  string              `json:"area"`
	SourceSHA             string              `json:"source_sha"`
	State                 string              `json:"state"`
	Evidence              []EvidenceReference `json:"evidence"`
	Findings              []Finding           `json:"findings"`
	Reviewer              ReviewerIdentity    `json:"reviewer"`
	ReviewedAt            time.Time           `json:"reviewed_at"`
	ValidUntil            time.Time           `json:"valid_until"`
	EvidenceHash          string              `json:"evidence_hash"`
	SigningKeyFingerprint string              `json:"signing_key_fingerprint"`
	Signature             string              `json:"signature"`
}

// CriterionStatus records one Section 35 disposition without converting a blocker into a pass.
type CriterionStatus struct {
	Number    int                 `json:"number"`
	State     string              `json:"state"`
	SourceSHA string              `json:"source_sha"`
	Evidence  []EvidenceReference `json:"evidence"`
	Blockers  []string            `json:"blockers"`
}

// Candidate is the complete cumulative input to final certification.
type Candidate struct {
	SchemaVersion  string                `json:"schema_version"`
	CandidateID    string                `json:"candidate_id"`
	SourceSHA      string                `json:"source_sha"`
	PreparedAt     time.Time             `json:"prepared_at"`
	ValidUntil     time.Time             `json:"valid_until"`
	SafetyManifest SafetyManifest        `json:"safety_manifest"`
	Prerequisites  []PrerequisiteVerdict `json:"prerequisites"`
	Reviews        []ReviewRecord        `json:"reviews"`
	Section35      []CriterionStatus     `json:"section_35"`
}

// ReleaseVerdict is written only after every cumulative input passes.
type ReleaseVerdict struct {
	SchemaVersion         string    `json:"schema_version"`
	CandidateID           string    `json:"candidate_id"`
	CandidateHash         string    `json:"candidate_hash"`
	SourceSHA             string    `json:"source_sha"`
	State                 string    `json:"state"`
	Certified             bool      `json:"certified"`
	ProfitabilityEvidence bool      `json:"profitability_evidence"`
	IssuedAt              time.Time `json:"issued_at"`
	ValidUntil            time.Time `json:"valid_until"`
	EvidenceHash          string    `json:"evidence_hash"`
	SigningKeyFingerprint string    `json:"signing_key_fingerprint"`
	Signature             string    `json:"signature"`
}
