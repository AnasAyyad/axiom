package bootstrap

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/observability"
	"axiom/internal/portfolio"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
	"axiom/internal/strategies/triangular"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ownerConsoleLiveShadowSession struct {
	claim                           postgresstore.PublicShadowClaim
	store                           *postgresstore.PublicShadowStore
	client                          shadowPublicClient
	collectors                      map[domain.Instrument]shadowPublicCollector
	public                          *marketrecorder.Recorder
	decisions                       *marketrecorder.Recorder
	catalog                         *postgresstore.RecordedDatasetCatalog
	processor                       backtest.Processor
	trendConfig                     trend.Configuration
	meanConfig                      meanreversion.Configuration
	triangularConfig                triangular.Configuration
	flushEvery                      time.Duration
	commit                          string
	entries                         atomic.Bool
	flushMutex                      sync.Mutex
	stateMutex                      sync.Mutex
	activityMutex                   sync.Mutex
	lastActivity                    string
	lastActivityAt                  time.Time
	lastMarketViewID                string
	lastTriangularTriggerGeneration uint64
	lastTriangularTriggerVersion    uint64
	metadata                        map[domain.Instrument]domain.InstrumentMetadata
	metadataIDs                     map[domain.Instrument]string
	maximumQuantity                 map[domain.Instrument]domain.Quantity
	history                         map[domain.Instrument][]exchangecontracts.Candle
	primaryHistory                  map[domain.Instrument][]exchangecontracts.Candle
	higherHistory                   map[domain.Instrument][]exchangecontracts.Candle
	seen                            map[domain.Instrument]time.Time
	positions                       map[domain.Instrument]trend.PositionState
	meanPositions                   map[domain.Instrument]meanreversion.PositionState
	cooldowns                       map[domain.Instrument]uint64
	balances                        portfolio.Snapshot
	lastOrdinal                     uint64
	datasetID                       string
	metrics                         *observability.Metrics
}

type shadowPublicClient interface {
	Instruments(context.Context, []domain.Instrument) ([]exchangecontracts.InstrumentRecord, error)
	Candles(context.Context, exchangecontracts.CandleRequest) ([]exchangecontracts.Candle, error)
	MonotonicOffset() uint64
}

type shadowPublicCollector interface {
	Run(context.Context) error
	Views() marketdata.MarketViewProvider
	HealthSnapshot() exchangecontracts.CollectorHealthSnapshot
}

