package recorder

import (
	"crypto/sha256"
	"regexp"
	"sync"
	"time"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

type rawRecord struct {
	row       segments.WireRow
	canonical bool
}

// Recorder appends raw rows before their linked canonical representations.
type Recorder struct {
	mutex              sync.Mutex
	root               string
	datasetID          string
	sessionID          string
	exchange           string
	ordinals           *runtimecore.IngestOrdinals
	finalizer          *segments.Finalizer
	commit             segments.Committer
	now                func() time.Time
	revision           uint64
	previous           string
	latest             DatasetManifest
	raw                []segments.WireRow
	canonical          []segments.CanonicalRow
	links              map[uint64]*rawRecord
	segments           []SegmentReference
	gaps               []Gap
	rawCount           uint64
	canonicalCount     uint64
	pendingBytes       uint64
	reservedBytes      uint64
	pendingLimit       uint64
	pendingHighWater   uint64
	flushRequired      chan struct{}
	profile            *CollectorProfile
	generationCoverage map[uint64]GenerationCoverage
}

// PendingCounts reports bounded in-memory wire/canonical rows for operations.
func (recorder *Recorder) PendingCounts() (uint64, uint64) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return uint64(len(recorder.raw)), uint64(len(recorder.canonical))
}

// PendingUsage reports current, bounded recorder capacity and its session high-water mark.
func (recorder *Recorder) PendingUsage() PendingUsage {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.pendingUsageLocked()
}

// FlushRequired is edge-coalesced notification that the recorder crossed its
// proactive capacity threshold. Callers must use FlushReady so an in-flight
// raw/canonical pair remains safe.
func (recorder *Recorder) FlushRequired() <-chan struct{} { return recorder.flushRequired }

// Recover finalizes or re-registers crash-safe ready segments before ingest.
func (recorder *Recorder) Recover() ([]segments.Manifest, error) {
	return recorder.finalizer.Recover(recorder.commit)
}

// New constructs one session-scoped append-only recorder.
func New(
	root, datasetID, sessionID, exchange string,
	ordinals *runtimecore.IngestOrdinals,
	commit segments.Committer,
	kill segments.KillPoint,
) (*Recorder, error) {
	return newRecorder(root, datasetID, sessionID, exchange, ordinals, commit, kill, nil)
}

// NewB2 constructs a recorder that emits V2 per-exchange qualification manifests.
func NewB2(
	root, datasetID, sessionID, exchange string,
	ordinals *runtimecore.IngestOrdinals,
	commit segments.Committer,
	kill segments.KillPoint,
	profile CollectorProfile,
) (*Recorder, error) {
	if !identifierPattern.MatchString(profile.Instance) || !identifierPattern.MatchString(profile.Region) ||
		!identifierPattern.MatchString(profile.MinimumReaderVersion) {
		return nil, recorderError("collector_profile_invalid")
	}
	return newRecorder(root, datasetID, sessionID, exchange, ordinals, commit, kill, &profile)
}

func newRecorder(
	root, datasetID, sessionID, exchange string,
	ordinals *runtimecore.IngestOrdinals,
	commit segments.Committer,
	kill segments.KillPoint,
	profile *CollectorProfile,
) (*Recorder, error) {
	if !identifierPattern.MatchString(datasetID) || !identifierPattern.MatchString(sessionID) ||
		!identifierPattern.MatchString(exchange) || ordinals == nil || commit == nil {
		return nil, recorderError("configuration_invalid")
	}
	finalizer, err := segments.NewFinalizer(root, kill)
	if err != nil {
		return nil, recorderError("storage_invalid")
	}
	return &Recorder{root: root, datasetID: datasetID, sessionID: sessionID, exchange: exchange,
		ordinals: ordinals, finalizer: finalizer, commit: commit, now: func() time.Time { return time.Now().UTC() },
		links: make(map[uint64]*rawRecord), pendingLimit: maximumPendingBytes,
		flushRequired: make(chan struct{}, 1), profile: profile,
		generationCoverage: make(map[uint64]GenerationCoverage)}, nil
}

