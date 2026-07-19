package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	postgresgenerated "axiom/internal/storage/postgres/generated"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recorderRoleWork struct {
	client     *binance.PublicClient
	collectors map[domain.Instrument]*binance.InstrumentCollector
	recorder   *marketrecorder.Recorder
	catalog    *postgresstore.A11DatasetCatalog
	metadata   *postgresstore.A11ShadowStore
	commit     string
	flush      time.Duration
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
	client, err := binance.NewPublicClient(product.Endpoint.Set, clock)
	if err != nil {
		return nil, err
	}
	session := recorderSession(runtimeConfig.InstanceID, time.Now().UTC())
	commit := segmentCommitter(pool, session)
	streamRecorder, err := marketrecorder.New(runtimeConfig.Recorder.Root, "binance-public-v1a", session,
		"binance", &runtimecore.IngestOrdinals{}, commit, nil)
	if err != nil {
		return nil, err
	}
	var catalog *postgresstore.A11DatasetCatalog
	if pool != nil {
		catalog, err = postgresstore.NewA11DatasetCatalog(pool)
		if err != nil {
			return nil, err
		}
	}
	var metadataStore *postgresstore.A11ShadowStore
	if pool != nil {
		metadataStore, err = postgresstore.NewA11ShadowStore(pool, runtimeConfig.InstanceID, clock)
		if err != nil {
			return nil, err
		}
	}
	sink, err := marketrecorder.NewBinanceStreamSink(streamRecorder)
	if err != nil {
		return nil, err
	}
	collectors, err := newRecorderCollectors(product, runtimeConfig.Recorder, client, sink, clock)
	if err != nil {
		return nil, err
	}
	return &recorderRoleWork{client: client, collectors: collectors, recorder: streamRecorder,
		catalog: catalog, metadata: metadataStore, commit: buildinfo.Current().Commit,
		flush: runtimeConfig.Recorder.FlushInterval}, nil
}

func newRecorderCollectors(product config.Configuration, runtimeConfig config.RecorderRuntime,
	client *binance.PublicClient, sink binance.PublicRecorder, clock domain.Clock,
) (map[domain.Instrument]*binance.InstrumentCollector, error) {
	collectors := make(map[domain.Instrument]*binance.InstrumentCollector, len(product.Instruments))
	for _, configured := range product.Instruments {
		base, baseErr := domain.ParseAssetSymbol(configured.Base)
		quote, quoteErr := domain.ParseAssetSymbol(configured.Quote)
		instrument, instrumentErr := domain.NewSpotInstrument(base, quote)
		if baseErr != nil || quoteErr != nil || instrumentErr != nil {
			return nil, fmt.Errorf("recorder_instrument_invalid")
		}
		collectorConfig := binance.DefaultCollectorConfig(instrument)
		collectorConfig.BookDepth = runtimeConfig.BookDepth
		collectorConfig.QueueCapacity = runtimeConfig.QueueCapacity
		collector, collectorErr := binance.NewInstrumentCollector(collectorConfig, client, sink, clock)
		if collectorErr != nil {
			return nil, collectorErr
		}
		collectors[instrument] = collector
	}
	if len(collectors) != 2 {
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
	errorsChannel := make(chan error, len(work.collectors))
	var group sync.WaitGroup
	for _, collector := range work.collectors {
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
			return work.flushPending(logger)
		case err := <-errorsChannel:
			if err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-flushTicker.C:
			if err := work.flushPending(logger); err != nil {
				return err
			}
		}
	}
}

func (work *recorderRoleWork) registerMetadata(ctx context.Context) error {
	if work.metadata == nil {
		return fmt.Errorf("recorder_metadata_store_unavailable")
	}
	instruments := make([]domain.Instrument, 0, len(work.collectors))
	for instrument := range work.collectors {
		instruments = append(instruments, instrument)
	}
	sort.Slice(instruments, func(left, right int) bool { return instruments[left].Symbol() < instruments[right].Symbol() })
	records, err := work.client.Instruments(ctx, instruments)
	if err != nil || len(records) != len(instruments) {
		return fmt.Errorf("recorder_metadata_unavailable")
	}
	for _, record := range records {
		if _, err = work.metadata.RegisterMetadata(ctx, record.Metadata); err != nil {
			return err
		}
	}
	return nil
}

// Ready requires both approved books to be healthy and fresh.
func (work *recorderRoleWork) Ready() bool {
	for instrument, collector := range work.collectors {
		view, err := collector.Views().Book("binance", instrument)
		if err != nil || !view.Eligible(work.client.MonotonicOffset(), 5*time.Second) {
			return false
		}
	}
	return true
}

func (work *recorderRoleWork) flushPending(logger *slog.Logger) error {
	raw, canonical := work.recorder.PendingCounts()
	if raw == 0 && canonical == 0 {
		return nil
	}
	if raw != canonical {
		return fmt.Errorf("recorder_segment_incomplete")
	}
	manifest, err := work.recorder.Flush()
	if err != nil {
		return err
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

func segmentCommitter(
	pool *pgxpool.Pool,
	session string,
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
			ID: manifest.Spec.Name, RecorderSession: session, ExchangeID: "binance", EventType: "mixed_public",
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
