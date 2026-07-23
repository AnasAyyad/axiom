package bybit

import (
	"sync"
	"sync/atomic"
	"time"
)

var bybitLatencyBounds = []time.Duration{
	10 * time.Microsecond, 50 * time.Microsecond, 100 * time.Microsecond,
	250 * time.Microsecond, 500 * time.Microsecond, time.Millisecond,
	2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
	25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
	time.Second, 5 * time.Second, 15 * time.Second, time.Duration(1<<63 - 1),
}

var bybitResyncBounds = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second,
	20 * time.Second, 30 * time.Second, 45 * time.Second, time.Minute, 90 * time.Second,
	2 * time.Minute, 3 * time.Minute, 5 * time.Minute, time.Duration(1<<63 - 1),
}

type durationHistogram struct {
	mutex   sync.Mutex
	bounds  []time.Duration
	counts  []uint64
	total   uint64
	maximum time.Duration
}

func newDurationHistogram(bounds []time.Duration) *durationHistogram {
	return &durationHistogram{bounds: append([]time.Duration(nil), bounds...), counts: make([]uint64, len(bounds))}
}

func (histogram *durationHistogram) record(value time.Duration) {
	if value < 0 {
		return
	}
	histogram.mutex.Lock()
	defer histogram.mutex.Unlock()
	if value > histogram.maximum {
		histogram.maximum = value
	}
	for index, bound := range histogram.bounds {
		if value <= bound {
			histogram.counts[index]++
			histogram.total++
			return
		}
	}
}

func (histogram *durationHistogram) percentile(numerator, denominator uint64) time.Duration {
	total, _, percentile, _ := histogram.summary(numerator, denominator, -1)
	if total == 0 {
		return 0
	}
	return percentile
}

func (histogram *durationHistogram) summary(
	numerator, denominator uint64,
	threshold time.Duration,
) (uint64, uint64, time.Duration, time.Duration) {
	histogram.mutex.Lock()
	defer histogram.mutex.Unlock()
	var over uint64
	if threshold >= 0 {
		for index, count := range histogram.counts {
			if histogram.bounds[index] > threshold {
				over += count
			}
		}
	}
	var percentile time.Duration
	if histogram.total != 0 && numerator != 0 && numerator <= denominator {
		target := (histogram.total*numerator + denominator - 1) / denominator
		var cumulative uint64
		for index, count := range histogram.counts {
			cumulative += count
			if cumulative >= target {
				percentile = histogram.bounds[index]
				break
			}
		}
	}
	return histogram.total, over, percentile, histogram.maximum
}

// ReconnectReasonCounts is the fixed Bybit lifecycle reason set.
type ReconnectReasonCounts struct {
	Subscription     uint64 `json:"subscription"`
	Stream           uint64 `json:"stream"`
	Snapshot         uint64 `json:"snapshot"`
	Clock            uint64 `json:"clock"`
	Heartbeat        uint64 `json:"heartbeat"`
	StaleBook        uint64 `json:"stale_book"`
	Queue            uint64 `json:"queue"`
	InvalidEvent     uint64 `json:"invalid_event"`
	SequenceGap      uint64 `json:"sequence_gap"`
	ScheduledRenewal uint64 `json:"scheduled_renewal"`
}

// CollectorStats is one bounded qualification snapshot.
type CollectorStats struct {
	Connections          uint64                `json:"connections"`
	Reconnects           uint64                `json:"reconnects"`
	Snapshots            uint64                `json:"snapshots"`
	Resets               uint64                `json:"resets"`
	DepthUpdates         uint64                `json:"depth_updates"`
	Trades               uint64                `json:"trades"`
	Tickers              uint64                `json:"tickers"`
	Candles              uint64                `json:"candles"`
	Heartbeats           uint64                `json:"heartbeats"`
	DecoderErrors        uint64                `json:"decoder_errors"`
	SequenceGaps         uint64                `json:"sequence_gaps"`
	QueueOverflows       uint64                `json:"queue_overflows"`
	QueueHighWater       uint64                `json:"queue_high_water"`
	HotPathP99           time.Duration         `json:"hot_path_p99"`
	ReconnectReasons     ReconnectReasonCounts `json:"reconnect_reasons"`
	ResyncSamples        uint64                `json:"resync_samples"`
	ResyncOver15Seconds  uint64                `json:"resync_over_15_seconds"`
	ResyncP95            time.Duration         `json:"resync_p95"`
	ResyncMax            time.Duration         `json:"resync_max"`
	ReconnectDiagnostics []ReconnectDiagnostic `json:"reconnect_diagnostics,omitempty"`
	DiagnosticsDropped   uint64                `json:"diagnostics_dropped"`
	FailureCauses        map[string]uint64     `json:"failure_causes"`
}

