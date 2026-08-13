package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	marketrecorder "axiom/internal/recorder"
	postgresstore "axiom/internal/storage/postgres"
)

var errEvaluationRecorderReservePause = errors.New("evaluation_recorder_shadow_reserve_pause")

const evaluationRecorderFlushBuffer int64 = 1 << 30

func (work *recorderRoleWork) flushPending(logger *slog.Logger, final bool) error {
	pauseAfter, err := work.authorizeCampaignFlush([]*marketrecorder.Recorder{work.recorder, work.bybitRecorder}, !final)
	if err != nil {
		return err
	}
	if err := work.flushRecorder(logger, work.recorder, final); err != nil {
		return err
	}
	if work.bybitRecorder != nil {
		if err = work.flushRecorder(logger, work.bybitRecorder, final); err != nil {
			return err
		}
	}
	if pauseAfter {
		return errEvaluationRecorderReservePause
	}
	return nil
}

func (work *recorderRoleWork) authorizeCampaignFlush(recorders []*marketrecorder.Recorder,
	protectReserve bool) (bool, error) {
	if work.rotationControl == nil {
		return false, nil
	}
	predicted := int64(0)
	for _, recorder := range recorders {
		if recorder == nil {
			continue
		}
		raw, canonical := recorder.PendingCounts()
		if raw == 0 && canonical == 0 {
			continue
		}
		usage := recorder.PendingUsage()
		if usage.UsedBytes > math.MaxInt64 || predicted > math.MaxInt64-int64(usage.UsedBytes) {
			return false, fmt.Errorf("evaluation_recorder_flush_forecast_overflow")
		}
		predicted += int64(usage.UsedBytes)
	}
	if predicted == 0 {
		return false, nil
	}
	if predicted > math.MaxInt64-evaluationRecorderFlushBuffer {
		return false, fmt.Errorf("evaluation_recorder_flush_forecast_overflow")
	}
	predicted += evaluationRecorderFlushBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return work.rotationControl.FlushAllowance(ctx, work.session, predicted, protectReserve)
}

func (work *recorderRoleWork) flushAndObserve(ctx context.Context, logger *slog.Logger) error {
	if err := work.flushPending(logger, false); err != nil {
		return err
	}
	if work.rotationControl == nil {
		return nil
	}
	observations := make([]postgresstore.EvaluationRecorderInstrumentObservation, 0,
		len(work.collectors)+len(work.bybitCollectors))
	for _, collector := range work.collectors {
		health, stats := collector.HealthSnapshot(), collector.Stats()
		observations = append(observations, postgresstore.EvaluationRecorderInstrumentObservation{
			ExchangeID: "binance", Instrument: health.Instrument, Eligible: health.Eligible,
			BookFresh: health.BookFresh, ClockEligible: health.ClockEligible, LatestEventAt: health.ObservedAt.UTC(),
			Messages: stats.Messages, QueueDrops: stats.ReconnectReasons.Queue, Gaps: stats.Gaps,
			DecoderErrors: stats.DecoderErrors})
	}
	for _, collector := range work.bybitCollectors {
		health, stats := collector.HealthSnapshot(), collector.Stats()
		messages := stats.DepthUpdates + stats.Trades + stats.Tickers + stats.Candles + stats.Snapshots + stats.Heartbeats
		observations = append(observations, postgresstore.EvaluationRecorderInstrumentObservation{
			ExchangeID: "bybit", Instrument: health.Instrument, Eligible: health.Eligible,
			BookFresh: health.BookFresh, ClockEligible: health.ClockEligible, LatestEventAt: health.ObservedAt.UTC(),
			Messages: messages, QueueDrops: stats.QueueOverflows, Gaps: stats.SequenceGaps,
			DecoderErrors: stats.DecoderErrors})
	}
	sort.Slice(observations, func(left, right int) bool {
		if observations[left].ExchangeID == observations[right].ExchangeID {
			return observations[left].Instrument < observations[right].Instrument
		}
		return observations[left].ExchangeID < observations[right].ExchangeID
	})
	observationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return work.rotationControl.Observe(observationContext, work.session, time.Now().UTC(), true, observations)
}

func (work *recorderRoleWork) flushCapacity(logger *slog.Logger, exchange string,
	recorder *marketrecorder.Recorder) error {
	usage := recorder.PendingUsage()
	logger.Info("recorder_capacity_flush_requested", "event_code", "recorder_capacity_flush_requested",
		"exchange", exchange, "pending_bytes", usage.PendingBytes, "reserved_bytes", usage.ReservedBytes,
		"used_bytes", usage.UsedBytes, "limit_bytes", usage.LimitBytes,
		"flush_threshold_bytes", usage.FlushThresholdBytes, "high_water_bytes", usage.HighWaterBytes,
		"raw_records", usage.RawRecords, "canonical_records", usage.CanonicalRecords)
	pauseAfter, err := work.authorizeCampaignFlush([]*marketrecorder.Recorder{recorder}, true)
	if err != nil {
		return err
	}
	if err = work.flushRecorder(logger, recorder, false); err != nil {
		return err
	}
	if pauseAfter {
		return errEvaluationRecorderReservePause
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
