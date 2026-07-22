package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	postgresgenerated "axiom/internal/storage/postgres/generated"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recorderRoleWork struct {
	client          *binance.PublicClient
	collectors      map[domain.Instrument]*binance.InstrumentCollector
	recorder        *marketrecorder.Recorder
	bybitClient     *bybit.PublicClient
	bybitCollectors map[domain.Instrument]*bybit.InstrumentCollector
	bybitRecorder   *marketrecorder.Recorder
	catalog         *postgresstore.A11DatasetCatalog
	metadata        *postgresstore.A11ShadowStore
	commit          string
	flush           time.Duration
}

func newRecorderRoleWork(
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	product config.Configuration,
	clock domain.Clock,
) (*recorderRoleWork, error) {
	if err := os.MkdirAll(runtimeConfig.Recorder.Root, 0o750); err != nil {
		return nil, fmt.Errorf("recorder_root_unavailable")
	}
	exchanges := product.PublicExchanges()
	monotonic := exchangecontracts.NewProcessMonotonicSource()
	client, err := binance.NewPublicClientWithMonotonic(exchanges[0].EndpointSet, clock, monotonic)
	if err != nil {
		return nil, err
	}
	session := recorderSession(runtimeConfig.InstanceID, time.Now().UTC())
	ordinals := &runtimecore.IngestOrdinals{}
	root := recorderExchangeRoot(runtimeConfig.Recorder.Root, "binance", len(exchanges))
	streamRecorder, err := newBinanceStreamRecorder(root, session, runtimeConfig, len(exchanges), ordinals, pool)
	if err != nil {
		return nil, err
	}
	catalog, metadataStore, err := newRecorderStores(pool, runtimeConfig.InstanceID, clock)
	if err != nil {
		return nil, err
	}
	sink, err := marketrecorder.NewBinanceStreamSink(streamRecorder)
	if err != nil {
		return nil, err
	}
	collectors, err := newBinanceCollectors(exchanges[0], runtimeConfig.Recorder, client, sink, clock)
	if err != nil {
		return nil, err
	}
	work := &recorderRoleWork{client: client, collectors: collectors, recorder: streamRecorder,
		catalog: catalog, metadata: metadataStore, commit: buildinfo.Current().Commit,
		flush: runtimeConfig.Recorder.FlushInterval}
	if len(exchanges) == 2 {
		if err = work.addBybit(runtimeConfig.InstanceID, runtimeConfig.Recorder, exchanges[1], session, ordinals,
			pool, clock, monotonic); err != nil {
			return nil, err
		}
	}
	return work, nil
}

func newRecorderStores(pool *pgxpool.Pool, instance string,
	clock domain.Clock) (*postgresstore.A11DatasetCatalog, *postgresstore.A11ShadowStore, error) {
	if pool == nil {
		return nil, nil, nil
	}
	catalog, err := postgresstore.NewA11DatasetCatalog(pool)
	if err != nil {
		return nil, nil, err
	}
	metadata, err := postgresstore.NewA11ShadowStore(pool, instance, clock)
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
) error {
	client, err := bybit.NewPublicClientWithMonotonic(exchange.EndpointSet, clock, monotonic)
	if err != nil {
		return err
	}
	recorder, err := marketrecorder.NewB2(filepath.Join(runtimeConfig.Root, "bybit"),
		bybitRecorderDatasetID(session), session+"-bybit", "bybit", ordinals,
		segmentCommitter(pool, session+"-bybit", "bybit"), nil,
		marketrecorder.CollectorProfile{Instance: instance,
			Region: runtimeConfig.CollectorRegion, MinimumReaderVersion: "dataset-reader.v2"})
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

func newRecorderCollectors(product config.Configuration, runtimeConfig config.RecorderRuntime,
	client *binance.PublicClient, sink binance.PublicRecorder, clock domain.Clock,
) (map[domain.Instrument]*binance.InstrumentCollector, error) {
	return newBinanceCollectors(product.PublicExchanges()[0], runtimeConfig, client, sink, clock)
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
func (work *recorderRoleWork) Run(ctx context.Context, logger *slog.Logger) error {
	if err := work.registerMetadata(ctx); err != nil {
		return err
	}
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, len(work.collectors)+len(work.bybitCollectors))
	var group sync.WaitGroup
	for _, collector := range work.collectors {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- collector.Run(workContext)
		}()
	}
	for _, collector := range work.bybitCollectors {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- collector.Run(workContext)
		}()
	}
	flushTicker := time.NewTicker(work.flush)
	defer flushTicker.Stop()
	for {
		select {
		case <-workContext.Done():
			group.Wait()
			return work.flushPending(logger, true)
		case err := <-errorsChannel:
			if err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-flushTicker.C:
			if err := work.flushPending(logger, false); err != nil {
				return err
			}
		}
	}
}

func (work *recorderRoleWork) registerMetadata(ctx context.Context) error {
	if work.metadata == nil {
		return fmt.Errorf("recorder_metadata_store_unavailable")
	}
	if err := work.registerExchangeMetadata(ctx, "binance", work.client, binanceInstruments(work.collectors)); err != nil {
		return err
	}
	if work.bybitClient != nil {
		return work.registerExchangeMetadata(ctx, "bybit", work.bybitClient,
			bybitInstruments(work.bybitCollectors))
	}
	return nil
}

