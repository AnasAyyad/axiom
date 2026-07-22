package meanreversion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Stable B3 mean-reversion reason codes.
const (
	ReasonEntryAccepted        = "mean_reversion.entry.accepted"
	ReasonExitATRStop          = "mean_reversion.exit.atr_stop"
	ReasonExitProtectiveZScore = "mean_reversion.exit.protective_zscore"
	ReasonExitNormalZScore     = "mean_reversion.exit.normal_zscore"
	ReasonExitMaximumHolding   = "mean_reversion.exit.maximum_holding"
	ReasonHoldPosition         = "mean_reversion.hold.position"
	ReasonWarmUp               = "mean_reversion.reject.warm_up"
	ReasonZeroDeviation        = "mean_reversion.reject.zero_deviation"
	ReasonCandleFinality       = "mean_reversion.reject.candle_finality"
	ReasonCandleOrder          = "mean_reversion.reject.candle_order"
	ReasonCandleGap            = "mean_reversion.reject.candle_gap"
	ReasonCandleConflict       = "mean_reversion.reject.candle_conflict"
	ReasonTimeframeAlignment   = "mean_reversion.reject.timeframe_alignment"
	ReasonDuplicateDecision    = "mean_reversion.reject.duplicate_decision"
	ReasonStaleSignal          = "mean_reversion.reject.stale_signal"
	ReasonUnhealthyMarket      = "mean_reversion.reject.unhealthy_market"
	ReasonMarketQuality        = "mean_reversion.reject.market_quality"
	ReasonRiskPause            = "mean_reversion.reject.exchange_risk_pause"
	ReasonSpread               = "mean_reversion.reject.spread"
	ReasonADX                  = "mean_reversion.reject.adx"
	ReasonDangerousRegime      = "mean_reversion.reject.dangerous_regime"
	ReasonEntryThreshold       = "mean_reversion.reject.entry_threshold"
	ReasonExistingPosition     = "mean_reversion.reject.existing_position"
	ReasonCooldown             = "mean_reversion.reject.cooldown"
	ReasonInvalidSizing        = "mean_reversion.reject.invalid_sizing"
	ReasonMinimumFilter        = "mean_reversion.reject.minimum_filter"
	ReasonRiskClipped          = "mean_reversion.reject.risk_clipped"
	ReasonNoPostLatencyPrice   = "mean_reversion.reject.no_post_latency_price"
	ReasonInvalidConfiguration = "mean_reversion.reject.invalid_configuration"
	ReasonMissedOrder          = "mean_reversion.order.missed"
	ReasonExpiredOrder         = "mean_reversion.order.expired"
)

// Action is the desired position change returned by the pure evaluator.
type Action string

// Stable position-change actions returned by the pure evaluator.
const (
	ActionNone  Action = "none"
	ActionEntry Action = "entry"
	ActionExit  Action = "exit"
)

// InputEvidence identifies every immutable fact used by one evaluation.
type InputEvidence struct {
	PrimaryCandleViewID       string `json:"primary_candle_view_id"`
	PrimaryCandleViewRevision uint64 `json:"primary_candle_view_revision"`
	HigherCandleViewID        string `json:"higher_candle_view_id"`
	HigherCandleViewRevision  uint64 `json:"higher_candle_view_revision"`
	MarketViewID              string `json:"market_view_id"`
	MarketViewRevision        uint64 `json:"market_view_revision"`
	CoherentViewID            string `json:"coherent_view_id"`
	CoherentVersionVectorHash string `json:"coherent_version_vector_hash"`
	InstrumentMetadataID      string `json:"instrument_metadata_id"`
	AssetEligibilityVersion   uint64 `json:"asset_eligibility_version"`
	ConfigurationSnapshotID   string `json:"configuration_snapshot_id"`
	ConfigurationVersion      string `json:"configuration_version"`
	ConfigurationHash         string `json:"configuration_hash"`
	StrategyVersion           string `json:"strategy_version"`
	StrategyHash              string `json:"strategy_hash"`
	PortfolioRevision         uint64 `json:"portfolio_revision"`
	PositionRevision          uint64 `json:"position_revision"`
	RiskPolicyID              string `json:"risk_policy_id"`
	RiskPolicyVersion         uint64 `json:"risk_policy_version"`
	RiskPolicyHash            string `json:"risk_policy_hash"`
	FeeModelID                string `json:"fee_model_id"`
	LatencyModelID            string `json:"latency_model_id"`
	FillModelID               string `json:"fill_model_id"`
	SlippageModelID           string `json:"slippage_model_id"`
	GapModelID                string `json:"gap_model_id"`
	CorrelationModelID        string `json:"correlation_model_id"`
	CorrelationID             string `json:"correlation_id"`
	CausationID               string `json:"causation_id"`
}

