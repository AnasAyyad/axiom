package crossarb

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestJournalSeparatesEveryB5PnLAndCostCategory(t *testing.T) {
	candidate, timeline := simulationFixture(t)
	timeline.directives = arrivalStates(execution.OrderFilled, execution.OrderFilled)
	result, err := Simulate(candidate, timeline, testLatency(), RecoveryPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	memory := accounting.NewMemoryJournal()
	runID, _ := domain.NewRunID("run-b5")
	portfolioID, _ := domain.NewPortfolioID("portfolio-b5")
	journal, err := NewCrossExchangeJournal(memory, JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: "crossarb",
		ConfigurationHash: candidate.ConfigurationHash,
		RecordedAt:        domain.EventTime{UTC: time.Unix(20, 0).UTC(), Sequence: 1},
		FirstOrdinal:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	attribution := completeAttribution()
	transactions, err := journal.Transactions(candidate, result, attribution)
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 11 {
		t.Fatalf("transactions = %d", len(transactions))
	}
	wanted := []string{
		"execution_pnl", "btc_inventory_market_pnl", "eth_inventory_market_pnl",
		"stablecoin_valuation", "fees", "spread", "slippage", "latency",
		"recovery", "inventory_restoration", "combined_pnl",
	}
	for index, name := range wanted {
		if !strings.Contains(transactions[index].Type, name) ||
			accounting.ValidateTransaction(transactions[index]) != nil {
			t.Fatalf("transaction %d = %#v", index, transactions[index])
		}
	}
	if err = journal.Post(candidate, result, attribution); err != nil {
		t.Fatal(err)
	}
	projections, hash, err := memory.Rebuild()
	if err != nil || len(projections) == 0 || hash == "" {
		t.Fatalf("rebuild = %#v, %q, %v", projections, hash, err)
	}
}

func TestJournalRejectsMissingIndependentAttribution(t *testing.T) {
	candidate, timeline := simulationFixture(t)
	timeline.directives = arrivalStates(execution.OrderFilled, execution.OrderFilled)
	result, err := Simulate(candidate, timeline, testLatency(), RecoveryPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := domain.NewRunID("run-b5")
	portfolioID, _ := domain.NewPortfolioID("portfolio-b5")
	journal, err := NewCrossExchangeJournal(accounting.NewMemoryJournal(), JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: "crossarb",
		ConfigurationHash: candidate.ConfigurationHash,
		RecordedAt:        domain.EventTime{UTC: time.Unix(20, 0).UTC(), Sequence: 1},
		FirstOrdinal:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	attribution := completeAttribution()
	attribution.ETHInventoryPnL.Amount = balance("0")
	if _, err = journal.Transactions(candidate, result, attribution); err == nil {
		t.Fatal("zero/missing ETH inventory attribution accepted")
	}
}

func completeAttribution() PortfolioAttribution {
	value := func(gain bool) AttributionValue {
		return AttributionValue{Amount: balance("0.01"), Gain: gain}
	}
	return PortfolioAttribution{
		ExecutionPnL: value(true), BTCInventoryPnL: value(false),
		ETHInventoryPnL: value(true), StablecoinValuation: value(false),
		Fees: balance("0.01"), Spread: balance("0.01"), Slippage: balance("0.01"),
		Latency: balance("0.01"), Recovery: balance("0.01"), Rebalancing: balance("0.01"),
		CombinedPnL: value(true),
	}
}
