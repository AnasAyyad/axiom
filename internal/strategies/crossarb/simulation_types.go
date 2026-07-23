package crossarb

import (
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/strategies/arbitrage"
)

// SimulationOutcome exhaustively classifies concurrent two-leg results.
type SimulationOutcome string

// Simulation outcomes enumerate every reviewed concurrent two-leg result.
const (
	// OutcomeBothFilled records two complete virtual fills.
	OutcomeBothFilled SimulationOutcome = "both_filled"
	// OutcomeBuyOnly records only acquired base inventory.
	OutcomeBuyOnly SimulationOutcome = "buy_only"
	// OutcomeSellOnly records only depleted base inventory.
	OutcomeSellOnly SimulationOutcome = "sell_only"
	// OutcomePartialBuy records a partial buy and no sell.
	OutcomePartialBuy SimulationOutcome = "partial_buy"
	// OutcomePartialSell records no buy and a partial sell.
	OutcomePartialSell SimulationOutcome = "partial_sell"
	// OutcomePartialBoth records partial fills on both venues.
	OutcomePartialBoth SimulationOutcome = "partial_both"
	// OutcomeBothMissed records two terminal virtual misses.
	OutcomeBothMissed SimulationOutcome = "both_missed"
	// OutcomeNegativeBeforeArrival records invalidated arrival economics.
	OutcomeNegativeBeforeArrival SimulationOutcome = "negative_before_arrival"
	// OutcomeDelayedUnknown records unresolved ownership after verification.
	OutcomeDelayedUnknown SimulationOutcome = "delayed_unknown"
)

// RecoveryPolicy is the explicit central-risk disposition for a single
// verification-complete bounded retry. It cannot authorize production I/O.
type RecoveryPolicy struct {
	RiskAllowsRetry bool
	MaximumRetries  uint32
}

// LegSimulation preserves every arrival/verification/retry fact independently.
type LegSimulation struct {
	Index              int
	Exchange           string
	ArrivalOffsetNanos uint64
	InitialState       execution.OrderState
	VerifiedState      execution.OrderState
	FinalState         execution.OrderState
	Input              domain.Quantity
	Result             *arbitrage.Result
	VerificationCount  uint32
	RetryCount         uint32
}

// VenueExposure is an exact inventory location change requiring restoration.
type VenueExposure struct {
	Exchange string
	Asset    domain.AssetSymbol
	Kind     string
	Quantity domain.Balance
}

// RecoveryResult records bounded retry/unwind/quarantine without treating
// delayed or unknown as failed before verification.
type RecoveryResult struct {
	VerificationCompleted bool
	RetryAttempted        bool
	RetrySucceeded        bool
	UnwindAttempted       bool
	UnwindSucceeded       bool
	Quarantined           bool
	Disposition           string
	Loss                  domain.Money
}

// SimulationResult is the deterministic restart-comparable B5 saga evidence.
type SimulationResult struct {
	CandidateID    string
	Outcome        SimulationOutcome
	Legs           []LegSimulation
	Exposures      []VenueExposure
	Recovery       RecoveryResult
	Saga           execution.Saga
	ActualBuy      *arbitrage.Result
	ActualSell     *arbitrage.Result
	ActualUSDTNet  domain.PnL
	LatencyVersion string
	CanonicalHash  string
}
