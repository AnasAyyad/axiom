package binance

import (
	"sync"
	"sync/atomic"
	"time"
)

type reconnectReason uint8

const (
	reconnectNone reconnectReason = iota
	reconnectSubscription
	reconnectStream
	reconnectSnapshot
	reconnectSnapshotBridge
	reconnectClock
	reconnectStaleBook
	reconnectQueue
	reconnectBuffer
	reconnectInvalidEvent
	reconnectSequenceGap
	reconnectScheduledRenewal
	reconnectReasonCount
)

var reconnectReasonNames = [...]string{
	"", "subscription", "stream", "snapshot", "snapshot_bridge", "clock", "stale_book",
	"queue", "buffer", "invalid_event", "sequence_gap", "scheduled_renewal",
}

func (reason reconnectReason) valid() bool {
	return reason > reconnectNone && reason < reconnectReasonCount
}

// String returns the fixed bounded evidence value.
func (reason reconnectReason) String() string {
	if !reason.valid() {
		return ""
	}
	return reconnectReasonNames[reason]
}

var latencyBounds = []time.Duration{
	10 * time.Microsecond, 50 * time.Microsecond, 100 * time.Microsecond,
	250 * time.Microsecond, 500 * time.Microsecond, time.Millisecond,
	2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
	25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
	time.Second, 5 * time.Second, 15 * time.Second, time.Duration(1<<63 - 1),
}
var resyncBounds = []time.Duration{
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
	return &durationHistogram{bounds: bounds, counts: make([]uint64, len(bounds))}
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
	histogram.mutex.Lock()
	defer histogram.mutex.Unlock()
	if histogram.total == 0 || numerator == 0 || numerator > denominator {
		return 0
	}
	target := (histogram.total*numerator + denominator - 1) / denominator
	var cumulative uint64
	for index, count := range histogram.counts {
		cumulative += count
		if cumulative >= target {
			return histogram.bounds[index]
		}
	}
	return histogram.bounds[len(histogram.bounds)-1]
}

