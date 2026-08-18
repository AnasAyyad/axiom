package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
	"axiom/internal/strategies/triangular"
)

type publicShadowProcessorIdentity struct {
	runID       domain.RunID
	portfolioID domain.PortfolioID
	accountID   domain.VirtualAccountID
	capital     domain.Balance
	claim       backtest.JobClaim
	initialAt   domain.EventTime
}

type publicShadowProcessorRuntime struct {
	processor  backtest.Processor
	trend      trend.Configuration
	mean       meanreversion.Configuration
	triangular triangular.Configuration
	balances   portfolio.Snapshot
}

func newPublicShadowProcessorIdentity(claim postgresstore.PublicShadowClaim) (publicShadowProcessorIdentity, error) {
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	accountID, accountErr := domain.NewVirtualAccountID(claim.AccountID)
	capital, capitalErr := domain.ParseBalance(claim.Configuration.Portfolio.StartingCapital.Value)
	if runErr != nil || portfolioErr != nil || accountErr != nil || capitalErr != nil {
		return publicShadowProcessorIdentity{}, fmt.Errorf("shadow_identity_invalid")
	}
	seed := ownerConsoleLocalHash([]byte("shadow-seed:" + claim.ID))
	jobClaim := backtest.JobClaim{ID: claim.ID,
		ExchangeID:    claim.ExchangeID,
		Configuration: claim.Configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "shadow",
			ConfigurationHash: claim.ConfigurationHash, StrategyVersion: claim.StrategyVersion, Seed: seed, Models: claim.Models}}
	return publicShadowProcessorIdentity{runID: runID, portfolioID: portfolioID, accountID: accountID,
		capital: capital, claim: jobClaim,
		initialAt: domain.EventTime{UTC: time.Unix(0, 1).UTC(), Sequence: 1}}, nil
}

func newOwnerConsoleTrendShadowProcessor(claim postgresstore.PublicShadowClaim,
	identity publicShadowProcessorIdentity,
) (publicShadowProcessorRuntime, error) {
	owned, err := portfolio.InitializeTrend(identity.runID, identity.portfolioID, identity.accountID,
		claim.ConfigurationHash, identity.capital, accounting.NewMemoryJournal(), identity.initialAt)
	if err != nil {
		return publicShadowProcessorRuntime{}, err
	}
	processor, err := newOwnerConsoleOperationalProcessorWithPortfolio(identity.claim, owned)
	if err != nil {
		return publicShadowProcessorRuntime{}, err
	}
	configured, err := trend.NewConfiguration(claim.Configuration.Trend)
	return publicShadowProcessorRuntime{processor: processor, trend: configured, balances: owned.Snapshot()}, err
}

func newOwnerConsoleMeanReversionShadowProcessor(claim postgresstore.PublicShadowClaim,
	identity publicShadowProcessorIdentity,
) (publicShadowProcessorRuntime, error) {
	owned, err := portfolio.InitializeMeanReversion(identity.runID, identity.portfolioID, identity.accountID,
		claim.ConfigurationHash, identity.capital, accounting.NewMemoryJournal(), identity.initialAt)
	if err != nil {
		return publicShadowProcessorRuntime{}, err
	}
	processor, err := newOwnerConsoleMeanReversionOperationalProcessorWithPortfolio(identity.claim, owned)
	if err != nil {
		return publicShadowProcessorRuntime{}, err
	}
	configured, err := meanreversion.NewConfiguration(claim.Configuration.MeanReversion)
	return publicShadowProcessorRuntime{processor: processor, mean: configured, balances: owned.Snapshot()}, err
}

func newOwnerConsoleTriangularShadowProcessor(claim postgresstore.PublicShadowClaim,
	identity publicShadowProcessorIdentity,
) (publicShadowProcessorRuntime, error) {
	owned, err := portfolio.InitializeTriangular(identity.runID, identity.portfolioID, identity.accountID,
		claim.ConfigurationHash, identity.capital, claim.ExchangeID, accounting.NewMemoryJournal(), identity.initialAt)
	if err != nil {
		return publicShadowProcessorRuntime{}, err
	}
	processor, err := newOwnerConsoleTriangularOperationalProcessorWithOwnership(identity.claim,
		identity.portfolioID, portfolio.TriangularStrategyOwner)
	if err != nil {
		return publicShadowProcessorRuntime{}, err
	}
	configured, err := triangular.ConfigurationFromReviewed(claim.Configuration.Triangular)
	return publicShadowProcessorRuntime{processor: processor, triangular: configured,
		balances: owned.Snapshot()}, err
}

