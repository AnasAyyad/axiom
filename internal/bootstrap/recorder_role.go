package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/pressure"

	"github.com/jackc/pgx/v5/pgxpool"
)

type recorderRoleWork struct {
	client          *binance.PublicClient
	collectors      map[domain.Instrument]*binance.InstrumentCollector
	recorder        *marketrecorder.Recorder
	bybitClient     *bybit.PublicClient
	bybitCollectors map[domain.Instrument]*bybit.InstrumentCollector
	bybitRecorder   *marketrecorder.Recorder
	catalog         *postgresstore.RecordedDatasetCatalog
	metadata        *postgresstore.PublicShadowStore
	commit          string
	flush           time.Duration
	pressurePolicy  pressure.Policy
	pressureStore   storagePressureWriter
	pressureProbe   func(string, time.Time) (pressure.Observation, error)
	root            string
	session         string
	rotationControl *postgresstore.EvaluationRecorderControlStore
	startupRotation *postgresstore.EvaluationRecorderRotation
}

type recorderRoleResumeState struct {
	binanceRoot, bybitRoot                          string
	lastOrdinal, binanceGeneration, bybitGeneration uint64
	binanceFound, bybitFound                        bool
}

func newRecorderRoleWork(ctx context.Context, pool *pgxpool.Pool, runtimeConfig config.Runtime,
	product config.Configuration, clock domain.Clock) (*recorderRoleWork, error) {
	if err := os.MkdirAll(runtimeConfig.Recorder.Root, 0o750); err != nil {
		return nil, fmt.Errorf("recorder_root_unavailable")
	}
	exchanges := product.PublicExchanges()
	rotationControl, rotation, campaignSession, session, err := evaluationRecorderStartup(ctx, pool,
		runtimeConfig.InstanceID, clock)
	if err != nil {
		return nil, err
	}
	resume, err := loadRecorderRoleResume(runtimeConfig.Recorder.Root, session, len(exchanges))
	if err != nil {
		return nil, err
	}
	if campaignSession && rotation.State == "ACTIVE" && rotation.ValidSeconds > 0 &&
		(!resume.binanceFound || (len(exchanges) == 2 && !resume.bybitFound)) {
		_ = rotationControl.Block(ctx, rotation.CampaignID, evaluation.ReasonPersistenceFailed)
		return nil, fmt.Errorf("evaluation_recorder_resume_evidence_missing")
	}
	work, ordinals, monotonic, err := newPrimaryRecorderRole(pool, runtimeConfig, exchanges, session, resume, clock)
	if err != nil {
		return nil, err
	}
	work.rotationControl = rotationControl
	if err = configureRecorderPressure(work, pool, runtimeConfig.InstanceID); err != nil {
		return nil, err
	}
	if len(exchanges) == 2 {
		if err = work.addBybit(runtimeConfig.InstanceID, runtimeConfig.Recorder, exchanges[1], session, ordinals,
			pool, clock, monotonic, resume.bybitFound, resume.bybitGeneration); err != nil {
			return nil, err
		}
	}
	if campaignSession {
		work.startupRotation = &rotation
	}
	return work, nil
}

func evaluationRecorderStartup(ctx context.Context, pool *pgxpool.Pool, instance string,
	clock domain.Clock) (*postgresstore.EvaluationRecorderControlStore, postgresstore.EvaluationRecorderRotation,
	bool, string, error) {
	var control *postgresstore.EvaluationRecorderControlStore
	var rotation postgresstore.EvaluationRecorderRotation
	campaignSession := false
	var err error
	if pool != nil {
		control, err = postgresstore.NewEvaluationRecorderControlStore(pool, clock)
		if err == nil {
			rotation, campaignSession, err = control.StartupRotation(ctx)
		}
		if err != nil {
			return nil, rotation, false, "", err
		}
	}
	session := recorderSession(instance, time.Now().UTC())
	if campaignSession {
		session = rotation.DesiredSessionID
	}
	return control, rotation, campaignSession, session, nil
}

