package sandbox

import (
	"context"
	"time"
)

// StrategySessionWork is the non-secret, per-account work snapshot an
// automatic sandbox strategy worker may consume. It is deliberately not an
// order intent and cannot authorize submission on its own: allocation, risk,
// arm, and dispatcher admission must all be rechecked immediately before any
// new entry is persisted.
//
// A cross-exchange session produces one snapshot per exact account epoch. A
// paired strategy worker must load and verify both snapshots before it can
// evaluate a two-venue candidate.
type StrategySessionWork struct {
	SessionID        SessionID
	Strategy         string
	Instrument       string
	Account          StrategySessionAccount
	ConfigurationID  string
	StrategySetHash  string
	SessionRevision  uint64
	StrategyRevision uint64
	StartedAt        time.Time
	ArmExpiresAt     time.Time
}

// ValidAt proves that the snapshot still represents a running, armed strategy
// session for one account. It intentionally proves neither current market
// freshness nor current entry eligibility; those are decision-time checks.
func (work StrategySessionWork) ValidAt(now time.Time) error {
	if work.SessionID == "" || work.ConfigurationID == "" ||
		(work.Instrument != "BTCUSDT" && work.Instrument != "ETHUSDT") ||
		!hash256(work.StrategySetHash) || work.SessionRevision == 0 ||
		work.StrategyRevision == 0 || work.StartedAt.IsZero() ||
		work.StartedAt.Location() != time.UTC || work.ArmExpiresAt.IsZero() ||
		work.ArmExpiresAt.Location() != time.UTC || now.IsZero() ||
		now.Location() != time.UTC || !work.StartedAt.Before(work.ArmExpiresAt) ||
		!now.Before(work.ArmExpiresAt) {
		return contractError("strategy_session_work_invalid")
	}
	if work.Account.ID == "" || work.Account.Epoch == 0 ||
		(work.Account.Exchange != ExchangeBinance && work.Account.Exchange != ExchangeBybit) {
		return contractError("strategy_session_work_invalid")
	}
	switch work.Strategy {
	case StrategyTrend, StrategyMeanReversion, StrategyTriangular:
		return nil
	case StrategyCrossExchangeArbitrage:
		return nil
	default:
		return contractError("strategy_session_work_invalid")
	}
}

// StrategySessionWorkSource returns only currently running, actively armed
// work for the caller's exact fenced account epoch. It is a scheduling source,
// not an order-admission interface.
type StrategySessionWorkSource interface {
	ActiveStrategySessionWork(context.Context, AccountID, uint64, string, uint64, time.Time, int) ([]StrategySessionWork, error)
}
