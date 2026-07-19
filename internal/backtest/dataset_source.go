package backtest

import "axiom/internal/replay"

// DatasetSource adapts a fully verified DatasetReader to the replay controller.
type DatasetSource struct {
	reader *DatasetReader
	last   uint64
}

// NewDatasetSource preserves the dataset's canonical ordinal and logical-time order.
func NewDatasetSource(reader *DatasetReader) (*DatasetSource, error) {
	if reader == nil {
		return nil, backtestError("dataset_source_invalid")
	}
	return &DatasetSource{reader: reader}, nil
}

// NewDatasetWindowSource constrains replay to one inclusive ordinal range.
func NewDatasetWindowSource(reader *DatasetReader, first, last uint64) (*DatasetSource, error) {
	if reader == nil || first == 0 || last < first || reader.SeekOrdinal(first) != nil {
		return nil, backtestError("dataset_window_invalid")
	}
	return &DatasetSource{reader: reader, last: last}, nil
}

// Next returns one defensive replay event from the verified dataset.
func (source *DatasetSource) Next() (replay.Event, bool, error) {
	event, ok, err := source.reader.Next()
	if err != nil || !ok {
		return replay.Event{}, ok, err
	}
	if source.last > 0 && event.Record.IngestOrdinal > source.last {
		return replay.Event{}, false, nil
	}
	return replay.Event{LogicalTime: event.Record.RecordedLogicalTime, Ordinal: event.Record.IngestOrdinal,
		Canonical: append([]byte(nil), event.Record.Canonical...)}, true, nil
}

// SeekOrdinal delegates to manifest-indexed dataset seeking.
func (source *DatasetSource) SeekOrdinal(ordinal uint64) error {
	return source.reader.SeekOrdinal(ordinal)
}

var _ replay.Source = (*DatasetSource)(nil)
