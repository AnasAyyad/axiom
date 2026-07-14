package runtimecore

import "sync/atomic"

// IngestOrdinals assigns one monotonic order before any concurrent fan-out.
type IngestOrdinals struct{ next atomic.Uint64 }

// Next returns the next non-zero session-local ingest ordinal.
func (ordinals *IngestOrdinals) Next() (uint64, error) {
	next := ordinals.next.Add(1)
	if next == 0 {
		return 0, runtimeError("ingest_ordinal_exhausted", "session")
	}
	return next, nil
}
