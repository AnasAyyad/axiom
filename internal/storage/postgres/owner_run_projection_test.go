package postgres

import (
	"testing"
	"time"
)

type ownerRunRow struct {
	values []any
	err    error
}

func (row ownerRunRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, destination := range destinations {
		switch target := destination.(type) {
		case *string:
			*target = row.values[index].(string)
		case *int64:
			*target = row.values[index].(int64)
		case *int:
			*target = int(row.values[index].(int64))
		case *time.Time:
			*target = row.values[index].(time.Time)
		case *[]string:
			*target = row.values[index].([]string)
		}
	}
	return nil
}

func TestOwnerDataCatalogueUsesReadableNamesInsteadOfStorageIdentifiers(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerDataCatalogue(ownerRunRow{values: []any{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"decision_inputs", "qualified", "A", now.Add(-time.Hour), now, int64(4), int64(0), []string{"binance"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Name == "" || item.Source != "approved_historical_data" || item.QualityTier == nil ||
		*item.QualityTier != "tier_a" || len(item.SupportedModes) != 2 {
		t.Fatalf("owner data catalogue=%+v", item)
	}
}

func TestOwnerRunProjectionUsesSemanticLabelsAndWaitingReasons(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerRun(ownerRunRow{values: []any{
		"replay-123", "replay", "PAUSED", int64(7), now, now, "mean-reversion-v1b-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.StrategyId != "mean-reversion" || item.StrategyVersion != "mean-reversion@1.0.0" ||
		item.Environment != "recorded_data" || item.WaitingReason == nil {
		t.Fatalf("semantic run projection=%+v", item)
	}
	if item.Id == "" || item.Revision != "7" || !item.OrderCapable {
		t.Fatalf("durable run fields missing=%+v", item)
	}
}
