package certification

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func digestJSON(value any) ([]byte, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("certification_evidence_encode_failed")
	}
	digest := sha256.Sum256(payload)
	return digest[:], hex.EncodeToString(digest[:]), nil
}

func publicKeyFingerprint(public ed25519.PublicKey) string {
	digest := sha256.Sum256(public)
	return hex.EncodeToString(digest[:])
}

func seal(key ed25519.PrivateKey, value any) (string, string, string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", "", "", fmt.Errorf("certification_signing_key_invalid")
	}
	digest, encoded, err := digestJSON(value)
	if err != nil {
		return "", "", "", err
	}
	public := key.Public().(ed25519.PublicKey)
	return encoded, publicKeyFingerprint(public),
		base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest)), nil
}

func verifySeal(public ed25519.PublicKey, value any, evidenceHash, fingerprint, signature string) bool {
	if len(public) != ed25519.PublicKeySize || fingerprint != publicKeyFingerprint(public) {
		return false
	}
	digest, encoded, err := digestJSON(value)
	if err != nil || encoded != evidenceHash {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	return err == nil && ed25519.Verify(public, digest, decoded)
}

// SealSafetyManifest authenticates one fully populated safety manifest.
func SealSafetyManifest(key ed25519.PrivateKey, manifest *SafetyManifest) error {
	manifest.EvidenceHash, manifest.SigningKeyFingerprint, manifest.Signature = "", "", ""
	hash, fingerprint, signature, err := seal(key, *manifest)
	if err != nil {
		return err
	}
	manifest.EvidenceHash, manifest.SigningKeyFingerprint, manifest.Signature = hash, fingerprint, signature
	return nil
}

func verifySafetyManifest(public ed25519.PublicKey, manifest SafetyManifest) bool {
	hash, fingerprint, signature := manifest.EvidenceHash, manifest.SigningKeyFingerprint, manifest.Signature
	manifest.EvidenceHash, manifest.SigningKeyFingerprint, manifest.Signature = "", "", ""
	return verifySeal(public, manifest, hash, fingerprint, signature)
}

// SealPrerequisiteVerdict authenticates one fully populated readiness verdict.
func SealPrerequisiteVerdict(key ed25519.PrivateKey, verdict *PrerequisiteVerdict) error {
	verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature = "", "", ""
	hash, fingerprint, signature, err := seal(key, *verdict)
	if err != nil {
		return err
	}
	verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature = hash, fingerprint, signature
	return nil
}

func verifyPrerequisiteVerdict(public ed25519.PublicKey, verdict PrerequisiteVerdict) bool {
	hash, fingerprint, signature := verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature
	verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature = "", "", ""
	return verifySeal(public, verdict, hash, fingerprint, signature)
}

// SealReviewRecord authenticates one fully populated review record.
func SealReviewRecord(key ed25519.PrivateKey, review *ReviewRecord) error {
	review.EvidenceHash, review.SigningKeyFingerprint, review.Signature = "", "", ""
	hash, fingerprint, signature, err := seal(key, *review)
	if err != nil {
		return err
	}
	review.EvidenceHash, review.SigningKeyFingerprint, review.Signature = hash, fingerprint, signature
	return nil
}

func verifyReviewRecord(public ed25519.PublicKey, review ReviewRecord) bool {
	hash, fingerprint, signature := review.EvidenceHash, review.SigningKeyFingerprint, review.Signature
	review.EvidenceHash, review.SigningKeyFingerprint, review.Signature = "", "", ""
	return verifySeal(public, review, hash, fingerprint, signature)
}

// CandidateHash returns the immutable digest signed by the final verdict.
func CandidateHash(candidate Candidate) (string, error) {
	_, encoded, err := digestJSON(candidate)
	return encoded, err
}

// SealReleaseVerdict authenticates a successful final verdict.
func SealReleaseVerdict(key ed25519.PrivateKey, verdict *ReleaseVerdict) error {
	verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature = "", "", ""
	hash, fingerprint, signature, err := seal(key, *verdict)
	if err != nil {
		return err
	}
	verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature = hash, fingerprint, signature
	return nil
}

// VerifyReleaseVerdict proves that one terminal verdict authenticates the exact
// candidate and remains within its short validity window.
func VerifyReleaseVerdict(
	candidate Candidate,
	verdict ReleaseVerdict,
	public ed25519.PublicKey,
	now time.Time,
) error {
	if verdict.SchemaVersion != ReleaseVerdictSchema || verdict.CandidateID != candidate.CandidateID ||
		verdict.SourceSHA != candidate.SourceSHA || verdict.State != CertifiedReleaseState ||
		!verdict.Certified || verdict.ProfitabilityEvidence ||
		!validWindow(verdict.IssuedAt, verdict.ValidUntil, now.UTC()) ||
		verdict.ValidUntil.Sub(verdict.IssuedAt) > maximumVerdictLifetime {
		return fmt.Errorf("release_verdict_invalid")
	}
	candidateHash, err := CandidateHash(candidate)
	if err != nil || verdict.CandidateHash != candidateHash {
		return fmt.Errorf("release_verdict_candidate_mismatch")
	}
	hash, fingerprint, signature := verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature
	verdict.EvidenceHash, verdict.SigningKeyFingerprint, verdict.Signature = "", "", ""
	if !verifySeal(public, verdict, hash, fingerprint, signature) {
		return fmt.Errorf("release_verdict_unsigned_or_tampered")
	}
	return nil
}
