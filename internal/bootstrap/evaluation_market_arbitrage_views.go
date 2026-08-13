package bootstrap

import (
	"fmt"
	"math/big"
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/risk"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

func floorEvaluationDecimal(value *big.Rat, scale int) string {
	if value == nil || value.Sign() <= 0 || scale < 0 || scale > 18 {
		return "0"
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(factor))
	integer := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	return new(big.Rat).SetFrac(integer, factor).FloatString(scale)
}

func (processor *evaluationMarketProcessor) evaluationRules(exchange string, metadata domain.InstrumentMetadata,
	now time.Time) (arbitrage.InstrumentRules, error) {
	maximum, err := domain.ParseQuantity("1000000000")
	fee, feeErr := evaluationFeeRate(processor.claim.CostStressBPS)
	zero, _ := domain.ParsePrice("0")
	if err != nil || feeErr != nil || now.IsZero() {
		return arbitrage.InstrumentRules{}, fmt.Errorf("evaluation_instrument_rules_invalid")
	}
	return arbitrage.InstrumentRules{Exchange: exchange, Metadata: metadata, MaximumQuantity: maximum,
		Fee: arbitrage.FeeSchedule{Version: processor.claim.Configuration.Models.Fee,
			Rate: fee, Asset: metadata.Instrument.Quote, ThirdAssetPriceInQuote: zero},
		Active: true, ObservedAt: now.UTC()}, nil
}

func (book *evaluationBook) recordedView() (exchangecontracts.BookSnapshot, marketdata.Observation, error) {
	bids, asks, ok := book.levels()
	if !ok || book.ordinal == 0 || book.logical == 0 || book.receivedAt.IsZero() || book.lastHash == "" {
		return exchangecontracts.BookSnapshot{}, marketdata.Observation{}, fmt.Errorf("evaluation_recorded_book_invalid")
	}
	received := domain.EventTime{UTC: book.receivedAt.UTC(), Sequence: 1}
	processed := domain.EventTime{UTC: book.receivedAt.UTC(), Sequence: 2}
	published := domain.EventTime{UTC: book.receivedAt.UTC(), Sequence: 3}
	observation := marketdata.Observation{ReceivedAt: received, ProcessedAt: processed, PublishedAt: published,
		ConnectionID: "evaluation-" + book.exchange, ConnectionGeneration: 1, SourceSequence: book.sequence,
		IngestOrdinal: book.ordinal, ReceivedOffsetNanos: book.logical,
		ProcessedOffsetNanos: book.logical, PublishedOffsetNanos: book.logical}
	snapshot := exchangecontracts.BookSnapshot{Exchange: exchangecontracts.ExchangeID(book.exchange),
		Instrument: book.instrument, LastSequence: book.sequence, ReceivedAt: received,
		Bids: bids, Asks: asks, RawPayloadHash: book.lastHash}
	if observation.Validate() != nil {
		return exchangecontracts.BookSnapshot{}, marketdata.Observation{}, fmt.Errorf("evaluation_recorded_book_invalid")
	}
	return snapshot, observation, nil
}

func (book *evaluationBook) levels() ([]exchangecontracts.PriceLevel, []exchangecontracts.PriceLevel, bool) {
	bids := make([]exchangecontracts.PriceLevel, 0, len(book.bids))
	asks := make([]exchangecontracts.PriceLevel, 0, len(book.asks))
	for _, level := range book.bids {
		bids = append(bids, level)
	}
	for _, level := range book.asks {
		asks = append(asks, level)
	}
	sort.Slice(bids, func(left, right int) bool { return bids[left].Price.Compare(bids[right].Price) > 0 })
	sort.Slice(asks, func(left, right int) bool { return asks[left].Price.Compare(asks[right].Price) < 0 })
	return bids, asks, len(bids) > 0 && len(asks) > 0 && bids[0].Price.Compare(asks[0].Price) < 0
}

func evaluationBookView(exchange string, snapshot exchangecontracts.BookSnapshot,
	observation marketdata.Observation) (marketdata.BookView, error) {
	depth := len(snapshot.Bids)
	if len(snapshot.Asks) > depth {
		depth = len(snapshot.Asks)
	}
	book, err := marketdata.NewBook(exchange, snapshot.Instrument, depth, depth, nil)
	if err != nil || book.BeginGeneration(observation.ConnectionID, observation.ConnectionGeneration) != nil ||
		book.ReplaceSnapshot(snapshot, observation) != nil {
		return marketdata.BookView{}, fmt.Errorf("evaluation_recorded_book_invalid")
	}
	return book.View(), nil
}

func evaluationTriangularRisk(markets []triangular.MarketInput, logical uint64,
	now time.Time) (triangular.RiskInput, error) {
	observations, err := evaluationRiskObservationsTriangular(markets, logical)
	if err != nil {
		return triangular.RiskInput{}, err
	}
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return triangular.RiskInput{Policies: []risk.Policy{policy}, Observations: observations,
		EvaluatedAt: now, Cautious: risk.CautiousControls{ReducedSize: true,
			StricterEdge: true, InstrumentEligible: true}}, nil
}

func evaluationCrossRisk(markets []crossarb.MarketInput, logical uint64,
	now time.Time) (crossarb.RiskInput, error) {
	triangularMarkets := make([]triangular.MarketInput, 0, len(markets))
	for _, market := range markets {
		triangularMarkets = append(triangularMarkets, triangular.MarketInput{Snapshot: market.Snapshot,
			Observation: market.Observation, Rules: market.Rules})
	}
	observations, err := evaluationRiskObservationsTriangular(triangularMarkets, logical)
	if err != nil {
		return crossarb.RiskInput{}, err
	}
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return crossarb.RiskInput{Policies: []risk.Policy{policy}, Observations: observations,
		EvaluatedAt: now, Cautious: risk.CautiousControls{ReducedSize: true,
			StricterEdge: true, InstrumentEligible: true}}, nil
}

func evaluationRiskObservationsTriangular(markets []triangular.MarketInput,
	logical uint64) (risk.Observations, error) {
	zero, _ := domain.ParsePercent("0")
	exposure, _ := domain.ParsePercent("0.10")
	reserve, _ := domain.ParsePercent("0.15")
	spread, _ := domain.ParsePercent("0")
	maximumAge := time.Duration(0)
	for _, market := range markets {
		if len(market.Snapshot.Bids) == 0 || len(market.Snapshot.Asks) == 0 ||
			logical < market.Observation.PublishedOffsetNanos {
			return risk.Observations{}, fmt.Errorf("evaluation_arbitrage_risk_invalid")
		}
		value, err := domain.CalculateRelativeSpread(market.Snapshot.Bids[0].Price,
			market.Snapshot.Asks[0].Price, 18)
		if err != nil {
			return risk.Observations{}, err
		}
		if value.Compare(spread) > 0 {
			spread = value
		}
		age := time.Duration(logical - market.Observation.PublishedOffsetNanos)
		if age > maximumAge {
			maximumAge = age
		}
	}
	openOrders, quality, queueLag, clockDrift, problem := uint32(0), uint8(100), time.Duration(0), time.Duration(0), false
	return risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero, Rolling24HourLoss: &zero,
		StrategyLoss: &zero, AssetExposure: &exposure, CombinedExposure: &exposure,
		ExchangeExposure: &exposure, Reserve: &reserve, ReservedCapital: &exposure,
		Spread: &spread, Slippage: &zero, OpenOrders: &openOrders, BookAge: &maximumAge,
		QueueLag: &queueLag, ClockDrift: &clockDrift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
			AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
			DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}, nil
}