func newOwnerConsoleLiveShadowRoleWorkWithMetrics(pool *pgxpool.Pool, runtimeConfig config.Runtime,
	metrics *observability.Metrics,
) (*shadowRoleWork, error) {
	store, err := postgresstore.NewPublicShadowStore(pool, runtimeConfig.InstanceID, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	factory := func(ctx context.Context, claim postgresstore.PublicShadowClaim) (shadowSession, error) {
		if claim.StrategyID == "cross-exchange-arbitrage-1-0-0" {
			session, sessionErr := newOwnerConsoleCrossExchangeShadowSession(ctx, pool, runtimeConfig, store, claim)
			if session != nil {
				session.metrics = metrics
			}
			return session, sessionErr
		}
		session, sessionErr := newOwnerConsoleLiveShadowSession(ctx, pool, runtimeConfig, store, claim)
		if session != nil {
			session.metrics = metrics
		}
		return session, sessionErr
	}
	work, err := newShadowRoleWork(store, factory, time.Second)
	if err != nil {
		return nil, err
	}
	recoveryIdentity := runtimeConfig.InstanceID + ":" + time.Now().UTC().Format(time.RFC3339Nano)
	work.preflight = func(ctx context.Context) error {
		return postgresstore.EnsureOwnerConsoleStartupRecovery(ctx, pool, recoveryIdentity,
			buildinfo.Current(), time.Now().UTC())
	}
	return work, nil
}

func newOwnerConsoleLiveShadowSession(ctx context.Context, pool *pgxpool.Pool, runtimeConfig config.Runtime,
	store *postgresstore.PublicShadowStore, claim postgresstore.PublicShadowClaim) (*ownerConsoleLiveShadowSession, error) {
	if err := os.MkdirAll(runtimeConfig.Recorder.Root, 0o750); err != nil {
		return nil, fmt.Errorf("shadow_recorder_root_unavailable")
	}
	publicRecorder, decisionRecorder, catalog, err := newPublicShadowRecorders(pool,
		runtimeConfig.Recorder.Root, claim.ID, claim.ExchangeID)
	if err != nil {
		return nil, err
	}
	client, collectors, err := newPublicShadowMarketRuntime(claim, runtimeConfig.Recorder, publicRecorder,
		&domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	processor, configuredTrend, configuredMean, configuredTriangular, balances, err := newPublicShadowProcessor(claim)
	if err != nil {
		return nil, err
	}
	session := &ownerConsoleLiveShadowSession{claim: claim, store: store, client: client, collectors: collectors,
		public: publicRecorder, decisions: decisionRecorder, catalog: catalog, processor: processor,
		trendConfig: configuredTrend, meanConfig: configuredMean, triangularConfig: configuredTriangular,
		flushEvery: runtimeConfig.Recorder.FlushInterval,
		commit:     claimConfigurationCommit(), metadata: make(map[domain.Instrument]domain.InstrumentMetadata),
		metadataIDs:     make(map[domain.Instrument]string),
		maximumQuantity: make(map[domain.Instrument]domain.Quantity),
		history:         make(map[domain.Instrument][]exchangecontracts.Candle),
		primaryHistory:  make(map[domain.Instrument][]exchangecontracts.Candle),
		higherHistory:   make(map[domain.Instrument][]exchangecontracts.Candle), seen: make(map[domain.Instrument]time.Time),
		positions:     make(map[domain.Instrument]trend.PositionState),
		meanPositions: make(map[domain.Instrument]meanreversion.PositionState),
		cooldowns:     make(map[domain.Instrument]uint64),
		balances:      balances}
	if session.commit == "" {
		return nil, fmt.Errorf("shadow_build_identity_invalid")
	}
	_ = ctx
	return session, nil
}

func newPublicShadowRecorders(pool *pgxpool.Pool, root, id, exchange string) (*marketrecorder.Recorder,
	*marketrecorder.Recorder, *postgresstore.RecordedDatasetCatalog, error) {
	if exchange != "binance" && exchange != "bybit" {
		return nil, nil, nil, fmt.Errorf("shadow_exchange_invalid")
	}
	catalog, err := postgresstore.NewRecordedDatasetCatalog(pool)
	if err != nil {
		return nil, nil, nil, err
	}
	publicSession, decisionSession := id+"-public", id+"-decisions"
	publicRecorder, err := marketrecorder.New(root, id+"-public-evidence", publicSession, exchange,
		&runtimecore.IngestOrdinals{}, segmentCommitter(pool, publicSession, exchange), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	decisionRecorder, err := marketrecorder.New(root, id+"-decision-inputs", decisionSession, exchange,
		&runtimecore.IngestOrdinals{}, segmentCommitter(pool, decisionSession, exchange), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return publicRecorder, decisionRecorder, catalog, nil
}

func newPublicShadowMarketRuntime(claim postgresstore.PublicShadowClaim, recorderRuntime config.RecorderRuntime,
	recorder *marketrecorder.Recorder, clock domain.Clock,
) (shadowPublicClient, map[domain.Instrument]shadowPublicCollector, error) {
	selectedInstruments, err := publicShadowScopeInstruments(claim, claim.ExchangeID)
	if err != nil {
		return nil, nil, err
	}
	var selected config.ExchangeConfiguration
	for _, venue := range claim.Configuration.PublicExchanges() {
		if venue.ID == claim.ExchangeID {
			selected = venue
			break
		}
	}
	if selected.ID == "" {
		return nil, nil, fmt.Errorf("shadow_exchange_configuration_missing")
	}
	switch claim.ExchangeID {
	case "binance":
		return newOwnerConsoleBinanceShadowMarketRuntime(selected, recorderRuntime, recorder,
			clock, selectedInstruments)
	case "bybit":
		return newOwnerConsoleBybitShadowMarketRuntime(selected, recorderRuntime, recorder,
			clock, selectedInstruments)
	default:
		return nil, nil, fmt.Errorf("shadow_exchange_invalid")
	}
}

func newOwnerConsoleBinanceShadowMarketRuntime(selected config.ExchangeConfiguration,
	runtime config.RecorderRuntime, recorder *marketrecorder.Recorder, clock domain.Clock,
	instruments map[string]bool,
) (shadowPublicClient, map[domain.Instrument]shadowPublicCollector, error) {
	client, err := binance.NewPublicClient(selected.EndpointSet, clock)
	if err != nil {
		return nil, nil, err
	}
	sink, err := marketrecorder.NewBinanceStreamSink(recorder)
	if err != nil {
		return nil, nil, err
	}
	configured, err := newBinanceCollectors(selected, runtime, client, sink, clock)
	if err != nil {
		return nil, nil, err
	}
	collectors, err := selectedPublicShadowCollectors(configured, instruments)
	return client, collectors, err
}

func newOwnerConsoleBybitShadowMarketRuntime(selected config.ExchangeConfiguration,
	runtime config.RecorderRuntime, recorder *marketrecorder.Recorder, clock domain.Clock,
	instruments map[string]bool,
) (shadowPublicClient, map[domain.Instrument]shadowPublicCollector, error) {
	client, err := bybit.NewPublicClient(selected.EndpointSet, clock)
	if err != nil {
		return nil, nil, err
	}
	sink, err := marketrecorder.NewPublicStreamSink(recorder,
		"bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		return nil, nil, err
	}
	configured, err := newBybitCollectors(selected, runtime, client, sink, clock)
	if err != nil {
		return nil, nil, err
	}
	collectors, err := selectedPublicShadowCollectors(configured, instruments)
	return client, collectors, err
}

func selectedPublicShadowCollectors[T shadowPublicCollector](configured map[domain.Instrument]T,
	selected map[string]bool,
) (map[domain.Instrument]shadowPublicCollector, error) {
	collectors := make(map[domain.Instrument]shadowPublicCollector)
	for instrument, collector := range configured {
		if len(selected) == 0 || selected[instrument.Symbol()] {
			collectors[instrument] = collector
		}
	}
	if len(collectors) == 0 {
		return nil, fmt.Errorf("shadow_instrument_configuration_missing")
	}
	return collectors, nil
}

func publicShadowScopeInstruments(claim postgresstore.PublicShadowClaim, exchange string) (map[string]bool, error) {
	selected := make(map[string]bool)
	if len(claim.MarketScopes) == 0 {
		if claim.InstrumentID != "" {
			selected[claim.InstrumentID] = true
		}
		return selected, nil
	}
	for _, scope := range claim.MarketScopes {
		if scope.ExchangeID != exchange || scope.InstrumentID == "" || selected[scope.InstrumentID] {
			return nil, fmt.Errorf("shadow_market_scope_invalid")
		}
		selected[scope.InstrumentID] = true
	}
	return selected, nil
}

func newPublicShadowProcessor(claim postgresstore.PublicShadowClaim) (backtest.Processor,
	trend.Configuration, meanreversion.Configuration, triangular.Configuration, portfolio.Snapshot, error) {
	identity, err := newPublicShadowProcessorIdentity(claim)
	if err != nil {
		return nil, trend.Configuration{}, meanreversion.Configuration{}, triangular.Configuration{},
			portfolio.Snapshot{}, err
	}
	var runtime publicShadowProcessorRuntime
	switch claim.StrategyID {
	case "trend-following-1-0-0":
		runtime, err = newOwnerConsoleTrendShadowProcessor(claim, identity)
	case "mean-reversion-1-0-0":
		runtime, err = newOwnerConsoleMeanReversionShadowProcessor(claim, identity)
	case "triangular-arbitrage-1-0-0":
		runtime, err = newOwnerConsoleTriangularShadowProcessor(claim, identity)
	default:
		err = fmt.Errorf("shadow_strategy_runtime_unavailable")
	}
	return runtime.processor, runtime.trend, runtime.mean, runtime.triangular, runtime.balances, err
}
