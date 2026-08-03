package sandbox

import (
	"testing"
	"time"
)

func TestAccountResetRequiresExplicitNonPnLAdjustments(t *testing.T) {
	at := time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC)
	incident := AccountResetIncident{
		ID: "reset-1", AccountID: "binance-testnet-a", PriorEpoch: 1,
		EvidenceHash: hashText("reset"), DetectedAt: at,
		Adjustments: []ExternalAdjustment{{
			ID: "adjustment-1", Asset: "USDT", Quantity: "-10",
			AdjustmentHash: hashText("adjustment"),
		}},
	}
	if err := incident.Validate(); err != nil {
		t.Fatalf("valid reset rejected: %v", err)
	}
	incident.Adjustments[0].Quantity = "0"
	if err := incident.Validate(); err == nil {
		t.Fatal("zero reset adjustment accepted")
	}
	incident.Adjustments = nil
	if err := incident.Validate(); err == nil {
		t.Fatal("reset without explicit adjustment accepted")
	}
}