type collectorCounters struct {
	connections      atomic.Uint64
	reconnects       atomic.Uint64
	snapshots        atomic.Uint64
	resets           atomic.Uint64
	depthUpdates     atomic.Uint64
	trades           atomic.Uint64
	tickers          atomic.Uint64
	candles          atomic.Uint64
	heartbeats       atomic.Uint64
	decoderErrors    atomic.Uint64
	sequenceGaps     atomic.Uint64
	queueOverflows   atomic.Uint64
	queueHighWater   atomic.Uint64
	reconnectReasons [reconnectReasonCount]atomic.Uint64
	hotPath          *durationHistogram
	resync           *durationHistogram
	diagnosticMutex  sync.Mutex
	diagnostics      []ReconnectDiagnostic
	diagnosticsLost  uint64
	failureCauses    map[string]uint64
}

func newCollectorCounters() *collectorCounters {
	return &collectorCounters{hotPath: newDurationHistogram(bybitLatencyBounds),
		resync: newDurationHistogram(bybitResyncBounds), failureCauses: make(map[string]uint64)}
}

func (counters *collectorCounters) snapshot() CollectorStats {
	resyncSamples, resyncOver, resyncP95, resyncMax := counters.resync.summary(95, 100, 15*time.Second)
	counters.diagnosticMutex.Lock()
	diagnostics := append([]ReconnectDiagnostic(nil), counters.diagnostics...)
	causes := make(map[string]uint64, len(counters.failureCauses))
	for cause, count := range counters.failureCauses {
		causes[cause] = count
	}
	dropped := counters.diagnosticsLost
	counters.diagnosticMutex.Unlock()
	return CollectorStats{Connections: counters.connections.Load(), Reconnects: counters.reconnects.Load(),
		Snapshots: counters.snapshots.Load(), Resets: counters.resets.Load(),
		DepthUpdates: counters.depthUpdates.Load(), Trades: counters.trades.Load(),
		Tickers: counters.tickers.Load(), Candles: counters.candles.Load(),
		Heartbeats: counters.heartbeats.Load(), DecoderErrors: counters.decoderErrors.Load(),
		SequenceGaps: counters.sequenceGaps.Load(), QueueOverflows: counters.queueOverflows.Load(),
		QueueHighWater: counters.queueHighWater.Load(), HotPathP99: counters.hotPath.percentile(99, 100),
		ReconnectReasons: ReconnectReasonCounts{
			Subscription:     counters.reconnectReasons[reconnectSubscription].Load(),
			Stream:           counters.reconnectReasons[reconnectStream].Load(),
			Snapshot:         counters.reconnectReasons[reconnectSnapshot].Load(),
			Clock:            counters.reconnectReasons[reconnectClock].Load(),
			Heartbeat:        counters.reconnectReasons[reconnectHeartbeat].Load(),
			StaleBook:        counters.reconnectReasons[reconnectStaleBook].Load(),
			Queue:            counters.reconnectReasons[reconnectQueue].Load(),
			InvalidEvent:     counters.reconnectReasons[reconnectInvalidEvent].Load(),
			SequenceGap:      counters.reconnectReasons[reconnectSequenceGap].Load(),
			ScheduledRenewal: counters.reconnectReasons[reconnectScheduledRenewal].Load(),
		}, ResyncSamples: resyncSamples, ResyncOver15Seconds: resyncOver,
		ResyncP95: resyncP95, ResyncMax: resyncMax,
		ReconnectDiagnostics: diagnostics, DiagnosticsDropped: dropped, FailureCauses: causes}
}

func (counters *collectorCounters) recordReconnectReason(reason reconnectReason) {
	if reason.valid() {
		counters.reconnectReasons[reason].Add(1)
	}
}

func (counters *collectorCounters) recordDiagnostic(diagnostic ReconnectDiagnostic) {
	counters.diagnosticMutex.Lock()
	defer counters.diagnosticMutex.Unlock()
	if len(counters.diagnostics) == maximumReconnectDiagnostics {
		copy(counters.diagnostics, counters.diagnostics[1:])
		counters.diagnostics = counters.diagnostics[:maximumReconnectDiagnostics-1]
		counters.diagnosticsLost++
	}
	counters.diagnostics = append(counters.diagnostics, diagnostic)
	if diagnostic.Cause != "" && diagnostic.Cause != "success" && diagnostic.Cause != "healthy" &&
		diagnostic.Phase != "health_restored" {
		cause := diagnostic.Cause
		if !validBoundedCause(cause) {
			cause = "unclassified"
		}
		if _, exists := counters.failureCauses[cause]; exists || len(counters.failureCauses) < 64 {
			counters.failureCauses[cause]++
		} else {
			counters.failureCauses["unclassified"]++
		}
	}
}

func (counters *collectorCounters) observeQueue(depth int) {
	value := uint64(depth)
	for {
		prior := counters.queueHighWater.Load()
		if value <= prior || counters.queueHighWater.CompareAndSwap(prior, value) {
			return
		}
	}
}

func validBoundedCause(cause string) bool {
	if len(cause) == 0 || len(cause) > 64 {
		return false
	}
	for _, value := range cause {
		if (value < 'a' || value > 'z') && value != '_' && (value < '0' || value > '9') {
			return false
		}
	}
	return true
}
