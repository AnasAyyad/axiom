package runtimecore

import "sort"

// ReplayCursor yields recorded input only by logical time and ingest ordinal.
type ReplayCursor struct {
	events []Envelope
	index  int
}

// NewReplayCursor validates unique ordinals and fixes authoritative replay order.
func NewReplayCursor(events []Envelope) (*ReplayCursor, error) {
	ordered := append([]Envelope(nil), events...)
	seen := make(map[uint64]struct{}, len(ordered))
	for _, event := range ordered {
		ordinal := event.IngestOrdinal()
		if ordinal == 0 {
			return nil, runtimeError("invalid_replay", "missing_ingest_ordinal")
		}
		if _, duplicate := seen[ordinal]; duplicate {
			return nil, runtimeError("invalid_replay", "duplicate_ingest_ordinal")
		}
		seen[ordinal] = struct{}{}
	}
	sort.Slice(ordered, func(left, right int) bool {
		return replayLess(ordered[left], ordered[right])
	})
	return &ReplayCursor{events: ordered}, nil
}

// Next returns the next recorded event without scheduler reordering.
func (cursor *ReplayCursor) Next() (Envelope, bool) {
	if cursor.index >= len(cursor.events) {
		return Envelope{}, false
	}
	event := cursor.events[cursor.index]
	cursor.index++
	return event, true
}

// Remaining returns the number of unread recorded events.
func (cursor *ReplayCursor) Remaining() int { return len(cursor.events) - cursor.index }

func replayLess(left, right Envelope) bool {
	if left.LogicalTime() != right.LogicalTime() {
		return left.LogicalTime() < right.LogicalTime()
	}
	return left.IngestOrdinal() < right.IngestOrdinal()
}
