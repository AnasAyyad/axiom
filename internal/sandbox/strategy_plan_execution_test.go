package sandbox

import (
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestStrategyPlanExecutionPreservesExactPartialFill(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	entry := strategyJournalEntry(t, now, "plan")
	requested, _ := domain.ParseQuantity("1")
	filled, _ := domain.ParseQuantity("0.4")
	price, _ := domain.ParsePrice("100")
	fee, _ := domain.ParseFee("0")
	fillID, _ := domain.NewVirtualFillID("partial")
	fact := StrategyPlanExecution{PlanID: "plan", Side: domain.SideBuy,
		RequestedQuantity: requested, CumulativeQuantity: filled,
		Fills:      []execution.FillFact{{ID: fillID, Quantity: filled, Price: price, Fee: fee, Ordinal: 1}},
		ObservedAt: now}
	fact.EvidenceHash = StrategyPlanExecutionEvidenceHash(fact)
	if fact.ValidFor(entry, now) != nil {
		t.Fatalf("partial execution rejected: %#v", fact)
	}
	fact.CumulativeQuantity = requested
	if fact.ValidFor(entry, now) == nil {
		t.Fatal("execution accepted a rounded-up partial fill")
	}
}

func TestStrategyPlanExecutionRejectsDuplicateFill(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	entry := strategyJournalEntry(t, now, "plan")
	quantity, _ := domain.ParseQuantity("1")
	price, _ := domain.ParsePrice("100")
	fee, _ := domain.ParseFee("0")
	fillID, _ := domain.NewVirtualFillID("duplicate")
	fact := StrategyPlanExecution{PlanID: "plan", Side: domain.SideBuy,
		RequestedQuantity: quantity, CumulativeQuantity: quantity,
		Fills: []execution.FillFact{{ID: fillID, Quantity: quantity, Price: price, Fee: fee, Ordinal: 1},
			{ID: fillID, Quantity: quantity, Price: price, Fee: fee, Ordinal: 2}}, ObservedAt: now}
	fact.EvidenceHash = StrategyPlanExecutionEvidenceHash(fact)
	if fact.ValidFor(entry, now) == nil {
		t.Fatal("duplicate fill accepted")
	}
}

func TestStrategyPlanExecutionUsesExactWeightedPartialFillPrice(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	entry := strategyJournalEntry(t, now, "plan")
	requested, _ := domain.ParseQuantity("1")
	firstQuantity, _ := domain.ParseQuantity("0.25")
	secondQuantity, _ := domain.ParseQuantity("0.5")
	filled, _ := firstQuantity.Add(secondQuantity)
	firstPrice, _ := domain.ParsePrice("100")
	secondPrice, _ := domain.ParsePrice("106")
	fee, _ := domain.ParseFee("0")
	firstID, _ := domain.NewVirtualFillID("weighted-first")
	secondID, _ := domain.NewVirtualFillID("weighted-second")
	fact := StrategyPlanExecution{PlanID: "plan", Side: domain.SideBuy,
		RequestedQuantity: requested, CumulativeQuantity: filled,
		Fills: []execution.FillFact{{ID: firstID, Quantity: firstQuantity, Price: firstPrice, Fee: fee, Ordinal: 1},
			{ID: secondID, Quantity: secondQuantity, Price: secondPrice, Fee: fee, Ordinal: 2}}, ObservedAt: now}
	fact.EvidenceHash = StrategyPlanExecutionEvidenceHash(fact)
	if fact.ValidFor(entry, now) != nil {
		t.Fatal("valid weighted fill rejected")
	}
	average, err := fact.AverageFillPrice()
	if err != nil || average.String() != "104" {
		t.Fatalf("average=%s error=%v", average.String(), err)
	}
}

func strategyJournalEntry(t *testing.T, now time.Time, planID string) StrategyDecisionJournalEntry {
	t.Helper()
	admission := strategyPlanAdmission(now)
	evidence, err := NewStrategyDecisionEvidence(admission,
		strategyPipelineEvent(), []byte(`{"id":"decision:sandbox-strategy-decision","ordinal":1,"action":"entry","candidate":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	return StrategyDecisionJournalEntry{Evidence: evidence, PlanID: planID, OccurredAt: now}
}