func loadRecorderRoleResume(root, session string, exchangeCount int) (recorderRoleResumeState, error) {
	state := recorderRoleResumeState{binanceRoot: recorderExchangeRoot(root, "binance", exchangeCount),
		bybitRoot: recorderExchangeRoot(root, "bybit", exchangeCount)}
	var err error
	state.lastOrdinal, state.binanceGeneration, state.bybitGeneration, state.binanceFound,
		state.bybitFound, err = recorderResumeHighWater(state.binanceRoot, state.bybitRoot, session,
		exchangeCount == 2)
	return state, err
}

func newPrimaryRecorderRole(pool *pgxpool.Pool, runtimeConfig config.Runtime,
	exchanges []config.ExchangeConfiguration, session string, resume recorderRoleResumeState,
	clock domain.Clock) (*recorderRoleWork, *runtimecore.IngestOrdinals, exchangecontracts.MonotonicSource, error) {
	monotonic := exchangecontracts.NewProcessMonotonicSource()
	client, err := binance.NewRecorderPublicClientWithMonotonic(exchanges[0].EndpointSet, clock, monotonic)
	if err != nil {
		return nil, nil, nil, err
	}
	if resume.binanceGeneration > 0 {
		if err = client.RestoreStreamGeneration(resume.binanceGeneration); err != nil {
			return nil, nil, nil, err
		}
	}
	ordinals, err := runtimecore.NewIngestOrdinalsAfter(resume.lastOrdinal)
	if err != nil {
		return nil, nil, nil, err
	}
	streamRecorder, err := newBinanceStreamRecorder(resume.binanceRoot, session, runtimeConfig,
		len(exchanges), ordinals, pool, resume.binanceFound)
	if err != nil {
		return nil, nil, nil, err
	}
	catalog, metadataStore, err := newRecorderStores(pool, runtimeConfig.InstanceID, clock)
	if err != nil {
		return nil, nil, nil, err
	}
	sink, err := marketrecorder.NewBinanceStreamSink(streamRecorder)
	if err != nil {
		return nil, nil, nil, err
	}
	collectors, err := newBinanceCollectors(exchanges[0], runtimeConfig.Recorder, client, sink, clock)
	if err != nil {
		return nil, nil, nil, err
	}
	work := &recorderRoleWork{client: client, collectors: collectors, recorder: streamRecorder,
		catalog: catalog, metadata: metadataStore, commit: buildinfo.Current().Commit,
		flush: runtimeConfig.Recorder.FlushInterval, root: runtimeConfig.Recorder.Root, session: session,
		pressurePolicy: pressure.Policy{HighFreeBytes: runtimeConfig.Recorder.HighFreeBytes,
			CriticalFreeBytes: runtimeConfig.Recorder.CriticalFreeBytes,
			SampleInterval:    runtimeConfig.Recorder.PressureInterval}}
	return work, ordinals, monotonic, nil
}

func newRecorderStores(pool *pgxpool.Pool, instance string,
	clock domain.Clock) (*postgresstore.RecordedDatasetCatalog, *postgresstore.PublicShadowStore, error) {
	if pool == nil {
		return nil, nil, nil
	}
	catalog, err := postgresstore.NewRecordedDatasetCatalog(pool)
	if err != nil {
		return nil, nil, err
	}
	metadata, err := postgresstore.NewPublicShadowStore(pool, instance, clock)
	if err != nil {
		return nil, nil, err
	}
	return catalog, metadata, nil
}

