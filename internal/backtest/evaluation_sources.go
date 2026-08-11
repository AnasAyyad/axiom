package backtest

import (
	"encoding/json"
	"sort"

	contracts "axiom/internal/exchanges/contracts"
	"axiom/internal/replay"
)

// EvaluationCandleSource expands evidence-preserving historical REST pages
// into one deterministic completed-candle event per ordinal. The page files
// remain the immutable source; this is only a replay view over their canonical
// arrays and therefore does not invent order-book facts.
type EvaluationCandleSource struct {
	events []replay.Event
	index  int
}

// NewEvaluationCandleSource constructs a deterministic candle-event view over
// a verified historical dataset.
func NewEvaluationCandleSource(reader *DatasetReader) (*EvaluationCandleSource, error) {
	if reader == nil {
		return nil, backtestError("evaluation_candle_source_invalid")
	}
	values := make([]contracts.Candle, 0)
	for {
		row, ok, err := reader.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		var page []contracts.Candle
		if json.Unmarshal(row.Record.Canonical, &page) != nil || len(page) == 0 {
			return nil, backtestError("evaluation_candle_page_invalid")
		}
		values = append(values, page...)
	}
	if len(values) == 0 {
		return nil, backtestError("evaluation_candle_source_empty")
	}
	sort.SliceStable(values, func(left, right int) bool {
		return values[left].OpenTime.Before(values[right].OpenTime)
	})
	events := make([]replay.Event, 0, len(values))
	var prior int64
	for _, candle := range values {
		if !candle.Closed || candle.CloseTime.UnixNano() <= 0 || candle.CloseTime.UnixNano() <= prior {
			return nil, backtestError("evaluation_candle_order_invalid")
		}
		payload, err := json.Marshal(candle)
		if err != nil {
			return nil, backtestError("evaluation_candle_encode_failed")
		}
		events = append(events, replay.Event{Ordinal: uint64(len(events) + 1),
			LogicalTime: uint64(candle.CloseTime.UnixNano()), Canonical: payload})
		prior = candle.CloseTime.UnixNano()
	}
	return &EvaluationCandleSource{events: events}, nil
}

// Next returns the next immutable historical candle event.
func (source *EvaluationCandleSource) Next() (replay.Event, bool, error) {
	if source == nil || source.index >= len(source.events) {
		return replay.Event{}, false, nil
	}
	value := source.events[source.index]
	source.index++
	value.Canonical = append([]byte(nil), value.Canonical...)
	return value, true, nil
}

// SeekOrdinal positions the next historical candle read at an exact ordinal.
func (source *EvaluationCandleSource) SeekOrdinal(ordinal uint64) error {
	if source == nil || ordinal == 0 || ordinal > uint64(len(source.events)) {
		return backtestError("evaluation_candle_seek_invalid")
	}
	source.index = int(ordinal - 1)
	return nil
}

// RecordCount returns the fixed number of normalized candle events.
func (source *EvaluationCandleSource) RecordCount() uint64 {
	if source == nil {
		return 0
	}
	return uint64(len(source.events))
}

// EvaluationMergedSource merges exchange-child datasets by the globally
// shared recorder ordinal. It retains streaming behavior and at most one
// buffered event per child, so a seven-day full-book dataset is not loaded
// into memory.
type EvaluationMergedSource struct {
	children []*DatasetReader
	buffer   []DatasetEvent
	present  []bool
	active   []bool
}

// NewEvaluationMergedSource merges verified exchange datasets by their shared
// recorder ordinal without buffering the complete dataset.
func NewEvaluationMergedSource(readers ...*DatasetReader) (*EvaluationMergedSource, error) {
	if len(readers) < 2 {
		return nil, backtestError("evaluation_merge_source_invalid")
	}
	for _, reader := range readers {
		if reader == nil {
			return nil, backtestError("evaluation_merge_source_invalid")
		}
	}
	return &EvaluationMergedSource{children: append([]*DatasetReader(nil), readers...),
		buffer: make([]DatasetEvent, len(readers)), present: make([]bool, len(readers)),
		active: func() []bool {
			values := make([]bool, len(readers))
			for index := range values {
				values[index] = true
			}
			return values
		}()}, nil
}

