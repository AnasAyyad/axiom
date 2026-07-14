package exchangecontracts

import (
	"sync"
	"testing"
	"time"
)

func TestRateBudgetPreservesRecoveryCapacity(t *testing.T) {
	t.Parallel()
	budget, err := NewRateBudget(BudgetConfig{
		Capacity: 10, RecoveryReserve: 3, RefillAmount: 2, RefillInterval: time.Second,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	public, err := budget.TryAcquire(0, BudgetPublic, 7)
	if err != nil || !public.Granted || public.Remaining != 3 {
		t.Fatalf("public grant mismatch: %+v %v", public, err)
	}
	blocked, err := budget.TryAcquire(0, BudgetPublic, 1)
	if err != nil || blocked.Granted || blocked.RetryAfter != time.Second || blocked.Remaining != 3 {
		t.Fatalf("reserve was not protected: %+v %v", blocked, err)
	}
	recovery, err := budget.TryAcquire(0, BudgetRecovery, 3)
	if err != nil || !recovery.Granted || recovery.Remaining != 0 {
		t.Fatalf("recovery could not use reserve: %+v %v", recovery, err)
	}
	refilled, err := budget.TryAcquire(2*time.Second, BudgetPublic, 1)
	if err != nil || !refilled.Granted || refilled.Remaining != 3 {
		t.Fatalf("deterministic refill mismatch: %+v %v", refilled, err)
	}
	if _, err := budget.TryAcquire(time.Second, BudgetPublic, 1); KindOf(err) != ErrorTimestamp {
		t.Fatalf("time regression did not fail closed: %v", err)
	}
	if _, err := budget.TryAcquire(2*time.Second, BudgetRecovery, 11); KindOf(err) != ErrorFilter {
		t.Fatalf("unserviceable weight did not fail closed: %v", err)
	}
}

func TestRateBudgetIsSharedUnderContention(t *testing.T) {
	t.Parallel()
	budget, err := NewRateBudget(BudgetConfig{
		Capacity: 50, RecoveryReserve: 10, RefillAmount: 1, RefillInterval: time.Hour,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	var mutex sync.Mutex
	grants := 0
	for count := 0; count < 100; count++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			decision, acquireErr := budget.TryAcquire(0, BudgetPublic, 1)
			if acquireErr != nil {
				t.Errorf("acquire: %v", acquireErr)
				return
			}
			if decision.Granted {
				mutex.Lock()
				grants++
				mutex.Unlock()
			}
		}()
	}
	wait.Wait()
	if grants != 40 {
		t.Fatalf("shared budget granted %d, want 40", grants)
	}
}
