package evaluation

import (
	"crypto/sha256"
	"encoding/json"
	"time"
)

// Report is an immutable full or partial campaign result. CanonicalPayload is
// server-generated JSON and its hash is stored independently.
type Report struct {
	State            string
	Verdict          Verdict
	Reason           ReasonCode
	Summary          string
	CanonicalPayload []byte
	Hash             [32]byte
	GeneratedAt      time.Time
}

// NewReport creates canonical immutable report evidence.
func NewReport(state string, verdict Verdict, reason ReasonCode, summary string, payload any, at time.Time) (Report, error) {
	canonical, err := json.Marshal(payload)
	if err != nil {
		return Report{}, err
	}
	value := Report{State: state, Verdict: verdict, Reason: reason, Summary: summary,
		CanonicalPayload: canonical, Hash: sha256.Sum256(canonical), GeneratedAt: at.UTC()}
	if !value.Valid() {
		return Report{}, ErrInvalidTransition
	}
	return value, nil
}

// Valid checks the immutable report envelope.
func (value Report) Valid() bool {
	if (value.State != "final" && value.State != "partial") || value.Summary == "" ||
		len(value.CanonicalPayload) < 2 || !json.Valid(value.CanonicalPayload) || value.GeneratedAt.IsZero() ||
		sha256.Sum256(value.CanonicalPayload) != value.Hash {
		return false
	}
	if value.State == "partial" {
		return value.Verdict == VerdictBlocked && value.Reason != ""
	}
	return value.Verdict == VerdictContinue || value.Verdict == VerdictImprove ||
		value.Verdict == VerdictReject || value.Verdict == VerdictBlocked
}
