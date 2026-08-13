package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// SandboxStrategySizingFacts is the complete non-secret sizing and policy
// input for one projected strategy evaluation. It is supplied by the owning
// account/policy projection; this builder cannot fetch an account, relax a
// cap, or declare an entry safe by itself.
type SandboxStrategySizingFacts struct {
	AccountSnapshot            sandbox.AccountSnapshot
	CentralRiskFacts           sandbox.StrategyRiskFacts
	PortfolioRevision          uint64
	PositionRevision           uint64
	AssetEligibility           uint64
	RiskPolicyID               string
	RiskPolicyVersion          uint64
	RiskPolicyHash             string
	MinimumReserve             domain.Money
	MaximumReserved            domain.Money
	MaximumOrderNotional       domain.Money
	EntryFeeRate               domain.Rate
	ExitFeeRate                domain.Rate
	GapAllowance               domain.Price
	LatencyDeterioration       domain.Price
	SlippageAllowance          domain.Price
	LiquidityDomain            string
	FencingToken               uint64
	EvaluationOrdinal          uint64
	EvaluationLogicalTime      uint64
	PriorEvaluationTriggerHash string
	ConfigurationHash          string
	ConfigurationVersion       string
	FeeModelID                 string
	LatencyModelID             string
	FillModelID                string
	SlippageModelID            string
	GapModelID                 string
	CorrelationModelID         string
}

// ValidFor rejects stale, cross-account, or oversized facts before a strategy
// is given a chance to produce a candidate. Central allocation and risk still
// re-evaluate any candidate afterwards.
func (facts SandboxStrategySizingFacts) ValidFor(work sandbox.StrategySessionWork, now time.Time) error {
	zeroMoney, moneyErr := domain.ParseMoney("0")
	maximumSandboxOrder, maximumErr := domain.ParseMoney("10")
	zeroPrice, priceErr := domain.ParsePrice("0")
	zeroRate, rateErr := domain.ParseRate("0")
	if moneyErr != nil || maximumErr != nil || priceErr != nil || rateErr != nil ||
		work.ValidAt(now) != nil || facts.AccountSnapshot.Validate() != nil ||
		facts.CentralRiskFacts.ValidFor(work, facts.AccountSnapshot, now) != nil ||
		facts.AccountSnapshot.AccountID != work.Account.ID || facts.AccountSnapshot.Epoch != work.Account.Epoch ||
		facts.AccountSnapshot.ObservedAt.After(now) || now.Sub(facts.AccountSnapshot.ObservedAt) > 250*time.Millisecond ||
		facts.PortfolioRevision == 0 || facts.PositionRevision == 0 || facts.AssetEligibility == 0 ||
		facts.RiskPolicyID == "" || facts.RiskPolicyVersion == 0 || !projectorHash256(facts.RiskPolicyHash) ||
		facts.CentralRiskFacts.PolicyID != facts.RiskPolicyID ||
		facts.CentralRiskFacts.PolicyVersion != facts.RiskPolicyVersion ||
		facts.CentralRiskFacts.PolicyHash != facts.RiskPolicyHash ||
		facts.MinimumReserve.Compare(zeroMoney) < 0 || facts.MaximumReserved.Compare(zeroMoney) < 0 ||
		facts.MaximumOrderNotional.Compare(zeroMoney) <= 0 ||
		facts.MaximumOrderNotional.Compare(maximumSandboxOrder) > 0 || facts.EntryFeeRate.Compare(zeroRate) < 0 ||
		facts.ExitFeeRate.Compare(zeroRate) < 0 || facts.GapAllowance.Compare(zeroPrice) < 0 ||
		facts.LatencyDeterioration.Compare(zeroPrice) < 0 || facts.SlippageAllowance.Compare(zeroPrice) < 0 ||
		facts.LiquidityDomain == "" || facts.FencingToken == 0 || facts.EvaluationOrdinal == 0 ||
		facts.EvaluationLogicalTime == 0 ||
		(facts.PriorEvaluationTriggerHash != "" && !projectorHash256(facts.PriorEvaluationTriggerHash)) ||
		facts.ConfigurationHash != work.ConfigurationHash ||
		!projectorHash256(facts.ConfigurationHash) || facts.ConfigurationVersion == "" || facts.FeeModelID == "" ||
		facts.LatencyModelID == "" || facts.FillModelID == "" || facts.SlippageModelID == "" ||
		facts.GapModelID == "" || facts.CorrelationModelID == "" {
		return fmt.Errorf("sandbox_strategy_sizing_facts_invalid")
	}
	return nil
}

