package binance

import (
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestRecorderRecoveryReserveAdmitsThreeDeepSnapshotsAndClocks(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, err := NewRecorderPublicClientWithMonotonic(
		publicEndpointSet, clock, func() time.Duration { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	if err = client.acquireBudget(exchangecontracts.OperationMetadata, 432,
		exchangecontracts.BudgetPublic); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	failures := make(chan error, 3)
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			failures <- client.acquireBudget(exchangecontracts.OperationSnapshot, 251,
				exchangecontracts.BudgetRecovery)
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		if failure != nil {
			t.Fatalf("three-collector recovery denied: %v", failure)
		}
	}
	if err = client.acquireBudget(exchangecontracts.OperationMetadata, 1,
		exchangecontracts.BudgetPublic); exchangecontracts.KindOf(err) != exchangecontracts.ErrorRateLimit {
		t.Fatalf("unrelated public call consumed recovery reserve: %v", err)
	}
	if err = client.acquireBudget(exchangecontracts.OperationSnapshot, 16,
		exchangecontracts.BudgetRecovery); exchangecontracts.KindOf(err) != exchangecontracts.ErrorRateLimit {
		t.Fatalf("recovery overspend admitted: %v", err)
	}
}

func TestRecorderDefaultsRetainFiveThousandLevelSnapshotReserve(t *testing.T) {
	config := DefaultCollectorConfig(approvedBTC(t))
	if config.SnapshotDepth != 5000 || config.BookDepth != 1000 || snapshotWeight(config.SnapshotDepth) != 250 {
		t.Fatalf("collector depth/budget changed: %#v weight=%d", config, snapshotWeight(config.SnapshotDepth))
	}
}