// Run owns production-public collectors and decision evaluation until stopped.
func (session *ownerConsoleLiveShadowSession) Run(ctx context.Context) error {
	if err := session.loadReferenceData(ctx); err != nil {
		return err
	}
	if err := session.recordShadowActivity(ctx, session.currentShadowActivity(time.Now().UTC())); err != nil {
		return err
	}
	collectors := make([]shadowPublicCollector, 0, len(session.collectors))
	for _, collector := range session.collectors {
		collectors = append(collectors, collector)
	}
	return runPublicShadowCollectors(ctx, collectors, session.flushEvery,
		func(loop context.Context) error {
			return session.recordShadowActivity(loop, session.currentShadowActivity(time.Now().UTC()))
		}, func(loop context.Context, _ exchangecontracts.BookCommit) error {
			started := time.Now()
			evaluateErr := session.evaluateReadyInputs(loop)
			if session.metrics != nil {
				if metricErr := session.metrics.ObserveOperationalReadinessStrategyRisk(
					time.Since(started), time.Now().UTC(),
				); evaluateErr == nil {
					evaluateErr = metricErr
				}
			}
			return evaluateErr
		}, session.FlushAvailable, session.public.FlushRequired(), session.decisions.FlushRequired())
}

func runPublicShadowCollectors(ctx context.Context, collectors []shadowPublicCollector,
	flushEvery time.Duration, activity func(context.Context) error,
	evaluate func(context.Context, exchangecontracts.BookCommit) error, flush func(context.Context) error,
	capacitySignals ...<-chan struct{},
) error {
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	marketUpdates := mergePublicShadowMarketUpdates(workContext, collectors)
	capacityRequired := mergePublicShadowCapacitySignals(workContext, capacitySignals)
	errorsChannel, group := startPublicShadowCollectorGroup(workContext, collectors)
	evaluateTicker := time.NewTicker(500 * time.Millisecond)
	flushTicker := time.NewTicker(flushEvery)
	defer evaluateTicker.Stop()
	defer flushTicker.Stop()
	for {
		select {
		case <-workContext.Done():
			group.Wait()
			return nil
		case err := <-errorsChannel:
			if err != nil {
				cancel()
				group.Wait()
				return err
			}
		case update := <-marketUpdates:
			if err := evaluate(workContext, update); err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-evaluateTicker.C:
			if err := activity(workContext); err != nil {
				cancel()
				group.Wait()
				return err
			}
			if err := evaluate(workContext, exchangecontracts.BookCommit{}); err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-flushTicker.C:
			if err := flush(workContext); err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-capacityRequired:
			if err := flush(workContext); err != nil {
				cancel()
				group.Wait()
				return err
			}
		}
	}
}

// mergePublicShadowCapacitySignals turns each recorder's edge-coalesced
// capacity notification into one session-level flush request. The recorder
// keeps its own hard memory bound; this path persists a complete prefix well
// before that bound is reached instead of waiting for the periodic RPO tick.
func mergePublicShadowCapacitySignals(ctx context.Context, signals []<-chan struct{}) <-chan struct{} {
	merged := make(chan struct{}, 1)
	for _, signal := range signals {
		if signal == nil {
			continue
		}
		go func(source <-chan struct{}) {
			for {
				select {
				case <-ctx.Done():
					return
				case _, open := <-source:
					if !open {
						return
					}
					select {
					case merged <- struct{}{}:
					default:
					}
				}
			}
		}(signal)
	}
	return merged
}

func startPublicShadowCollectorGroup(ctx context.Context,
	collectors []shadowPublicCollector,
) (<-chan error, *sync.WaitGroup) {
	errorsChannel := make(chan error, len(collectors))
	group := &sync.WaitGroup{}
	for _, collector := range collectors {
		group.Add(1)
		go func(value shadowPublicCollector) {
			defer group.Done()
			errorsChannel <- value.Run(ctx)
		}(collector)
	}
	return errorsChannel, group
}

type shadowPublicMarketUpdateSource interface {
	MarketUpdates() <-chan struct{}
	LatestBookCommit() exchangecontracts.BookCommit
}

func mergePublicShadowMarketUpdates(ctx context.Context, collectors []shadowPublicCollector) <-chan exchangecontracts.BookCommit {
	merged := make(chan exchangecontracts.BookCommit, len(collectors))
	for _, collector := range collectors {
		source, ok := collector.(shadowPublicMarketUpdateSource)
		if !ok {
			continue
		}
		updates := source.MarketUpdates()
		if updates == nil {
			continue
		}
		go func(source shadowPublicMarketUpdateSource, updates <-chan struct{}) {
			for {
				select {
				case <-ctx.Done():
					return
				case _, open := <-updates:
					if !open {
						return
					}
					commit := source.LatestBookCommit()
					if commit.Validate() == nil {
						select {
						case merged <- commit:
						default:
						}
					}
				}
			}
		}(source, updates)
	}
	return merged
}

