package bootstrap

import (
	"encoding/json"
	"fmt"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/replay"
)

func (processor *evaluationMarketProcessor) reducePublicEvidence(event replay.Event) ([]exchangecontracts.Candle, error) {
	var historical exchangecontracts.Candle
	if json.Unmarshal(event.Canonical, &historical) == nil && historical.Interval != "" && historical.Instrument.Base != "" {
		if !historical.Closed || historical.RawPayloadHash == "" {
			return nil, fmt.Errorf("evaluation_historical_candle_invalid")
		}
		processor.addCandle(historical)
		return []exchangecontracts.Candle{historical}, nil
	}
	var stream exchangecontracts.StreamEvent
	if json.Unmarshal(event.Canonical, &stream) == nil && stream.Kind != "" {
		if stream.Snapshot != nil {
			if err := processor.replaceBook(*stream.Snapshot, event.Ordinal, event.LogicalTime); err != nil {
				return nil, err
			}
		}
		if stream.Depth != nil {
			if err := processor.applyDepth(*stream.Depth, event.Ordinal, event.LogicalTime); err != nil {
				return nil, err
			}
		}
		if stream.Candle != nil && stream.Candle.Closed {
			processor.addCandle(*stream.Candle)
			return []exchangecontracts.Candle{*stream.Candle}, nil
		}
		return nil, nil
	}
	var gap exchangecontracts.SourceGap
	if json.Unmarshal(event.Canonical, &gap) == nil && gap.Instrument.Base != "" &&
		gap.FirstSequence > 0 && gap.LastSequence >= gap.FirstSequence && gap.Reason != "" {
		processor.invalidateGapBooks(gap)
		return nil, nil
	}
	var snapshot exchangecontracts.BookSnapshot
	if json.Unmarshal(event.Canonical, &snapshot) == nil && snapshot.Exchange != "" &&
		snapshot.Instrument.Base != "" && snapshot.LastSequence > 0 &&
		len(snapshot.Bids) > 0 && len(snapshot.Asks) > 0 {
		return nil, processor.replaceBook(snapshot, event.Ordinal, event.LogicalTime)
	}
	// Lifecycle, subscription, heartbeat, rebuild, and bounded decoder evidence
	// remain audit inputs. A declared gap invalidates its local replay book above;
	// only a later full snapshot makes that book eligible again.
	return nil, nil
}

func (processor *evaluationMarketProcessor) invalidateGapBooks(gap exchangecontracts.SourceGap) {
	if gap.Exchange != "" {
		if book := processor.books[evaluationBookKey(string(gap.Exchange), gap.Instrument)]; book != nil {
			book.valid = false
		}
		return
	}
	// Older manifests did not embed the exchange in the canonical gap fact.
	// Conservatively invalidate every matching venue rather than guessing.
	for _, book := range processor.books {
		if book.instrument == gap.Instrument {
			book.valid = false
		}
	}
}