// Next returns the next globally ordered recorded event.
func (source *EvaluationMergedSource) Next() (replay.Event, bool, error) {
	if source == nil {
		return replay.Event{}, false, backtestError("evaluation_merge_source_invalid")
	}
	for index, reader := range source.children {
		if !source.active[index] {
			continue
		}
		if source.present[index] {
			continue
		}
		value, ok, err := reader.Next()
		if err != nil {
			return replay.Event{}, false, err
		}
		if ok {
			source.buffer[index], source.present[index] = value, true
		}
	}
	selected := -1
	for index := range source.buffer {
		if !source.present[index] {
			continue
		}
		if selected < 0 || source.buffer[index].Record.IngestOrdinal < source.buffer[selected].Record.IngestOrdinal {
			selected = index
		}
	}
	if selected < 0 {
		return replay.Event{}, false, nil
	}
	value := source.buffer[selected].Record
	source.present[selected] = false
	for index := range source.buffer {
		if index != selected && source.present[index] &&
			source.buffer[index].Record.IngestOrdinal == value.IngestOrdinal {
			return replay.Event{}, false, backtestError("evaluation_merge_ordinal_conflict")
		}
	}
	return replay.Event{Ordinal: value.IngestOrdinal, LogicalTime: value.RecordedLogicalTime,
		Canonical: append([]byte(nil), value.Canonical...)}, true, nil
}

// SeekOrdinal positions every child at or after the requested shared ordinal.
func (source *EvaluationMergedSource) SeekOrdinal(ordinal uint64) error {
	if source == nil || ordinal == 0 {
		return backtestError("evaluation_merge_seek_invalid")
	}
	for index, reader := range source.children {
		source.active[index] = true
		if err := reader.SeekAtOrAfter(ordinal); err != nil {
			first, last, rangeErr := reader.OrdinalRange()
			if rangeErr != nil || ordinal <= last || first == 0 {
				return backtestError("evaluation_merge_seek_invalid")
			}
			source.active[index] = false
		}
		source.present[index] = false
	}
	return nil
}

// EvaluationWindowSource constrains any deterministic evaluation source to an
// inclusive ordinal window without changing its canonical evidence.
type EvaluationWindowSource struct {
	delegate replay.Source
	last     uint64
}

// NewEvaluationWindowSource constrains a source to one inclusive ordinal
// window while retaining its canonical events unchanged.
func NewEvaluationWindowSource(delegate replay.Source, first, last uint64) (*EvaluationWindowSource, error) {
	if delegate == nil || first == 0 || last < first || delegate.SeekOrdinal(first) != nil {
		return nil, backtestError("evaluation_window_invalid")
	}
	return &EvaluationWindowSource{delegate: delegate, last: last}, nil
}

// Next returns the next event only while it remains inside the fixed window.
func (source *EvaluationWindowSource) Next() (replay.Event, bool, error) {
	value, ok, err := source.delegate.Next()
	if err != nil || !ok {
		return replay.Event{}, ok, err
	}
	if value.Ordinal > source.last {
		return replay.Event{}, false, nil
	}
	return value, true, nil
}

// SeekOrdinal seeks within the fixed inclusive evaluation window.
func (source *EvaluationWindowSource) SeekOrdinal(ordinal uint64) error {
	if ordinal == 0 || ordinal > source.last {
		return backtestError("evaluation_window_seek_invalid")
	}
	return source.delegate.SeekOrdinal(ordinal)
}

var _ replay.Source = (*EvaluationCandleSource)(nil)
var _ replay.Source = (*EvaluationMergedSource)(nil)
var _ replay.Source = (*EvaluationWindowSource)(nil)
