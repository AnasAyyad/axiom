package binance

import (
	"sync"
	"sync/atomic"
	"time"
)

var latencyBounds = []time.Duration{
	10 * time.Microsecond, 50 * time.Microsecond, 100 * time.Microsecond,
	250 * time.Microsecond, 500 * time.Microsecond, time.Millisecond,
	2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
	25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
	time.Second, 5 * time.Second, 15 * time.Second, time.Duration(1<<63 - 1),
}

type durationHistogram struct {
	mutex  sync.Mutex
	counts []uint64
	total  uint64
}

func newDurationHistogram() *durationHistogram {
	return &durationHistogram{counts: make([]uint64, len(latencyBounds))}
}

func (histogram *durationHistogram) record(value time.Duration) {
	if value < 0 {
		return
	}
	histogram.mutex.Lock()
	defer histogram.mutex.Unlock()
	for index, bound := range latencyBounds {
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
			return latencyBounds[index]
		}
	}
	return latencyBounds[len(latencyBounds)-1]
}

// CollectorStats contains bounded counters and latency histograms.
type CollectorStats struct {
	messages       atomic.Uint64
	depthUpdates   atomic.Uint64
	trades         atomic.Uint64
	candles        atomic.Uint64
	reconnects     atomic.Uint64
	rebuilds       atomic.Uint64
	gaps           atomic.Uint64
	decoderErrors  atomic.Uint64
	queueHighWater atomic.Uint64
	hotPath        *durationHistogram
	resync         *durationHistogram
}

// CollectorStatsSnapshot is a stable low-cardinality qualification record.
type CollectorStatsSnapshot struct {
	Messages       uint64        `json:"messages"`
	DepthUpdates   uint64        `json:"depth_updates"`
	Trades         uint64        `json:"trades"`
	Candles        uint64        `json:"candles"`
	Reconnects     uint64        `json:"reconnects"`
	Rebuilds       uint64        `json:"rebuilds"`
	Gaps           uint64        `json:"gaps"`
	DecoderErrors  uint64        `json:"decoder_errors"`
	QueueHighWater uint64        `json:"queue_high_water"`
	HotPathP99     time.Duration `json:"hot_path_p99"`
	ResyncP95      time.Duration `json:"resync_p95"`
}

func newCollectorStats() *CollectorStats {
	return &CollectorStats{hotPath: newDurationHistogram(), resync: newDurationHistogram()}
}

// Snapshot returns qualification counters without identifiers or raw errors.
func (stats *CollectorStats) Snapshot() CollectorStatsSnapshot {
	return CollectorStatsSnapshot{Messages: stats.messages.Load(), DepthUpdates: stats.depthUpdates.Load(),
		Trades: stats.trades.Load(), Candles: stats.candles.Load(), Reconnects: stats.reconnects.Load(),
		Rebuilds: stats.rebuilds.Load(), Gaps: stats.gaps.Load(), DecoderErrors: stats.decoderErrors.Load(),
		QueueHighWater: stats.queueHighWater.Load(), HotPathP99: stats.hotPath.percentile(99, 100),
		ResyncP95: stats.resync.percentile(95, 100)}
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
