package sandbox

import (
	"context"
	"encoding/json"
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
	SessionID         SessionID
	Strategy          string
	Instrument        string
	Account           StrategySessionAccount
	ConfigurationID   string
	ConfigurationHash string
	StrategySetHash   string
	SessionRevision   uint64
	StrategyRevision  uint64
	ArmID             string
	ArmRevision       uint64
	StartedAt         time.Time
	ArmExpiresAt      time.Time
}

// StrategySessionAdmission is the complete decision-time proof for one
// automatic sandbox strategy account. It intentionally contains no candidate,
// allocation, risk result, or order: those are separate mandatory stages.
type StrategySessionAdmission struct {
	Work         StrategySessionWork
	Arm          Arm
	Eligibility  EligibilitySnapshot
	Safety       EntrySafetySnapshot
	StartupCycle uint64
	ApprovedAt   time.Time
}

// Valid proves the admission still binds one exact, live strategy work
// snapshot to a healthy entry decision at its recorded instant.
func (admission StrategySessionAdmission) Valid() error {
	if admission.ApprovedAt.IsZero() || admission.ApprovedAt.Location() != time.UTC ||
		admission.StartupCycle == 0 || admission.Work.ValidAt(admission.ApprovedAt) != nil ||
		admission.Arm.Validate() != nil || !admission.Arm.Active(admission.ApprovedAt) ||
		admission.Arm.ID != admission.Work.ArmID ||
		admission.Arm.SessionID != admission.Work.SessionID ||
		admission.Arm.Revision != admission.Work.ArmRevision ||
		!armContainsAccount(admission.Arm, admission.Work.Account.ID) ||
		!admission.Eligibility.Eligible ||
		admission.Eligibility.Exchange != string(admission.Work.Account.Exchange) ||
		admission.Eligibility.Instrument != admission.Work.Instrument ||
		admission.Eligibility.ObservedAt.IsZero() ||
		admission.Eligibility.ObservedAt.Location() != time.UTC ||
		admission.Eligibility.ObservedAt.After(admission.ApprovedAt) ||
		admission.ApprovedAt.Sub(admission.Eligibility.ObservedAt) > 250*time.Millisecond ||
		admission.Safety.AccountID != admission.Work.Account.ID ||
		admission.Safety.AccountEpoch != admission.Work.Account.Epoch ||
		admission.Safety.Exchange != admission.Work.Account.Exchange ||
		!admission.Safety.ObservedAt.Equal(admission.ApprovedAt) ||
		admission.Safety.CanSubmitEntry() != nil {
		return contractError("strategy_session_admission_invalid")
	}
	return nil
}

func armContainsAccount(arm Arm, account AccountID) bool {
	for _, item := range arm.AccountIDs {
		if item == account {
			return true
		}
	}
	return false
}

// ValidAt proves that the snapshot still represents a running, armed strategy
// session for one account. It intentionally proves neither current market
// freshness nor current entry eligibility; those are decision-time checks.
func (work StrategySessionWork) ValidAt(now time.Time) error {
	if work.SessionID == "" || work.ConfigurationID == "" ||
		!hash256(work.ConfigurationHash) ||
		(work.Instrument != "BTCUSDT" && work.Instrument != "ETHUSDT") ||
		!hash256(work.StrategySetHash) || work.SessionRevision == 0 ||
		work.StrategyRevision == 0 || work.ArmID == "" || work.ArmRevision == 0 ||
		work.StartedAt.IsZero() ||
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

// StrategySessionAdmissionSource rechecks a scheduled strategy snapshot at
// decision time. It is not a planner or an order-submission authority.
type StrategySessionAdmissionSource interface {
	StrategySessionAdmission(context.Context, StrategySessionWork, time.Time, [4]bool) (StrategySessionAdmission, error)
}

// StrategySessionConfiguration is the immutable, non-secret configuration
// payload selected during session preparation. Runtime code must validate and
// decode it before constructing a strategy; callers cannot substitute a newer
// active configuration under an existing arm.
type StrategySessionConfiguration struct {
	ID      string
	Hash    string
	Payload []byte
}

// ValidFor proves the configuration record still matches one scheduled work
// item. JSON validity is checked here; product-schema validation belongs to
// the strategy runtime that decodes the exact payload.
func (configuration StrategySessionConfiguration) ValidFor(work StrategySessionWork) bool {
	return work.ConfigurationID != "" && work.ConfigurationHash != "" &&
		configuration.ID == work.ConfigurationID &&
		configuration.Hash == work.ConfigurationHash && hash256(configuration.Hash) &&
		len(configuration.Payload) > 0 && json.Valid(configuration.Payload)
}

// StrategySessionConfigurationSource loads the immutable configuration bound
// to one still-running, armed session work item.
type StrategySessionConfigurationSource interface {
	StrategySessionConfiguration(context.Context, StrategySessionWork, time.Time) (StrategySessionConfiguration, error)
}