func (histogram *durationHistogram) summary(
	numerator, denominator uint64,
	threshold time.Duration,
) (uint64, uint64, time.Duration, time.Duration) {
	histogram.mutex.Lock()
	defer histogram.mutex.Unlock()
	var over uint64
	for index, count := range histogram.counts {
		if histogram.bounds[index] > threshold {
			over += count
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

// CollectorStats contains bounded counters and latency histograms.
type CollectorStats struct {
	messages         atomic.Uint64
	depthUpdates     atomic.Uint64
	trades           atomic.Uint64
	candles          atomic.Uint64
	reconnects       atomic.Uint64
	rebuilds         atomic.Uint64
	gaps             atomic.Uint64
	decoderErrors    atomic.Uint64
	queueHighWater   atomic.Uint64
	hotPath          *durationHistogram
	resync           *durationHistogram
	reconnectReasons [reconnectReasonCount]atomic.Uint64
	diagnosticMutex  sync.Mutex
	diagnostics      []ReconnectDiagnostic
	diagnosticsLost  uint64
}

// ReconnectReasonCounts is a fixed bounded set of lifecycle causes.
type ReconnectReasonCounts struct {
	Subscription     uint64 `json:"subscription"`
	Stream           uint64 `json:"stream"`
	Snapshot         uint64 `json:"snapshot"`
	SnapshotBridge   uint64 `json:"snapshot_bridge"`
	Clock            uint64 `json:"clock"`
	StaleBook        uint64 `json:"stale_book"`
	Queue            uint64 `json:"queue"`
	Buffer           uint64 `json:"buffer"`
	InvalidEvent     uint64 `json:"invalid_event"`
	SequenceGap      uint64 `json:"sequence_gap"`
	ScheduledRenewal uint64 `json:"scheduled_renewal"`
}

// CollectorStatsSnapshot is a stable low-cardinality qualification record.
type CollectorStatsSnapshot struct {
	Messages             uint64                `json:"messages"`
	DepthUpdates         uint64                `json:"depth_updates"`
	Trades               uint64                `json:"trades"`
	Candles              uint64                `json:"candles"`
	Reconnects           uint64                `json:"reconnects"`
	Rebuilds             uint64                `json:"rebuilds"`
	Gaps                 uint64                `json:"gaps"`
	DecoderErrors        uint64                `json:"decoder_errors"`
	QueueHighWater       uint64                `json:"queue_high_water"`
	HotPathP99           time.Duration         `json:"hot_path_p99"`
	ReconnectReasons     ReconnectReasonCounts `json:"reconnect_reasons"`
	ResyncSamples        uint64                `json:"resync_samples"`
	ResyncOver15Seconds  uint64                `json:"resync_over_15_seconds"`
	ResyncP95            time.Duration         `json:"resync_p95"`
	ResyncMax            time.Duration         `json:"resync_max"`
	ReconnectDiagnostics []ReconnectDiagnostic `json:"reconnect_diagnostics,omitempty"`
	DiagnosticsDropped   uint64                `json:"diagnostics_dropped"`
}

func newCollectorStats() *CollectorStats {
	return &CollectorStats{hotPath: newDurationHistogram(latencyBounds), resync: newDurationHistogram(resyncBounds)}
}

// Snapshot returns qualification counters without identifiers or raw errors.
func (stats *CollectorStats) Snapshot() CollectorStatsSnapshot {
	resyncSamples, resyncOver, resyncP95, resyncMax := stats.resync.summary(95, 100, 15*time.Second)
	stats.diagnosticMutex.Lock()
	diagnostics := append([]ReconnectDiagnostic(nil), stats.diagnostics...)
	diagnosticsLost := stats.diagnosticsLost
	stats.diagnosticMutex.Unlock()
	return CollectorStatsSnapshot{Messages: stats.messages.Load(), DepthUpdates: stats.depthUpdates.Load(),
		Trades: stats.trades.Load(), Candles: stats.candles.Load(), Reconnects: stats.reconnects.Load(),
		Rebuilds: stats.rebuilds.Load(), Gaps: stats.gaps.Load(), DecoderErrors: stats.decoderErrors.Load(),
		QueueHighWater: stats.queueHighWater.Load(), HotPathP99: stats.hotPath.percentile(99, 100),
		ReconnectReasons: ReconnectReasonCounts{
			Subscription:     stats.reconnectReasons[reconnectSubscription].Load(),
			Stream:           stats.reconnectReasons[reconnectStream].Load(),
			Snapshot:         stats.reconnectReasons[reconnectSnapshot].Load(),
			SnapshotBridge:   stats.reconnectReasons[reconnectSnapshotBridge].Load(),
			Clock:            stats.reconnectReasons[reconnectClock].Load(),
			StaleBook:        stats.reconnectReasons[reconnectStaleBook].Load(),
			Queue:            stats.reconnectReasons[reconnectQueue].Load(),
			Buffer:           stats.reconnectReasons[reconnectBuffer].Load(),
			InvalidEvent:     stats.reconnectReasons[reconnectInvalidEvent].Load(),
			SequenceGap:      stats.reconnectReasons[reconnectSequenceGap].Load(),
			ScheduledRenewal: stats.reconnectReasons[reconnectScheduledRenewal].Load(),
		}, ResyncSamples: resyncSamples, ResyncOver15Seconds: resyncOver,
		ResyncP95: resyncP95, ResyncMax: resyncMax,
		ReconnectDiagnostics: diagnostics, DiagnosticsDropped: diagnosticsLost}
}

func (stats *CollectorStats) recordDiagnostic(diagnostic ReconnectDiagnostic) {
	stats.diagnosticMutex.Lock()
	defer stats.diagnosticMutex.Unlock()
	if len(stats.diagnostics) == maximumReconnectDiagnostics {
		copy(stats.diagnostics, stats.diagnostics[1:])
		stats.diagnostics = stats.diagnostics[:maximumReconnectDiagnostics-1]
		stats.diagnosticsLost++
	}
	stats.diagnostics = append(stats.diagnostics, diagnostic)
}

func (stats *CollectorStats) recordReconnectReason(reason reconnectReason) {
	if reason.valid() {
		stats.reconnectReasons[reason].Add(1)
	}
}

func (stats *CollectorStats) observeQueue(depth int) {
	value := uint64(depth)
	for {
		prior := stats.queueHighWater.Load()
		if value <= prior || stats.queueHighWater.CompareAndSwap(prior, value) {
			return
		}
	}
}
