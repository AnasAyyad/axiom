package evaluation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const historicalPageLimit uint32 = 1000

// HistoricalImportSpec is one fixed official-candle import stream. WindowEnd
// and Checkpoint are exclusive and UTC, making every page deterministic.
type HistoricalImportSpec struct {
	ID          string
	CampaignID  string
	Exchange    string
	Instrument  domain.Instrument
	Interval    string
	WindowStart time.Time
	WindowEnd   time.Time
	Checkpoint  time.Time
}

// HistoricalPageArtifact is one crash-safe raw/canonical page pair.
type HistoricalPageArtifact struct {
	ID                 string
	ByteCount          int64
	RawPayloadHash     [sha256.Size]byte
	NormalizedPageHash [sha256.Size]byte
	NextCheckpoint     time.Time
	RowCount           uint64
}

// HistoricalImportProgress is committed only after its artifact is durable.
type HistoricalImportProgress struct {
	Artifact       HistoricalPageArtifact
	PageStart      time.Time
	NextCheckpoint time.Time
	RowCount       uint64
	Complete       bool
}

// HistoricalPageSink persists exact raw bytes before their linked canonical
// representation. Implementations must be idempotent for import ID/page start.
type HistoricalPageSink interface {
	PersistHistoricalPage(context.Context, HistoricalImportSpec,
		exchangecontracts.HistoricalCandlePage) (HistoricalPageArtifact, error)
}

type historicalPageResumeSink interface {
	LoadHistoricalPage(context.Context, HistoricalImportSpec) (HistoricalPageArtifact, bool, error)
}

// HistoricalImporter has only production-public market-data capabilities.
type HistoricalImporter struct {
	sources map[string]exchangecontracts.HistoricalPageReader
	sink    HistoricalPageSink
}

// NewHistoricalImporter fixes the two reviewed public sources.
func NewHistoricalImporter(binance, bybit exchangecontracts.HistoricalPageReader,
	sink HistoricalPageSink) (*HistoricalImporter, error) {
	if binance == nil || bybit == nil || sink == nil {
		return nil, fmt.Errorf("evaluation_historical_import_dependencies_missing")
	}
	return &HistoricalImporter{sources: map[string]exchangecontracts.HistoricalPageReader{
		"binance": binance, "bybit": bybit,
	}, sink: sink}, nil
}

// ImportPage imports at most 1000 exact, contiguous candles and persists one
// checkpoint. It never fabricates order books or repairs missing candle rows.
func (importer *HistoricalImporter) ImportPage(ctx context.Context,
	spec HistoricalImportSpec) (HistoricalImportProgress, error) {
	duration, err := HistoricalIntervalDuration(spec.Interval)
	if err != nil || !validHistoricalImportSpec(spec, duration) {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonSafetyFailed, false,
			"historical_import_spec_invalid", err)
	}
	if resumed, found, resumeErr := importer.resumeHistoricalPage(ctx, spec); found || resumeErr != nil {
		return resumed, resumeErr
	}
	source := importer.sources[spec.Exchange]
	if source == nil {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonSafetyFailed, false,
			"historical_import_source_invalid", nil)
	}
	requestEnd := spec.Checkpoint.Add(time.Duration(historicalPageLimit)*duration - time.Millisecond)
	windowLast := spec.WindowEnd.Add(-time.Millisecond)
	if requestEnd.After(windowLast) {
		requestEnd = windowLast
	}
	request := exchangecontracts.CandleRequest{HistoryRequest: exchangecontracts.HistoryRequest{
		Instrument: spec.Instrument, Start: spec.Checkpoint, End: requestEnd, Limit: historicalPageLimit,
	}, Interval: spec.Interval}
	page, err := source.CandlePage(ctx, request)
	if err != nil {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonDataUnavailable, true,
			"historical_import_source_unavailable", err)
	}
	if err = validateHistoricalPage(spec, page, duration, requestEnd); err != nil {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonDataCorrupt, false,
			"historical_import_page_invalid", err)
	}
	return importer.persistHistoricalPage(ctx, spec, page, duration)
}

func (importer *HistoricalImporter) resumeHistoricalPage(ctx context.Context,
	spec HistoricalImportSpec) (HistoricalImportProgress, bool, error) {
	resumable, ok := importer.sink.(historicalPageResumeSink)
	if !ok {
		return HistoricalImportProgress{}, false, nil
	}
	artifact, found, err := resumable.LoadHistoricalPage(ctx, spec)
	if err != nil {
		return HistoricalImportProgress{}, true, newHistoricalImportError(ReasonPersistenceFailed, false,
			"historical_import_recovery_failed", err)
	}
	if !found {
		return HistoricalImportProgress{}, false, nil
	}
	if artifact.ID == "" || artifact.RowCount == 0 || artifact.NextCheckpoint.After(spec.WindowEnd) ||
		!artifact.NextCheckpoint.After(spec.Checkpoint) {
		return HistoricalImportProgress{}, true, newHistoricalImportError(ReasonDataCorrupt, false,
			"historical_import_recovery_invalid", nil)
	}
	progress := HistoricalImportProgress{Artifact: artifact, PageStart: spec.Checkpoint,
		NextCheckpoint: artifact.NextCheckpoint, RowCount: artifact.RowCount,
		Complete: artifact.NextCheckpoint.Equal(spec.WindowEnd)}
	return progress, true, nil
}

