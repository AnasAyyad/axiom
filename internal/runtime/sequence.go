package runtimecore

// SequenceResult is one scoped per-source continuity decision.
type SequenceResult string

// Source sequence validation results never reorder recorded input.
const (
	SequenceAccepted   SequenceResult = "accepted"
	SequenceDuplicate  SequenceResult = "duplicate"
	SequenceRegression SequenceResult = "regression"
	SequenceGap        SequenceResult = "gap"
	SequenceInvalid    SequenceResult = "invalid_generation"
)

// SequenceTracker validates one connection generation in ingest order.
type SequenceTracker struct {
	generation uint64
	last       uint64
	started    bool
	valid      bool
}

// NewSequenceTracker starts a valid non-zero connection generation.
func NewSequenceTracker(generation uint64) (*SequenceTracker, error) {
	if generation == 0 {
		return nil, runtimeError("invalid_sequence", "generation")
	}
	return &SequenceTracker{generation: generation, valid: true}, nil
}

// Observe validates continuity without sorting or repairing evidence.
func (tracker *SequenceTracker) Observe(generation, sequence uint64) SequenceResult {
	if !tracker.valid || generation != tracker.generation || sequence == 0 {
		return SequenceInvalid
	}
	if !tracker.started {
		tracker.last, tracker.started = sequence, true
		return SequenceAccepted
	}
	if sequence == tracker.last {
		return SequenceDuplicate
	}
	if sequence < tracker.last {
		tracker.valid = false
		return SequenceRegression
	}
	if sequence != tracker.last+1 {
		tracker.valid = false
		return SequenceGap
	}
	tracker.last = sequence
	return SequenceAccepted
}