func (session *ownerConsoleLiveShadowSession) loadReferenceData(ctx context.Context) error {
	instruments := make([]domain.Instrument, 0, len(session.collectors))
	for instrument := range session.collectors {
		instruments = append(instruments, instrument)
	}
	sort.Slice(instruments, func(left, right int) bool { return instruments[left].Symbol() < instruments[right].Symbol() })
	records, err := session.client.Instruments(ctx, instruments)
	if err != nil {
		return publicShadowPublicDataError("shadow_metadata_unavailable", err)
	}
	if err = session.registerShadowReferenceData(ctx, instruments, records); err != nil {
		return err
	}
	if err = session.recordShadowActivity(ctx, session.warmingShadowActivity(time.Now().UTC())); err != nil {
		return err
	}
	for _, instrument := range instruments {
		if err = session.loadShadowWarmup(ctx, instrument); err != nil {
			return err
		}
	}
	return nil
}

func (session *ownerConsoleLiveShadowSession) registerShadowReferenceData(ctx context.Context,
	instruments []domain.Instrument, records []exchangecontracts.InstrumentRecord,
) error {
	if len(records) != len(instruments) {
		return fmt.Errorf("shadow_metadata_count_invalid")
	}
	for _, record := range records {
		if string(record.Exchange) != session.claim.ExchangeID || session.collectors[record.Metadata.Instrument] == nil ||
			session.metadata[record.Metadata.Instrument].Version != 0 {
			return fmt.Errorf("shadow_metadata_membership_invalid")
		}
		evidence, err := session.store.RegisterPublicInstrument(ctx, record)
		if err != nil {
			return err
		}
		session.metadata[evidence.Metadata.Instrument] = evidence.Metadata
		session.metadataIDs[evidence.Metadata.Instrument] = evidence.ID
		session.maximumQuantity[evidence.Metadata.Instrument] = evidence.MaximumQuantity
	}
	return nil
}

func (session *ownerConsoleLiveShadowSession) loadShadowWarmup(ctx context.Context,
	instrument domain.Instrument,
) error {
	switch session.claim.StrategyID {
	case "trend-following-1-0-0":
		candles, err := session.loadShadowCandles(ctx, instrument, "4h", 4*time.Hour)
		if err != nil {
			return err
		}
		if len(candles) < session.trendConfig.EMARegime {
			return fmt.Errorf("shadow_candle_history_insufficient")
		}
		session.history[instrument] = candles
		return nil
	case "mean-reversion-1-0-0":
		return session.loadMeanReversionShadowWarmup(ctx, instrument)
	case "triangular-arbitrage-1-0-0":
		return nil
	default:
		return fmt.Errorf("shadow_strategy_runtime_unavailable")
	}
}

func (session *ownerConsoleLiveShadowSession) loadMeanReversionShadowWarmup(ctx context.Context,
	instrument domain.Instrument,
) error {
	primary, primaryErr := session.loadShadowCandles(ctx, instrument,
		session.meanConfig.PrimaryTimeframe, time.Hour)
	higher, higherErr := session.loadShadowCandles(ctx, instrument,
		session.meanConfig.HigherTimeframe, 4*time.Hour)
	minimumPrimary := session.meanConfig.ZScorePeriod
	if session.meanConfig.ADXPeriod*2 > minimumPrimary {
		minimumPrimary = session.meanConfig.ADXPeriod * 2
	}
	minimumHigher := session.meanConfig.EMARegimePeriod + session.meanConfig.EMADeclineLookback
	if primaryErr != nil {
		return primaryErr
	}
	if higherErr != nil {
		return higherErr
	}
	if len(primary) < minimumPrimary || len(higher) < minimumHigher {
		return fmt.Errorf("shadow_candle_history_insufficient")
	}
	session.primaryHistory[instrument], session.higherHistory[instrument] = primary, higher
	return nil
}

func (session *ownerConsoleLiveShadowSession) loadShadowCandles(ctx context.Context, instrument domain.Instrument,
	interval string, duration time.Duration) ([]exchangecontracts.Candle, error) {
	end := time.Now().UTC()
	candles, err := session.client.Candles(ctx, exchangecontracts.CandleRequest{
		HistoryRequest: exchangecontracts.HistoryRequest{Instrument: instrument,
			Start: end.Add(-1000 * duration), End: end, Limit: 1000},
		Interval: interval})
	if err != nil {
		return nil, publicShadowPublicDataError("shadow_candle_history_unavailable", err)
	}
	return candles, nil
}

func publicShadowPublicDataError(code string, err error) error {
	cause, status, _, metadata := exchangecontracts.DiagnosticOf(err)
	if cause == "" {
		cause = "unspecified"
	}
	return fmt.Errorf("%s kind=%s cause=%s status=%d stage=%s",
		code, exchangecontracts.KindOf(err), cause, status, metadata.SetupStage)
}