type publicMetadataClient interface {
	Instruments(context.Context, []domain.Instrument) ([]exchangecontracts.InstrumentRecord, error)
}

func (work *recorderRoleWork) registerExchangeMetadata(ctx context.Context, exchange string,
	client publicMetadataClient, instruments []domain.Instrument) error {
	sort.Slice(instruments, func(left, right int) bool { return instruments[left].Symbol() < instruments[right].Symbol() })
	records, err := client.Instruments(ctx, instruments)
	if err != nil || len(records) != len(instruments) {
		return fmt.Errorf("recorder_metadata_unavailable")
	}
	for _, record := range records {
		if _, err = work.metadata.RegisterPublicMetadata(ctx, exchange, record.Metadata); err != nil {
			return err
		}
	}
	return nil
}

func binanceInstruments(collectors map[domain.Instrument]*binance.InstrumentCollector) []domain.Instrument {
	instruments := make([]domain.Instrument, 0, len(collectors))
	for instrument := range collectors {
		instruments = append(instruments, instrument)
	}
	return instruments
}

func bybitInstruments(collectors map[domain.Instrument]*bybit.InstrumentCollector) []domain.Instrument {
	instruments := make([]domain.Instrument, 0, len(collectors))
	for instrument := range collectors {
		instruments = append(instruments, instrument)
	}
	return instruments
}

// Ready requires both approved books to be healthy and fresh.
func (work *recorderRoleWork) Ready() bool {
	for instrument, collector := range work.collectors {
		view, err := collector.Views().Book("binance", instrument)
		if err != nil || !view.Eligible(work.client.MonotonicOffset(), 5*time.Second) {
			return false
		}
	}
	for instrument, collector := range work.bybitCollectors {
		view, err := collector.Views().Book("bybit", instrument)
		if err != nil || !view.Eligible(work.bybitClient.MonotonicOffset(), 5*time.Second) {
			return false
		}
	}
	return true
}

func (work *recorderRoleWork) flushPending(logger *slog.Logger, final bool) error {
	if err := work.flushRecorder(logger, work.recorder, final); err != nil {
		return err
	}
	if work.bybitRecorder != nil {
		return work.flushRecorder(logger, work.bybitRecorder, final)
	}
	return nil
}

func (work *recorderRoleWork) flushRecorder(logger *slog.Logger,
	recorder *marketrecorder.Recorder, final bool) error {
	raw, canonical := recorder.PendingCounts()
	if raw == 0 && canonical == 0 {
		return nil
	}
	if final && raw != canonical {
		return fmt.Errorf("recorder_segment_incomplete")
	}
	var manifest marketrecorder.DatasetManifest
	flushed := true
	var err error
	if final {
		manifest, err = recorder.Flush()
	} else {
		manifest, flushed, err = recorder.FlushReady()
	}
	if err != nil {
		return err
	}
	if !flushed {
		return nil
	}
	if work.catalog == nil {
		return fmt.Errorf("recorder_catalog_unavailable")
	}
	catalogContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	datasetID, err := work.catalog.Register(catalogContext, manifest, work.commit)
	if err != nil {
		return err
	}
	logger.Info("recorder_segment_flushed", "event_code", "recorder_segment_flushed",
		"dataset_id", datasetID, "revision", manifest.Revision, "records", manifest.CanonicalCount,
		"gap_count", len(manifest.Gaps))
	return nil
}

func recorderSession(instance string, started time.Time) string {
	digest := sha256.Sum256([]byte(instance + started.Format(time.RFC3339Nano)))
	return "recorder-" + hex.EncodeToString(digest[:8])
}

func recorderDatasetID(session string) string { return "binance-public-v1a-" + session }

func segmentCommitter(
	pool *pgxpool.Pool,
	session string,
	exchange string,
) segments.Committer {
	queries := postgresgenerated.New(pool)
	return func(manifest segments.Manifest) error {
		if manifest.Spec.RecordCount > math.MaxInt64 || manifest.Spec.FirstOrdinal > math.MaxInt64 ||
			manifest.Spec.LastOrdinal > math.MaxInt64 {
			return fmt.Errorf("segment_ordinal_overflow")
		}
		finalized := time.Now().UTC()
		commitContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := queries.InsertMarketDataSegment(commitContext, postgresgenerated.InsertMarketDataSegmentParams{
			ID: manifest.Spec.Name, RecorderSession: session, ExchangeID: exchange, EventType: "mixed_public",
			SchemaVersion: manifest.Spec.SchemaVersion, ParserVersion: manifest.Spec.ParserVersion,
			NormalizationVersion: manifest.Spec.NormalizationVersion, Path: manifest.Path,
			Checksum: manifest.Checksum, OrderedContentHash: manifest.OrderedContentHash,
			RecordCount: int64(manifest.Spec.RecordCount), FirstOrdinal: int64(manifest.Spec.FirstOrdinal),
			LastOrdinal: int64(manifest.Spec.LastOrdinal), StartedAt: timestamp(manifest.Spec.StartedAt),
			EndedAt: timestamp(manifest.Spec.EndedAt), State: "ready", FinalizedAt: timestamp(finalized),
		})
		return err
	}
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
