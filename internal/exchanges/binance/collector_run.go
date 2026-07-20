package binance

import (
	"context"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type observedResult struct {
	event ObservedStreamEvent
	err   error
}

type snapshotResult struct {
	snapshot exchangecontracts.BookSnapshot
	token    StreamRecordToken
	err      error
}

func (collector *InstrumentCollector) runGeneration(ctx context.Context, resyncStarted time.Time) generationOutcome {
	stream, connectionID, generation, outcome, ok := collector.beginGeneration(ctx)
	if !ok {
		return outcome
	}
	defer stream.Close()
	channels := collector.startSynchronization(ctx, stream, connectionID, generation)
	return collector.awaitSynchronization(ctx, channels, connectionID, generation, resyncStarted)
}

func (collector *InstrumentCollector) beginGeneration(
	ctx context.Context,
) (ObservedStream, string, uint64, generationOutcome, bool) {
	stream, err := collector.source.SubscribeRecorded(ctx, exchangecontracts.StreamRequest{
		Instrument: collector.config.Instrument,
		Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth, exchangecontracts.StreamTrades,
			exchangecontracts.StreamCandle},
	}, collector.recorder)
	if err != nil {
		return nil, "", 0, generationOutcome{reason: reconnectSubscription}, false
	}
	connectionID, generation := stream.ConnectionID(), stream.Generation()
	if err = collector.book.BeginGeneration(connectionID, generation); err != nil {
		_ = stream.Close()
		return nil, "", 0, generationOutcome{fatal: err}, false
	}
	if _, err = collector.recordFact(ctx, RecordLifecycle, connectionID, generation,
		lifecycleFact{State: "SYNCING", Generation: generation}); err != nil {
		_ = stream.Close()
		return nil, "", 0, generationOutcome{fatal: err}, false
	}
	if _, err = collector.recordFact(ctx, RecordSubscription, connectionID, generation,
		subscriptionFact{Streams: []string{"depth@100ms", "kline_4h", "trade"}, Generation: generation}); err != nil {
		_ = stream.Close()
		return nil, "", 0, generationOutcome{fatal: err}, false
	}
	health, _, err := collector.source.SampleServerTimeRecorded(ctx, collector.config.Instrument,
		connectionID, generation, collector.recorder)
	if isRecorderFailure(err) {
		_ = stream.Close()
		return nil, "", 0, generationOutcome{fatal: err}, false
	}
	if err != nil || !health.Eligible {
		outcome := collector.pauseOutcome(ctx, connectionID, generation, 0, reconnectClock)
		_ = stream.Close()
		return nil, "", 0, outcome, false
	}
	return stream, connectionID, generation, generationOutcome{}, true
}

type generationChannels struct {
	events    chan observedResult
	overflow  chan struct{}
	snapshots chan snapshotResult
}

func (collector *InstrumentCollector) startSynchronization(
	ctx context.Context,
	stream ObservedStream,
	connectionID string,
	generation uint64,
) generationChannels {
	events := make(chan observedResult, collector.config.QueueCapacity)
	overflow := make(chan struct{}, 1)
	go collector.receiveGeneration(ctx, stream, events, overflow)
	snapshots := make(chan snapshotResult, 1)
	go func() {
		snapshot, token, snapshotErr := collector.source.SnapshotRecorded(ctx,
			exchangecontracts.SnapshotRequest{Instrument: collector.config.Instrument,
				Depth: collector.config.SnapshotDepth}, connectionID, generation, collector.recorder)
		snapshots <- snapshotResult{snapshot: snapshot, token: token, err: snapshotErr}
	}()
	return generationChannels{events: events, overflow: overflow, snapshots: snapshots}
}

func (collector *InstrumentCollector) awaitSynchronization(
	ctx context.Context,
	channels generationChannels,
	connectionID string,
	generation uint64,
	resyncStarted time.Time,
) generationOutcome {
	pending := make([]ObservedStreamEvent, 0, collector.config.QueueCapacity)
	for {
		select {
		case <-ctx.Done():
			return generationOutcome{}
		case <-channels.overflow:
			return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), reconnectQueue)
		case observed := <-channels.events:
			if observed.err != nil {
				return collector.streamFailureOutcome(ctx, connectionID, generation, observed.err)
			}
			collector.stats.observeQueue(len(channels.events))
			if observed.event.Event.Kind == exchangecontracts.StreamDepth {
				if len(pending) == cap(pending) {
					return collector.pauseOutcome(ctx, connectionID, generation, 0, reconnectBuffer)
				}
				pending = append(pending, observed.event)
			}
		case result := <-channels.snapshots:
			return collector.completeSynchronization(ctx, channels, result, pending, connectionID, generation, resyncStarted)
		}
	}
}

func (collector *InstrumentCollector) streamFailureOutcome(
	ctx context.Context,
	connectionID string,
	generation uint64,
	err error,
) generationOutcome {
	if isRecorderFailure(err) {
		return generationOutcome{fatal: err}
	}
	if exchangecontracts.KindOf(err) == exchangecontracts.ErrorValidation {
		collector.stats.decoderErrors.Add(1)
	}
	return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), reconnectStream)
}

func (collector *InstrumentCollector) completeSynchronization(
	ctx context.Context,
	channels generationChannels,
	result snapshotResult,
	pending []ObservedStreamEvent,
	connectionID string,
	generation uint64,
	resyncStarted time.Time,
) generationOutcome {
	if isRecorderFailure(result.err) {
		return generationOutcome{fatal: result.err}
	}
	if result.err != nil || result.token.IngestOrdinal == 0 {
		return collector.pauseOutcome(ctx, connectionID, generation, 0, reconnectSnapshot)
	}
	if err := collector.installSnapshot(ctx, result, pending, connectionID, generation); err != nil {
		if isFatalCollectorError(err) {
			return generationOutcome{fatal: err}
		}
		return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), reconnectSnapshotBridge)
	}
	collector.stats.rebuilds.Add(1)
	if _, err := collector.recordFact(ctx, RecordRebuild, connectionID, generation,
		rebuildFact{SnapshotSequence: result.snapshot.LastSequence, BufferedDepth: len(pending), Generation: generation}); err != nil {
		return generationOutcome{fatal: err}
	}
	if _, err := collector.recordFact(ctx, RecordLifecycle, connectionID, generation,
		lifecycleFact{State: "HEALTHY", Generation: generation}); err != nil {
		return generationOutcome{fatal: err}
	}
	collector.recordResynchronization(resyncStarted)
	outcome := collector.runHealthy(ctx, channels.events, channels.overflow, connectionID, generation)
	outcome.reachedHealthy = true
	return outcome
}

func (collector *InstrumentCollector) receiveGeneration(
	ctx context.Context,
	stream ObservedStream,
	events chan<- observedResult,
	overflow chan<- struct{},
) {
	for {
		event, err := stream.ReceiveObserved(ctx)
		result := observedResult{event: event, err: err}
		select {
		case events <- result:
		case <-ctx.Done():
			return
		default:
			select {
			case overflow <- struct{}{}:
			default:
			}
			_ = stream.Close()
			return
		}
		if err != nil {
			return
		}
	}
}
