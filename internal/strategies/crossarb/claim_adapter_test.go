package crossarb

import (
	"sync"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	runtimecore "axiom/internal/runtime"
)

func TestCandidateClaimsAreAtomicFencedRestartSafeAndExclusive(t *testing.T) {
	input := evaluationFixture(t, "BTC", false)
	candidates, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidates[0]
	set := portfolio.NewAtomicClaimSet()
	for _, requirement := range candidate.Claims {
		key := portfolio.ClaimKey{
			Kind: mustClaimKind(t, requirement.Kind), Owner: requirement.Owner,
			Exchange: requirement.Exchange, Resource: stringsLower(requirement.Resource),
		}
		if err = set.OpenResource(key, requirement.Quantity); err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	success := make(chan portfolio.ClaimGroup, 2)
	for index := range 2 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id, _ := domain.NewReservationID("b5-claim-" + string(rune('a'+index)))
			group, claimErr := ClaimCandidate(set, candidate, id, runtimecore.FencingToken(index+1), 200)
			if claimErr == nil {
				success <- group
			}
		}(index)
	}
	wait.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatalf("successful claims = %d; want 1", len(success))
	}
	state := set.State()
	restored, err := portfolio.RestoreAtomicClaimSet(state)
	if err != nil || portfolio.CanonicalClaimSetHash(restored.State()) != portfolio.CanonicalClaimSetHash(state) {
		t.Fatalf("restart = %v", err)
	}
	state.Groups[0].Items[0].Remaining = balance("999")
	if _, err = portfolio.RestoreAtomicClaimSet(state); err == nil {
		t.Fatal("tampered claim checkpoint accepted")
	}
}

func mustClaimKind(t *testing.T, value string) portfolio.ClaimKind {
	t.Helper()
	result, ok := claimKind(value)
	if !ok {
		t.Fatalf("claim kind %q", value)
	}
	return result
}

func stringsLower(value string) string {
	result := []byte(value)
	for index, character := range result {
		if character >= 'A' && character <= 'Z' {
			result[index] = character + ('a' - 'A')
		}
	}
	return string(result)
}
