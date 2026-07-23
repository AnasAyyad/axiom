package triangular

import (
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
)

func TestCycleJournalSeparatesAndBalancesFullCycleEconomics(t *testing.T) {
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	result, err := Simulate(candidate, &scriptedTimeline{
		markets: profitableInput(t, false).Markets,
	}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	memory := accounting.NewMemoryJournal()
	journal := newTestCycleJournal(t, memory, candidate)
	if err = journal.Post(candidate, result); err != nil {
		t.Fatal(err)
	}
	transactions := memory.Transactions()
	types := make(map[string]bool)
	for _, transaction := range transactions {
		if err = accounting.ValidateTransaction(transaction); err != nil {
			t.Fatalf("unbalanced transaction %s: %v", transaction.Type, err)
		}
		types[transaction.Type] = true
	}
	for _, required := range []string{"b4_trade_economics", "b4_fees"} {
		if !types[required] {
			t.Fatalf("missing %s in %#v", required, types)
		}
	}
	if _, digest, rebuildErr := memory.Rebuild(); rebuildErr != nil || digest == "" {
		t.Fatalf("journal did not rebuild: %s %v", digest, rebuildErr)
	}
}

func TestCycleJournalSeparatesRecoveryAndStrandedInventory(t *testing.T) {
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "10")
	recovered, err := Simulate(candidate, &scriptedTimeline{
		markets: input.Markets, failures: map[string]bool{"BTC/ETH": true},
	}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	recoveryMemory := accounting.NewMemoryJournal()
	if err = newTestCycleJournal(t, recoveryMemory, candidate).Post(candidate, recovered); err != nil {
		t.Fatal(err)
	}
	if !hasTransactionType(recoveryMemory.Transactions(), "b4_recovery_unwind") {
		t.Fatalf("recovery loss was not separated: %#v", recoveryMemory.Transactions())
	}

	stranded, err := Simulate(candidate, &scriptedTimeline{
		markets: input.Markets, failures: map[string]bool{"BTC/ETH": true, "BTC/USDT": true},
	}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	strandedMemory := accounting.NewMemoryJournal()
	if err = newTestCycleJournal(t, strandedMemory, candidate).Post(candidate, stranded); err != nil {
		t.Fatal(err)
	}
	if !hasTransactionType(strandedMemory.Transactions(), "b4_stranded_inventory") {
		t.Fatalf("stranded inventory was not separated: %#v", strandedMemory.Transactions())
	}
}

func TestCycleJournalRejectsMismatchedCandidateAndPostsExplicitReconciliation(t *testing.T) {
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	memory := accounting.NewMemoryJournal()
	journal := newTestCycleJournal(t, memory, candidate)
	result := SimulationResult{CandidateID: "different"}
	if err := journal.Post(candidate, result); err == nil {
		t.Fatal("mismatched simulation was journaled")
	}
	asset, _ := domain.ParseAssetSymbol("BTC")
	quantity, _ := domain.ParseBalance("0.0001")
	if err := journal.PostReconciliation(candidate, asset, quantity, 90); err != nil {
		t.Fatal(err)
	}
	if !hasTransactionType(memory.Transactions(), "b4_reconciliation_adjustment") {
		t.Fatal("explicit reconciliation adjustment missing")
	}
}

func newTestCycleJournal(
	t *testing.T,
	memory accounting.Journal,
	candidate Candidate,
) *CycleJournal {
	t.Helper()
	runID, _ := domain.NewRunID("b4-run")
	portfolioID, _ := domain.NewPortfolioID("portfolio-a")
	journal, err := NewCycleJournal(memory, JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: "portfolio-a",
		ConfigurationHash: candidate.ConfigurationHash,
		RecordedAt: domain.EventTime{
			UTC: time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC), Sequence: 1,
		},
		FirstOrdinal: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func hasTransactionType(transactions []accounting.Transaction, wanted string) bool {
	for _, transaction := range transactions {
		if transaction.Type == wanted {
			return true
		}
	}
	return false
}
