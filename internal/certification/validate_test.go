package certification

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

func TestValidateCandidateSuccess(t *testing.T) {
	candidate, trust, _ := validCandidate(t)
	if err := ValidateCandidate(candidate, trust, testNow); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
}

func TestValidateCandidateRejectsMissingEvidence(t *testing.T) {
	candidate, trust, _ := validCandidate(t)
	candidate.Prerequisites = candidate.Prerequisites[1:]
	assertRejected(t, candidate, trust, "formal_prerequisites_missing")
}

func TestValidateCandidateRejectsWrongRevision(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	candidate.Prerequisites[0].SourceSHA = strings.Repeat("b", 40)
	resealPrerequisite(t, key, &candidate.Prerequisites[0])
	assertRejected(t, candidate, trust, "formal_prerequisite_invalid")
}

func TestValidateCandidateRejectsTampering(t *testing.T) {
	candidate, trust, _ := validCandidate(t)
	candidate.Prerequisites[0].EvidenceID = "tampered-evidence-id"
	assertRejected(t, candidate, trust, "formal_prerequisite_unsigned_or_tampered")
}

func TestValidateCandidateRejectsExpiredEvidence(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	candidate.Prerequisites[0].ValidUntil = testNow.Add(-time.Minute)
	resealPrerequisite(t, key, &candidate.Prerequisites[0])
	assertRejected(t, candidate, trust, "formal_prerequisite_invalid")
}

func TestValidateCandidateRejectsDuplicateIdentities(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	candidate.Prerequisites[1].EvidenceID = candidate.Prerequisites[0].EvidenceID
	resealPrerequisite(t, key, &candidate.Prerequisites[1])
	assertRejected(t, candidate, trust, "duplicate_identity")
}

func TestValidateCandidateRejectsUnresolvedHighSeverityFinding(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	candidate.Reviews[0].Findings = []Finding{{
		ID: "finding-high-open", Severity: "high", Owner: "security-owner",
		Evidence: "review evidence", Remediation: "remove the unsafe path", ClosureStatus: "open",
	}}
	resealReview(t, key, &candidate.Reviews[0])
	assertRejected(t, candidate, trust, "unresolved_high_severity_finding")
}

func TestValidateCandidateRejectsMutableOrUnsignedInputs(t *testing.T) {
	t.Run("mutable artifact", func(t *testing.T) {
		candidate, trust, key := validCandidate(t)
		candidate.SafetyManifest.Artifacts[0].Immutable = false
		resealSafety(t, key, &candidate.SafetyManifest)
		assertRejected(t, candidate, trust, "artifact_identity_invalid")
	})
	t.Run("unsigned prerequisite", func(t *testing.T) {
		candidate, trust, _ := validCandidate(t)
		candidate.Prerequisites[0].Signature = ""
		assertRejected(t, candidate, trust, "formal_prerequisite_unsigned_or_tampered")
	})
}

func TestReleaseVerdictIsSignedAndNoReplace(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	if err := ValidateCandidate(candidate, trust, testNow); err != nil {
		t.Fatal(err)
	}
	verdict, err := NewReleaseVerdict(candidate, key, testNow, testNow.Add(24*time.Hour))
	if err != nil || !verdict.Certified || verdict.ProfitabilityEvidence || verdict.Signature == "" {
		t.Fatalf("verdict=%+v error=%v", verdict, err)
	}
	if err = VerifyReleaseVerdict(candidate, verdict, key.Public().(ed25519.PublicKey), testNow); err != nil {
		t.Fatalf("signed verdict did not verify: %v", err)
	}
	root := t.TempDir()
	if _, err = WriteVerdictNoReplace(root, verdict); err != nil {
		t.Fatal(err)
	}
	if _, err = WriteVerdictNoReplace(root, verdict); err == nil {
		t.Fatal("release verdict was overwritten")
	}
}