// BuildTrendInput constructs one complete pure Trend input from credential-free
// market data, a replayed session-owned position, and explicit policy facts.
// It does not evaluate, allocate, approve, plan, or submit anything.
func BuildTrendInput(
	work sandbox.StrategySessionWork,
	configuration trend.Configuration,
	market sandbox.StrategyMarketInput,
	position trend.PositionState,
	facts SandboxStrategySizingFacts,
	now time.Time,
) (trend.Input, error) {
	if work.Strategy != sandbox.StrategyTrend || facts.ValidFor(work, now) != nil ||
		market.Instrument.Symbol() != work.Instrument || market.ObservedAt.UTC.IsZero() ||
		!market.ObservedAt.UTC.Equal(now) || market.Metadata.Metadata.Instrument != market.Instrument ||
		market.Metadata.Metadata.Validate() != nil || !projectorHash256(market.Metadata.RawPayloadHash) ||
		!validStrategyInputBook(market.Book, market.Instrument, now) {
		return trend.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	candles := append([]exchangecontracts.Candle(nil), market.Candles["4h"]...)
	if !validStrategyInputCandles(candles, market.Instrument, "4h", 200, now) {
		return trend.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	equity, available, err := strategySettlementMoney(facts.AccountSnapshot, "USDT")
	if err != nil {
		return trend.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	reference := market.Book.Asks[0].Price
	if position.Open {
		reference = market.Book.Bids[0].Price
	}
	evidence := trend.InputEvidence{CandleViewID: "sandbox-candles-" + work.Instrument,
		CandleViewRevision: uint64(len(candles)), MarketViewID: "sandbox-book-" + work.Instrument,
		MarketViewRevision: market.Book.LastSequence, InstrumentMetadataID: "metadata-" + market.Metadata.RawPayloadHash[:24],
		AssetEligibilityVersion: facts.AssetEligibility, ConfigurationVersion: facts.ConfigurationVersion,
		ConfigurationHash: configuration.Hash, StrategyVersion: configuration.Version,
		PortfolioRevision: facts.PortfolioRevision, PositionRevision: facts.PositionRevision,
		FeeModelID: facts.FeeModelID, LatencyModelID: facts.LatencyModelID, FillModelID: facts.FillModelID,
		SlippageModelID: facts.SlippageModelID, GapModelID: facts.GapModelID,
		CorrelationID: work.StrategySetHash, CausationID: strategyInputCausation(work, facts)}
	return trend.Input{Ordinal: facts.EvaluationOrdinal, LogicalTime: facts.EvaluationLogicalTime,
		Now: now, Instrument: market.Instrument, Candles: candles, MarketHealthy: true,
		BookAge: now.Sub(market.Book.ReceivedAt.UTC), Position: position,
		Sizing: trend.SizingState{Equity: equity, AvailableCash: available, MinimumReserve: facts.MinimumReserve,
			NotionalLimits: []domain.Money{facts.MaximumOrderNotional}, EntryReference: reference,
			FirstExecutablePrice: reference, GapAllowance: facts.GapAllowance,
			LatencyDeterioration: facts.LatencyDeterioration, EntryFeeRate: facts.EntryFeeRate,
			ExitFeeRate: facts.ExitFeeRate, InstrumentMetadata: market.Metadata.Metadata,
			CentralRiskEligible: true, LiquidityDomain: facts.LiquidityDomain, FencingToken: facts.FencingToken},
		Evidence: evidence}, nil
}

// BuildMeanReversionInput constructs the corresponding dual-timeframe input.
// The immutable evaluator retains its legacy configuration-version contract,
// so the caller must explicitly supply that version rather than silently
// substituting the active runtime schema label.
func BuildMeanReversionInput(
	work sandbox.StrategySessionWork,
	configuration meanreversion.Configuration,
	market sandbox.StrategyMarketInput,
	position meanreversion.PositionState,
	facts SandboxStrategySizingFacts,
	now time.Time,
) (meanreversion.Input, error) {
	if work.Strategy != sandbox.StrategyMeanReversion || facts.ValidFor(work, now) != nil ||
		market.Instrument.Symbol() != work.Instrument ||
		market.ObservedAt.UTC.IsZero() || !market.ObservedAt.UTC.Equal(now) ||
		market.Metadata.Metadata.Instrument != market.Instrument || market.Metadata.Metadata.Validate() != nil ||
		!projectorHash256(market.Metadata.RawPayloadHash) || !validStrategyInputBook(market.Book, market.Instrument, now) {
		return meanreversion.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	primary := append([]exchangecontracts.Candle(nil), market.Candles["1h"]...)
	higher := append([]exchangecontracts.Candle(nil), market.Candles["4h"]...)
	if !validStrategyInputCandles(primary, market.Instrument, "1h", 28, now) ||
		!validStrategyInputCandles(higher, market.Instrument, "4h", 210, now) {
		return meanreversion.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	equity, available, err := strategySettlementMoney(facts.AccountSnapshot, "USDT")
	if err != nil {
		return meanreversion.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	reference := market.Book.Asks[0].Price
	if position.Open {
		reference = market.Book.Bids[0].Price
	}
	spread, err := strategyBookSpread(market.Book.Bids[0].Price, market.Book.Asks[0].Price)
	if err != nil {
		return meanreversion.Input{}, fmt.Errorf("sandbox_strategy_input_invalid")
	}
	coherent := strategyInputHash(market.Book.RawPayloadHash, primary[len(primary)-1].RawPayloadHash,
		higher[len(higher)-1].RawPayloadHash, fmt.Sprintf("%d", market.Book.LastSequence))
	evidence := meanReversionSandboxEvidence(work, configuration, market, facts, primary, higher, coherent)
	return meanreversion.Input{Ordinal: facts.EvaluationOrdinal, LogicalTime: facts.EvaluationLogicalTime,
		Now: now, Instrument: market.Instrument, PrimaryCandles: primary, HigherCandles: higher,
		MarketHealthy: true, MarketDataQualityPass: true, ExchangeRiskPaused: false, Spread: spread,
		BookAge: now.Sub(market.Book.ReceivedAt.UTC), Position: position,
		Sizing: meanreversion.SizingState{Equity: equity, AvailableCash: available, MinimumReserve: facts.MinimumReserve,
			NotionalLimits: []domain.Money{facts.MaximumOrderNotional}, FirstExecutablePrice: reference,
			FirstExecutableAt: now, GapAllowance: facts.GapAllowance, SlippageAllowance: facts.SlippageAllowance,
			EntryFeeRate: facts.EntryFeeRate, ExitFeeRate: facts.ExitFeeRate, InstrumentMetadata: market.Metadata.Metadata,
			CentralRiskEligible: true, LiquidityDomain: facts.LiquidityDomain, FencingToken: facts.FencingToken},
		Evidence: evidence}, nil
}

func meanReversionSandboxEvidence(work sandbox.StrategySessionWork,
	configuration meanreversion.Configuration, market sandbox.StrategyMarketInput,
	facts SandboxStrategySizingFacts, primary, higher []exchangecontracts.Candle,
	coherent string,
) meanreversion.InputEvidence {
	return meanreversion.InputEvidence{PrimaryCandleViewID: "sandbox-1h-" + work.Instrument,
		PrimaryCandleViewRevision: uint64(len(primary)), HigherCandleViewID: "sandbox-4h-" + work.Instrument,
		HigherCandleViewRevision: uint64(len(higher)), MarketViewID: "sandbox-book-" + work.Instrument,
		MarketViewRevision: market.Book.LastSequence, CoherentViewID: coherent,
		CoherentVersionVectorHash: coherent, InstrumentMetadataID: "metadata-" + market.Metadata.RawPayloadHash[:24],
		AssetEligibilityVersion: facts.AssetEligibility, ConfigurationSnapshotID: work.ConfigurationID,
		ConfigurationVersion: facts.ConfigurationVersion, ConfigurationHash: configuration.Hash,
		StrategyVersion: configuration.Version, StrategyHash: configuration.Hash,
		PortfolioRevision: facts.PortfolioRevision, PositionRevision: facts.PositionRevision,
		RiskPolicyID: facts.RiskPolicyID, RiskPolicyVersion: facts.RiskPolicyVersion,
		RiskPolicyHash: facts.RiskPolicyHash, FeeModelID: facts.FeeModelID,
		LatencyModelID: facts.LatencyModelID, FillModelID: facts.FillModelID,
		SlippageModelID: facts.SlippageModelID, GapModelID: facts.GapModelID,
		CorrelationModelID: facts.CorrelationModelID, CorrelationID: work.StrategySetHash,
		CausationID: strategyInputCausation(work, facts)}
}

func strategySettlementMoney(snapshot sandbox.AccountSnapshot, asset domain.AssetSymbol) (domain.Money, domain.Money, error) {
	for _, balance := range snapshot.Balances {
		if balance.Asset != asset {
			continue
		}
		available, err := domain.ParseMoney(balance.Available.String())
		if err != nil {
			return domain.Money{}, domain.Money{}, err
		}
		total, err := balance.Available.Add(balance.Reserved)
		if err != nil {
			return domain.Money{}, domain.Money{}, err
		}
		equity, err := domain.ParseMoney(total.String())
		return equity, available, err
	}
	return domain.Money{}, domain.Money{}, fmt.Errorf("strategy_settlement_balance_missing")
}

func validStrategyInputBook(book exchangecontracts.BookSnapshot, instrument domain.Instrument, now time.Time) bool {
	if book.Instrument != instrument || book.ReceivedAt.Validate() != nil || book.ReceivedAt.UTC.After(now) ||
		now.Sub(book.ReceivedAt.UTC) >= 250*time.Millisecond || book.LastSequence == 0 || len(book.Bids) == 0 ||
		len(book.Asks) == 0 || !projectorHash256(book.RawPayloadHash) {
		return false
	}
	zeroPrice, priceErr := domain.ParsePrice("0")
	zeroQuantity, quantityErr := domain.ParseQuantity("0")
	if priceErr != nil || quantityErr != nil || book.Bids[0].Price.Compare(zeroPrice) <= 0 ||
		book.Asks[0].Price.Compare(zeroPrice) <= 0 || book.Bids[0].Quantity.Compare(zeroQuantity) <= 0 ||
		book.Asks[0].Quantity.Compare(zeroQuantity) <= 0 || book.Asks[0].Price.Compare(book.Bids[0].Price) < 0 {
		return false
	}
	for index := range book.Bids {
		if book.Bids[index].Price.Compare(zeroPrice) <= 0 || book.Bids[index].Quantity.Compare(zeroQuantity) <= 0 ||
			(index > 0 && book.Bids[index-1].Price.Compare(book.Bids[index].Price) <= 0) {
			return false
		}
	}
	for index := range book.Asks {
		if book.Asks[index].Price.Compare(zeroPrice) <= 0 || book.Asks[index].Quantity.Compare(zeroQuantity) <= 0 ||
			(index > 0 && book.Asks[index-1].Price.Compare(book.Asks[index].Price) >= 0) {
			return false
		}
	}
	return true
}

func validStrategyInputCandles(candles []exchangecontracts.Candle, instrument domain.Instrument, interval string, minimum int, now time.Time) bool {
	if len(candles) < minimum {
		return false
	}
	for index, candle := range candles {
		if candle.Instrument != instrument || candle.Interval != interval || !candle.Closed || candle.CloseTime.After(now) ||
			!projectorHash256(candle.RawPayloadHash) || (index > 0 && !candles[index-1].CloseTime.Before(candle.CloseTime)) {
			return false
		}
	}
	return true
}

func strategyBookSpread(bid, ask domain.Price) (domain.Percent, error) {
	return domain.CalculateRelativeSpread(bid, ask, 18)
}

func strategyInputCausation(work sandbox.StrategySessionWork, facts SandboxStrategySizingFacts) string {
	return "sandbox-strategy-" + string(work.SessionID) + "-" + fmt.Sprintf("%d", facts.EvaluationOrdinal)
}

func strategyInputHash(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func projectorHash256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}
