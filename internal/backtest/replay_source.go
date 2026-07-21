package backtest

import "axiom/internal/replay"

// ReplaySource adapts a verified bounded dataset reader to replay controls.
type ReplaySource struct{ Reader *DatasetReader }

// Next yields one defensive canonical replay event.
func (source ReplaySource) Next() (replay.Event, bool, error) {
	if source.Reader == nil {
		return replay.Event{}, false, backtestError("dataset_reader_missing")
	}
	event, ok, err := source.Reader.Next()
	if err != nil || !ok {
		return replay.Event{}, ok, err
	}
	return replay.Event{LogicalTime: event.Record.RecordedLogicalTime,
		Ordinal: event.Record.IngestOrdinal, Canonical: event.Record.Canonical}, true, nil
}

// SeekOrdinal positions the verified dataset reader using manifest indexes.
func (source ReplaySource) SeekOrdinal(ordinal uint64) error {
	if source.Reader == nil {
		return backtestError("dataset_reader_missing")
	}
	return source.Reader.SeekOrdinal(ordinal)
}

var _ replay.Source = ReplaySource{}
