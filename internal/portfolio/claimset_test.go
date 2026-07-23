package portfolio

import (
	"sync"
	"testing"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

func TestAtomicClaimSetIsAllOrNothingUnderContention(t *testing.T) {
	set := NewAtomicClaimSet()
	balance := claimKey(ClaimBalance, "usdt")
	liquidity := claimKey(ClaimLiquidity, "btcusdt-ask-v1")
	openClaimResource(t, set, balance, "50")
	openClaimResource(t, set, liquidity, "1")

	var wait sync.WaitGroup
	successes := make(chan ClaimGroup, 2)
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id, _ := domain.NewReservationID("atomic-" + string(rune('a'+index)))
			group, err := set.ClaimAtomically(id, "triangular", []ClaimItem{
				{Key: balance, Quantity: claimBalance("40")},
				{Key: liquidity, Quantity: claimBalance("0.75")},
			}, runtimecore.FencingToken(index+1))
			if err == nil {
				successes <- group
			}
		}(index)
	}
	wait.Wait()
	close(successes)
	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Fatalf("expected exactly one complete claim, got %d", count)
	}
	balanceState, _ := set.Resource(balance)
	liquidityState, _ := set.Resource(liquidity)
	if balanceState.Available.String() != "10" || balanceState.Held.String() != "40" ||
		liquidityState.Available.String() != "0.25" || liquidityState.Held.String() != "0.75" {
		t.Fatalf("partial or duplicated claim observed: %#v %#v", balanceState, liquidityState)
	}
}

func TestAtomicClaimSetSettlementQuarantineAndRestart(t *testing.T) {
	set := NewAtomicClaimSet()
	balance := claimKey(ClaimBalance, "usdt")
	fee := claimKey(ClaimFeeBuffer, "bnb")
	liquidity := claimKey(ClaimLiquidity, "triangle-v7")
	openClaimResource(t, set, balance, "100")
	openClaimResource(t, set, fee, "2")
	openClaimResource(t, set, liquidity, "4")
	id, _ := domain.NewReservationID("restart-safe")
	group, err := set.ClaimAtomically(id, "triangular", []ClaimItem{
		{Key: balance, Quantity: claimBalance("25")},
		{Key: fee, Quantity: claimBalance("0.1")},
		{Key: liquidity, Quantity: claimBalance("1")},
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	group, err = set.Settle(id, group.Revision, group.Fence, []ClaimItem{
		{Key: balance, Quantity: claimBalance("10")},
		{Key: liquidity, Quantity: claimBalance("0.4")},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	state := set.State()
	restored, err := RestoreAtomicClaimSet(state)
	if err != nil {
		t.Fatal(err)
	}
	if CanonicalClaimSetHash(restored.State()) != CanonicalClaimSetHash(state) {
		t.Fatal("restart changed canonical claim state")
	}
	if err = restored.Close(id, group.Revision, group.Fence, ClaimQuarantined); err != nil {
		t.Fatal(err)
	}
	quarantined, _ := restored.Group(id)
	if quarantined.State != ClaimQuarantined {
		t.Fatalf("unexpected state %q", quarantined.State)
	}
	feeState, _ := restored.Resource(fee)
	if feeState.Available.String() != "1.9" || feeState.Held.String() != "0.1" {
		t.Fatalf("quarantine released fee ownership: %#v", feeState)
	}
}

func TestAtomicClaimSetRejectsTamperedCheckpoint(t *testing.T) {
	set := NewAtomicClaimSet()
	key := claimKey(ClaimRecovery, "usdt")
	openClaimResource(t, set, key, "10")
	id, _ := domain.NewReservationID("tamper")
	if _, err := set.ClaimAtomically(id, "crossarb", []ClaimItem{
		{Key: key, Quantity: claimBalance("3")},
	}, 4); err != nil {
		t.Fatal(err)
	}
	state := set.State()
	state.Resources[0].Held = claimBalance("2")
	if _, err := RestoreAtomicClaimSet(state); err == nil {
		t.Fatal("expected held projection tamper rejection")
	}
}

func claimKey(kind ClaimKind, resource string) ClaimKey {
	return ClaimKey{Kind: kind, Owner: "portfolio-a", Exchange: "binance", Resource: resource}
}

func claimBalance(value string) domain.Balance {
	result, err := domain.ParseBalance(value)
	if err != nil {
		panic(err)
	}
	return result
}

func openClaimResource(t *testing.T, set *AtomicClaimSet, key ClaimKey, quantity string) {
	t.Helper()
	if err := set.OpenResource(key, claimBalance(quantity)); err != nil {
		t.Fatal(err)
	}
}
