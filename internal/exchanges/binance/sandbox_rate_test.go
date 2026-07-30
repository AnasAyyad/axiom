package binance

import (
	"errors"
	"testing"
	"time"
)

func TestBinanceRateBudgetPreservesCancelAndReconciliationCapacity(t *testing.T) {
	budget, err := newSandboxRateBudget(100, 20, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	if err = budget.acquire(now, 80, sandboxRequestEntry); err != nil {
		t.Fatal(err)
	}
	if err = budget.acquire(now, 1, sandboxRequestEntry); !errors.Is(err, ErrSandboxRateBudget) {
		t.Fatalf("entry error=%v want exhausted", err)
	}
	if err = budget.acquire(now, 5, sandboxRequestCancel); err != nil {
		t.Fatalf("cancel reserve unavailable: %v", err)
	}
	if err = budget.acquire(now, 15, sandboxRequestReconcile); err != nil {
		t.Fatalf("reconciliation reserve unavailable: %v", err)
	}
	if err = budget.acquire(now, 1, sandboxRequestCancel); !errors.Is(err, ErrSandboxRateBudget) {
		t.Fatalf("total capacity error=%v want exhausted", err)
	}
	if err = budget.acquire(
		now.Add(time.Minute),
		80,
		sandboxRequestEntry,
	); err != nil {
		t.Fatalf("window did not reset: %v", err)
	}
}