func (work *recorderRoleWork) addBybit(
	instance string,
	runtimeConfig config.RecorderRuntime,
	exchange config.ExchangeConfiguration,
	session string,
	ordinals *runtimecore.IngestOrdinals,
	pool *pgxpool.Pool,
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
	resume bool,
	lastGeneration uint64,
) error {
	client, err := bybit.NewPublicClientWithMonotonic(exchange.EndpointSet, clock, monotonic)
	if err != nil {
		return err
	}
	if lastGeneration > 0 {
		if err = client.RestoreStreamGeneration(lastGeneration); err != nil {
			return err
		}
	}
	profile := marketrecorder.CollectorProfile{Instance: instance,
		Region: runtimeConfig.CollectorRegion, MinimumReaderVersion: "dataset-reader.v2"}
	var recorder *marketrecorder.Recorder
	if resume {
		recorder, _, err = marketrecorder.ResumeCoherentMarketData(filepath.Join(runtimeConfig.Root, "bybit"),
			bybitRecorderDatasetID(session), session+"-bybit", "bybit", ordinals,
			segmentCommitter(pool, session+"-bybit", "bybit"), nil, profile)
	} else {
		recorder, err = marketrecorder.NewCoherentMarketData(filepath.Join(runtimeConfig.Root, "bybit"),
			bybitRecorderDatasetID(session), session+"-bybit", "bybit", ordinals,
			segmentCommitter(pool, session+"-bybit", "bybit"), nil, profile)
	}
	if err != nil {
		return err
	}
	sink, err := marketrecorder.NewPublicStreamSink(recorder,
		"bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		return err
	}
	collectors, err := newBybitCollectors(exchange, runtimeConfig, client, sink, clock)
	if err != nil {
		return err
	}
	work.bybitClient, work.bybitRecorder, work.bybitCollectors = client, recorder, collectors
	return nil
}

func newBinanceCollectors(exchange config.ExchangeConfiguration, runtimeConfig config.RecorderRuntime,
	client *binance.PublicClient, sink exchangecontracts.PublicRecorder, clock domain.Clock,
) (map[domain.Instrument]*binance.InstrumentCollector, error) {
	collectors := make(map[domain.Instrument]*binance.InstrumentCollector, len(exchange.Instruments))
	for _, configured := range exchange.Instruments {
		base, baseErr := domain.ParseAssetSymbol(configured.Base)
		quote, quoteErr := domain.ParseAssetSymbol(configured.Quote)
		instrument, instrumentErr := domain.NewSpotInstrument(base, quote)
		if baseErr != nil || quoteErr != nil || instrumentErr != nil {
			return nil, fmt.Errorf("recorder_instrument_invalid")
		}
		collectorConfig := binance.DefaultCollectorConfig(instrument)
		collectorConfig.BookDepth = runtimeConfig.BookDepth
		collectorConfig.QueueCapacity = runtimeConfig.QueueCapacity
		collectorConfig.CandleIntervals = append([]string(nil), exchange.CandleIntervals...)
		collector, collectorErr := binance.NewInstrumentCollector(collectorConfig, client, sink, clock)
		if collectorErr != nil {
			return nil, collectorErr
		}
		collectors[instrument] = collector
	}
	if len(collectors) != len(exchange.Instruments) {
		return nil, fmt.Errorf("recorder_universe_invalid")
	}
	return collectors, nil
}

func newBybitCollectors(exchange config.ExchangeConfiguration, runtimeConfig config.RecorderRuntime,
	client *bybit.PublicClient, sink exchangecontracts.PublicRecorder, clock domain.Clock,
) (map[domain.Instrument]*bybit.InstrumentCollector, error) {
	collectors := make(map[domain.Instrument]*bybit.InstrumentCollector, len(exchange.Instruments))
	for _, configured := range exchange.Instruments {
		base, baseErr := domain.ParseAssetSymbol(configured.Base)
		quote, quoteErr := domain.ParseAssetSymbol(configured.Quote)
		instrument, instrumentErr := domain.NewSpotInstrument(base, quote)
		if baseErr != nil || quoteErr != nil || instrumentErr != nil {
			return nil, fmt.Errorf("recorder_instrument_invalid")
		}
		collectorConfig := bybit.DefaultCollectorConfig(instrument)
		collectorConfig.BookDepth = runtimeConfig.BookDepth
		collectorConfig.QueueCapacity = runtimeConfig.QueueCapacity
		collectorConfig.CandleIntervals = append([]string(nil), exchange.CandleIntervals...)
		collector, collectorErr := bybit.NewInstrumentCollector(collectorConfig, client, sink, clock)
		if collectorErr != nil {
			return nil, collectorErr
		}
		collectors[instrument] = collector
	}
	if len(collectors) != 3 {
		return nil, fmt.Errorf("recorder_universe_invalid")
	}
	return collectors, nil
}

// Run owns collector and flush lifecycles until cancellation or a fatal defect.
