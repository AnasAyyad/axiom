package marketdata

import (
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestBookBridgesBufferedDeltasAndPublishesExactViews(t *testing.T) {
	instrument := testInstrument(t)
	defects := make([]Defect, 0)
	book, err := NewBook("binance", instrument, 10, 20, func(defect Defect) { defects = append(defects, defect) })
	if err != nil || book.BeginGeneration("connection-1", 1) != nil {
		t.Fatal(err)
	}
	update := testDepth(t, instrument, 101, 102,
		[][2]string{{"100", "1.5"}}, [][2]string{{"101", "0"}, {"101.5", "2"}})
	if err = book.Buffer(DepthEvent{Update: update, Observation: testObservation(1, 102, 2, 20)}); err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(t, instrument, 100)
	if err = book.InstallSnapshot(snapshot, testObservation(1, 100, 1, 10)); err != nil {
		t.Fatal(err)
	}
	view := book.View()
	if view.Health() != HealthHealthy || view.Sequence() != 102 || view.Version() != 2 || len(defects) != 0 {
		t.Fatalf("unexpected bridged view: %#v defects=%#v", view.record, defects)
	}
	vwap, notional, err := view.VWAPToBuyBase(testQuantity(t, "3"), 8)
	if err != nil || vwap.String() != "101.66666667" || notional.String() != "305" {
		t.Fatalf("buy VWAP = %q/%q, %v", vwap.String(), notional.String(), err)
	}
	slippage, _ := domain.ParsePercent("0.01")
	depth, err := view.MaxBaseWithinSlippage(domain.SideBuy, slippage, 8)
	if err != nil || depth.String() != "5" {
		t.Fatalf("slippage depth = %q, %v", depth.String(), err)
	}
	bids := view.Bids()
	bids[0].Quantity = testQuantity(t, "999")
	if book.View().Bids()[0].Quantity.String() != "1.5" {
		t.Fatal("caller mutated published book")
	}
	if !view.Eligible(25, 10*time.Nanosecond) || view.Eligible(33, 10*time.Nanosecond) {
		t.Fatal("monotonic age eligibility is incorrect")
	}
}

func TestBookFailsClosedOnGapCrossStaleAndChecksum(t *testing.T) {
	tests := []struct {
		name string
		act  func(*testing.T, *Book, domain.Instrument) error
		code string
	}{
		{name: "gap", code: "sequence_gap", act: func(t *testing.T, book *Book, instrument domain.Instrument) error {
			return book.Apply(DepthEvent{Update: testDepth(t, instrument, 102, 103, nil, nil),
				Observation: testObservation(1, 103, 2, 20)})
		}},
		{name: "cross", code: "book_crossed", act: func(t *testing.T, book *Book, instrument domain.Instrument) error {
			return book.Apply(DepthEvent{Update: testDepth(t, instrument, 101, 101, [][2]string{{"102", "1"}}, nil),
				Observation: testObservation(1, 101, 2, 20)})
		}},
		{name: "stale", code: "book_stale", act: func(_ *testing.T, book *Book, _ domain.Instrument) error {
			return book.MarkStale(30, 5)
		}},
		{name: "checksum", code: "checksum_mismatch", act: func(_ *testing.T, book *Book, _ domain.Instrument) error {
			return book.VerifyChecksum(false)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instrument := testInstrument(t)
			book, _ := NewBook("binance", instrument, 10, 20, nil)
			_ = book.BeginGeneration("connection-1", 1)
			if err := book.InstallSnapshot(testSnapshot(t, instrument, 100), testObservation(1, 100, 1, 10)); err != nil {
				t.Fatal(err)
			}
			if err := test.act(t, book, instrument); errorCode(err) != test.code || book.View().Health() != HealthPaused {
				t.Fatalf("failure = %v, state=%s", err, book.View().Health())
			}
		})
	}
}

func TestBookDuplicateIsIdempotentAndBufferIsBounded(t *testing.T) {
	instrument := testInstrument(t)
	book, _ := NewBook("binance", instrument, 2, 2, nil)
	_ = book.BeginGeneration("connection-1", 1)
	event := DepthEvent{Update: testDepth(t, instrument, 101, 101, nil, nil), Observation: testObservation(1, 101, 1, 10)}
	firstErr, secondErr := book.Buffer(event), book.Buffer(event)
	if firstErr != nil || secondErr != nil {
		t.Fatal("bounded duplicate buffering failed")
	}
	if err := book.Buffer(event); errorCode(err) != "buffer_overflow" {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestBookRejectsConflictingDuplicate(t *testing.T) {
	instrument := testInstrument(t)
	book, _ := NewBook("binance", instrument, 2, 4, nil)
	_ = book.BeginGeneration("connection-1", 1)
	if err := book.InstallSnapshot(testSnapshot(t, instrument, 100), testObservation(1, 100, 1, 10)); err != nil {
		t.Fatal(err)
	}
	first := testDepth(t, instrument, 101, 101, nil, nil)
	if err := book.Apply(DepthEvent{Update: first, Observation: testObservation(1, 101, 2, 20)}); err != nil {
		t.Fatal(err)
	}
	conflict := first
	conflict.RawPayloadHash = testHash("c")
	if err := book.Apply(DepthEvent{Update: conflict, Observation: testObservation(1, 101, 3, 30)}); errorCode(err) != "duplicate_conflict" {
		t.Fatalf("conflicting duplicate = %v", err)
	}
}

func TestBookApplyMonotonicAcceptsIdentifierJumpsAndRejectsRegression(t *testing.T) {
	instrument := testInstrument(t)
	book, _ := NewBook("binance", instrument, 10, 20, nil)
	_ = book.BeginGeneration("connection-1", 1)
	if err := book.ReplaceSnapshot(testSnapshot(t, instrument, 10), testObservation(1, 10, 1, 10)); err != nil {
		t.Fatal(err)
	}
	jump := DepthEvent{Update: testDepth(t, instrument, 15, 15, [][2]string{{"100", "3"}}, nil),
		Observation: testObservation(1, 15, 2, 20)}
	if err := book.ApplyMonotonic(jump); err != nil {
		t.Fatalf("monotonic identifier jump rejected: %v", err)
	}
	if view := book.View(); view.Sequence() != 15 || view.Health() != HealthHealthy {
		t.Fatalf("jump view sequence=%d health=%s", view.Sequence(), view.Health())
	}
	regression := DepthEvent{Update: testDepth(t, instrument, 14, 14, nil, nil),
		Observation: testObservation(1, 14, 3, 30)}
	if err := book.ApplyMonotonic(regression); errorCode(err) != "sequence_regression" ||
		book.View().Health() != HealthPaused {
		t.Fatalf("regression failure=%v health=%s", err, book.View().Health())
	}
}

func testSnapshot(t *testing.T, instrument domain.Instrument, sequence uint64) exchangecontracts.BookSnapshot {
	t.Helper()
	return exchangecontracts.BookSnapshot{Exchange: "binance", Instrument: instrument, LastSequence: sequence,
		ReceivedAt: testEventTime(1), Bids: testLevels(t, [][2]string{{"100", "2"}, {"99", "3"}}),
		Asks: testLevels(t, [][2]string{{"101", "1"}, {"102", "3"}}), RawPayloadHash: testHash("a")}
}

func testDepth(
	t *testing.T,
	instrument domain.Instrument,
	first, last uint64,
	bids, asks [][2]string,
) exchangecontracts.DepthUpdate {
	t.Helper()
	return exchangecontracts.DepthUpdate{Exchange: "binance", Instrument: instrument, FirstSequence: first,
		LastSequence: last, ExchangeTime: time.UnixMilli(1_700_000_000_000).UTC(), ReceivedAt: testEventTime(1),
		Bids: testLevels(t, bids), Asks: testLevels(t, asks), RawPayloadHash: testHash("b")}
}

func testLevels(t *testing.T, values [][2]string) []exchangecontracts.PriceLevel {
	t.Helper()
	levels := make([]exchangecontracts.PriceLevel, 0, len(values))
	for _, value := range values {
		price, priceErr := domain.ParsePrice(value[0])
		quantity, quantityErr := domain.ParseQuantity(value[1])
		if priceErr != nil || quantityErr != nil {
			t.Fatal(priceErr, quantityErr)
		}
		levels = append(levels, exchangecontracts.PriceLevel{Price: price, Quantity: quantity})
	}
	return levels
}

func testInstrument(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

func testQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	quantity, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return quantity
}

func testObservation(generation, source, ordinal, offset uint64) Observation {
	base := time.Unix(1_700_000_000, int64(offset)).UTC()
	return Observation{ExchangeTime: base, ReceivedAt: domain.EventTime{UTC: base, Sequence: ordinal*3 - 2},
		ProcessedAt:  domain.EventTime{UTC: base.Add(time.Nanosecond), Sequence: ordinal*3 - 1},
		PublishedAt:  domain.EventTime{UTC: base.Add(2 * time.Nanosecond), Sequence: ordinal * 3},
		ConnectionID: "connection-1", ConnectionGeneration: generation, SourceSequence: source,
		IngestOrdinal: ordinal, ReceivedOffsetNanos: offset, ProcessedOffsetNanos: offset + 1,
		PublishedOffsetNanos: offset + 2}
}

func testEventTime(sequence uint64) domain.EventTime {
	return domain.EventTime{UTC: time.Unix(1_700_000_000, 0).UTC(), Sequence: sequence}
}

func testHash(value string) string {
	const hashLength = 64
	result := value
	for len(result) < hashLength {
		result += value
	}
	return result[:hashLength]
}

func errorCode(err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return ""
	}
	return failure.Code
}
