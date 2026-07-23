package triangular

import (
	"sync"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	runtimecore "axiom/internal/runtime"
)

func TestClaimCandidateAcquiresEveryResourceAtomicallyAndPreventsReuse(t *testing.T) {
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	set := NewCandidateClaimFixture(t, candidate, "portfolio-a")
	firstID, _ := domain.NewReservationID("b4-claim-first")
	group, err := ClaimCandidate(set, candidate, "portfolio-a", firstID, runtimecore.FencingToken(7), 1_010)
	if err != nil || group.State != portfolio.ClaimActive || len(group.Items) != len(candidate.Claims) {
		t.Fatalf("complete claim was not acquired: %#v %v", group, err)
	}
	secondID, _ := domain.NewReservationID("b4-claim-second")
	if _, err = ClaimCandidate(set, candidate, "portfolio-a", secondID, runtimecore.FencingToken(7), 1_010); err == nil {
		t.Fatal("displayed liquidity or funds were reused")
	}
	for _, item := range group.Items {
		resource, exists := set.Resource(item.Key)
		if !exists || resource.Held.Compare(item.Quantity) != 0 {
			t.Fatalf("resource projection mismatch: %#v", resource)
		}
	}
}

func TestClaimCandidateFailureLeavesNoPartialHoldAndContentionHasOneWinner(t *testing.T) {
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	set := NewCandidateClaimFixture(t, candidate, "portfolio-a")
	missing := candidate.Claims[len(candidate.Claims)-1]
	missingKey := candidateClaimKey(missing, "portfolio-a")
	reduced := portfolio.NewAtomicClaimSet()
	for _, requirement := range candidate.Claims {
		key := candidateClaimKey(requirement, "portfolio-a")
		if key == missingKey {
			continue
		}
		if err := reduced.OpenResource(key, requirement.Quantity); err != nil {
			t.Fatal(err)
		}
	}
	id, _ := domain.NewReservationID("b4-partial-rejected")
	if _, err := ClaimCandidate(reduced, candidate, "portfolio-a", id, 1, 1_010); err == nil {
		t.Fatal("missing final resource did not reject the group")
	}
	for _, resource := range reduced.State().Resources {
		if resource.Held.String() != "0" {
			t.Fatalf("partial hold leaked: %#v", resource)
		}
	}

	var wait sync.WaitGroup
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			claimID, _ := domain.NewReservationID("b4-contention-" + uintString(uint64(index+1)))
			_, claimErr := ClaimCandidate(set, candidate, "portfolio-a", claimID, 9, 1_010)
			results <- claimErr
		}(index)
	}
	wait.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected one contention winner, got %d", successes)
	}
}

func TestClaimCandidateRejectsExpiredCandidateBeforeOwnership(t *testing.T) {
	candidate := candidateFor(t, profitableInput(t, false), CycleUSDTBTCETHUSDT, "10")
	set := NewCandidateClaimFixture(t, candidate, "portfolio-a")
	id, _ := domain.NewReservationID("b4-expired-claim")
	if _, err := ClaimCandidate(set, candidate, "portfolio-a", id, 1, candidate.ExpiresOffsetNanos+1); err == nil {
		t.Fatal("expired candidate was claimed")
	}
	for _, resource := range set.State().Resources {
		if resource.Held.String() != "0" {
			t.Fatalf("expired claim leaked a hold: %#v", resource)
		}
	}
}

// NewCandidateClaimFixture opens exactly the candidate's required resources.
func NewCandidateClaimFixture(t *testing.T, candidate Candidate, owner string) *portfolio.AtomicClaimSet {
	t.Helper()
	set := portfolio.NewAtomicClaimSet()
	for _, requirement := range candidate.Claims {
		if err := set.OpenResource(candidateClaimKey(requirement, owner), requirement.Quantity); err != nil {
			t.Fatal(err)
		}
	}
	return set
}

func candidateClaimKey(requirement ClaimRequirement, owner string) portfolio.ClaimKey {
	kind, _ := claimKind(requirement.Kind)
	return portfolio.ClaimKey{
		Kind: kind, Owner: owner, Exchange: requirement.Exchange, Resource: requirement.Resource,
	}
}
