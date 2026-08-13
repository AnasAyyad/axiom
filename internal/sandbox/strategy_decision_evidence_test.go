package sandbox

import (
	"testing"
	"time"

	"axiom/internal/replay"
)

func TestStrategyDecisionEvidencePreservesNoActionEvaluation(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	admission := strategyPlanAdmission(now)
	event := replay.Event{Ordinal: 2, LogicalTime: 3, Canonical: []byte(`{"market":"complete"}`)}
	evidence, err := NewStrategyDecisionEvidence(admission, event,
		[]byte(`{"id":"decision:no-action","ordinal":2,"action":"none","reason_code":"trend.reject.existing_position"}`))
	if err != nil || evidence.ValidFor(admission, now) != nil {
		t.Fatalf("no-action evidence=%#v error=%v", evidence, err)
	}
	plan := ApprovedSandboxPlan{SessionID: admission.Work.SessionID}
	if evidence.ValidForPlan(plan) == nil {
		t.Fatal("no-action decision was accepted as an order plan")
	}
}

func TestStrategyDecisionEvidenceRejectsMismatchedCanonicalIdentity(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	_, err := NewStrategyDecisionEvidence(strategyPlanAdmission(now),
		replay.Event{Ordinal: 2, LogicalTime: 3, Canonical: []byte(`{"market":"complete"}`)},
		[]byte(`{"id":"decision:wrong-ordinal","ordinal":3,"action":"none"}`))
	if err == nil {
		t.Fatal("mismatched decision ordinal accepted")
	}
}

func TestStrategyDecisionJournalEntryRequiresExactActiveWork(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	admission := strategyPlanAdmission(now)
	evidence, err := NewStrategyDecisionEvidence(admission,
		replay.Event{Ordinal: 2, LogicalTime: 3, Canonical: []byte(`{"market":"complete"}`)},
		[]byte(`{"id":"decision:no-action","ordinal":2,"action":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	entry := StrategyDecisionJournalEntry{Evidence: evidence, OccurredAt: now}
	if entry.ValidFor(admission.Work, now) != nil {
		t.Fatalf("journal entry rejected: %#v", entry)
	}
	entry.Evidence.StrategyRevision++
	if entry.ValidFor(admission.Work, now) == nil {
		t.Fatal("journal entry accepted a different strategy revision")
	}
}
