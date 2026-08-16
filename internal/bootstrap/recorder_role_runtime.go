package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Run operates the recorder until cancellation, a terminal safety failure, or
// a campaign-owned safe rotation boundary.
func (work *recorderRoleWork) Run(ctx context.Context, logger *slog.Logger) error {
	if err := work.prepareRecorderRun(ctx, logger); err != nil {
		return err
	}
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel, group := work.startRecorderCollectors(workContext)
	if work.startupRotation != nil {
		if err := work.rotationControl.MarkActive(workContext, work.startupRotation.CampaignID,
			work.session); err != nil {
			cancel()
			group.Wait()
			return err
		}
	}
	binanceCapacity, bybitCapacity := work.capacitySignals()
	return work.runRecorderLoop(workContext, logger, cancel, group, errorsChannel,
		binanceCapacity, bybitCapacity)
}

func (work *recorderRoleWork) prepareRecorderRun(ctx context.Context, logger *slog.Logger) error {
	if critical, err := work.observeStoragePressure(ctx, logger); err != nil {
		return err
	} else if critical {
		return fmt.Errorf("recorder_storage_pressure_critical")
	}
	if err := work.registerMetadata(ctx); err != nil {
		return err
	}
	if work.startupRotation != nil && work.startupRotation.State == "PAUSED" {
		if err := work.waitForEvaluationRecorderResume(ctx); err != nil {
			return err
		}
	}
	if work.startupRotation != nil && work.startupRotation.State == "FINALIZING" {
		if err := work.rotationControl.MarkCompleted(ctx, work.startupRotation.CampaignID, work.session); err != nil {
			return err
		}
		return fmt.Errorf("evaluation_recorder_completed")
	}
	return nil
}

func (work *recorderRoleWork) runRecorderLoop(workContext context.Context, logger *slog.Logger,
	cancel context.CancelFunc, group *sync.WaitGroup, errorsChannel <-chan error,
	binanceCapacity, bybitCapacity <-chan struct{}) error {
	flushTicker := time.NewTicker(work.flush)
	defer flushTicker.Stop()
	pressureTicker := time.NewTicker(work.pressurePolicy.SampleInterval)
	defer pressureTicker.Stop()
	rotationTicker := time.NewTicker(time.Second)
	defer rotationTicker.Stop()
	telemetryTicker := time.NewTicker(5 * time.Second)
	defer telemetryTicker.Stop()
	for {
		select {
		case <-workContext.Done():
			group.Wait()
			return work.flushPending(logger, true)
		case err := <-errorsChannel:
			if err == nil {
				err = fmt.Errorf("recorder_collector_unexpected_exit")
			}
			cancel()
			group.Wait()
			return err
		case <-flushTicker.C:
			if err := work.flushAndObserve(workContext, logger); err != nil {
				return work.handleRecorderFlushError(err, cancel, group, logger)
			}
		case <-rotationTicker.C:
			if terminal, err := work.handleRecorderRotation(workContext, cancel, group, logger); terminal || err != nil {
				return err
			}
		case observedAt := <-telemetryTicker.C:
			if err := work.publishOperationalReadinessMetrics(observedAt.UTC()); err != nil {
				return err
			}
		case <-pressureTicker.C:
			if err := work.handleStoragePressure(workContext, logger, cancel, group); err != nil {
				return err
			}
		case <-binanceCapacity:
			if err := work.flushCapacity(logger, "binance", work.recorder); err != nil {
				return work.handleRecorderFlushError(err, cancel, group, logger)
			}
		case <-bybitCapacity:
			if err := work.flushCapacity(logger, "bybit", work.bybitRecorder); err != nil {
				return work.handleRecorderFlushError(err, cancel, group, logger)
			}
		}
	}
}

func (work *recorderRoleWork) publishOperationalReadinessMetrics(observedAt time.Time) error {
	if work.metrics == nil {
		return nil
	}
	var decodeBookP99, resyncP95 time.Duration
	for _, collector := range work.collectors {
		stats := collector.Stats()
		if stats.HotPathP99 > decodeBookP99 {
			decodeBookP99 = stats.HotPathP99
		}
		if stats.ResyncP95 > resyncP95 {
			resyncP95 = stats.ResyncP95
		}
	}
	for _, collector := range work.bybitCollectors {
		stats := collector.Stats()
		if stats.HotPathP99 > decodeBookP99 {
			decodeBookP99 = stats.HotPathP99
		}
		if stats.ResyncP95 > resyncP95 {
			resyncP95 = stats.ResyncP95
		}
	}
	return work.metrics.SetOperationalReadinessCollectorLatency(decodeBookP99, resyncP95, observedAt)
}

