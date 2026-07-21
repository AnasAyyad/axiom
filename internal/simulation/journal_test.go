package simulation

import (
	"strings"
	"testing"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestFillJournalBalancesFillFeeRebateDustAndRecovery(t *testing.T) {
	memory := accounting.NewMemoryJournal()
	runID, _ := domain.NewRunID("run-one")
	portfolioID, _ := domain.NewPortfolioID("trend-one")
	journal, err := NewFillJournal(memory, JournalContext{RunID: runID, PortfolioID: portfolioID,
		Owner: "trend-one", ConfigurationHash: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	order := journalOrder(t)
	fill := journalFill(t)
	if err = journal.PostFill(order, fill); err != nil {
		t.Fatal(err)
	}
	asset, _ := domain.ParseAssetSymbol("BTC")
	quantity, _ := domain.ParseBalance("0.000001")
	dustEvent, _ := domain.NewEventID("dust-one")
	if err = journal.PostAdjustment("dust", accounting.RoundingDust, asset, quantity, dustEvent, 2); err != nil {
		t.Fatal(err)
	}
	recoveryEvent, _ := domain.NewEventID("recovery-one")
	if err = journal.PostAdjustment("recovery", accounting.RecoveryLoss, asset, quantity, recoveryEvent, 3); err != nil {
		t.Fatal(err)
	}
	projections, hash, err := memory.Rebuild()
	if err != nil || len(projections) == 0 || len(hash) != 64 || len(memory.Transactions()) != 3 {
		t.Fatalf("rebuild = %d %s %v", len(projections), hash, err)
	}
}

func journalOrder(t *testing.T) execution.OrderIdentity {
	t.Helper()
	orderID, _ := domain.NewVirtualOrderID("order-one")
	planID, _ := domain.NewExecutionPlanID("plan-one")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	return execution.OrderIdentity{ID: orderID, PlanID: planID, ClientOrderID: "client-one",
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity}
}

func journalFill(t *testing.T) execution.FillFact {
	t.Helper()
	fillID, _ := domain.NewVirtualFillID("fill-one")
	quantity, _ := domain.ParseQuantity("1")
	price, _ := domain.ParsePrice("100")
	fee, _ := domain.ParseFee("0.1")
	rebate, _ := domain.ParseFee("0.01")
	asset, _ := domain.ParseAssetSymbol("USDT")
	return execution.FillFact{ID: fillID, Quantity: quantity, Price: price, Fee: fee,
		Rebate: rebate, FeeAsset: asset, Ordinal: 1}
}