func TestReleaseVerdictVerificationRejectsTampering(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	if err := ValidateCandidate(candidate, trust, testNow); err != nil {
		t.Fatal(err)
	}
	verdict, err := NewReleaseVerdict(candidate, key, testNow, testNow.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	verdict.SourceSHA = strings.Repeat("b", 40)
	if err = VerifyReleaseVerdict(candidate, verdict, key.Public().(ed25519.PublicKey), testNow); err == nil {
		t.Fatal("tampered release verdict verified")
	}
}

func TestReleaseVerdictRejectsSymlinkDestination(t *testing.T) {
	candidate, trust, key := validCandidate(t)
	if err := ValidateCandidate(candidate, trust, testNow); err != nil {
		t.Fatal(err)
	}
	verdict, err := NewReleaseVerdict(candidate, key, testNow, testNow.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	link := root + "-link"
	if err = os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err = WriteVerdictNoReplace(link, verdict); err == nil {
		t.Fatal("symlink verdict directory accepted")
	}
}

func assertRejected(t *testing.T, candidate Candidate, trust TrustStore, reason string) {
	t.Helper()
	err := ValidateCandidate(candidate, trust, testNow)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("wanted %q rejection, got %v", reason, err)
	}
}

func validCandidate(t *testing.T) (Candidate, TrustStore, ed25519.PrivateKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	reviewer, trust := validReviewerTrust(key)
	source := strings.Repeat("a", 40)
	artifacts := validArtifacts(source)
	reference := func(index int) []EvidenceReference {
		artifact := artifacts[index%len(artifacts)]
		return []EvidenceReference{{ArtifactName: artifact.Name, Digest: artifact.Digest}}
	}
	assertions := SafetyAssertions{
		SpotOnly: true, OwnedInventoryOnlySells: true, CentralAllocatorAllOrders: true,
		CentralRiskAllOrders: true, NoProductionPrivateSubmit: true, NoTransfers: true,
		NoWithdrawals: true, NoMargin: true, NoFutures: true, NoPerpetuals: true,
		NoOptions: true, NoLeverage: true, NoBorrowing: true, NoLending: true,
		NoStaking: true, NoShortSelling: true, NoProductionBroker: true,
		NoProductionSigner: true, NoProductionCredentialInput: true,
		NoProductionPrivateEndpoint: true, NoHiddenRoute: true, NoDormantToggle: true,
		NoEnvironmentBypass: true,
	}
	safety := SafetyManifest{
		SchemaVersion: SafetyManifestSchema, ManifestID: "safety-manifest-1", SourceSHA: source,
		Artifacts: artifacts, Assertions: assertions,
		SignedDestinations: append([]SignedDestination(nil), requiredSignedDestinations...), Reviewer: reviewer,
		ReviewedAt: testNow.Add(-time.Hour), ValidUntil: testNow.Add(30 * 24 * time.Hour),
	}
	resealSafety(t, key, &safety)
	prerequisites := validPrerequisites(t, key, reviewer, source, reference)
	reviews := validReviews(t, key, reviewer, source, reference)
	criteria := validCriteria(source, reference)
	return Candidate{
		SchemaVersion: CandidateSchema, CandidateID: "candidate-1", SourceSHA: source,
		PreparedAt: testNow.Add(-time.Hour), ValidUntil: testNow.Add(30 * 24 * time.Hour),
		SafetyManifest: safety, Prerequisites: prerequisites, Reviews: reviews, Section35: criteria,
	}, trust, key
}

func validReviewerTrust(key ed25519.PrivateKey) (ReviewerIdentity, TrustStore) {
	public := key.Public().(ed25519.PublicKey)
	reviewer := ReviewerIdentity{ID: "independent-reviewer-1", Role: "v1-reviewer", Independent: true}
	trust := TrustStore{SchemaVersion: TrustStoreSchema, Reviewers: []TrustedReviewer{{
		Reviewer: reviewer, PublicKey: base64.StdEncoding.EncodeToString(public),
		KeyFingerprint: publicKeyFingerprint(public), ValidUntil: testNow.Add(60 * 24 * time.Hour),
	}}}
	return reviewer, trust
}

func validArtifacts(source string) []ArtifactIdentity {
	artifactNames := make([]string, 0, len(requiredArtifacts))
	for name := range requiredArtifacts {
		artifactNames = append(artifactNames, name)
	}
	slicesSort(artifactNames)
	artifacts := make([]ArtifactIdentity, 0, len(artifactNames))
	for index, name := range artifactNames {
		artifacts = append(artifacts, ArtifactIdentity{
			Name: name, Kind: requiredArtifacts[name], Digest: fmt.Sprintf("sha256:%064x", index+1),
			SourceSHA: source, Immutable: true, SignatureVerified: true,
		})
	}
	return artifacts
}

func validPrerequisites(t *testing.T, key ed25519.PrivateKey, reviewer ReviewerIdentity,
	source string, reference func(int) []EvidenceReference) []PrerequisiteVerdict {
	t.Helper()
	prerequisites := make([]PrerequisiteVerdict, 0, len(requiredPhaseGates))
	for index, gate := range requiredPhaseGates {
		verdict := PrerequisiteVerdict{
			SchemaVersion: PrerequisiteSchema, EvidenceID: "phase-" + gate + "-verdict", GateID: gate,
			SourceSHA: source, State: "PASSED", Formal: true, Qualified: true,
			ProfitabilityEvidence: false, Evidence: reference(index), Reviewer: reviewer,
			IssuedAt: testNow.Add(-time.Hour), ValidUntil: testNow.Add(30 * 24 * time.Hour),
		}
		resealPrerequisite(t, key, &verdict)
		prerequisites = append(prerequisites, verdict)
	}
	return prerequisites
}

func validReviews(t *testing.T, key ed25519.PrivateKey, reviewer ReviewerIdentity,
	source string, reference func(int) []EvidenceReference) []ReviewRecord {
	t.Helper()
	reviews := make([]ReviewRecord, 0, len(requiredReviewAreas))
	for index, area := range requiredReviewAreas {
		review := ReviewRecord{
			SchemaVersion: ReviewSchema, ReviewID: "review-" + area, Area: area,
			SourceSHA: source, State: "APPROVED", Evidence: reference(index), Findings: []Finding{},
			Reviewer: reviewer, ReviewedAt: testNow.Add(-time.Hour), ValidUntil: testNow.Add(30 * 24 * time.Hour),
		}
		resealReview(t, key, &review)
		reviews = append(reviews, review)
	}
	return reviews
}

func validCriteria(source string, reference func(int) []EvidenceReference) []CriterionStatus {
	criteria := make([]CriterionStatus, 0, 22)
	for number := 1; number <= 22; number++ {
		criteria = append(criteria, CriterionStatus{Number: number, State: "PASSED", SourceSHA: source,
			Evidence: reference(number), Blockers: []string{}})
	}
	return criteria
}

func resealSafety(t *testing.T, key ed25519.PrivateKey, safety *SafetyManifest) {
	t.Helper()
	if err := SealSafetyManifest(key, safety); err != nil {
		t.Fatal(err)
	}
}

func resealPrerequisite(t *testing.T, key ed25519.PrivateKey, verdict *PrerequisiteVerdict) {
	t.Helper()
	if err := SealPrerequisiteVerdict(key, verdict); err != nil {
		t.Fatal(err)
	}
}

func resealReview(t *testing.T, key ed25519.PrivateKey, review *ReviewRecord) {
	t.Helper()
	if err := SealReviewRecord(key, review); err != nil {
		t.Fatal(err)
	}
}

func slicesSort(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
