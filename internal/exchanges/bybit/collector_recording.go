package bybit

import (
	"context"
	"encoding/json"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func (collector *InstrumentCollector) recordLifecycle(
	ctx context.Context,
	stream ObservedStream,
	state string,
	reason string,
) error {
	fact := exchangecontracts.LifecycleEvent{Exchange: collectorExchange,
		Instrument: collector.config.Instrument, State: state, Reason: reason,
		ConnectionID: stream.ConnectionID(), ConnectionGeneration: stream.Generation(),
		ObservedAt: collector.clock.Now()}
	return collector.recordFact(ctx, exchangecontracts.RecordLifecycle, stream.ConnectionID(), stream.Generation(), fact)
}

func (collector *InstrumentCollector) recordRebuild(
	ctx context.Context,
	observed exchangecontracts.ObservedStreamEvent,
	sequence uint64,
) error {
	fact := struct {
		Sequence   uint64 `json:"sequence"`
		Generation uint64 `json:"generation"`
	}{Sequence: sequence, Generation: observed.ConnectionGeneration}
	return collector.recordFact(ctx, exchangecontracts.RecordRebuild, observed.ConnectionID,
		observed.ConnectionGeneration, fact)
}

func (collector *InstrumentCollector) recordGap(
	ctx context.Context,
	connectionID string,
	generation uint64,
	first uint64,
	last uint64,
	reason string,
) error {
	now := collector.clock.Now().UTC
	gap := exchangecontracts.SourceGap{Exchange: collectorExchange, Instrument: collector.config.Instrument,
		ConnectionGeneration: generation, FirstSequence: first,
		LastSequence: last, StartedAt: now, EndedAt: now, Reason: reason}
	if err := collector.recorder.RecordSourceGap(ctx, gap); err != nil {
		return recorderFailure{err}
	}
	collector.stats.sequenceGaps.Add(1)
	return collector.recordFact(ctx, exchangecontracts.RecordGap, connectionID, generation, gap)
}

func (collector *InstrumentCollector) recordFact(
	ctx context.Context,
	kind exchangecontracts.PublicRecordKind,
	connectionID string,
	generation uint64,
	fact any,
) error {
	payload, err := json.Marshal(fact)
	if err != nil {
		return streamError()
	}
	token, err := collector.recorder.RecordPublicRaw(ctx, exchangecontracts.PublicRawRecord{
		Kind: kind, Raw: payload, Instrument: collector.config.Instrument,
		ReceivedAt: collector.clock.Now(), ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: collector.source.MonotonicOffset()})
	if err != nil {
		return recorderFailure{err}
	}
	if err = collector.recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
		Kind: kind, Token: token, Canonical: payload}); err != nil {
		return recorderFailure{err}
	}
	return nil
}