func latestTriangularTime(markets []triangular.MarketInput) time.Time {
	result := markets[0].Snapshot.ReceivedAt.UTC
	for _, market := range markets[1:] {
		if market.Snapshot.ReceivedAt.UTC.After(result) {
			result = market.Snapshot.ReceivedAt.UTC
		}
	}
	return result.UTC()
}

func evaluationInstruments() (map[string]domain.Instrument, error) {
	values := make(map[string]domain.Instrument, 3)
	for _, pair := range [][2]string{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}} {
		base, baseErr := domain.ParseAssetSymbol(pair[0])
		quote, quoteErr := domain.ParseAssetSymbol(pair[1])
		instrument, err := domain.NewSpotInstrument(base, quote)
		if baseErr != nil || quoteErr != nil || err != nil {
			return nil, fmt.Errorf("evaluation_instrument_invalid")
		}
		values[instrument.Symbol()] = instrument
	}
	return values, nil
}

func cloneEvaluationBalances(values map[domain.AssetSymbol]domain.Balance) map[domain.AssetSymbol]domain.Balance {
	result := make(map[domain.AssetSymbol]domain.Balance, len(values))
	for asset, value := range values {
		result[asset] = value
	}
	return result
}

func cloneEvaluationVenueBalances(values crossarb.VenueBalances) crossarb.VenueBalances {
	result := make(crossarb.VenueBalances, len(values))
	for exchange, balances := range values {
		result[exchange] = cloneEvaluationBalances(balances)
	}
	return result
}
