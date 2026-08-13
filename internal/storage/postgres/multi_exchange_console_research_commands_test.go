package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
)

func TestMultiExchangeConsoleReportExportIsDeterministicAndFailsClosed(t *testing.T) {
	championSuite := "suite-champion"
	challengerSuite := "suite-challenger"
	report := generated.ChampionChallengerReport{
		Id:                        "report-multi_exchange_console",
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
			first, contentType, err := renderMultiExchangeConsoleReport(report, format)
			if err != nil || first == "" || contentType == "" {
				t.Fatalf("first multi-exchange console export content=%q type=%q error=%v", first, contentType, err)
			}
			second, repeatedType, err := renderMultiExchangeConsoleReport(report, format)
			if err != nil || second != first || repeatedType != contentType {
				t.Fatalf("multi-exchange console export is not deterministic: %q %q %v", second, repeatedType, err)
			}
		})
	}
	if _, _, err := renderMultiExchangeConsoleReport(report, "html"); !errors.Is(err, console.ErrInvalidRequest) {
		t.Fatalf("unsupported multi-exchange console report format error=%v", err)
	}
}