func (importer *HistoricalImporter) persistHistoricalPage(ctx context.Context, spec HistoricalImportSpec,
	page exchangecontracts.HistoricalCandlePage, duration time.Duration) (HistoricalImportProgress, error) {
	artifact, err := importer.sink.PersistHistoricalPage(ctx, spec, page)
	if err != nil {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonPersistenceFailed, false,
			"historical_import_persist_failed", err)
	}
	last := page.Candles[len(page.Candles)-1]
	next := last.OpenTime.Add(duration)
	if next.After(spec.WindowEnd) {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonDataCorrupt, false,
			"historical_import_checkpoint_invalid", nil)
	}
	if artifact.NextCheckpoint.IsZero() {
		artifact.NextCheckpoint = next
	}
	if artifact.RowCount == 0 {
		artifact.RowCount = uint64(len(page.Candles))
	}
	if !artifact.NextCheckpoint.Equal(next) || artifact.RowCount != uint64(len(page.Candles)) {
		return HistoricalImportProgress{}, newHistoricalImportError(ReasonPersistenceFailed, false,
			"historical_import_artifact_mismatch", nil)
	}
	return HistoricalImportProgress{Artifact: artifact, PageStart: spec.Checkpoint,
		NextCheckpoint: next, RowCount: uint64(len(page.Candles)), Complete: next.Equal(spec.WindowEnd)}, nil
}

// HistoricalIntervalDuration returns only the reviewed campaign intervals.
func HistoricalIntervalDuration(interval string) (time.Duration, error) {
	switch interval {
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	default:
		return 0, fmt.Errorf("evaluation_historical_interval_invalid")
	}
}

func validHistoricalImportSpec(spec HistoricalImportSpec, duration time.Duration) bool {
	if spec.ID == "" || spec.CampaignID == "" || (spec.Exchange != "binance" && spec.Exchange != "bybit") ||
		duration <= 0 || spec.WindowStart.Location() != time.UTC || spec.WindowEnd.Location() != time.UTC ||
		spec.Checkpoint.Location() != time.UTC || spec.WindowStart.IsZero() || !spec.WindowStart.Before(spec.WindowEnd) ||
		spec.Checkpoint.Before(spec.WindowStart) || !spec.Checkpoint.Before(spec.WindowEnd) ||
		spec.WindowEnd.Sub(spec.WindowStart)%duration != 0 || spec.Checkpoint.Sub(spec.WindowStart)%duration != 0 {
		return false
	}
	instrument, err := domain.NewSpotInstrument(spec.Instrument.Base, spec.Instrument.Quote)
	return err == nil && instrument == spec.Instrument &&
		(spec.Instrument.Symbol() == "BTCUSDT" || spec.Instrument.Symbol() == "ETHUSDT")
}

func validateHistoricalPage(spec HistoricalImportSpec, page exchangecontracts.HistoricalCandlePage,
	duration time.Duration, requestEnd time.Time) error {
	if !page.Valid() || string(page.Exchange) != spec.Exchange || page.Instrument != spec.Instrument ||
		page.Interval != spec.Interval || !page.Start.Equal(spec.Checkpoint) || !page.End.Equal(requestEnd) {
		return fmt.Errorf("evaluation_historical_page_identity_invalid")
	}
	expectedRows := int(requestEnd.Add(time.Millisecond).Sub(spec.Checkpoint) / duration)
	if expectedRows <= 0 || len(page.Candles) != expectedRows {
		return fmt.Errorf("evaluation_historical_page_gap")
	}
	for index, candle := range page.Candles {
		expectedOpen := spec.Checkpoint.Add(time.Duration(index) * duration)
		exactClose := expectedOpen.Add(duration)
		exchangeClose := exactClose.Add(-time.Millisecond)
		if !candle.OpenTime.Equal(expectedOpen) || candle.OpenTime.Location() != time.UTC ||
			candle.CloseTime.Location() != time.UTC || candle.ReceivedAt.UTC.Location() != time.UTC ||
			(!candle.CloseTime.Equal(exactClose) && !candle.CloseTime.Equal(exchangeClose)) ||
			candle.ReceivedAt.UTC.Before(candle.CloseTime) {
			return fmt.Errorf("evaluation_historical_candle_invalid")
		}
	}
	return nil
}

// HistoricalImportError is bounded, stable orchestration evidence.
type HistoricalImportError struct {
	Reason      ReasonCode
	Code        string
	Recoverable bool
	cause       error
}

// Error returns the stable non-sensitive importer reason code.
func (failure *HistoricalImportError) Error() string { return failure.Code }

// Unwrap returns the internal cause for classification without exposing it in
// owner-visible campaign evidence.
func (failure *HistoricalImportError) Unwrap() error { return failure.cause }

func newHistoricalImportError(reason ReasonCode, recoverable bool, code string, cause error) error {
	return &HistoricalImportError{Reason: reason, Code: code, Recoverable: recoverable, cause: cause}
}

// HistoricalFailure safely classifies importer failures for the campaign.
func HistoricalFailure(err error) (HistoricalImportError, bool) {
	var failure *HistoricalImportError
	if !errors.As(err, &failure) || failure == nil {
		return HistoricalImportError{}, false
	}
	return *failure, true
}
