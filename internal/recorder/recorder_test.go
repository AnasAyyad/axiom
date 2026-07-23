package recorder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

func TestB1PublicStreamSinkPersistsBybitRawCanonicalAndManifestIdentity(t *testing.T) {
	root := t.TempDir()
	committed := make([]segments.Manifest, 0, 2)
	recorder, err := New(root, "bybit-public-b1", "session-b1-bybit", "bybit",
		&runtimecore.IngestOrdinals{}, func(manifest segments.Manifest) error {
			committed = append(committed, manifest)
			return nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := NewPublicStreamSink(recorder, "bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		t.Fatal(err)
	}
	instrument := recorderInstrument(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	token, err := sink.RecordPublicRaw(context.Background(), exchangecontracts.PublicRawRecord{
		Kind: exchangecontracts.RecordStreamFrame, Raw: []byte(`{"wire":"bybit"}`), Instrument: instrument,
		ReceivedAt: domain.EventTime{UTC: now, Sequence: 1}, ConnectionID: "bybit-public-1",
		ConnectionGeneration: 1, MonotonicOffsetNanos: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = sink.RecordPublicCanonical(context.Background(), exchangecontracts.PublicCanonicalRecord{
		Kind: exchangecontracts.RecordStreamFrame, Token: token,
		Canonical: []byte(`{"exchange":"bybit","sequence":42}`), SourceSequence: "42",
	}); err != nil {
		t.Fatal(err)
	}
	manifest, err := recorder.Flush()
	if err != nil || manifest.RawRecordCount != 1 || manifest.CanonicalCount != 1 || len(committed) != 2 {
		t.Fatalf("Bybit manifest=%#v committed=%d error=%v", manifest, len(committed), err)
	}
	records, err := ValidateDataset(root, manifest)
	parserFound := false
	for _, reference := range manifest.Segments {
		parserFound = parserFound || (reference.Kind == "canonical" &&
			reference.Manifest.Spec.ParserVersion == "bybit-public-parser.v1")
	}
	if err != nil || len(records) != 1 || manifest.Exchange != "bybit" || !parserFound ||
		string(records[0].Canonical) != `{"exchange":"bybit","sequence":42}` {
		t.Fatalf("Bybit dataset=%#v error=%v", records, err)
	}
	if recovered, recoverErr := recorder.Recover(); recoverErr != nil || len(recovered) != 0 {
		t.Fatalf("clean Bybit recovery=%#v error=%v", recovered, recoverErr)
	}
}

func TestRecorderLinksWireCanonicalAndValidatesManifestChain(t *testing.T) {
	root := t.TempDir()
	committed := make([]segments.Manifest, 0)
	recorder, err := New(root, "dataset-a7", "session-a7", "binance", &runtimecore.IngestOrdinals{},
		func(manifest segments.Manifest) error {
			committed = append(committed, manifest)
			return nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Unix(1_700_000_000, 0).UTC()
	recorder.now = func() time.Time { return created }
	link := recordPair(t, recorder, 100, `{"kind":"depth"}`, `{"sequence":100}`)
	if link.IngestOrdinal != 1 {
		t.Fatalf("first ordinal = %d", link.IngestOrdinal)
	}
	instrument := recorderInstrument(t)
	if err = recorder.RecordGap(Gap{Exchange: "binance", Instrument: instrument, ConnectionGeneration: 1,
		FirstSourceSequence: 101, LastSourceSequence: 102, StartedAt: created, EndedAt: created.Add(time.Second),
		Reason: "sequence_gap"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Revision != 1 || manifest.Complete || manifest.RawRecordCount != 1 ||
		manifest.CanonicalCount != 1 || len(manifest.Segments) != 2 || len(committed) != 2 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	verifyFirstManifest(t, root, recorder, manifest)
	verifySecondManifest(t, root, recorder, manifest, created)
}

func TestDecisionInputUsesRawBeforeCanonicalDatasetBoundary(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"ordinal":1,"logical_time":10}`)
	ordinal, err := recorder.RecordDecisionInputBuilt(DecisionInput{Instrument: recorderInstrument(t),
		EventID: "decision-input-1", LogicalTime: 10, ReceivedAt: time.Unix(1, 0).UTC()},
		func(assigned uint64) ([]byte, error) {
			if assigned != 1 {
				t.Fatalf("assigned ordinal = %d", assigned)
			}
			return payload, nil
		})
	if err != nil || ordinal != 1 {
		t.Fatalf("decision input ordinal = %d, %v", ordinal, err)
	}
	manifest, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	records, err := ValidateDataset(recorder.root, manifest)
	if err != nil || len(records) != 1 || string(records[0].Canonical) != string(payload) {
		t.Fatalf("decision dataset = %#v, %v", records, err)
	}
}

func verifyFirstManifest(t *testing.T, root string, recorder *Recorder, manifest DatasetManifest) {
	t.Helper()
	path := filepath.Join(root, "session-a7-000001.dataset.json")
	stored, err := ReadManifest(path)
	if err != nil || stored.Hash != manifest.Hash {
		t.Fatalf("stored manifest = %#v, %v", stored, err)
	}
	records, err := ValidateDataset(root, stored)
	if err != nil || len(records) != 1 || string(records[0].Canonical) != `{"sequence":100}` {
		t.Fatalf("validated records = %#v, %v", records, err)
	}
	verification, err := VerifyDataset(root, stored)
	if err != nil || verification.RecordCount != 1 || verification.SegmentPairs != 1 ||
		len(verification.ReplaySHA256) != 64 {
		t.Fatalf("bounded verification = %#v, %v", verification, err)
	}
	records[0].Canonical[0] = 'x'
	revalidated, err := ValidateDataset(root, stored)
	if err != nil || string(revalidated[0].Canonical) != `{"sequence":100}` {
		t.Fatal("validated dataset was mutable")
	}
}

func verifySecondManifest(t *testing.T, root string, recorder *Recorder, first DatasetManifest, created time.Time) {
	t.Helper()
	recorder.now = func() time.Time { return created.Add(time.Minute) }
	recordPair(t, recorder, 103, `{"kind":"depth-2"}`, `{"sequence":103}`)
	second, err := recorder.Flush()
	if err != nil || second.Revision != 2 || second.PreviousHash != first.Hash ||
		second.RawRecordCount != 2 || len(second.Segments) != 4 {
		t.Fatalf("second manifest = %#v, %v", second, err)
	}
	if _, err = ValidateDataset(root, second); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderRejectsMissingDuplicateAndMutatedLinks(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	input := rawInput(t, 1, []byte(`{"kind":"depth"}`))
	link, err := recorder.RecordRaw(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, flushErr := recorder.Flush(); recorderCode(flushErr) != "segment_incomplete" {
		t.Fatalf("in-flight final flush error=%v", flushErr)
	}
	bad := link
	bad.PayloadHash[0]++
	canonical := CanonicalInput{Link: bad, EventID: "event-1", ParserVersion: "parser-v1",
		NormalizationVersion: "normalizer-v1", Canonical: []byte(`{"sequence":1}`)}
	if err = recorder.RecordCanonical(canonical); recorderCode(err) != "raw_link_invalid" {
		t.Fatalf("mutated link error = %v", err)
	}
	canonical.Link = link
	if err = recorder.RecordCanonical(canonical); err != nil {
		t.Fatal(err)
	}
	if err = recorder.RecordCanonical(canonical); recorderCode(err) != "raw_link_invalid" {
		t.Fatalf("duplicate canonical error = %v", err)
	}
}

func TestRecorderPeriodicFlushKeepsInFlightSuffix(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	recordPair(t, recorder, 1, `{"kind":"depth"}`, `{"sequence":1}`)
	second, err := recorder.RecordRaw(rawInput(t, 2, []byte(`{"kind":"depth"}`)))
	if err != nil {
		t.Fatal(err)
	}
	firstManifest, flushed, err := recorder.FlushReady()
	if err != nil || !flushed || firstManifest.Revision != 1 || firstManifest.RawRecordCount != 1 {
		t.Fatalf("first periodic flush=%#v flushed=%t error=%v", firstManifest, flushed, err)
	}
	if raw, canonical := recorder.PendingCounts(); raw != 1 || canonical != 0 {
		t.Fatalf("deferred counts=%d/%d", raw, canonical)
	}
	if err = recorder.RecordCanonical(CanonicalInput{Link: second, EventID: eventID(second.IngestOrdinal),
		ParserVersion: "parser-v1", NormalizationVersion: "normalizer-v1", Canonical: []byte(`{"sequence":2}`)}); err != nil {
		t.Fatal(err)
	}
	secondManifest, err := recorder.Flush()
	if err != nil || secondManifest.Revision != 2 || secondManifest.RawRecordCount != 2 ||
		secondManifest.PreviousHash != firstManifest.Hash {
		t.Fatalf("second flush=%#v error=%v", secondManifest, err)
	}
	if _, err = ValidateDataset(recorder.root, secondManifest); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderFlushRetainsInjectedFilesystemCause(t *testing.T) {
	recorder, err := New(t.TempDir(), "dataset-a7", "session-a7", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil },
		func(stage segments.Stage) error {
			if stage == segments.StageCreated {
				return syscall.ENOSPC
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	recordPair(t, recorder, 1, `{"kind":"depth"}`, `{"sequence":1}`)
	_, err = recorder.Flush()
	detail, ok := FailureDetail(err)
	if !ok || detail.Code != "wire_finalize_failed" || detail.Stage != "wire_finalize" ||
		detail.Cause != "disk_full" || detail.Class != "filesystem" || detail.Errno != int(syscall.ENOSPC) {
		t.Fatalf("flush detail=%#v error=%v", detail, err)
	}
}

func TestRecorderIOErrorClassifiesQualificationFailures(t *testing.T) {
	tests := []struct {
		err   error
		cause string
	}{
		{syscall.ENOSPC, "disk_full"},
		{syscall.EDQUOT, "quota_exceeded"},
		{syscall.EIO, "io_failure"},
		{syscall.EMFILE, "file_descriptor_exhausted"},
		{syscall.EACCES, "permission_denied"},
		{syscall.EROFS, "read_only_filesystem"},
	}
	for _, test := range tests {
		detail, ok := FailureDetail(recorderIOError("flush_failed", "manifest_write", test.err))
		if !ok || detail.Cause != test.cause || detail.Errno == 0 || detail.Stage != "manifest_write" {
			t.Fatalf("error=%v detail=%#v", test.err, detail)
		}
	}
}

func TestRecorderFlushReadyWaitsForFirstCanonicalPair(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	link, err := recorder.RecordRaw(rawInput(t, 1, []byte(`{"wire":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	if manifest, flushed, flushErr := recorder.FlushReady(); flushErr != nil || flushed || manifest.Revision != 0 {
		t.Fatalf("in-flight ready flush = %#v %t %v", manifest, flushed, flushErr)
	}
	if err = recorder.RecordCanonical(CanonicalInput{Link: link, EventID: eventID(link.IngestOrdinal),
		ParserVersion: "parser-v1", NormalizationVersion: "normalizer-v1", Canonical: []byte(`{"canonical":1}`)}); err != nil {
		t.Fatal(err)
	}
	manifest, flushed, err := recorder.FlushReady()
	if err != nil || !flushed || manifest.RawRecordCount != 1 || manifest.CanonicalCount != 1 {
		t.Fatalf("completed ready flush = %#v %t %v", manifest, flushed, err)
	}
}

func TestRecorderFailsClosedAtConfiguredMemoryBound(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"kind":"depth"}`)
	recorder.pendingLimit = uint64(maximumEventBytes + 2*recordMemoryOverhead + len(payload) - 1)
	if _, err = recorder.RecordRaw(rawInput(t, 1, payload)); recorderCode(err) != "recorder_capacity_exceeded" {
		t.Fatalf("capacity error = %v", err)
	}
	if raw, canonical := recorder.PendingCounts(); raw != 0 || canonical != 0 {
		t.Fatalf("rejected record consumed memory: %d/%d", raw, canonical)
	}
}

func TestRecorderSignalsAndFlushesBeforeCapacity(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	recorder.pendingLimit = 16 * 1024 * 1024
	var latest DatasetManifest
	flushes := 0
	for sequence := uint64(1); sequence <= 6000; sequence++ {
		recordPair(t, recorder, sequence, `{"kind":"depth"}`, `{"sequence":1}`)
		select {
		case <-recorder.FlushRequired():
			before := recorder.PendingUsage()
			if before.UsedBytes < before.FlushThresholdBytes || before.UsedBytes >= before.LimitBytes {
				t.Fatalf("unsafe pre-flush usage=%#v", before)
			}
			manifest, flushed, flushErr := recorder.FlushReady()
			if flushErr != nil || !flushed {
				t.Fatalf("capacity flush=%#v flushed=%t error=%v", manifest, flushed, flushErr)
			}
			latest = manifest
			flushes++
			after := recorder.PendingUsage()
			if after.UsedBytes >= after.FlushThresholdBytes || after.HighWaterBytes < before.UsedBytes {
				t.Fatalf("unsafe post-flush usage=%#v before=%#v", after, before)
			}
		default:
		}
	}
	if flushes < 2 || latest.Revision != uint64(flushes) {
		t.Fatalf("capacity flushes=%d manifest=%#v", flushes, latest)
	}
	if manifest, flushed, flushErr := recorder.FlushReady(); flushErr != nil {
		t.Fatal(flushErr)
	} else if flushed {
		latest = manifest
	}
	usage := recorder.PendingUsage()
	if usage.UsedBytes != 0 || usage.HighWaterBytes == 0 || usage.HighWaterBytes >= usage.LimitBytes {
		t.Fatalf("terminal usage=%#v", usage)
	}
	if _, err = ValidateDataset(recorder.root, latest); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderConcurrentRawOrdinalsRemainUniqueAndReplayOrdered(t *testing.T) {
	recorder, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	const count = 64
	links, errors := recordConcurrentPairs(t, recorder, count)
	verifyConcurrentRecords(t, recorder, links, errors, count)
}

func recordConcurrentPairs(t *testing.T, recorder *Recorder, count int) (chan RawLink, chan error) {
	t.Helper()
	links := make(chan RawLink, count)
	errors := make(chan error, count)
	var group sync.WaitGroup
	for index := 1; index <= count; index++ {
		group.Add(1)
		go func(sequence int) {
			defer group.Done()
			input := rawInput(t, uint64(sequence), []byte(`{"kind":"depth"}`))
			fixed := time.Unix(1_700_000_000, 0).UTC()
			input.ReceivedAt, input.ExchangeTime, input.RecordedLogicalTime = fixed, &fixed, 1
			link, recordErr := recorder.RecordRaw(input)
			if recordErr == nil {
				recordErr = recorder.RecordCanonical(CanonicalInput{Link: link, EventID: eventID(link.IngestOrdinal),
					ParserVersion: "parser-v1", NormalizationVersion: "normalizer-v1",
					Canonical: []byte(eventID(link.IngestOrdinal))})
			}
			links <- link
			errors <- recordErr
		}(index)
	}
	group.Wait()
	close(links)
	close(errors)
	return links, errors
}

func verifyConcurrentRecords(t *testing.T, recorder *Recorder, links chan RawLink, errors chan error, count int) {
	t.Helper()
	seen := make(map[uint64]struct{}, count)
	for link := range links {
		seen[link.IngestOrdinal] = struct{}{}
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != count {
		t.Fatalf("unique ordinals = %d", len(seen))
	}
	manifest, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	records, err := ValidateDataset(recorder.root, manifest)
	if err != nil || len(records) != count {
		t.Fatalf("records = %d, %v", len(records), err)
	}
	for index, record := range records {
		if record.IngestOrdinal != uint64(index+1) {
			t.Fatalf("record %d ordinal = %d", index, record.IngestOrdinal)
		}
	}
}

func TestDatasetValidationDetectsSegmentMutation(t *testing.T) {
	recorder, _ := testRecorder(t)
	recordPair(t, recorder, 1, `{"kind":"depth"}`, `{"sequence":1}`)
	manifest, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(recorder.root, manifest.Segments[0].Manifest.Path)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("mutation"))
	_ = file.Close()
	if _, err = ValidateDataset(recorder.root, manifest); recorderCode(err) != "segment_checksum_invalid" {
		t.Fatalf("mutation error = %v", err)
	}
}

func testRecorder(t *testing.T) (*Recorder, error) {
	t.Helper()
	return New(t.TempDir(), "dataset-a7", "session-a7", "binance", &runtimecore.IngestOrdinals{},
		func(segments.Manifest) error { return nil }, nil)
}

func recordPair(t *testing.T, recorder *Recorder, sequence uint64, raw, canonical string) RawLink {
	t.Helper()
	link, err := recorder.RecordRaw(rawInput(t, sequence, []byte(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err = recorder.RecordCanonical(CanonicalInput{Link: link, EventID: eventID(link.IngestOrdinal),
		ParserVersion: "parser-v1", NormalizationVersion: "normalizer-v1", Canonical: []byte(canonical)}); err != nil {
		t.Fatal(err)
	}
	return link
}

func rawInput(t *testing.T, sequence uint64, payload []byte) RawInput {
	t.Helper()
	now := time.Unix(1_700_000_000+int64(sequence), 0).UTC()
	return RawInput{Exchange: "binance", EventType: EventDepth, Instrument: recorderInstrument(t),
		SessionID: "session-a7", ConnectionID: "connection-1", ConnectionGeneration: 1,
		MonotonicOffsetNanos: sequence, RecordedLogicalTime: sequence, SourceSequence: eventID(sequence),
		ExchangeTime: &now, ReceivedAt: now, Payload: append([]byte(nil), payload...)}
}

func recorderInstrument(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

func eventID(sequence uint64) string { return fmt.Sprintf("event-%d", sequence) }

func recorderCode(err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return ""
	}
	return failure.Code
}
