package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/observability"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errPublicShadowCrossExchangeMarketInputUnavailable = errors.New("shadow_cross_exchange_market_input_unavailable")

type ownerConsoleCrossExchangeShadowSession struct {
	claim          postgresstore.PublicShadowClaim
	store          *postgresstore.PublicShadowStore
	clients        map[string]shadowPublicClient
	collectors     map[runtimecore.MarketKey]shadowPublicCollector
	public         map[string]*marketrecorder.Recorder
	decisions      *marketrecorder.Recorder
	catalog        *postgresstore.RecordedDatasetCatalog
	processor      backtest.Processor
	configuration  crossarb.Configuration
	flushEvery     time.Duration
	commit         string
	entries        atomic.Bool
	flushMutex     sync.Mutex
	stateMutex     sync.Mutex
	activityMutex  sync.Mutex
	lastActivity   string
	lastActivityAt time.Time
	lastViewID     string
	lastTrigger    map[string]exchangecontracts.BookCommit
	coherenceStats crossExchangeCoherenceStatistics
	datasetID      string
	metrics        *observability.Metrics
	lastOrdinal    uint64
	metadata       map[runtimecore.MarketKey]domain.InstrumentMetadata
	maximum        map[runtimecore.MarketKey]domain.Quantity
	balances       map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot
}

func newOwnerConsoleCrossExchangeShadowSession(
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	store *postgresstore.PublicShadowStore,
	claim postgresstore.PublicShadowClaim,
) (*ownerConsoleCrossExchangeShadowSession, error) {
	if claim.StrategyID != "cross-exchange-arbitrage-1-0-0" || len(claim.MarketScopes) != 2 ||
		len(claim.VenueAccountIDs) != 2 || store == nil {
		return nil, fmt.Errorf("shadow_cross_exchange_composition_invalid")
	}
	if err := os.MkdirAll(runtimeConfig.Recorder.Root, 0o750); err != nil {
		return nil, fmt.Errorf("shadow_recorder_root_unavailable")
	}
	configuration, err := crossarb.ConfigurationFromReviewed(claim.Configuration.CrossExchange)
	if err != nil {
		return nil, fmt.Errorf("shadow_cross_exchange_configuration_invalid")
	}
	clients, collectors, publicRecorders, err := newOwnerConsoleCrossExchangePublicRuntime(claim,
		runtimeConfig.Recorder, &domain.SystemClock{}, pool)
	if err != nil {
		return nil, err
	}
	decisionRecorder, catalog, err := newOwnerConsoleCrossExchangeDecisionRecorder(pool,
		runtimeConfig.Recorder.Root, claim.ID)
	if err != nil {
		return nil, err
	}
	processor, err := newOwnerConsoleCrossExchangeShadowProcessor(claim)
	if err != nil {
		return nil, err
	}
	session := &ownerConsoleCrossExchangeShadowSession{claim: claim, store: store, clients: clients,
		collectors: collectors, public: publicRecorders, decisions: decisionRecorder, catalog: catalog,
		processor: processor, configuration: configuration, flushEvery: runtimeConfig.Recorder.FlushInterval,
		commit: claimConfigurationCommit(), metadata: make(map[runtimecore.MarketKey]domain.InstrumentMetadata, 2),
		maximum:        make(map[runtimecore.MarketKey]domain.Quantity, 2),
		lastTrigger:    make(map[string]exchangecontracts.BookCommit, 2),
		coherenceStats: newCrossExchangeCoherenceStatistics()}
	if session.commit == "" || session.flushEvery <= 0 {
		return nil, fmt.Errorf("shadow_cross_exchange_build_identity_invalid")
	}
	_ = ctx
	return session, nil
}

func newOwnerConsoleCrossExchangeShadowProcessor(claim postgresstore.PublicShadowClaim) (backtest.Processor, error) {
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("shadow_cross_exchange_identity_invalid")
	}
	jobClaim := backtest.JobClaim{ID: claim.ID, ExchangeID: "paired-public",
		Configuration: claim.Configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "shadow",
			ConfigurationHash: claim.ConfigurationHash, StrategyVersion: claim.StrategyVersion,
			Seed: ownerConsoleLocalHash([]byte("shadow-seed:" + claim.ID)), Models: claim.Models}}
	return newOwnerConsoleCrossExchangeOperationalProcessorWithOwnership(jobClaim, portfolioID, "cross_exchange")
}

