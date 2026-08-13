package certification

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"time"
)

const maximumEvidenceLifetime = 90 * 24 * time.Hour

var (
	commitPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

var requiredReadinessGates = []string{
	"traceability", "application-baseline", "configuration-reference", "runtime-recovery", "durable-storage", "observability", "exchange-integration", "public-data-qualification", "strategy-execution", "portfolio-risk", "research-registry", "owner-console",
	"exchange-expansion", "coherent-market-data", "mean-reversion", "triangular-arbitrage", "cross-exchange-arbitrage", "inventory-rebalancing", "research-promotion", "multi-exchange-console",
	"credential-security", "authentication-control", "dispatcher-recovery", "binance-testnet", "bybit-demo", "sandbox-qualification",
	"owner-control", "owner-experience", "run-lab", "operational-evidence", "operational-readiness",
}

var requiredReviewAreas = []string{
	"security",
	"accounting",
	"reconciliation",
	"determinism-reproducibility",
	"authorization-reauthentication",
	"secret-leakage-redaction",
	"operations-recovery",
}

var requiredArtifacts = map[string]string{
	"source-tree":                "source",
	"platform-binary":            "binary",
	"storage-backup-binary":      "binary",
	"application-image":          "image",
	"backup-image":               "image",
	"application-sbom":           "sbom",
	"backup-sbom":                "sbom",
	"openapi-contract":           "contract",
	"go-contract":                "contract",
	"typescript-contract":        "contract",
	"configuration-bundle":       "configuration",
	"migration-bundle":           "migration",
	"frontend-dist":              "ui",
	"compose-render":             "configuration",
	"outbound-request-capture":   "capture",
	"prohibited-capability-scan": "scan",
	"binary-symbol-scan":         "scan",
	"vulnerability-scan":         "scan",
	"license-scan":               "scan",
}

var requiredSignedDestinations = []SignedDestination{
	{Exchange: "binance", Transport: "rest", Host: "testnet.binance.vision"},
	{Exchange: "binance", Transport: "websocket", Host: "ws-api.testnet.binance.vision"},
	{Exchange: "bybit", Transport: "rest", Host: "api-demo.bybit.com"},
	{Exchange: "bybit", Transport: "websocket", Host: "stream-demo.bybit.com"},
}

// RequiredReadinessGates returns the exact cumulative product-readiness set.
func RequiredReadinessGates() []string { return append([]string(nil), requiredReadinessGates...) }

// RequiredReviewAreas returns the exact release certification independent-review set.
func RequiredReviewAreas() []string { return append([]string(nil), requiredReviewAreas...) }

func reject(reason string) error { return fmt.Errorf("release_certification_rejected: %s", reason) }

// ValidateCandidate checks every signed, current, exact-source release certification prerequisite.
func ValidateCandidate(candidate Candidate, trust TrustStore, now time.Time) error {
	now = now.UTC()
	if candidate.SchemaVersion != CandidateSchema || !identityPattern.MatchString(candidate.CandidateID) ||
		!commitPattern.MatchString(candidate.SourceSHA) || !validWindow(candidate.PreparedAt, candidate.ValidUntil, now) {
		return reject("candidate_identity")
	}
	trusted, err := validateTrustStore(trust, now)
	if err != nil {
		return err
	}
	artifacts, err := validateSafetyManifest(candidate.SafetyManifest, candidate.SourceSHA, trusted, now)
	if err != nil {
		return err
	}
	if err = validatePrerequisites(candidate.Prerequisites, candidate.SourceSHA, artifacts, trusted, now); err != nil {
		return err
	}
	if err = validateReviews(candidate.Reviews, candidate.SourceSHA, artifacts, trusted, now); err != nil {
		return err
	}
	if err = validateSection35(candidate.Section35, candidate.SourceSHA, artifacts); err != nil {
		return err
	}
	return nil
}

func validWindow(issued, validUntil, now time.Time) bool {
	return !issued.IsZero() && !validUntil.IsZero() && issued.Location() == time.UTC &&
		validUntil.Location() == time.UTC && !issued.After(now.Add(5*time.Minute)) &&
		validUntil.After(now) && validUntil.After(issued) && validUntil.Sub(issued) <= maximumEvidenceLifetime
}

func validateTrustStore(store TrustStore, now time.Time) (map[string]TrustedReviewer, error) {
	if store.SchemaVersion != TrustStoreSchema || len(store.Reviewers) == 0 {
		return nil, reject("trust_store_missing")
	}
	trusted := make(map[string]TrustedReviewer, len(store.Reviewers))
	for _, entry := range store.Reviewers {
		decoded, err := base64.StdEncoding.DecodeString(entry.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize || !validReviewer(entry.Reviewer) ||
			!entry.Reviewer.Independent || entry.ValidUntil.Location() != time.UTC || !entry.ValidUntil.After(now) ||
			entry.KeyFingerprint != publicKeyFingerprint(ed25519.PublicKey(decoded)) {
			return nil, reject("trust_store_invalid")
		}
		if _, duplicate := trusted[entry.Reviewer.ID]; duplicate {
			return nil, reject("duplicate_identity")
		}
		trusted[entry.Reviewer.ID] = entry
	}
	return trusted, nil
}

func validReviewer(reviewer ReviewerIdentity) bool {
	return identityPattern.MatchString(reviewer.ID) && identityPattern.MatchString(reviewer.Role)
}

func trustedKey(trusted map[string]TrustedReviewer, reviewer ReviewerIdentity, fingerprint string) (ed25519.PublicKey, bool) {
	entry, found := trusted[reviewer.ID]
	if !found || entry.Reviewer != reviewer || fingerprint != entry.KeyFingerprint {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.PublicKey)
	return ed25519.PublicKey(decoded), err == nil && len(decoded) == ed25519.PublicKeySize
}

func validateSafetyManifest(manifest SafetyManifest, sourceSHA string, trusted map[string]TrustedReviewer,
	now time.Time) (map[string]ArtifactIdentity, error) {
	if manifest.SchemaVersion != SafetyManifestSchema || !identityPattern.MatchString(manifest.ManifestID) ||
		manifest.SourceSHA != sourceSHA || manifest.SourceDirty || !validReviewer(manifest.Reviewer) ||
		!manifest.Reviewer.Independent || !validWindow(manifest.ReviewedAt, manifest.ValidUntil, now) ||
		!allSafetyAssertions(manifest.Assertions) || !exactSignedDestinations(manifest.SignedDestinations) {
		return nil, reject("safety_manifest_invalid")
	}
	key, found := trustedKey(trusted, manifest.Reviewer, manifest.SigningKeyFingerprint)
	if !found || !verifySafetyManifest(key, manifest) {
		return nil, reject("safety_manifest_unsigned_or_tampered")
	}
	if len(manifest.Artifacts) != len(requiredArtifacts) {
		return nil, reject("artifact_set_incomplete")
	}
	artifacts := make(map[string]ArtifactIdentity, len(manifest.Artifacts))
	digests := make(map[string]bool, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		expectedKind, required := requiredArtifacts[artifact.Name]
		if !required || artifact.Kind != expectedKind || !digestPattern.MatchString(artifact.Digest) ||
			artifact.SourceSHA != sourceSHA || !artifact.Immutable || !artifact.SignatureVerified {
			return nil, reject("artifact_identity_invalid")
		}
		if _, duplicate := artifacts[artifact.Name]; duplicate || digests[artifact.Digest] {
			return nil, reject("duplicate_identity")
		}
		artifacts[artifact.Name], digests[artifact.Digest] = artifact, true
	}
	return artifacts, nil
}

func allSafetyAssertions(value SafetyAssertions) bool {
	return value.SpotOnly && value.OwnedInventoryOnlySells && value.CentralAllocatorAllOrders &&
		value.CentralRiskAllOrders && value.NoProductionPrivateSubmit && value.NoTransfers &&
		value.NoWithdrawals && value.NoMargin && value.NoFutures && value.NoPerpetuals &&
		value.NoOptions && value.NoLeverage && value.NoBorrowing && value.NoLending &&
		value.NoStaking && value.NoShortSelling && value.NoProductionBroker &&
		value.NoProductionSigner && value.NoProductionCredentialInput &&
		value.NoProductionPrivateEndpoint && value.NoHiddenRoute && value.NoDormantToggle &&
		value.NoEnvironmentBypass
}

func exactSignedDestinations(actual []SignedDestination) bool {
	if len(actual) != len(requiredSignedDestinations) {
		return false
	}
	seen := make(map[SignedDestination]bool, len(actual))
	for _, destination := range actual {
		if seen[destination] || !slices.Contains(requiredSignedDestinations, destination) {
			return false
		}
		seen[destination] = true
	}
	return true
}

func validatePrerequisites(verdicts []PrerequisiteVerdict, sourceSHA string,
	artifacts map[string]ArtifactIdentity, trusted map[string]TrustedReviewer, now time.Time) error {
	if len(verdicts) != len(requiredReadinessGates) {
		return reject("formal_prerequisites_missing")
	}
	gates, evidenceIDs, evidenceHashes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, verdict := range verdicts {
		if verdict.SchemaVersion != PrerequisiteSchema || !identityPattern.MatchString(verdict.EvidenceID) ||
			!slices.Contains(requiredReadinessGates, verdict.GateID) || verdict.SourceSHA != sourceSHA ||
			verdict.State != "PASSED" || !verdict.Formal || !verdict.Qualified || verdict.ProfitabilityEvidence ||
			!validReviewer(verdict.Reviewer) || !verdict.Reviewer.Independent ||
			!validWindow(verdict.IssuedAt, verdict.ValidUntil, now) ||
			validateReferences(verdict.Evidence, artifacts) != nil {
			return reject("formal_prerequisite_invalid")
		}
		if gates[verdict.GateID] || evidenceIDs[verdict.EvidenceID] || evidenceHashes[verdict.EvidenceHash] {
			return reject("duplicate_identity")
		}
		key, found := trustedKey(trusted, verdict.Reviewer, verdict.SigningKeyFingerprint)
		if !found || !verifyPrerequisiteVerdict(key, verdict) {
			return reject("formal_prerequisite_unsigned_or_tampered")
		}
		gates[verdict.GateID], evidenceIDs[verdict.EvidenceID], evidenceHashes[verdict.EvidenceHash] = true, true, true
	}
	return nil
}

func validateReviews(reviews []ReviewRecord, sourceSHA string, artifacts map[string]ArtifactIdentity,
	trusted map[string]TrustedReviewer, now time.Time) error {
	if len(reviews) != len(requiredReviewAreas) {
		return reject("independent_reviews_missing")
	}
	areas, reviewIDs, evidenceHashes, findingIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, review := range reviews {
		if review.SchemaVersion != ReviewSchema || !identityPattern.MatchString(review.ReviewID) ||
			!slices.Contains(requiredReviewAreas, review.Area) || review.SourceSHA != sourceSHA ||
			review.State != "APPROVED" || !validReviewer(review.Reviewer) || !review.Reviewer.Independent ||
			!validWindow(review.ReviewedAt, review.ValidUntil, now) ||
			validateReferences(review.Evidence, artifacts) != nil {
			return reject("independent_review_invalid")
		}
		if areas[review.Area] || reviewIDs[review.ReviewID] || evidenceHashes[review.EvidenceHash] {
			return reject("duplicate_identity")
		}
		for _, finding := range review.Findings {
			if validateFinding(finding, review.ReviewedAt) != nil || findingIDs[finding.ID] {
				return reject("review_finding_invalid")
			}
			if (finding.Severity == "critical" || finding.Severity == "high") && finding.ClosureStatus != "closed" {
				return reject("unresolved_high_severity_finding")
			}
			findingIDs[finding.ID] = true
		}
		key, found := trustedKey(trusted, review.Reviewer, review.SigningKeyFingerprint)
		if !found || !verifyReviewRecord(key, review) {
			return reject("independent_review_unsigned_or_tampered")
		}
		areas[review.Area], reviewIDs[review.ReviewID], evidenceHashes[review.EvidenceHash] = true, true, true
	}
	return nil
}

func validateFinding(finding Finding, reviewedAt time.Time) error {
	if !identityPattern.MatchString(finding.ID) || finding.Owner == "" || finding.Evidence == "" ||
		finding.Remediation == "" || !slices.Contains([]string{"critical", "high", "medium", "low", "info"}, finding.Severity) ||
		!slices.Contains([]string{"open", "closed"}, finding.ClosureStatus) {
		return reject("finding_fields")
	}
	if finding.ClosureStatus == "closed" {
		if !digestPattern.MatchString(finding.ClosureEvidenceDigest) || finding.ClosedAt.IsZero() ||
			finding.ClosedAt.Location() != time.UTC || finding.ClosedAt.After(reviewedAt) {
			return reject("finding_closure")
		}
	} else if finding.ClosureEvidenceDigest != "" || !finding.ClosedAt.IsZero() {
		return reject("finding_open_with_closure")
	}
	return nil
}

func validateSection35(criteria []CriterionStatus, sourceSHA string, artifacts map[string]ArtifactIdentity) error {
	if len(criteria) != 22 {
		return reject("section_35_incomplete")
	}
	seen := make(map[int]bool, 22)
	for _, criterion := range criteria {
		if criterion.Number < 1 || criterion.Number > 22 || seen[criterion.Number] ||
			criterion.State != "PASSED" || criterion.SourceSHA != sourceSHA || len(criterion.Blockers) != 0 ||
			validateReferences(criterion.Evidence, artifacts) != nil {
			return reject("section_35_blocked_or_invalid")
		}
		seen[criterion.Number] = true
	}
	return nil
}

func validateReferences(references []EvidenceReference, artifacts map[string]ArtifactIdentity) error {
	if len(references) == 0 {
		return reject("evidence_reference_missing")
	}
	seen := make(map[string]bool, len(references))
	for _, reference := range references {
		artifact, found := artifacts[reference.ArtifactName]
		if !found || artifact.Digest != reference.Digest || seen[reference.ArtifactName] {
			return reject("evidence_reference_invalid")
		}
		seen[reference.ArtifactName] = true
	}
	return nil
}
