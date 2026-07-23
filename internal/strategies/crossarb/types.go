package crossarb

import (
	"time"

	"axiom/internal/domain"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
)

// Direction is one exhaustive approved two-venue ordering.
type Direction string

// B5 evaluates both directions for every approved instrument.
const (
	BuyBinanceSellBybit Direction = "buy_binance_sell_bybit"
	BuyBybitSellBinance Direction = "buy_bybit_sell_binance"
)

// Error is one bounded fail-closed strategy rejection.
type Error struct{ Code string }

// Error returns the stable strategy-scoped rejection code.
func (failure *Error) Error() string { return "crossarb:" + failure.Code }

func strategyError(code string) error { return &Error{Code: code} }

// Market couples an immutable executable book to reviewed filters and fees.
type Market struct {
	Book  marketdata.BookView
	Rules arbitrage.InstrumentRules
}

// VenueInventory is one immutable strategy-owned venue snapshot. Total fields
// cover the same asset across all eligible venues and therefore form shares.
type VenueInventory struct {
	Owner             string
	Exchange          string
	BaseAsset         domain.AssetSymbol
	OwnedBase         domain.Balance
	TotalEligibleBase domain.Balance
	OwnedUSDT         domain.Balance
	TotalEligibleUSDT domain.Balance
	Revision          uint64
}

// RestorationEconomics charges the full closed inventory cycle separately.
type RestorationEconomics struct {
	ModelVersion                   string
	LatencyModelVersion            string
	RecoveryModelVersion           string
	InventoryShadowPriceVersion    string
	ConcentrationModelVersion      string
	LatencyDeterioration           domain.Money
	RecoveryAllowance              domain.Money
	MarginalInventoryReplacement   domain.Money
	NaturalReversalCost            domain.Money
	AdvisoryRebalancingCost        domain.Money
	ExchangeConcentrationPenalty   domain.Money
	USDTVenueConcentrationPenalty  domain.Money
	MaximumOneLegLoss              domain.Money
	EstimatedRestorationDelayNanos uint64
	NaturalReverseAvailable        bool
	AdvisoryRebalancingRequired    bool
}

// EvaluationInput is the complete immutable B5 decision evidence.
type EvaluationInput struct {
	CoherentView              runtimecore.CoherentView
	Markets                   []Market
	Inventory                 []VenueInventory
	QuoteBudget               domain.Balance
	FeeBalances               map[string]domain.Balance
	DecisionOffsetNanos       uint64
	Configuration             Configuration
	ConfigurationHash         string
	InstrumentMetadataSetHash string
	Restoration               RestorationEconomics
}

// ClaimRequirement is one exact member of the atomic two-leg claim group.
type ClaimRequirement struct {
	Kind     string
	Owner    string
	Exchange string
	Resource string
	Quantity domain.Balance
}

// InventoryControl is the band decision for one direction.
type InventoryControl struct {
	SellVenueShare domain.Percent
	BuyVenueShare  domain.Percent
	State          BandState
	NaturalReverse bool
}

// ClosedCycleEconomics keeps every required attribution independent.
type ClosedCycleEconomics struct {
	GrossSpread                   domain.PnL
	BuyFee                        domain.Money
	SellFee                       domain.Money
	SpreadDepthCost               domain.Money
	LatencyDeterioration          domain.Money
	RecoveryAllowance             domain.Money
	ExpectedExecutionPnL          domain.PnL
	MaximumOneLegLoss             domain.Money
	MarginalInventoryReplacement  domain.Money
	NaturalReversalCost           domain.Money
	AdvisoryRebalancingCost       domain.Money
	ExchangeConcentrationPenalty  domain.Money
	USDTVenueConcentrationPenalty domain.Money
	ExpectedClosedCycleProfit     domain.PnL
	WorstClosedCycleProfit        domain.PnL
	RestorationDelayNanos         uint64
}

// RebalancingNeed is advisory evidence only. It intentionally has no route,
// executor, authenticated API, or dispatch operation.
type RebalancingNeed struct {
	Required            bool
	Asset               domain.AssetSymbol
	DepletedExchange    string
	OverweightExchange  string
	PreferredAction     string
	EstimatedCost       domain.Money
	EstimatedDelayNanos uint64
}

// Candidate is one executable-depth, inventory-backed, closed-cycle-positive
// B5 simulation candidate.
type Candidate struct {
	ID                        string
	Direction                 Direction
	Instrument                domain.Instrument
	BuyExchange               string
	SellExchange              string
	Buy                       arbitrage.Result
	Sell                      arbitrage.Result
	CoherentViewID            string
	CoherentVersionVectorHash string
	ViewMembers               []runtimecore.ViewReference
	Inventory                 InventoryControl
	Economics                 ClosedCycleEconomics
	Rebalancing               RebalancingNeed
	Claims                    []ClaimRequirement
	DetectedOffsetNanos       uint64
	DecisionOffsetNanos       uint64
	ExpiresOffsetNanos        uint64
	ConfigurationVersion      string
	ConfigurationHash         string
	ModelVersion              string
	InstrumentMetadataSetHash string
	CreatedAt                 time.Time
}