func newOwnerConsoleCrossExchangePublicRuntime(
	claim postgresstore.PublicShadowClaim,
	runtimeConfig config.RecorderRuntime,
	clock domain.Clock,
	pool *pgxpool.Pool,
) (map[string]shadowPublicClient, map[runtimecore.MarketKey]shadowPublicCollector,
	map[string]*marketrecorder.Recorder, error) {
	keys, err := ownerConsoleCrossExchangeMarketKeys(claim)
	if err != nil {
		return nil, nil, nil, err
	}
	configured := make(map[string]config.ExchangeConfiguration, 2)
	for _, venue := range claim.Configuration.PublicExchanges() {
		configured[venue.ID] = venue
	}
	monotonic := exchangecontracts.NewProcessMonotonicSource()
	ordinals := &runtimecore.IngestOrdinals{}
	clients := make(map[string]shadowPublicClient, 2)
	collectors := make(map[runtimecore.MarketKey]shadowPublicCollector, 2)
	recorders := make(map[string]*marketrecorder.Recorder, 2)
	for _, key := range keys {
		venue, exists := configured[key.Exchange]
		if !exists {
			return nil, nil, nil, fmt.Errorf("shadow_exchange_configuration_missing")
		}
		client, collector, recorder, memberErr := newOwnerConsoleCrossExchangePublicMember(
			claim, runtimeConfig, clock, pool, ordinals, monotonic, key, venue)
		if memberErr != nil {
			return nil, nil, nil, memberErr
		}
		clients[key.Exchange], collectors[key], recorders[key.Exchange] = client, collector, recorder
	}
	if len(clients) != 2 || len(collectors) != 2 || len(recorders) != 2 {
		return nil, nil, nil, fmt.Errorf("shadow_cross_exchange_runtime_incomplete")
	}
	return clients, collectors, recorders, nil
}

func newOwnerConsoleCrossExchangePublicMember(claim postgresstore.PublicShadowClaim,
	runtime config.RecorderRuntime, clock domain.Clock, pool *pgxpool.Pool,
	ordinals *runtimecore.IngestOrdinals, monotonic exchangecontracts.MonotonicSource,
	key runtimecore.MarketKey, venue config.ExchangeConfiguration,
) (shadowPublicClient, shadowPublicCollector, *marketrecorder.Recorder, error) {
	sessionID := claim.ID + "-public-" + key.Exchange
	recorder, err := marketrecorder.NewCoherentMarketData(filepath.Join(runtime.Root, claim.ID, key.Exchange),
		claim.ID+"-public-evidence-"+key.Exchange, sessionID, key.Exchange, ordinals,
		segmentCommitter(pool, sessionID, key.Exchange), nil, marketrecorder.CollectorProfile{
			Instance: "owner_console-shadow-" + claim.ID, Region: runtime.CollectorRegion,
			MinimumReaderVersion: "dataset-reader.v2"})
	if err != nil {
		return nil, nil, nil, err
	}
	client, collector, err := newOwnerConsoleCrossExchangePublicCollector(key, venue, runtime, recorder, clock, monotonic)
	return client, collector, recorder, err
}

func newOwnerConsoleCrossExchangePublicCollector(key runtimecore.MarketKey, venue config.ExchangeConfiguration,
	runtime config.RecorderRuntime, recorder *marketrecorder.Recorder, clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
) (shadowPublicClient, shadowPublicCollector, error) {
	switch key.Exchange {
	case "binance":
		client, err := binance.NewRecorderPublicClientWithMonotonic(venue.EndpointSet, clock, monotonic)
		if err != nil {
			return nil, nil, err
		}
		sink, err := marketrecorder.NewBinanceStreamSink(recorder)
		if err != nil {
			return nil, nil, err
		}
		configured, err := newBinanceCollectors(venue, runtime, client, sink, clock)
		if err != nil || configured[key.Instrument] == nil {
			return nil, nil, fmt.Errorf("shadow_cross_exchange_collector_invalid")
		}
		return client, configured[key.Instrument], nil
	case "bybit":
		client, err := bybit.NewPublicClientWithMonotonic(venue.EndpointSet, clock, monotonic)
		if err != nil {
			return nil, nil, err
		}
		sink, err := marketrecorder.NewPublicStreamSink(recorder,
			"bybit-public-parser.v1", "bybit-public-normalizer.v1")
		if err != nil {
			return nil, nil, err
		}
		configured, err := newBybitCollectors(venue, runtime, client, sink, clock)
		if err != nil || configured[key.Instrument] == nil {
			return nil, nil, fmt.Errorf("shadow_cross_exchange_collector_invalid")
		}
		return client, configured[key.Instrument], nil
	default:
		return nil, nil, fmt.Errorf("shadow_exchange_invalid")
	}
}

func newOwnerConsoleCrossExchangeDecisionRecorder(pool *pgxpool.Pool, root, id string) (
	*marketrecorder.Recorder, *postgresstore.RecordedDatasetCatalog, error) {
	catalog, err := postgresstore.NewRecordedDatasetCatalog(pool)
	if err != nil {
		return nil, nil, err
	}
	sessionID := id + "-decisions"
	recorder, err := marketrecorder.New(filepath.Join(root, id, "decisions"), id+"-decision-inputs",
		sessionID, "binance", &runtimecore.IngestOrdinals{}, segmentCommitter(pool, sessionID, "binance"), nil)
	return recorder, catalog, err
}