// RecordRaw assigns the authoritative ingest ordinal and snapshots wire bytes.
func (recorder *Recorder) RecordRaw(input RawInput) (RawLink, error) {
	if err := validateRawInput(input, recorder.exchange, recorder.sessionID); err != nil {
		return RawLink{}, err
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	// Allocate and append under the same lock. This prevents a later ordinal
	// from becoming flush-visible before an earlier concurrent RecordRaw call.
	ordinal, err := recorder.ordinals.Next()
	if err != nil {
		return RawLink{}, recorderError("ordinal_exhausted")
	}
	return recorder.recordRawLocked(input, ordinal)
}

func (recorder *Recorder) recordRawLocked(input RawInput, ordinal uint64) (RawLink, error) {
	if ordinal == 0 || validateRawInput(input, recorder.exchange, recorder.sessionID) != nil {
		return RawLink{}, recorderError("raw_input_invalid")
	}
	charge := uint64(len(input.Payload) + recordMemoryOverhead)
	reservation := uint64(maximumEventBytes + recordMemoryOverhead)
	used, required := recorder.pendingBytes+recorder.reservedBytes, charge+reservation
	if used > recorder.pendingLimit || required > recorder.pendingLimit-used {
		return RawLink{}, recorderError("recorder_capacity_exceeded")
	}
	hash := sha256.Sum256(input.Payload)
	row := segments.WireRow{IngestOrdinal: ordinal, Exchange: input.Exchange, EventType: string(input.EventType),
		BaseAsset: string(input.Instrument.Base), QuoteAsset: string(input.Instrument.Quote),
		SourceSessionID: input.SessionID, ConnectionID: input.ConnectionID,
		ConnectionGeneration: input.ConnectionGeneration, MonotonicOffsetNanos: input.MonotonicOffsetNanos,
		RecordedLogicalTime: input.RecordedLogicalTime, SourceSequence: input.SourceSequence,
		ExchangeTimeUnixNano: timePointer(input.ExchangeTime), ReceivedAtUnixNano: input.ReceivedAt.UnixNano(),
		Payload: append([]byte(nil), input.Payload...), PayloadSHA256: hash}
	if err := segments.ValidateWireRow(row); err != nil {
		return RawLink{}, recorderError("wire_row_invalid")
	}
	recorder.raw = append(recorder.raw, row)
	recorder.links[ordinal] = &rawRecord{row: row}
	recorder.pendingBytes += charge
	recorder.reservedBytes += reservation
	recorder.updateCapacitySignalLocked()
	return RawLink{IngestOrdinal: ordinal, PayloadHash: hash}, nil
}

// RecordCanonical appends exactly one canonical representation for a raw link.
func (recorder *Recorder) RecordCanonical(input CanonicalInput) error {
	if !identifierPattern.MatchString(input.EventID) || input.ParserVersion == "" ||
		input.NormalizationVersion == "" || len(input.Canonical) == 0 || len(input.Canonical) > maximumEventBytes {
		return recorderError("canonical_input_invalid")
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.recordCanonicalLocked(input)
}

func (recorder *Recorder) recordCanonicalLocked(input CanonicalInput) error {
	record := recorder.links[input.Link.IngestOrdinal]
	if record == nil || record.canonical || record.row.PayloadSHA256 != input.Link.PayloadHash {
		return recorderError("raw_link_invalid")
	}
	reservation := uint64(maximumEventBytes + recordMemoryOverhead)
	if recorder.reservedBytes < reservation {
		return recorderError("recorder_capacity_invalid")
	}
	canonicalHash := sha256.Sum256(input.Canonical)
	row := segments.CanonicalRow{IngestOrdinal: record.row.IngestOrdinal, EventID: input.EventID,
		Exchange: record.row.Exchange, EventType: record.row.EventType,
		BaseAsset: record.row.BaseAsset, QuoteAsset: record.row.QuoteAsset,
		SourceSessionID: record.row.SourceSessionID, ConnectionID: record.row.ConnectionID,
		ConnectionGeneration: record.row.ConnectionGeneration, MonotonicOffsetNanos: record.row.MonotonicOffsetNanos,
		RecordedLogicalTime: record.row.RecordedLogicalTime, SourceSequence: record.row.SourceSequence,
		ExchangeTimeUnixNano: cloneTimePointer(record.row.ExchangeTimeUnixNano),
		ReceivedAtUnixNano:   record.row.ReceivedAtUnixNano, ParserVersion: input.ParserVersion,
		NormalizationVersion: input.NormalizationVersion, WirePayloadSHA256: record.row.PayloadSHA256,
		CanonicalEvent: append([]byte(nil), input.Canonical...), CanonicalSHA256: canonicalHash}
	if input.SourceSequence != "" {
		row.SourceSequence = input.SourceSequence
	}
	if input.ExchangeTime != nil {
		if input.ExchangeTime.IsZero() || input.ExchangeTime.Location() != time.UTC {
			return recorderError("canonical_input_invalid")
		}
		row.ExchangeTimeUnixNano = timePointer(input.ExchangeTime)
	}
	if err := segments.ValidateCanonicalRow(row); err != nil {
		return recorderError("canonical_row_invalid")
	}
	record.canonical = true
	recorder.canonical = append(recorder.canonical, row)
	recorder.reservedBytes -= reservation
	recorder.pendingBytes += uint64(len(input.Canonical) + recordMemoryOverhead)
	recorder.updateCapacitySignalLocked()
	return nil
}

func (recorder *Recorder) pendingUsageLocked() PendingUsage {
	used := recorder.pendingBytes + recorder.reservedBytes
	threshold := recorder.pendingLimit / capacityFlushDivisor
	if threshold == 0 && recorder.pendingLimit != 0 {
		threshold = 1
	}
	return PendingUsage{RawRecords: uint64(len(recorder.raw)),
		CanonicalRecords: uint64(len(recorder.canonical)), PendingBytes: recorder.pendingBytes,
		ReservedBytes: recorder.reservedBytes, UsedBytes: used, LimitBytes: recorder.pendingLimit,
		FlushThresholdBytes: threshold, HighWaterBytes: recorder.pendingHighWater}
}

func (recorder *Recorder) updateCapacitySignalLocked() {
	usage := recorder.pendingUsageLocked()
	if usage.UsedBytes > recorder.pendingHighWater {
		recorder.pendingHighWater = usage.UsedBytes
		usage.HighWaterBytes = usage.UsedBytes
	}
	if usage.FlushThresholdBytes == 0 || usage.UsedBytes < usage.FlushThresholdBytes {
		select {
		case <-recorder.flushRequired:
		default:
		}
		return
	}
	select {
	case recorder.flushRequired <- struct{}{}:
	default:
	}
}

// RecordDecisionInput appends an exact in-process decision input through the
// same raw-before-canonical durability boundary used for public wire data.
func (recorder *Recorder) RecordDecisionInput(input DecisionInput) (uint64, error) {
	return recorder.RecordDecisionInputBuilt(input, func(uint64) ([]byte, error) {
		return append([]byte(nil), input.Payload...), nil
	})
}

// RecordDecisionInputBuilt atomically binds the canonical payload to the
// authoritative recorder ordinal before either representation is appended.
func (recorder *Recorder) RecordDecisionInputBuilt(input DecisionInput, build DecisionInputBuilder) (uint64, error) {
	if !identifierPattern.MatchString(input.EventID) || input.LogicalTime == 0 || input.ReceivedAt.IsZero() ||
		input.ReceivedAt.Location() != time.UTC || build == nil {
		return 0, recorderError("decision_input_invalid")
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	ordinal, err := recorder.ordinals.Next()
	if err != nil {
		return 0, recorderError("ordinal_exhausted")
	}
	payload, err := build(ordinal)
	if err != nil || len(payload) == 0 {
		return 0, recorderError("decision_input_invalid")
	}
	link, err := recorder.recordRawLocked(RawInput{Exchange: recorder.exchange, EventType: EventDecisionInput,
		Instrument: input.Instrument, SessionID: recorder.sessionID, ConnectionID: "decision-input",
		ConnectionGeneration: 1, MonotonicOffsetNanos: input.LogicalTime,
		RecordedLogicalTime: input.LogicalTime, SourceSequence: input.EventID,
		ReceivedAt: input.ReceivedAt, Payload: append([]byte(nil), payload...)}, ordinal)
	if err != nil {
		return 0, err
	}
	if err = recorder.recordCanonicalLocked(CanonicalInput{Link: link, EventID: input.EventID,
		ParserVersion: "decision-input.v1", NormalizationVersion: "decision-input.v1",
		Canonical: append([]byte(nil), payload...), SourceSequence: input.EventID}); err != nil {
		return 0, err
	}
	return link.IngestOrdinal, nil
}

// RecordGap appends one explicit, ordered source-sequence gap.
func (recorder *Recorder) RecordGap(gap Gap) error {
	if err := validateGap(gap, recorder.exchange); err != nil {
		return err
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if count := len(recorder.gaps); count > 0 {
		prior := recorder.gaps[count-1]
		if gap.StartedAt.Before(prior.EndedAt) || (gap.ConnectionGeneration == prior.ConnectionGeneration &&
			gap.FirstSourceSequence <= prior.LastSourceSequence) {
			return recorderError("gap_regressed")
		}
	}
	recorder.gaps = append(recorder.gaps, gap)
	return nil
}

func validateRawInput(input RawInput, exchange, session string) error {
	if input.Exchange != exchange || input.SessionID != session || !validEventType(input.EventType) ||
		!identifierPattern.MatchString(input.ConnectionID) || input.ConnectionGeneration == 0 ||
		input.MonotonicOffsetNanos == 0 || input.RecordedLogicalTime == 0 ||
		input.ReceivedAt.IsZero() || input.ReceivedAt.Location() != time.UTC ||
		len(input.Payload) == 0 || len(input.Payload) > maximumEventBytes {
		return recorderError("raw_input_invalid")
	}
	if _, err := domain.NewSpotInstrument(input.Instrument.Base, input.Instrument.Quote); err != nil {
		return recorderError("raw_input_invalid")
	}
	if input.ExchangeTime != nil && (input.ExchangeTime.IsZero() || input.ExchangeTime.Location() != time.UTC) {
		return recorderError("raw_input_invalid")
	}
	return nil
}

func validEventType(eventType EventType) bool {
	switch eventType {
	case EventDepth, EventTrade, EventCandle, EventSnapshot, EventLifecycle, EventSubscription,
		EventHeartbeat, EventGap, EventRebuild, EventDecoderError, EventClockSample, EventStreamFrame, EventDecisionInput:
		return true
	default:
		return false
	}
}

func validateGap(gap Gap, exchange string) error {
	if gap.Exchange != exchange || gap.ConnectionGeneration == 0 || gap.FirstSourceSequence == 0 ||
		gap.LastSourceSequence < gap.FirstSourceSequence || gap.StartedAt.IsZero() || gap.EndedAt.Before(gap.StartedAt) ||
		gap.StartedAt.Location() != time.UTC || gap.EndedAt.Location() != time.UTC || !identifierPattern.MatchString(gap.Reason) {
		return recorderError("gap_invalid")
	}
	if _, err := domain.NewSpotInstrument(gap.Instrument.Base, gap.Instrument.Quote); err != nil {
		return recorderError("gap_invalid")
	}
	return nil
}

func timePointer(value *time.Time) *int64 {
	if value == nil {
		return nil
	}
	nanoseconds := value.UnixNano()
	return &nanoseconds
}

func cloneTimePointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
