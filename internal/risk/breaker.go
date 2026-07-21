package risk

import "time"

// BreakerKind identifies one explicit A9 circuit breaker.
type BreakerKind string

// Supported circuit breakers.
const (
	BreakerGap            BreakerKind = "gap_or_stale_data"
	BreakerReconciliation BreakerKind = "reconciliation_mismatch"
	BreakerUnknownOrder   BreakerKind = "unknown_order"
	BreakerLoss           BreakerKind = "loss_or_drawdown"
	BreakerSlippage       BreakerKind = "excessive_slippage"
	BreakerPersistence    BreakerKind = "persistence_failure"
	BreakerDisk           BreakerKind = "disk_failure"
	BreakerClockDrift     BreakerKind = "clock_drift"
	BreakerAPI            BreakerKind = "api_failure"
	BreakerQueueLag       BreakerKind = "queue_lag"
	BreakerLeaseLoss      BreakerKind = "lease_loss"
)

// TripBreaker immediately escalates, audits, and alerts one explicit breaker.
func (engine *Engine) TripBreaker(kind BreakerKind, at time.Time) error {
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	state, action, reason, ok := breakerDisposition(kind)
	if !ok || at.IsZero() || at.Location() != time.UTC {
		return riskError("circuit_breaker_invalid")
	}
	if stateRank(state) > stateRank(engine.state) {
		if err := engine.transitionLocked(state, reason, "system", at); err != nil {
			return err
		}
	}
	if engine.alerts.Emit(reason, action, state) != nil {
		return riskError("risk_alert_failed")
	}
	return nil
}

func breakerDisposition(kind BreakerKind) (State, Action, string, bool) {
	switch kind {
	case BreakerPersistence, BreakerDisk, BreakerLeaseLoss, BreakerLoss:
		return StateLocked, ActionLockEngine, string(kind), true
	case BreakerReconciliation, BreakerUnknownOrder:
		return StateLocked, ActionQuarantine, string(kind), true
	case BreakerGap, BreakerAPI, BreakerClockDrift, BreakerQueueLag:
		return StatePaused, ActionPauseExchange, string(kind), true
	case BreakerSlippage:
		return StatePaused, ActionPauseInstrument, string(kind), true
	default:
		return "", "", "", false
	}
}