func ownerConsoleCrossExchangeMarketKeys(claim postgresstore.PublicShadowClaim) ([]runtimecore.MarketKey, error) {
	if claim.StrategyID != "cross-exchange-arbitrage-1-0-0" || claim.ExchangeID != "binance" ||
		len(claim.MarketScopes) != 2 {
		return nil, fmt.Errorf("shadow_cross_exchange_market_scope_invalid")
	}
	keys := make([]runtimecore.MarketKey, 0, 2)
	for index, exchange := range []string{"binance", "bybit"} {
		scope := claim.MarketScopes[index]
		instrument, err := sandboxSagaInstrument(scope.InstrumentID)
		if err != nil || scope.Ordinal != int16(index+1) || scope.ExchangeID != exchange ||
			scope.InstrumentID != claim.InstrumentID || scope.Purpose != "paired_market" {
			return nil, fmt.Errorf("shadow_cross_exchange_market_scope_invalid")
		}
		keys = append(keys, runtimecore.MarketKey{Exchange: exchange, Instrument: instrument})
	}
	return keys, nil
}

// Run consumes coherent public venue generations until the session stops.
func (session *ownerConsoleCrossExchangeShadowSession) Run(ctx context.Context) error {
	if err := session.loadReferenceData(ctx); err != nil {
		return err
	}
	if err := session.recordActivity(ctx, session.currentActivity(time.Now().UTC())); err != nil {
		return err
	}
	collectors := make([]shadowPublicCollector, 0, len(session.collectors))
	for _, collector := range session.collectors {
		collectors = append(collectors, collector)
	}
	return runPublicShadowCollectors(ctx, collectors, session.flushEvery,
		func(loop context.Context) error {
			return session.recordActivity(loop, session.currentActivity(time.Now().UTC()))
		}, func(loop context.Context, trigger exchangecontracts.BookCommit) error {
			started := time.Now()
			evaluateErr := session.evaluateReadyInput(loop, trigger)
			if session.metrics != nil {
				if metricErr := session.metrics.ObserveOperationalReadinessStrategyRisk(
					time.Since(started), time.Now().UTC(),
				); evaluateErr == nil {
					evaluateErr = metricErr
				}
			}
			return evaluateErr
		}, session.FlushAvailable)
}

func (session *ownerConsoleCrossExchangeShadowSession) loadReferenceData(ctx context.Context) error {
	keys, _ := ownerConsoleCrossExchangeMarketKeys(session.claim)
	for _, key := range keys {
		records, err := session.clients[key.Exchange].Instruments(ctx, []domain.Instrument{key.Instrument})
		if err != nil {
			return publicShadowPublicDataError("shadow_metadata_unavailable", err)
		}
		if len(records) != 1 || string(records[0].Exchange) != key.Exchange ||
			records[0].Metadata.Instrument != key.Instrument {
			return fmt.Errorf("shadow_metadata_membership_invalid")
		}
		evidence, err := session.store.RegisterPublicInstrument(ctx, records[0])
		if err != nil {
			return err
		}
		session.metadata[key], session.maximum[key] = evidence.Metadata, evidence.MaximumQuantity
	}
	return session.recordActivity(ctx, session.warmingActivity(time.Now().UTC()))
}

type ownerConsoleCrossExchangeMarketSource struct {
	session *ownerConsoleCrossExchangeShadowSession
	trigger exchangecontracts.BookCommit
}

// CaptureSandboxSagaMarketViews returns one complete coherent public generation.
func (source *ownerConsoleCrossExchangeMarketSource) CaptureSandboxSagaMarketViews(ctx context.Context,
	keys []runtimecore.MarketKey, now time.Time,
) (SandboxSagaMarketViewSet, error) {
	session := source.session
	if session == nil || ctx == nil || len(keys) != 2 || len(session.collectors) != 2 ||
		source.trigger.Validate() != nil {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("shadow_cross_exchange_market_capture_invalid")
	}
	feeRate, err := publicShadowFeeRate(session.claim.Configuration.Models.Fee)
	if err != nil {
		return SandboxSagaMarketViewSet{}, err
	}
	result, maximumOrdinal, triggerMatched, err := source.captureCrossExchangeMembers(keys, feeRate)
	if err != nil {
		return SandboxSagaMarketViewSet{}, err
	}
	logical := session.clients["binance"].MonotonicOffset()
	if !triggerMatched || maximumOrdinal == 0 || logical == 0 || result.FirstDetectedOffset == 0 ||
		result.FirstDetectedOffset > logical {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("shadow_cross_exchange_market_capture_invalid")
	}
	result.Trigger = runtimecore.AsOfTrigger{MonotonicNanos: logical, IngestOrdinal: maximumOrdinal, UTC: now}
	return result, nil
}

