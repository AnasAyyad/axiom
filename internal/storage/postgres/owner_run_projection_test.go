package postgres

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
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
		case **string:
			if row.values[index] == nil {
				*target = nil
				continue
			}
			value := row.values[index].(string)
			*target = &value
		case **time.Time:
			if row.values[index] == nil {
				*target = nil
				continue
			}
			value := row.values[index].(time.Time)
			*target = &value
		}
	}
	return nil
}

func TestOwnerDataCatalogueUsesReadableNamesInsteadOfStorageIdentifiers(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerDataCatalogue(ownerRunRow{values: []any{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"decision_inputs", "qualified", "A", now.Add(-time.Hour), now, int64(4), int64(0), []string{"binance"},
		[]string{"BTCUSDT"}, []string{"book", "trade"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Name == "" || item.Source != "approved_historical_data" || item.QualityTier == nil ||
		*item.QualityTier != "tier_a" || len(item.SupportedModes) != 2 ||
		len(item.Instruments) != 1 || item.Instruments[0] != "BTCUSDT" ||
		len(item.CoverageTypes) != 2 {
		t.Fatalf("owner data catalogue=%+v", item)
	}
}

func TestOwnerRunProjectionUsesSemanticLabelsAndWaitingReasons(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerRun(ownerRunRow{values: []any{
		"replay-123", "replay", "PAUSED", int64(7), now, now, "mean-reversion-1-0-0",
		[]string{}, nil, nil, []string{}, nil, nil,
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
	if len(item.AvailableActions) != 2 || item.AvailableActions[0] != "resume" ||
		item.AvailableActions[1] != "step" {
		t.Fatalf("safe run controls=%+v", item.AvailableActions)
	}
}

func TestOwnerRunProjectionIncludesAutomaticExchangeSandboxSessions(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerRun(ownerRunRow{values: []any{
		"sandbox-session", "sandbox", "running", int64(3), now, now,
		"cross-exchange-arbitrage", []string{"binance", "bybit"}, "BTCUSDT", nil,
		[]string{"strategy_plan_approved", "waiting_for_binance_coordinator"}, nil, nil,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Mode != generated.RunResourceModeSandbox ||
		item.Environment != generated.PairedExchangeSandbox ||
		item.StrategyId != "cross-exchange-arbitrage" || item.Instrument == nil ||
		*item.Instrument != "BTC/USDT" || item.Exchanges == nil || len(*item.Exchanges) != 2 ||
		item.WaitingReason == nil || !strings.Contains(*item.WaitingReason, "Binance Spot Testnet") ||
		!strings.Contains(*item.WaitingReason, "Bybit Demo") {
		t.Fatalf("sandbox run projection=%+v", item)
	}
	if len(item.AvailableActions) != 1 || item.AvailableActions[0] != generated.RunActionStop {
		t.Fatalf("sandbox run controls=%+v", item.AvailableActions)
	}
}

func TestOwnerRunProjectionRetainsReviewedBybitShadowSelection(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	next := now.Add(4*time.Hour + 2*time.Second)
	waiting := "No Trend decision is due yet; waiting for the next finalized four-hour candle."
	item, err := scanOwnerRun(ownerRunRow{values: []any{
		"shadow-session", "shadow", "RUNNING", int64(2), now, now,
		"trend-following-1-0-0", []string{"bybit"}, "ETHUSDT", nil, []string{}, waiting, next,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Environment != generated.ProductionPublic ||
		item.StrategyId != "trend-following" || item.Instrument == nil || *item.Instrument != "ETH/USDT" ||
		item.Exchanges == nil || len(*item.Exchanges) != 1 || (*item.Exchanges)[0] != "bybit" ||
		item.WaitingReason == nil || *item.WaitingReason != waiting || item.NextEvaluationAt == nil ||
		time.Time(*item.NextEvaluationAt) != next {
		t.Fatalf("Bybit shadow projection=%+v", item)
	}
}

func TestOwnerRunProjectionMapsEveryInstalledOfflineStrategy(t *testing.T) {
	for _, test := range []struct {
		storedVersion string
		strategyID    string
		version       string
		name          string
	}{
		{"trend-following-1-0-0", "trend-following", "trend-following@1.0.0", "Trend Following"},
		{"mean-reversion-1-0-0", "mean-reversion", "mean-reversion@1.0.0", "Mean Reversion"},
		{"triangular-arbitrage-1-0-0", "triangular-arbitrage", "triangular-arbitrage@1.0.0", "Triangular Arbitrage"},
		{"cross-exchange-arbitrage-1-0-0", "cross-exchange-arbitrage", "cross-exchange-arbitrage@1.0.0", "Cross-Exchange Arbitrage"},
		{"inventory-rebalancing-1-0-0", "inventory-rebalancing", "inventory-rebalancing@1.0.0", "Inventory Rebalancing"},
	} {
		t.Run(test.strategyID, func(t *testing.T) {
			strategyID, version, name, found := ownerRunStrategy(test.storedVersion)
			if !found || strategyID != test.strategyID || version != test.version || name != test.name {
				t.Fatalf("stored version %q mapped to %q %q %q found=%t", test.storedVersion, strategyID, version, name, found)
			}
		})
	}
}

func TestOwnerRunProjectionMarksInventoryRebalancingAdvisoryOnly(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	item, err := scanOwnerRun(ownerRunRow{values: []any{
		"inventory-backtest", "backtest", "SUCCEEDED", int64(3), now, now,
		"inventory-rebalancing-1-0-0", []string{}, nil, nil, []string{}, nil, nil,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.StrategyId != "inventory-rebalancing" || item.OrderCapable || item.Environment != "recorded_data" {
		t.Fatalf("inventory advisory run=%+v", item)
	}
}

func TestOwnerRunActionsFollowTheDurableControlPolicy(t *testing.T) {
	tests := []struct {
		mode, state string
		want        []string
	}{
		{mode: "backtest", state: "RUNNING", want: []string{}},
		{mode: "replay", state: "RUNNING", want: []string{"pause"}},
		{mode: "replay", state: "PAUSED", want: []string{"resume", "step"}},
		{mode: "shadow", state: "QUEUED", want: []string{"stop"}},
		{mode: "shadow", state: "CANCEL_REQUESTED", want: []string{}},
		{mode: "sandbox", state: "prepared", want: []string{"stop"}},
		{mode: "sandbox", state: "stopped", want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.mode+"_"+test.state, func(t *testing.T) {
			got := ownerRunActions(test.mode, test.state)
			if len(got) != len(test.want) {
				t.Fatalf("actions=%v want=%v", got, test.want)
			}
			for index := range got {
				if string(got[index]) != test.want[index] {
					t.Fatalf("actions=%v want=%v", got, test.want)
				}
			}
		})
	}
}
