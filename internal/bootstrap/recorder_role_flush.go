package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	marketrecorder "axiom/internal/recorder"
)

func (work *recorderRoleWork) flushPending(logger *slog.Logger, final bool) error {
	if err := work.flushRecorder(logger, work.recorder, final); err != nil {
		return err
	}
	if work.bybitRecorder != nil {
		return work.flushRecorder(logger, work.bybitRecorder, final)
	}
	return nil
}

func (work *recorderRoleWork) flushCapacity(logger *slog.Logger, exchange string,
	recorder *marketrecorder.Recorder) error {
	usage := recorder.PendingUsage()
	logger.Info("recorder_capacity_flush_requested", "event_code", "recorder_capacity_flush_requested",
		"exchange", exchange, "pending_bytes", usage.PendingBytes, "reserved_bytes", usage.ReservedBytes,
		"used_bytes", usage.UsedBytes, "limit_bytes", usage.LimitBytes,
		"flush_threshold_bytes", usage.FlushThresholdBytes, "high_water_bytes", usage.HighWaterBytes,
		"raw_records", usage.RawRecords, "canonical_records", usage.CanonicalRecords)
	return work.flushRecorder(logger, recorder, false)
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
