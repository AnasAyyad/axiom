package binance

import (
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func TestBinanceTestnetResetRequiresHistoryWipeAndBalanceDiscontinuity(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	previous := resetSnapshot(t, now, "50")
	previous.OrdersHash = canonicalHash("prior-orders")
	previous.SnapshotHash = canonicalHash(previous)
	current := resetSnapshot(t, now.Add(time.Minute), "100")
	current.OrdersHash = canonicalSandboxOrdersHash(nil)
	current.FillsHash = canonicalSandboxFillsHash(nil)
	current.SnapshotHash = canonicalHash(current)

	incident, detected, err := DetectSandboxReset(previous, current)
	if err != nil || !detected || incident.PriorEpoch != 1 ||
		len(incident.Adjustments) != 1 ||
		incident.Adjustments[0].Asset != "USDT" ||
		incident.Adjustments[0].Quantity != "50" {
		t.Fatalf("incident=%#v detected=%t err=%v", incident, detected, err)
	}

	noWipe := current
	noWipe.OrdersHash = canonicalHash("still-present")
	noWipe.SnapshotHash = canonicalHash(noWipe)
	if _, detected, err = DetectSandboxReset(previous, noWipe); err != nil || detected {
		t.Fatalf("balance-only change detected=%t err=%v", detected, err)
	}

	noDiscontinuity := current
	noDiscontinuity.Balances = previous.Balances
	noDiscontinuity.SnapshotHash = canonicalHash(noDiscontinuity)
	if _, detected, err = DetectSandboxReset(previous, noDiscontinuity); err != nil || detected {
		t.Fatalf("history-only change detected=%t err=%v", detected, err)
	}
}

func resetSnapshot(
	t *testing.T,
	observedAt time.Time,
	usdt string,
) sandbox.AccountSnapshot {
	t.Helper()
	available, err := domain.ParseBalance(usdt)
	if err != nil {
		t.Fatal(err)
	}
	zero, _ := domain.ParseBalance("0")
	snapshot := sandbox.AccountSnapshot{
		AccountID: "binance-testnet-account",
		Epoch:     1,
		Balances: []sandbox.Balance{{
			Asset:     "USDT",
			Available: available,
			Reserved:  zero,
		}},
		OrdersHash: canonicalSandboxOrdersHash(nil),
		FillsHash:  canonicalSandboxFillsHash(nil),
		ObservedAt: observedAt,
	}
	snapshot.SnapshotHash = canonicalHash(snapshot)
	return snapshot
}