// PositionState is the immutable pre-decision mean-reversion projection.
type PositionState struct {
	Open              bool            `json:"open"`
	Quantity          domain.Quantity `json:"quantity"`
	ActualEntryPrice  domain.Price    `json:"actual_entry_price"`
	InitialStop       domain.Price    `json:"initial_stop"`
	HeldCandles       uint64          `json:"held_candles"`
	CooldownRemaining uint64          `json:"cooldown_remaining"`
}

// SizingState contains exact facts supplied by portfolio and model owners.
type SizingState struct {
	Equity               domain.Money              `json:"equity"`
	AvailableCash        domain.Money              `json:"available_cash"`
	MinimumReserve       domain.Money              `json:"minimum_reserve"`
	NotionalLimits       []domain.Money            `json:"notional_limits"`
	FirstExecutablePrice domain.Price              `json:"first_executable_price"`
	FirstExecutableAt    time.Time                 `json:"first_executable_at"`
	GapAllowance         domain.Price              `json:"gap_allowance"`
	SlippageAllowance    domain.Price              `json:"slippage_allowance"`
	EntryFeeRate         domain.Rate               `json:"entry_fee_rate"`
	ExitFeeRate          domain.Rate               `json:"exit_fee_rate"`
	InstrumentMetadata   domain.InstrumentMetadata `json:"instrument_metadata"`
	CentralRiskEligible  bool                      `json:"central_risk_eligible"`
	LiquidityDomain      string                    `json:"liquidity_domain"`
	FencingToken         uint64                    `json:"fencing_token"`
}

// Input is one immutable dual-timeframe B3 evaluation request.
type Input struct {
	Ordinal               uint64                     `json:"ordinal"`
	LogicalTime           uint64                     `json:"logical_time"`
	Now                   time.Time                  `json:"now"`
	Instrument            domain.Instrument          `json:"instrument"`
	PrimaryCandles        []exchangecontracts.Candle `json:"primary_candles"`
	HigherCandles         []exchangecontracts.Candle `json:"higher_candles"`
	MarketHealthy         bool                       `json:"market_healthy"`
	MarketDataQualityPass bool                       `json:"market_data_quality_pass"`
	ExchangeRiskPaused    bool                       `json:"exchange_risk_paused"`
	Spread                domain.Percent             `json:"spread"`
	BookAge               time.Duration              `json:"book_age"`
	Position              PositionState              `json:"position"`
	Sizing                SizingState                `json:"sizing"`
	Evidence              InputEvidence              `json:"evidence"`
}

// Explanation is the complete stable B3 decision explanation.
type Explanation struct {
	ReasonCode         string            `json:"reason_code"`
	Evidence           InputEvidence     `json:"evidence"`
	PrimarySignalHash  string            `json:"primary_signal_hash"`
	HigherSignalHash   string            `json:"higher_signal_hash"`
	PrimarySignalClose time.Time         `json:"primary_signal_close"`
	HigherSignalClose  time.Time         `json:"higher_signal_close"`
	RollingMean        domain.Price      `json:"rolling_mean"`
	PopulationStdDev   domain.Price      `json:"population_stddev"`
	ZScore             string            `json:"zscore"`
	ADX14              string            `json:"adx14"`
	HigherEMA200       domain.Price      `json:"higher_ema200"`
	EMADeclineFraction string            `json:"ema_decline_fraction"`
	Regime             string            `json:"regime"`
	ATR14              domain.Price      `json:"atr14"`
	PriceAtProtectiveZ domain.Price      `json:"price_at_protective_z"`
	RiskBudget         domain.Money      `json:"risk_budget"`
	StressedUnitRisk   domain.Price      `json:"stressed_unit_risk"`
	Attributes         map[string]string `json:"attributes"`
}

// Candidate is one exact desired simulated position change.
type Candidate struct {
	DecisionID          domain.DecisionID `json:"decision_id"`
	DecisionLogicalTime uint64            `json:"decision_logical_time"`
	Instrument          domain.Instrument `json:"instrument"`
	Side                domain.Side       `json:"side"`
	Quantity            domain.Quantity   `json:"quantity"`
	LimitPrice          domain.Price      `json:"limit_price"`
	Notional            domain.Notional   `json:"notional"`
	ExpiresAt           uint64            `json:"expires_at"`
	ReasonCode          string            `json:"reason_code"`
	Explanation         Explanation       `json:"explanation"`
}

// Decision records accepted changes and every deterministic rejection.
type Decision struct {
	ID            domain.DecisionID `json:"id"`
	Ordinal       uint64            `json:"ordinal"`
	Action        Action            `json:"action"`
	ReasonCode    string            `json:"reason_code"`
	Candidate     *Candidate        `json:"candidate,omitempty"`
	Explanation   Explanation       `json:"explanation"`
	CooldownStart uint64            `json:"cooldown_start"`
}

// CanonicalHash returns the deterministic decision evidence identity.
func (decision Decision) CanonicalHash() string {
	canonical, _ := json.Marshal(decision)
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}
