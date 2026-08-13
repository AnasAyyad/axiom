package runtimecore

import (
	"math"
	"sync/atomic"
)

// IngestOrdinals assigns one monotonic order before any concurrent fan-out.
type IngestOrdinals struct{ next atomic.Uint64 }

// NewIngestOrdinalsAfter restores a durable session-local ordinal fence.
func NewIngestOrdinalsAfter(last uint64) (*IngestOrdinals, error) {
	if last == math.MaxUint64 {
		return nil, runtimeError("ingest_ordinal_exhausted", "session")
	}
	value := &IngestOrdinals{}
	value.next.Store(last)
	return value, nil
}

// Next returns the next non-zero session-local ingest ordinal.
func (ordinals *IngestOrdinals) Next() (uint64, error) {
	next := ordinals.next.Add(1)
	if next == 0 {
		return 0, runtimeError("ingest_ordinal_exhausted", "session")
	}
	return next, nil
}
