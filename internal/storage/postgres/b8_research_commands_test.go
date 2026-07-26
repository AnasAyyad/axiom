package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
)

func TestB8ReportExportIsDeterministicAndFailsClosed(t *testing.T) {
	championSuite := "suite-champion"
	challengerSuite := "suite-challenger"
	report := generated.ChampionChallengerReport{
		Id:                        "report-b8",
		ChampionStrategyVersion:   "champion-v1",
		ChallengerStrategyVersion: "challenger-v2",
		ChampionSuiteId:           &championSuite,
		ChallengerSuiteId:         &challengerSuite,
		Confidence:                "local_tier_b",
		Viability:                 "viable_for_more_research",
		Disposition:               "retain_champion",
		Disclaimer:                "No production profitability claim.",
		ManifestHash:              strings.Repeat("8", 64),
		Revision:                  "1",
		CreatedAt:                 time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
	}
	for _, format := range []string{"json", "csv"} {
		t.Run(format, func(t *testing.T) {
			first, contentType, err := renderB8Report(report, format)
			if err != nil || first == "" || contentType == "" {
				t.Fatalf("first B8 export content=%q type=%q error=%v", first, contentType, err)
			}
			second, repeatedType, err := renderB8Report(report, format)
			if err != nil || second != first || repeatedType != contentType {
				t.Fatalf("B8 export is not deterministic: %q %q %v", second, repeatedType, err)
			}
		})
	}
	if _, _, err := renderB8Report(report, "html"); !errors.Is(err, console.ErrInvalidRequest) {
		t.Fatalf("unsupported B8 report format error=%v", err)
	}
}
