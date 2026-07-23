package triangular

import (
	"axiom/internal/domain"
	"axiom/internal/marketdata"
	"axiom/internal/strategies/arbitrage"
)

// Cycle identifies one of the two exhaustive approved paths.
type Cycle string

// Approved B4 cycle orderings.
const (
	CycleUSDTBTCETHUSDT Cycle = "USDT-BTC-ETH-USDT"
	CycleUSDTETHBTCUSDT Cycle = "USDT-ETH-BTC-USDT"
)

// Error is one bounded strategy rejection.
type Error struct{ Code string }

// Error returns a stable reason without market payload content.
func (failure *Error) Error() string { return "triangular:" + failure.Code }

func strategyError(code string) error { return &Error{Code: code} }

// Market couples one immutable book to its reviewed filters and fee model.
type Market struct {
	Book  marketdata.BookView
	Rules arbitrage.InstrumentRules
}

// EvaluationInput is the complete immutable evidence and ownership snapshot.
type EvaluationInput struct {
	Exchange             string
	Markets              []Market
	DecisionOffsetNanos  uint64
	FirstDetectedOffset  uint64
	AvailableSettlement  domain.Balance
	StrategyBudget       domain.Balance
	GlobalReserveFloor   domain.Balance
	RecoveryAllowance    domain.Balance
	FeeBalances          map[domain.AssetSymbol]domain.Balance
	Configuration        Configuration
	ConfigurationHash    string
	InstrumentMetadataID string
}

// ClaimRequirement describes one resource the allocator must acquire as part
// of the all-or-nothing claim group before execution.
type ClaimRequirement struct {
	Kind     string
	Exchange string
	Resource string
	Quantity domain.Balance
}

// Candidate is one exact profitable cycle size with all costs and evidence.
type Candidate struct {
	ID                     string
	Cycle                  Cycle
	Exchange               string
	Start                  domain.Quantity
	Final                  domain.Quantity
	WorstCaseFinal         domain.Quantity
	ExpectedNet            domain.PnL
	WorstCaseNet           domain.PnL
	ExpectedEdge           domain.Percent
	WorstCaseEdge          domain.Percent
	LatencyDeterioration   domain.Money
	AdditionalSafetyMargin domain.Percent
	DetectedOffsetNanos    uint64
	DecisionOffsetNanos    uint64
	ExpiresOffsetNanos     uint64
	MaximumBookAgeNanos    uint64
	Legs                   []arbitrage.Result
	Claims                 []ClaimRequirement
	ConfigurationVersion   string
	ConfigurationHash      string
	ModelVersion           string
	InstrumentMetadataID   string
}
