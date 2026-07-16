package trend

import (
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Stable Trend reason codes.
const (
	ReasonEntryAccepted        = "trend.entry.accepted"
	ReasonExitInitialStop      = "trend.exit.initial_stop"
	ReasonExitTrailingStop     = "trend.exit.trailing_stop"
	ReasonExitEMA50            = "trend.exit.ema50"
	ReasonWarmUp               = "trend.reject.warm_up"
	ReasonCandleFinality       = "trend.reject.candle_finality"
	ReasonCandleOrder          = "trend.reject.candle_order"
	ReasonCandleGap            = "trend.reject.candle_gap"
	ReasonCandleConflict       = "trend.reject.candle_conflict"
	ReasonDuplicateDecision    = "trend.reject.duplicate_decision"
	ReasonStaleSignal          = "trend.reject.stale_signal"
	ReasonUnhealthyMarket      = "trend.reject.unhealthy_market"
	ReasonRegimeFailed         = "trend.reject.regime"
	ReasonConfirmationFailed   = "trend.reject.confirmation"
	ReasonBreakoutFailed       = "trend.reject.breakout"
	ReasonExistingPosition     = "trend.reject.existing_position"
	ReasonCooldown             = "trend.reject.cooldown"
	ReasonInvalidSizing        = "trend.reject.invalid_sizing"
	ReasonMinimumFilter        = "trend.reject.minimum_filter"
	ReasonRiskClipped          = "trend.reject.risk_clipped"
	ReasonInvalidConfiguration = "trend.reject.invalid_configuration"
	ReasonMissedOrder          = "trend.order.missed"
	ReasonExpiredOrder         = "trend.order.expired"
)

// Action is the desired position change returned by the strategy.
type Action string

// Supported Trend actions.
const (
	ActionNone  Action = "none"
	ActionEntry Action = "entry"
	ActionExit  Action = "exit"
)

// InputEvidence identifies every immutable fact used by one evaluation.
type InputEvidence struct {
	CandleViewID            string `json:"candle_view_id"`
	CandleViewRevision      uint64 `json:"candle_view_revision"`
	MarketViewID            string `json:"market_view_id"`
	MarketViewRevision      uint64 `json:"market_view_revision"`
	InstrumentMetadataID    string `json:"instrument_metadata_id"`
	AssetEligibilityVersion uint64 `json:"asset_eligibility_version"`
	ConfigurationVersion    string `json:"configuration_version"`
	ConfigurationHash       string `json:"configuration_hash"`
	StrategyVersion         string `json:"strategy_version"`
	PortfolioRevision       uint64 `json:"portfolio_revision"`
	PositionRevision        uint64 `json:"position_revision"`
	FeeModelID              string `json:"fee_model_id"`
	LatencyModelID          string `json:"latency_model_id"`
	FillModelID             string `json:"fill_model_id"`
	SlippageModelID         string `json:"slippage_model_id"`
	GapModelID              string `json:"gap_model_id"`
	CorrelationID           string `json:"correlation_id"`
	CausationID             string `json:"causation_id"`
}

// PositionState is the immutable pre-decision Trend position projection.
type PositionState struct {
	Open                  bool            `json:"open"`
	Quantity              domain.Quantity `json:"quantity"`
	ActualEntryPrice      domain.Price    `json:"actual_entry_price"`
	SignalATR             domain.Price    `json:"signal_atr"`
	InitialStop           domain.Price    `json:"initial_stop"`
	TrailingStop          domain.Price    `json:"trailing_stop"`
	HighestFavorableClose domain.Price    `json:"highest_favorable_close"`
	CooldownRemaining     uint64          `json:"cooldown_remaining"`
}

// SizingState contains immutable exact values supplied by portfolio/model owners.
type SizingState struct {
	Equity               domain.Money              `json:"equity"`
	AvailableCash        domain.Money              `json:"available_cash"`
	MinimumReserve       domain.Money              `json:"minimum_reserve"`
	NotionalLimits       []domain.Money            `json:"notional_limits"`
	EntryReference       domain.Price              `json:"entry_reference"`
	FirstExecutablePrice domain.Price              `json:"first_executable_price"`
	GapAllowance         domain.Price              `json:"gap_allowance"`
	LatencyDeterioration domain.Price              `json:"latency_deterioration"`
	EntryFeeRate         domain.Rate               `json:"entry_fee_rate"`
	ExitFeeRate          domain.Rate               `json:"exit_fee_rate"`
	InstrumentMetadata   domain.InstrumentMetadata `json:"instrument_metadata"`
	CentralRiskEligible  bool                      `json:"central_risk_eligible"`
	LiquidityDomain      string                    `json:"liquidity_domain"`
	FencingToken         uint64                    `json:"fencing_token"`
}

// Input is one immutable complete Trend evaluation request.
type Input struct {
	Ordinal       uint64                     `json:"ordinal"`
	LogicalTime   uint64                     `json:"logical_time"`
	Now           time.Time                  `json:"now"`
	Instrument    domain.Instrument          `json:"instrument"`
	Candles       []exchangecontracts.Candle `json:"candles"`
	MarketHealthy bool                       `json:"market_healthy"`
	BookAge       time.Duration              `json:"book_age"`
	Position      PositionState              `json:"position"`
	Sizing        SizingState                `json:"sizing"`
	Evidence      InputEvidence              `json:"evidence"`
}

// Explanation is the complete stable decision explanation.
type Explanation struct {
	ReasonCode        string            `json:"reason_code"`
	Evidence          InputEvidence     `json:"evidence"`
	SignalCandleHash  string            `json:"signal_candle_hash"`
	SignalCandleClose time.Time         `json:"signal_candle_close"`
	EMA50             domain.Price      `json:"ema50"`
	EMA200            domain.Price      `json:"ema200"`
	ATR14             domain.Price      `json:"atr14"`
	BreakoutHigh      domain.Price      `json:"breakout_high"`
	RiskBudget        domain.Money      `json:"risk_budget"`
	StressedUnitRisk  domain.Price      `json:"stressed_unit_risk"`
	Attributes        map[string]string `json:"attributes"`
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
