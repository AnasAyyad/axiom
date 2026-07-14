package runtimecore

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestEnvelopeCanonicalEncodingAndMandatoryFields(t *testing.T) {
	event := testEnvelope(t, "event-a", 10, 1, 500, 7)
	first, err := event.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := event.CanonicalJSON()
	if string(first) != string(second) || !json.Valid(first) {
		t.Fatalf("canonical encoding is unstable: %s", first)
	}
	input := event.record
	input.PayloadHash = "bad"
	if _, err := NewEnvelope(input); err == nil {
		t.Fatal("invalid payload hash accepted")
	}
}

func TestIngestOrdinalsAreUniqueBeforeConcurrentFanout(t *testing.T) {
	ordinals := &IngestOrdinals{}
	values := make(chan uint64, 1000)
	var group sync.WaitGroup
	for range 1000 {
		group.Add(1)
		go func() {
			defer group.Done()
			value, err := ordinals.Next()
			if err != nil {
				t.Error(err)
			}
			values <- value
		}()
	}
	group.Wait()
	close(values)
	seen := make(map[uint64]struct{}, 1000)
	for value := range values {
		seen[value] = struct{}{}
	}
	if len(seen) != 1000 {
		t.Fatalf("unique ordinals = %d", len(seen))
	}
}

func TestReplayUsesOnlyLogicalTimeThenIngestOrdinal(t *testing.T) {
	events := []Envelope{
		testEnvelope(t, "event-z", 10, 1, 900, 99),
		testEnvelope(t, "event-a", 10, 2, 100, 1),
		testEnvelope(t, "event-m", 5, 3, 500, 50),
	}
	cursor, err := NewReplayCursor([]Envelope{events[1], events[0], events[2]})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"event-m", "event-z", "event-a"}
	for _, expected := range want {
		event, ok := cursor.Next()
		if !ok || event.ID().Value() != expected {
			t.Fatalf("replay event = %q, want %q", event.ID().Value(), expected)
		}
	}
}

func TestReplayRejectsMissingAndDuplicateOrdinals(t *testing.T) {
	valid := testEnvelope(t, "event-a", 10, 1, 100, 1)
	missing := valid
	missing.record.IngestOrdinal = 0
	if _, err := NewReplayCursor([]Envelope{missing}); err == nil {
		t.Fatal("missing ordinal accepted")
	}
	duplicate := testEnvelope(t, "event-b", 20, 1, 200, 2)
	if _, err := NewReplayCursor([]Envelope{valid, duplicate}); err == nil {
		t.Fatal("duplicate ordinal accepted")
	}
}

func FuzzReplayOrdering(f *testing.F) {
	f.Add(uint64(10), uint64(2), uint64(10), uint64(1))
	f.Add(uint64(5), uint64(1), uint64(10), uint64(2))
	f.Fuzz(func(t *testing.T, leftTime, leftOrdinal, rightTime, rightOrdinal uint64) {
		leftTime, rightTime = nonzero(leftTime), nonzero(rightTime)
		leftOrdinal, rightOrdinal = nonzero(leftOrdinal), nonzero(rightOrdinal)
		left := testEnvelope(t, "event-left", LogicalTime(leftTime), leftOrdinal, 1, 1)
		right := testEnvelope(t, "event-right", LogicalTime(rightTime), rightOrdinal, 2, 2)
		cursor, err := NewReplayCursor([]Envelope{left, right})
		if leftOrdinal == rightOrdinal {
			if err == nil {
				t.Fatal("duplicate ordinal accepted")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		first, _ := cursor.Next()
		if replayLess(right, left) && first.ID() != right.ID() {
			t.Fatal("replay comparator was not honored")
		}
		if replayLess(left, right) && first.ID() != left.ID() {
			t.Fatal("replay comparator was not honored")
		}
	})
}

func nonzero(value uint64) uint64 {
	if value == 0 {
		return 1
	}
	return value
}

func testEnvelope(t testing.TB, idValue string, logical LogicalTime, ordinal uint64, exchangeNanos int64, sequence uint64) Envelope {
	t.Helper()
	id, _ := domain.NewEventID(idValue)
	runID, _ := domain.NewRunID("run-a")
	sessionID, _ := domain.NewSourceSessionID("session-a")
	connectionID, _ := domain.NewConnectionID("connection-a")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	event, err := NewEnvelope(EnvelopeInput{
		SchemaVersion: "event.v1", ParserVersion: "parser.v1", ID: id, RunID: runID,
		SourceSessionID: sessionID, ConnectionID: connectionID, ConnectionGeneration: 1,
		Exchange: "binance", Instrument: instrument,
		ExchangeTime:        OptionalTime{Present: true, Value: time.Unix(0, exchangeNanos).UTC()},
		SourceSequence:      OptionalUint64{Present: true, Value: sequence},
		ReceivedAt:          domain.EventTime{UTC: time.Date(2026, 7, 14, 8, 0, 0, int(ordinal), time.UTC), Sequence: ordinal},
		RecordedLogicalTime: logical, IngestOrdinal: ordinal, PayloadHash: PayloadDigest([]byte(idValue)),
		Partition: "binance:BTCUSDT", Run: RunMetadata{
			ConfigurationHash: PayloadDigest([]byte("configuration")), BuildCommit: "fb54396",
			OrderingVersion: "replay-order-v1", SchedulerVersion: "scheduler-v1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}