func (work *recorderRoleWork) handleRecorderFlushError(err error, cancel context.CancelFunc,
	group *sync.WaitGroup, logger *slog.Logger) error {
	if errors.Is(err, errEvaluationRecorderReservePause) {
		return work.pauseForShadowReserve(cancel, group, logger)
	}
	return err
}

func (work *recorderRoleWork) handleRecorderRotation(ctx context.Context, cancel context.CancelFunc,
	group *sync.WaitGroup, logger *slog.Logger) (bool, error) {
	campaignID, completion, err := work.rotationControl.CompletionRequested(ctx, work.session)
	if err != nil {
		return false, err
	}
	if completion {
		return true, work.finalizeRecorderCampaign(cancel, group, logger, campaignID, true)
	}
	rotation, pending, err := work.rotationControl.PendingRotation(ctx)
	if err != nil || !pending {
		return false, err
	}
	return true, work.finalizeRecorderCampaign(cancel, group, logger, rotation.CampaignID, false)
}

func (work *recorderRoleWork) finalizeRecorderCampaign(cancel context.CancelFunc, group *sync.WaitGroup,
	logger *slog.Logger, campaignID string, completion bool) error {
	cancel()
	group.Wait()
	if err := work.flushPending(logger, true); err != nil {
		_ = work.rotationControl.Block(context.Background(), campaignID, evaluation.ReasonPersistenceFailed)
		return err
	}
	if completion {
		if err := work.rotationControl.MarkCompleted(context.Background(), campaignID, work.session); err != nil {
			return err
		}
		return fmt.Errorf("evaluation_recorder_completed")
	}
	if err := work.rotationControl.MarkFinalized(context.Background(), campaignID, work.session); err != nil {
		return err
	}
	return fmt.Errorf("evaluation_recorder_rotation_requested")
}

func (work *recorderRoleWork) pauseForShadowReserve(cancel context.CancelFunc, group *sync.WaitGroup,
	logger *slog.Logger) error {
	cancel()
	group.Wait()
	if err := work.flushPending(logger, true); err != nil {
		return err
	}
	ctx, release := context.WithTimeout(context.Background(), 10*time.Second)
	defer release()
	if err := work.rotationControl.PauseSession(ctx, work.session); err != nil {
		return err
	}
	return errEvaluationRecorderReservePause
}

func (work *recorderRoleWork) waitForEvaluationRecorderResume(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		rotation, err := work.rotationControl.Rotation(ctx, work.startupRotation.CampaignID)
		if err != nil {
			return err
		}
		switch rotation.State {
		case "ACTIVE":
			return nil
		case "PAUSED":
		case "BLOCKED", "COMPLETED":
			return fmt.Errorf("evaluation_recorder_not_resumable")
		default:
			return fmt.Errorf("evaluation_recorder_pause_state_invalid")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (work *recorderRoleWork) capacitySignals() (<-chan struct{}, <-chan struct{}) {
	binanceCapacity := work.recorder.FlushRequired()
	var bybitCapacity <-chan struct{}
	if work.bybitRecorder != nil {
		bybitCapacity = work.bybitRecorder.FlushRequired()
	}
	return binanceCapacity, bybitCapacity
}

func (work *recorderRoleWork) startRecorderCollectors(ctx context.Context) (<-chan error, *sync.WaitGroup) {
	errorsChannel := make(chan error, len(work.collectors)+len(work.bybitCollectors))
	group := &sync.WaitGroup{}
	for _, collector := range work.collectors {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- collector.Run(ctx)
		}()
	}
	for _, collector := range work.bybitCollectors {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- collector.Run(ctx)
		}()
	}
	return errorsChannel, group
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

// Ready requires every approved book and its instrument clock to be healthy.
func (work *recorderRoleWork) Ready() bool {
	for _, collector := range work.collectors {
		if !collector.HealthSnapshot().Eligible {
			return false
		}
	}
	for _, collector := range work.bybitCollectors {
		if !collector.HealthSnapshot().Eligible {
			return false
		}
	}
	return true
}

func recorderSession(instance string, started time.Time) string {
	digest := sha256.Sum256([]byte(instance + started.Format(time.RFC3339Nano)))
	return "recorder-" + hex.EncodeToString(digest[:8])
}

func recorderDatasetID(session string) string { return "binance-public-recording-" + session }

func segmentCommitter(
	pool *pgxpool.Pool,
	session string,
	exchange string,
) segments.Committer {
	store, _ := postgresstore.NewRecordedSegmentCommitter(pool)
	return func(manifest segments.Manifest) error {
		if store == nil {
			return fmt.Errorf("segment_committer_unavailable")
		}
		commitContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return store.Commit(commitContext, session, exchange, manifest, time.Now().UTC())
	}
}
