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
		case *time.Time:
			*target = row.values[index].(time.Time)
		}
	}
	return nil
}

func TestOwnerRunProjectionUsesSemanticLabelsAndWaitingReasons(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerRun(ownerRunRow{values: []any{
		"replay-123", "replay", "PAUSED", int64(7), now, now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.StrategyId != "trend-following" || item.StrategyVersion != "trend-following@1.0.0" ||
		item.Environment != "recorded_data" || item.WaitingReason == nil {
		t.Fatalf("semantic run projection=%+v", item)
	}
	if item.Id == "" || item.Revision != "7" || !item.OrderCapable {
		t.Fatalf("durable run fields missing=%+v", item)
	}
}
