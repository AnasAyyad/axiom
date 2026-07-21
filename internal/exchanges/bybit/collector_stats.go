package bybit

import "sync/atomic"

// CollectorStats is one bounded qualification snapshot.
type CollectorStats struct {
	Connections    uint64 `json:"connections"`
	Reconnects     uint64 `json:"reconnects"`
	Snapshots      uint64 `json:"snapshots"`
	Resets         uint64 `json:"resets"`
	DepthUpdates   uint64 `json:"depth_updates"`
	Trades         uint64 `json:"trades"`
	Tickers        uint64 `json:"tickers"`
	Candles        uint64 `json:"candles"`
	Heartbeats     uint64 `json:"heartbeats"`
	DecoderErrors  uint64 `json:"decoder_errors"`
	SequenceGaps   uint64 `json:"sequence_gaps"`
	QueueOverflows uint64 `json:"queue_overflows"`
}

type collectorCounters struct {
	connections    atomic.Uint64
	reconnects     atomic.Uint64
	snapshots      atomic.Uint64
	resets         atomic.Uint64
	depthUpdates   atomic.Uint64
	trades         atomic.Uint64
	tickers        atomic.Uint64
	candles        atomic.Uint64
	heartbeats     atomic.Uint64
	decoderErrors  atomic.Uint64
	sequenceGaps   atomic.Uint64
	queueOverflows atomic.Uint64
}

func (counters *collectorCounters) snapshot() CollectorStats {
	return CollectorStats{Connections: counters.connections.Load(), Reconnects: counters.reconnects.Load(),
		Snapshots: counters.snapshots.Load(), Resets: counters.resets.Load(),
		DepthUpdates: counters.depthUpdates.Load(), Trades: counters.trades.Load(),
		Tickers: counters.tickers.Load(), Candles: counters.candles.Load(),
		Heartbeats: counters.heartbeats.Load(), DecoderErrors: counters.decoderErrors.Load(),
		SequenceGaps: counters.sequenceGaps.Load(), QueueOverflows: counters.queueOverflows.Load()}
}
