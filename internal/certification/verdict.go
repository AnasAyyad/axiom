package certification

import (
	"crypto/ed25519"
	"fmt"
	"time"
)

const maximumVerdictLifetime = 30 * 24 * time.Hour

// NewReleaseVerdict creates and signs a passing verdict after validation.
func NewReleaseVerdict(candidate Candidate, key ed25519.PrivateKey, now, validUntil time.Time) (ReleaseVerdict, error) {
	now, validUntil = now.UTC(), validUntil.UTC()
	if len(key) != ed25519.PrivateKeySize || !validUntil.After(now) ||
		validUntil.Sub(now) > maximumVerdictLifetime {
		return ReleaseVerdict{}, fmt.Errorf("release_verdict_configuration_invalid")
	}
	candidateHash, err := CandidateHash(candidate)
	if err != nil {
		return ReleaseVerdict{}, err
	}
	verdict := ReleaseVerdict{
		SchemaVersion: ReleaseVerdictSchema, CandidateID: candidate.CandidateID,
		CandidateHash: candidateHash, SourceSHA: candidate.SourceSHA,
		State: CertifiedReleaseState, Certified: true, ProfitabilityEvidence: false,
		IssuedAt: now, ValidUntil: validUntil,
	}
	if err = SealReleaseVerdict(key, &verdict); err != nil {
		return ReleaseVerdict{}, err
	}
	return verdict, nil
}
