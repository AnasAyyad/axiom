package binance

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestPublicClientConstructorAndRESTSurfaceAreCredentialFree(t *testing.T) {
	clock, err := domain.NewReplayClock(time.UnixMilli(1_700_000_000_000).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewPublicClient("other", clock); err == nil {
		t.Fatal("arbitrary endpoint set accepted")
	}
	client, err := NewPublicClient(publicEndpointSet, clock)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string][]byte{
		"/api/v3/ping":         []byte("{}"),
		"/api/v3/time":         []byte(`{"serverTime":1700000000015}`),
		"/api/v3/depth":        fixture(t, "depth-snapshot.json"),
		"/api/v3/exchangeInfo": fixture(t, "exchange-info.json"),
	}
	transport := &scriptedTransport{fixtures: fixtures}
	client.httpClient = &http.Client{Transport: transport, CheckRedirect: rejectPublicRedirect}
	var monotonic time.Duration
	client.monotonic = func() time.Duration {
		current := monotonic
		monotonic += 20 * time.Millisecond
		return current
	}
	if err = client.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	health, err := client.SampleServerTime(context.Background())
	if err != nil || !health.Eligible || health.Uncertainty != 60*time.Millisecond {
		t.Fatalf("time sample = %#v, %v", health, err)
	}
	instrument := approvedBTC(t)
	snapshot, err := client.Snapshot(context.Background(), exchangecontracts.SnapshotRequest{Instrument: instrument, Depth: 100})
	if err != nil || snapshot.LastSequence == 0 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	records, err := client.Instruments(context.Background(), []domain.Instrument{instrument})
	if err != nil || len(records) != 1 || records[0].NativeSymbol != "BTCUSDT" {
		t.Fatalf("metadata = %#v, %v", records, err)
	}
	for _, request := range transport.requests {
		if request.URL.Hostname() != "data-api.binance.vision" || request.Method != http.MethodGet ||
			request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" ||
			request.Header.Get("X-MBX-APIKEY") != "" {
			t.Fatalf("unsafe request emitted: %s %s", request.Method, request.URL.Path)
		}
	}
}

func TestPublicClientMapsStatusAndBoundsBodies(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"3"}},
			Body: io.NopCloser(strings.NewReader("bounded"))}, nil
	})}
	if err := client.Ping(context.Background()); exchangecontracts.KindOf(err) != exchangecontracts.ErrorRateLimit {
		t.Fatalf("rate-limit mapping = %v", err)
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(strings.Repeat("x", publicBodyLimit+1)))}, nil
	})}
	if err := client.Ping(context.Background()); exchangecontracts.KindOf(err) != exchangecontracts.ErrorValidation {
		t.Fatalf("oversized body mapping = %v", err)
	}
}

func TestPublicStreamRetainsRawFrameAndNormalizesExpectedStream(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	depth := fixture(t, "depth-update.json")
	frame := []byte(`{"stream":"btcusdt@depth@100ms","data":` + string(depth) + `}`)
	connector := &fakeConnector{connection: &fakeConnection{frames: [][]byte{frame}}}
	client.connector = connector
	stream, err := client.SubscribeObserved(context.Background(), exchangecontracts.StreamRequest{
		Instrument: approvedBTC(t), Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth},
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := stream.ReceiveObserved(context.Background())
	if err != nil || string(observed.Raw) != string(frame) || observed.Event.Depth == nil ||
		observed.Event.Depth.Instrument.Symbol() != "BTCUSDT" || observed.ConnectionGeneration != 1 {
		t.Fatalf("observed stream = %#v, %v", observed, err)
	}
	if connector.target == nil || connector.target.Hostname() != "data-stream.binance.vision" ||
		connector.target.Query().Get("streams") != "btcusdt@depth@100ms" {
		t.Fatalf("unexpected target: %v", connector.target)
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedStreamPersistsRawBeforeCanonicalOrDecoderFailure(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	depth := fixture(t, "depth-update.json")
	valid := []byte(`{"stream":"btcusdt@depth@100ms","data":` + string(depth) + `}`)
	for _, test := range []struct {
		name  string
		frame []byte
		want  []string
	}{
		{name: "canonical", frame: valid, want: []string{"raw", "canonical"}},
		{name: "decoder", frame: []byte(`{"unexpected":true}`), want: []string{"raw", "decoder"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client.connector = &fakeConnector{connection: &fakeConnection{frames: [][]byte{test.frame}}}
			sink := &fakeFrameSink{}
			stream, err := client.SubscribeRecorded(context.Background(), exchangecontracts.StreamRequest{
				Instrument: approvedBTC(t), Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth},
			}, sink)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = stream.ReceiveObserved(context.Background())
			if strings.Join(sink.calls, ",") != strings.Join(test.want, ",") {
				t.Fatalf("recording order = %v, want %v", sink.calls, test.want)
			}
		})
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile("../../../testdata/exchanges/binance/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func approvedBTC(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

type scriptedTransport struct {
	fixtures map[string][]byte
	requests []*http.Request
}

func (transport *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request.Clone(request.Context()))
	body, ok := transport.fixtures[request.URL.Path]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("missing"))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type fakeConnector struct {
	connection websocketConnection
	target     *url.URL
}

func (connector *fakeConnector) Connect(_ context.Context, target *url.URL) (websocketConnection, error) {
	copy := *target
	connector.target = &copy
	return connector.connection, nil
}

type fakeConnection struct {
	mutex  sync.Mutex
	frames [][]byte
	closed bool
}

type fakeFrameSink struct{ calls []string }

func (sink *fakeFrameSink) RecordPublicRaw(
	_ context.Context,
	frame PublicRawRecord,
) (StreamRecordToken, error) {
	sink.calls = append(sink.calls, "raw")
	if len(frame.Raw) == 0 {
		return StreamRecordToken{}, io.ErrUnexpectedEOF
	}
	return StreamRecordToken{IngestOrdinal: 1}, nil
}

func (sink *fakeFrameSink) RecordPublicCanonical(
	_ context.Context,
	record PublicCanonicalRecord,
) error {
	if record.Kind == RecordDecoderError {
		sink.calls = append(sink.calls, "decoder")
	} else {
		sink.calls = append(sink.calls, "canonical")
	}
	return nil
}

func (sink *fakeFrameSink) RecordSourceGap(context.Context, SourceGap) error { return nil }

func (connection *fakeConnection) Receive() ([]byte, error) {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.closed || len(connection.frames) == 0 {
		return nil, io.EOF
	}
	frame := connection.frames[0]
	connection.frames = connection.frames[1:]
	return append([]byte(nil), frame...), nil
}

func (connection *fakeConnection) Close() error {
	connection.mutex.Lock()
	connection.closed = true
	connection.mutex.Unlock()
	return nil
}
