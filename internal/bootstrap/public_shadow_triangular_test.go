package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/triangular"
)

func TestOwnerConsoleTriangularInputBindsThreeBooksCapitalRiskAndReplay(t *testing.T) {
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	claim, session := newOwnerConsoleTriangularTestSession(t, now)
	input, err := session.buildTriangularInput(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	assertOwnerConsoleTriangularInput(t, input)
	assertOwnerConsoleTriangularRecordedPipeline(t, claim, input)
	input.Markets[1].Rules.Fee.ThirdAssetPriceInQuote = domain.Price{}
	evaluation, evaluationErr := input.EvaluationInput()
	if evaluationErr != nil {
		t.Fatal(evaluationErr)
	}
	if _, err = triangular.Evaluate(evaluation); err == nil {
		t.Fatal("missing exact third-asset fee mark was accepted")
	}
}

func newOwnerConsoleTriangularTestSession(t *testing.T, now time.Time) (
	postgresstore.PublicShadowClaim, *ownerConsoleLiveShadowSession,
) {
	t.Helper()
	configuration := config.DefaultMultiStrategyConfiguration()
	configurationHash := strings.Repeat("a", 64)
	claim := postgresstore.PublicShadowClaim{ID: "triangle-shadow", RunID: "triangle-shadow",
		AccountID: "triangle-shadow-account", PortfolioID: "triangle-shadow-portfolio",
		StrategyID: "triangular-arbitrage-1-0-0", StrategyVersion: "triangular-arbitrage@1.0.0", ExchangeID: "binance",
		Configuration: configuration, ConfigurationHash: configurationHash,
		MarketScopeRequired: true, MarketScopes: []postgresstore.PublicShadowMarketScope{
			{Ordinal: 1, ExchangeID: "binance", InstrumentID: "BTCUSDT", Purpose: "triangle_market"},
			{Ordinal: 2, ExchangeID: "binance", InstrumentID: "ETHBTC", Purpose: "triangle_market"},
			{Ordinal: 3, ExchangeID: "binance", InstrumentID: "ETHUSDT", Purpose: "triangle_market"},
		}}
	collectors, metadata, maximum := ownerConsoleTriangularTestMarkets(t, now, claim.ExchangeID)
	runID, _ := domain.NewRunID(claim.RunID)
	portfolioID, _ := domain.NewPortfolioID(claim.PortfolioID)
	accountID, _ := domain.NewVirtualAccountID(claim.AccountID)
	capital, _ := domain.ParseBalance("500")
	owned, err := portfolio.InitializeTriangular(runID, portfolioID, accountID, configurationHash, capital,
		claim.ExchangeID, accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Unix(0, 1).UTC(), Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := triangular.ConfigurationFromReviewed(configuration.Triangular)
	if err != nil {
		t.Fatal(err)
	}
	session := &ownerConsoleLiveShadowSession{claim: claim,
		client: ownerConsoleTriangularPublicClient{offset: 1_000_000}, collectors: collectors,
		metadata: metadata, maximumQuantity: maximum, triangularConfig: configured,
		balances: owned.Snapshot()}
	return claim, session
}

func assertOwnerConsoleTriangularInput(t *testing.T, input triangular.Input) {
	t.Helper()
	if len(input.Markets) != 3 || input.AvailableSettlement.String() != "500" ||
		input.GlobalReserveFloor.String() != "75" || input.RecoveryAllowance.String() != "2" ||
		input.FeeBalances["USDT"].String() != "500" || len(input.FeeBalances) != 1 ||
		input.CentralRisk == nil || input.CentralRisk.Observations.Reserve.String() != "0.15" ||
		input.CentralRisk.Observations.ReservedCapital.String() != "0.204" ||
		input.Simulation == nil || len(input.Simulation.Markets) != 12 {
		t.Fatalf("triangular input=%#v", input)
	}
	if _, err := input.EvaluationInput(); err != nil {
		t.Fatal(err)
	}
	timeline, latency, err := input.RecordedSimulation()
	if err != nil || timeline == nil || latency.Version != "fixed-zero-v1" || latency.LegNanos != [3]uint64{1, 1, 1} {
		t.Fatalf("triangular recorded simulation=%#v error=%v", latency, err)
	}
}

func assertOwnerConsoleTriangularRecordedPipeline(t *testing.T, claim postgresstore.PublicShadowClaim,
	input triangular.Input,
) {
	t.Helper()
	processor, _, _, _, processorBalances, err := newPublicShadowProcessor(claim)
	if err != nil {
		t.Fatal(err)
	}
	prepared, projection, err := triangular.AttachCleanRecordedReduction(input,
		"shadow/triangular/"+claim.ID)
	if err != nil || projection == nil {
		t.Fatalf("prepared projection=%#v error=%v", projection, err)
	}
	payload, _ := json.Marshal(prepared)
	result, err := processor.Process(context.Background(), replay.Event{Ordinal: prepared.Ordinal,
		LogicalTime: prepared.LogicalTime, Canonical: payload})
	if err != nil {
		t.Fatal(err)
	}
	var reduction struct {
		Transactions []accounting.Transaction `json:"transactions"`
	}
	if json.Unmarshal(result.Balances, &reduction) != nil || len(reduction.Transactions) == 0 {
		t.Fatalf("live Triangle reduction=%s", result.Balances)
	}
	for _, transaction := range reduction.Transactions {
		if transaction.PortfolioID.Value() != claim.PortfolioID || transaction.RunID.Value() != claim.RunID {
			t.Fatalf("live Triangle journal ownership=%#v", transaction)
		}
		for _, line := range transaction.Lines {
			if line.Account.Owner != portfolio.TriangularStrategyOwner {
				t.Fatalf("live Triangle journal owner=%q", line.Account.Owner)
			}
		}
	}
	if processorBalances.Ownership.Strategy != portfolio.TriangularStrategyOwner {
		t.Fatalf("live Triangle processor balances=%#v", processorBalances.Ownership)
	}
}

func ownerConsoleTriangularTestMarkets(t *testing.T, now time.Time, exchange string) (
	map[domain.Instrument]shadowPublicCollector,
	map[domain.Instrument]domain.InstrumentMetadata,
	map[domain.Instrument]domain.Quantity,
) {
	t.Helper()
	collectors := make(map[domain.Instrument]shadowPublicCollector, 3)
	metadata := make(map[domain.Instrument]domain.InstrumentMetadata, 3)
	maximum := make(map[domain.Instrument]domain.Quantity, 3)
	prices := map[string][2]string{
		"BTCUSDT": {"29999", "30000"}, "ETHBTC": {"0.0666", "0.0667"}, "ETHUSDT": {"2100", "2101"},
	}
	for index, symbol := range []string{"BTCUSDT", "ETHBTC", "ETHUSDT"} {
		instrument := sagaInstrument(t, symbol)
		view := sagaBookViewPrices(t, exchange, instrument, now, uint64(900_000+index*10_000),
			uint64(index+1), prices[symbol][0], prices[symbol][1])
		collectors[instrument] = &publicShadowSagaCollector{provider: publicShadowSagaViews{view: view},
			health: exchangecontracts.CollectorHealthSnapshot{Exchange: exchange, Instrument: symbol,
				BookHealth: "HEALTHY", BookHealthy: true, BookFresh: true, BookEligible: true,
				ClockEligible: true, ClockObservedAt: now.Add(-time.Second),
				ClockUncertainty: time.Millisecond, Eligible: true}}
		tick, _ := domain.ParsePrice("0.0001")
		step, _ := domain.ParseQuantity("0.0001")
		minimumNotional, _ := domain.ParseNotional("0.00001")
		metadata[instrument] = domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: now.Add(-time.Minute), PriceTick: tick, QuantityStep: step,
			MinimumQuantity: step, MinimumNotional: minimumNotional}
		maximum[instrument], _ = domain.ParseQuantity("1000")
	}
	return collectors, metadata, maximum
}

type ownerConsoleTriangularPublicClient struct{ offset uint64 }

func (client ownerConsoleTriangularPublicClient) Instruments(context.Context, []domain.Instrument) ([]exchangecontracts.InstrumentRecord, error) {
	return nil, nil
}
func (client ownerConsoleTriangularPublicClient) Candles(context.Context, exchangecontracts.CandleRequest) ([]exchangecontracts.Candle, error) {
	return nil, nil
}
func (client ownerConsoleTriangularPublicClient) MonotonicOffset() uint64 { return client.offset }
