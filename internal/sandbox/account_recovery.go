package sandbox

import (
	"context"
	"encoding/hex"
	"time"

	"axiom/internal/domain"
)

// ExternalAdjustment is an exchange-authoritative balance discontinuity caused
// by a sandbox reset. It is never profit-and-loss.
type ExternalAdjustment struct {
	ID             string
	Asset          domain.AssetSymbol
	Quantity       string
	AdjustmentHash string
}

// AccountResetIncident creates a new account epoch and explicit external
// adjustments without rewriting historical orders, fills, or journal P&L.
type AccountResetIncident struct {
	ID           string
	AccountID    AccountID
	PriorEpoch   uint64
	EvidenceHash string
	DetectedAt   time.Time
	Adjustments  []ExternalAdjustment
}

// AccountRecoveryRepository persists authoritative snapshots and coherent
// testnet/demo account resets.
type AccountRecoveryRepository interface {
	RecordAccountSnapshot(context.Context, string, AccountSnapshot) error
	RecordAccountReset(context.Context, AccountResetIncident) error
}

// Validate checks the complete epoch-reset incident.
func (incident AccountResetIncident) Validate() error {
	if incident.ID == "" || incident.AccountID == "" || incident.PriorEpoch == 0 ||
		!recoveryHash(incident.EvidenceHash) || incident.DetectedAt.IsZero() ||
		incident.DetectedAt.Location() != time.UTC || len(incident.Adjustments) == 0 {
		return contractError("account_reset_invalid")
	}
	seenIDs := make(map[string]struct{}, len(incident.Adjustments))
	seenAssets := make(map[domain.AssetSymbol]struct{}, len(incident.Adjustments))
	zero, _ := domain.ParsePnL("0")
	for _, adjustment := range incident.Adjustments {
		quantity, err := domain.ParsePnL(adjustment.Quantity)
		if adjustment.ID == "" || err != nil || quantity.Compare(zero) == 0 ||
			!recoveryHash(adjustment.AdjustmentHash) {
			return contractError("account_reset_adjustment_invalid")
		}
		if _, err = domain.ParseAssetSymbol(string(adjustment.Asset)); err != nil {
			return contractError("account_reset_adjustment_invalid")
		}
		if _, duplicate := seenIDs[adjustment.ID]; duplicate {
			return contractError("account_reset_adjustment_invalid")
		}
		if _, duplicate := seenAssets[adjustment.Asset]; duplicate {
			return contractError("account_reset_adjustment_invalid")
		}
		seenIDs[adjustment.ID] = struct{}{}
		seenAssets[adjustment.Asset] = struct{}{}
	}
	return nil
}

func recoveryHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
