package binance

import (
	"context"
	"errors"
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

func TestPublicMetadataAcceptsCompleteThreeInstrumentEvaluationUniverse(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.UnixMilli(1_700_000_000_000).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		symbol := request.URL.Query().Get("symbol")
		base, quote := "BTC", "USDT"
		switch symbol {
		case "ETHUSDT":
			base = "ETH"
		case "ETHBTC":
			base, quote = "ETH", "BTC"
		case "BTCUSDT":
		default:
			t.Fatalf("unexpected symbol %q", symbol)
		}
		payload := strings.ReplaceAll(string(fixture(t, "exchange-info.json")), "BTCUSDT", symbol)
		payload = strings.ReplaceAll(payload, `"baseAsset": "BTC"`, `"baseAsset": "`+base+`"`)
		payload = strings.ReplaceAll(payload, `"quoteAsset": "USDT"`, `"quoteAsset": "`+quote+`"`)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	btc, _ := domain.NewSpotInstrument("BTC", "USDT")
	ethUSDT, _ := domain.NewSpotInstrument("ETH", "USDT")
	ethBTC, _ := domain.NewSpotInstrument("ETH", "BTC")
	records, err := client.Instruments(context.Background(), []domain.Instrument{btc, ethUSDT, ethBTC})
	if err != nil || len(records) != 3 || records[0].NativeSymbol != "BTCUSDT" ||
		records[1].NativeSymbol != "ETHUSDT" || records[2].NativeSymbol != "ETHBTC" {
		t.Fatalf("evaluation metadata = %#v, %v", records, err)
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

func TestPublicClientCombinesThreeApprovedInstrumentsOnOneObservedStream(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	depth := string(fixture(t, "depth-update.json"))
	frames := make([][]byte, 0, 3)
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
		payload := strings.ReplaceAll(depth, "BTCUSDT", symbol)
		frames = append(frames, []byte(`{"stream":"`+strings.ToLower(symbol)+`@depth@100ms","data":`+payload+`}`))
	}
	connector := &fakeConnector{connection: &fakeConnection{frames: frames}}
	client.connector = connector
	requests := make([]exchangecontracts.StreamRequest, 0, 3)
	for _, assets := range [][2]domain.AssetSymbol{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}} {
		instrument, err := domain.NewSpotInstrument(assets[0], assets[1])
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, exchangecontracts.StreamRequest{Instrument: instrument,
			Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth}})
	}
	stream, err := client.SubscribeCombinedObserved(context.Background(), requests)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if connector.target == nil || connector.target.Query().Get("streams") !=
		"btcusdt@depth@100ms/ethbtc@depth@100ms/ethusdt@depth@100ms" {
		t.Fatalf("combined target = %v", connector.target)
	}
	seen := make(map[string]bool, 3)
	for range 3 {
		observed, receiveErr := stream.ReceiveObserved(context.Background())
		if receiveErr != nil || observed.Event.Depth == nil {
			t.Fatalf("combined receive = %#v, %v", observed, receiveErr)
		}
		if observed.ConnectionGeneration != 1 || observed.ConnectionID != "binance-public-1" {
			t.Fatalf("combined connection evidence = %#v", observed)
		}
		seen[observed.Event.Depth.Instrument.Symbol()] = true
	}
	if !seen["BTCUSDT"] || !seen["ETHUSDT"] || !seen["ETHBTC"] {
		t.Fatalf("combined instruments = %v", seen)
	}
}

func TestPublicClientRejectsInvalidCombinedObservedRequestsBeforeNetwork(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	connector := &fakeConnector{connection: &fakeConnection{}}
	client.connector = connector
	btc := approvedBTC(t)
	request := exchangecontracts.StreamRequest{Instrument: btc,
		Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth}}
	for _, requests := range [][]exchangecontracts.StreamRequest{
		nil,
		{request},
		{request, request},
		{request, request, request, request},
	} {
		if _, err := client.SubscribeCombinedObserved(context.Background(), requests); err == nil {
			t.Fatalf("combined request unexpectedly accepted: %#v", requests)
		}
		if connector.target != nil {
			t.Fatal("invalid combined request reached the network")
		}
	}
}

func TestResponseErrorRetainsBoundedUpstreamEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status     int
		retryAfter string
		kind       exchangecontracts.ErrorKind
		cause      string
		retry      time.Duration
	}{
		{status: http.StatusTooManyRequests, retryAfter: "15", kind: exchangecontracts.ErrorRateLimit,
			cause: "http_rate_limit", retry: 15 * time.Second},
		{status: http.StatusTeapot, retryAfter: "999", kind: exchangecontracts.ErrorRateLimit,
			cause: "http_rate_limit"},
		{status: http.StatusServiceUnavailable, kind: exchangecontracts.ErrorTransient,
			cause: "http_server_error"},
		{status: http.StatusFound, kind: exchangecontracts.ErrorCapability, cause: "http_redirect"},
		{status: http.StatusBadRequest, kind: exchangecontracts.ErrorValidation, cause: "http_client_error"},
	}
	for _, test := range tests {
		response := &http.Response{StatusCode: test.status, Header: make(http.Header)}
		if test.retryAfter != "" {
			response.Header.Set("Retry-After", test.retryAfter)
		}
		err := responseError(response, exchangecontracts.OperationSnapshot)
		var failure *exchangecontracts.Error
		if !errors.As(err, &failure) || failure.Kind != test.kind || failure.HTTPStatus != test.status ||
			failure.Cause != test.cause || failure.RetryAfter != test.retry {
			t.Fatalf("status=%d failure=%#v", test.status, err)
		}
	}
	if err := responseError(&http.Response{StatusCode: http.StatusNoContent},
		exchangecontracts.OperationSnapshot); err != nil {
		t.Fatalf("success response=%v", err)
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
