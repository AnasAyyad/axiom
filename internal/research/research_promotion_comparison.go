package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// ChampionChallengerInput records one explicit version comparison without
// changing either version's maturity.
type ChampionChallengerInput struct {
	ID                     string        `json:"id"`
	StrategyFamily         string        `json:"strategy_family"`
	ChampionVersionID      string        `json:"champion_version_id"`
	ChallengerVersionID    string        `json:"challenger_version_id"`
	ChampionEvidenceHash   string        `json:"champion_evidence_hash"`
	ChallengerEvidenceHash string        `json:"challenger_evidence_hash"`
	Overall                []ResultSlice `json:"overall"`
	Regimes                []ResultSlice `json:"regimes"`
	Disposition            string        `json:"disposition"`
	Reason                 string        `json:"reason"`
	CreatedAt              time.Time     `json:"created_at"`
}

// ChampionChallengerReport is immutable comparison evidence. Explicit
// promotion remains a separate authenticated command.
type ChampionChallengerReport struct {
	Contract string `json:"contract"`
	ChampionChallengerInput
	Disclaimer   string `json:"disclaimer"`
	ManifestHash string `json:"-"`
}

// BuildChampionChallengerReport validates and seals a comparison report.
func BuildChampionChallengerReport(
	input ChampionChallengerInput,
) (ChampionChallengerReport, []byte, error) {
	input.Overall = append([]ResultSlice(nil), input.Overall...)
	input.Regimes = append([]ResultSlice(nil), input.Regimes...)
	sort.Slice(input.Overall, func(left, right int) bool {
		return input.Overall[left].Name < input.Overall[right].Name
	})
	sort.Slice(input.Regimes, func(left, right int) bool {
		return input.Regimes[left].Name < input.Regimes[right].Name
	})
	if !validChampionChallengerInput(input) {
		return ChampionChallengerReport{}, nil, researchError("champion_challenger_invalid")
	}
	report := ChampionChallengerReport{Contract: "champion-challenger.v1",
		ChampionChallengerInput: input, Disclaimer: DisclaimerNoProductionProfitability}
	canonical, err := json.Marshal(report)
	if err != nil {
		return ChampionChallengerReport{}, nil, researchError("champion_challenger_serialization_failed")
	}
	digest := sha256.Sum256(canonical)
	report.ManifestHash = hex.EncodeToString(digest[:])
	return report, canonical, nil
}

// ValidateChampionChallengerCanonical proves exact stored comparison bytes.
func ValidateChampionChallengerCanonical(
	canonical []byte,
	expectedHash string,
) (ChampionChallengerReport, error) {
	var stored ChampionChallengerReport
	if !json.Valid(canonical) || json.Unmarshal(canonical, &stored) != nil ||
		stored.Contract != "champion-challenger.v1" ||
		stored.Disclaimer != DisclaimerNoProductionProfitability ||
		!validEvidenceHash(expectedHash) {
		return ChampionChallengerReport{}, researchError("champion_challenger_canonical_invalid")
	}
	rebuilt, encoded, err := BuildChampionChallengerReport(stored.ChampionChallengerInput)
	if err != nil || rebuilt.ManifestHash != expectedHash || !bytes.Equal(encoded, canonical) {
		return ChampionChallengerReport{}, researchError("champion_challenger_canonical_invalid")
	}
	return rebuilt, nil
}

func validChampionChallengerInput(input ChampionChallengerInput) bool {
	if !researchIdentifier.MatchString(input.ID) ||
		!researchIdentifier.MatchString(input.StrategyFamily) ||
		!researchIdentifier.MatchString(input.ChampionVersionID) ||
		!researchIdentifier.MatchString(input.ChallengerVersionID) ||
		input.ChampionVersionID == input.ChallengerVersionID ||
		!validEvidenceHash(input.ChampionEvidenceHash) ||
		!validEvidenceHash(input.ChallengerEvidenceHash) ||
		len(input.Overall) < 2 || len(input.Regimes) == 0 ||
		(input.Disposition != "retain_champion" &&
			input.Disposition != "recommend_challenger" &&
			input.Disposition != "reject_challenger") ||
		input.Reason == "" || input.CreatedAt.Location() != time.UTC {
		return false
	}
	for _, result := range append(input.Overall, input.Regimes...) {
		if _, _, err := parseResult(result); err != nil {
			return false
		}
	}
	return true
}
