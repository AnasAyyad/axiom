package triangular

import (
	"context"
	"encoding/json"
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
)

func TestSagaAdapterEvaluatesAllCandidatesThenClaimsOneDeterministicWinner(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	strategy := NewSagaStrategyAdapter()
	candidateSet, err := strategy.EvaluateSaga(context.Background(), replay.Event{
		Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded sagaCandidateSet
	if err = json.Unmarshal(candidateSet.Payload, &decoded); err != nil || len(decoded.Candidates) < 2 {
		t.Fatalf("candidate set=%#v error=%v", decoded, err)
	}
	winner := rankedCandidates(decoded.Candidates)[0]
	claims := NewCandidateClaimFixture(t, winner, "portfolio-a")
	allocator, err := NewAtomicSagaAllocator(claims, "portfolio-a", 7)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.AllocateSaga(context.Background(), candidateSet)
	if err != nil {
		t.Fatal(err)
	}
	var allocated sagaAllocation
	if err = json.Unmarshal(allocation.Payload, &allocated); err != nil || allocated.Candidate.ID != winner.ID ||
		allocated.Claims.State != portfolio.ClaimActive {
		t.Fatalf("allocation=%#v error=%v", allocated, err)
	}
	if err = allocator.CloseSagaAllocation(context.Background(), allocation, backtest.AllocationReleased); err != nil {
		t.Fatal(err)
	}
	stored, ok := claims.Group(allocated.Claims.ID)
	if !ok || stored.State != portfolio.ClaimReleased {
		t.Fatalf("claim state=%#v exists=%t", stored, ok)
	}
}

func TestSagaAllocatorFailsClosedWhenNoCandidateHasACompleteClaimSet(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	payload, _ := json.Marshal(input)
	candidateSet, err := NewSagaStrategyAdapter().EvaluateSaga(context.Background(), replay.Event{
		Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	claims := portfolio.NewAtomicClaimSet()
	allocator, err := NewAtomicSagaAllocator(claims, "portfolio-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allocator.AllocateSaga(context.Background(), candidateSet); err == nil {
		t.Fatal("allocator accepted a candidate without every shared resource")
	}
	if len(claims.State().Groups) != 0 {
		t.Fatal("failed allocation created a partial claim")
	}
}