func (source *ownerConsoleCrossExchangeMarketSource) captureCrossExchangeMembers(
	keys []runtimecore.MarketKey, feeRate domain.Rate,
) (SandboxSagaMarketViewSet, uint64, bool, error) {
	session := source.session
	result := SandboxSagaMarketViewSet{Members: make([]SandboxSagaMarketMember, 0, 2)}
	maximumOrdinal := uint64(0)
	triggerMatched := false
	for _, key := range keys {
		collector := session.collectors[key]
		view, viewErr := collectorBook(collector, key)
		health := exchangecontracts.CollectorHealthSnapshot{}
		if collector != nil {
			health = collector.HealthSnapshot()
		}
		metadata, metadataFound := session.metadata[key]
		maximum, maximumFound := session.maximum[key]
		if viewErr != nil || !metadataFound || !maximumFound || !health.Eligible ||
			health.Exchange != key.Exchange || health.Instrument != key.Instrument.Symbol() {
			return SandboxSagaMarketViewSet{}, 0, false,
				fmt.Errorf("shadow_cross_exchange_market_member_unavailable")
		}
		if key.Exchange == source.trigger.Exchange && key.Instrument == source.trigger.Instrument {
			triggerMatched = sameBookCommitView(source.trigger, view)
		}
		if ordinal := view.Observation().IngestOrdinal; ordinal > maximumOrdinal {
			maximumOrdinal = ordinal
		}
		if published := view.Observation().PublishedOffsetNanos; published > result.FirstDetectedOffset {
			result.FirstDetectedOffset = published
		}
		result.Members = append(result.Members, SandboxSagaMarketMember{View: view,
			Clock: exchangecontracts.ClockHealth{ObservedAt: health.ClockObservedAt, Offset: health.ClockOffset,
				Uncertainty: health.ClockUncertainty, Eligible: health.ClockEligible},
			Rules: arbitrage.InstrumentRules{Exchange: key.Exchange, Metadata: metadata,
				MaximumQuantity: maximum, Fee: arbitrage.FeeSchedule{Version: session.claim.Configuration.Models.Fee,
					Rate: feeRate, Asset: domain.AssetSymbol("USDT")}, Active: true, ObservedAt: metadata.EffectiveAt},
			CollectorInstance: "owner_console-shadow-" + session.claim.ID + "-" + key.Exchange,
			CollectorRegion:   "engine-local"})
	}
	return result, maximumOrdinal, triggerMatched, nil
}

func (session *ownerConsoleCrossExchangeShadowSession) captureMarket(ctx context.Context,
	now time.Time, trigger exchangecontracts.BookCommit,
) (SandboxCrossExchangeMarketInput, error) {
	keys, err := ownerConsoleCrossExchangeMarketKeys(session.claim)
	if err != nil {
		return SandboxCrossExchangeMarketInput{}, err
	}
	source := &ownerConsoleCrossExchangeMarketSource{session: session, trigger: trigger}
	set, err := source.CaptureSandboxSagaMarketViews(ctx, keys, now)
	if err != nil {
		session.observeCrossExchangeComparison(crossExchangeCoherenceComparison{Trigger: trigger,
			Strict: crossExchangeCoherenceVerdict{PolicyVersion: runtimecore.InitialCoherentMarketDataCoherentPolicy().Version,
				Reason: "capture_failure"},
			Actionable: crossExchangeCoherenceVerdict{PolicyVersion: runtimecore.InitialCrossExchangeActionablePolicy().Version,
				Reason: "capture_failure"}})
		return SandboxCrossExchangeMarketInput{}, errPublicShadowCrossExchangeMarketInputUnavailable
	}
	capture, comparison := compareCrossExchangeCapture(ctx, keys, now, trigger, set)
	session.observeCrossExchangeComparison(comparison)
	if !comparison.Strict.Passed {
		return SandboxCrossExchangeMarketInput{}, errPublicShadowCrossExchangeMarketInputUnavailable
	}
	markets := make([]crossarb.MarketInput, 0, 2)
	for _, member := range capture.members {
		markets = append(markets, crossarb.MarketInput{Snapshot: member.snapshot,
			Observation: member.view.Observation(), Rules: member.rules})
	}
	view := capture.coherent
	return SandboxCrossExchangeMarketInput{Markets: markets,
		Coherent: crossarb.CoherentViewInput{Identity: view.Identity(), Policy: view.Policy(),
			Trigger: view.Trigger(), Members: view.Members()}, Trigger: capture.trigger,
		InstrumentMetadataSetHash: sandboxSagaMarketEvidenceHash(view.Identity(), capture.rules)}, nil
}
