package bybit

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestB1PublicClientRESTSurfaceIsCompiledAndCredentialFree(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	if _, err := NewPublicClient("arbitrary", clock); err == nil {
		t.Fatal("arbitrary endpoint set accepted")
	}
	client, err := NewPublicClient(publicEndpointSet, clock)
	if err != nil {
		t.Fatal(err)
	}
	transport := &bybitScriptedTransport{fixtures: map[string][]byte{
		"/v5/market/time":      []byte(`{"retCode":0,"retMsg":"OK","result":{"timeSecond":"1700000000","timeNano":"1700000000000000000"},"retExtInfo":{},"time":1700000000000}`),
		"/v5/market/orderbook": []byte(`{"retCode":0,"retMsg":"OK","result":{"s":"BTCUSDT","b":[["100","2"]],"a":[["101","3"]],"ts":1700000000000,"u":42,"seq":99,"cts":1700000000000},"retExtInfo":{},"time":1700000000000}`),
		"/v5/market/tickers":   []byte(`{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"symbol":"BTCUSDT","bid1Price":"100","bid1Size":"2","ask1Price":"101","ask1Size":"3","lastPrice":"100.5"}]},"retExtInfo":{},"time":1700000000000}`),
	}}
	client.httpClient = &http.Client{Transport: transport, CheckRedirect: rejectPublicRedirect}
	var monotonic time.Duration
	client.monotonic = func() time.Duration {
		value := monotonic
		monotonic += 20 * time.Millisecond
		return value
	}
	if health, sampleErr := client.SampleServerTime(context.Background()); sampleErr != nil || !health.Eligible {
		t.Fatalf("time health=%#v error=%v", health, sampleErr)
	}
	instrument := approvedInstruments()[0]
	snapshot, snapshotErr := client.Snapshot(context.Background(), exchangecontracts.SnapshotRequest{
		Instrument: instrument, Depth: 1000,
	})
	if snapshotErr != nil || snapshot.LastSequence != 42 {
		t.Fatalf("snapshot=%#v error=%v", snapshot, snapshotErr)
	}
	if ticker, tickerErr := client.Ticker(context.Background(), instrument); tickerErr != nil ||
		ticker.Instrument != instrument {
		t.Fatalf("ticker=%#v error=%v", ticker, tickerErr)
	}
	for _, request := range transport.requests {
		if request.Method != http.MethodGet || request.URL.Hostname() != "api.bybit.com" ||
			request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" ||
			request.Header.Get("X-Bapi-Api-Key") != "" || request.Header.Get("X-Bapi-Sign") != "" {
			t.Fatalf("unsafe request emitted: %s %s", request.Method, request.URL.String())
		}
	}
}

func TestB1PublicClientBoundsBodiesAndMapsRateLimits(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	client, _ := NewPublicClient(publicEndpointSet, clock)
	client.httpClient = &http.Client{Transport: bybitRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"3"}},
			Body: io.NopCloser(strings.NewReader("bounded"))}, nil
	})}
	if _, err := client.SampleServerTime(context.Background()); exchangecontracts.KindOf(err) != exchangecontracts.ErrorRateLimit {
		t.Fatalf("rate limit mapping=%v", err)
	}
	client.httpClient = &http.Client{Transport: bybitRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(strings.Repeat("x", publicBodyLimit+1)))}, nil
	})}
	if _, err := client.SampleServerTime(context.Background()); exchangecontracts.KindOf(err) != exchangecontracts.ErrorValidation {
		t.Fatalf("oversized body mapping=%v", err)
	}
}

func TestB1EndpointPolicyDeniesCredentialsPrivateRoutesAndNonPublicAddresses(t *testing.T) {
	for _, raw := range []string{
		"https://api.bybit.com/v5/order/create",
		"https://api-testnet.bybit.com/v5/market/time",
		"https://api.bybit.com/v5/market/time?unexpected=true",
	} {
		target, _ := url.Parse(raw)
		if _, err := validateRESTTarget(http.MethodGet, target, make(http.Header)); err == nil {
			t.Fatalf("unsafe REST target accepted: %s", raw)
		}
	}
	target, _ := url.Parse(publicRESTOrigin + "/v5/market/time")
	headers := http.Header{"X-Bapi-Api-Key": {"forbidden"}}
	if _, err := validateRESTTarget(http.MethodGet, target, headers); err == nil {
		t.Fatal("credential header accepted")
	}
	for _, raw := range []string{
		"wss://stream-testnet.bybit.com/v5/public/spot",
		"wss://stream.bybit.com/v5/private",
		"ws://stream.bybit.com/v5/public/spot",
	} {
		websocketTarget, _ := url.Parse(raw)
		if _, err := validateWebSocketTarget(websocketTarget); err == nil {
			t.Fatalf("unsafe WebSocket target accepted: %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "192.0.2.1", "::1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("non-public address accepted: %s", raw)
		}
	}
}

func TestB1RecordedStreamPersistsRawBeforeCanonicalAndDecoderFailure(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Unix(1_700_000_000, 0).UTC())
	valid := []byte(`{"topic":"orderbook.1000.BTCUSDT","type":"snapshot","ts":1700000000000,"data":{"s":"BTCUSDT","b":[["100","2"]],"a":[["101","3"]],"u":42,"seq":99,"cts":1700000000000},"cts":1700000000000}`)
	for _, test := range []struct {
		name  string
		frame []byte
		want  string
	}{
		{name: "canonical", frame: valid, want: "raw,canonical"},
		{name: "decoder", frame: []byte(`{"unexpected":true}`), want: "raw,decoder"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, _ := NewPublicClient(publicEndpointSet, clock)
			connection := &bybitFakeConnection{frames: [][]byte{test.frame}}
			connector := &bybitFakeConnector{connection: connection}
			client.connector = connector
			sink := &bybitFrameSink{}
			stream, err := client.SubscribeRecorded(context.Background(), exchangecontracts.StreamRequest{
				Instrument: approvedInstruments()[0], Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth},
			}, sink)
			if err != nil {
				t.Fatal(err)
			}
			sink.reset()
			_, _ = stream.ReceiveObserved(context.Background())
			if calls := strings.Join(sink.snapshot(), ","); calls != test.want {
				t.Fatalf("recording order=%s want=%s", calls, test.want)
			}
			if test.name == "decoder" && string(sink.decoderSnapshot()) !=
				`{"kind":"decoder_error","failure_kind":"validation_rejected","operation":"stream","cause":"decoder_schema_rejected"}` {
				t.Fatalf("decoder evidence=%s", sink.decoderSnapshot())
			}
			if connector.target == nil || connector.target.String() != publicWSOrigin || len(connection.sent) != 1 ||
				!strings.Contains(string(connection.sent[0]), "orderbook.1000.BTCUSDT") {
				t.Fatalf("compiled stream target or subscription missing: %v %q", connector.target, connection.sent)
			}
			sink.reset()
			if err = stream.Ping(context.Background()); err != nil || strings.Join(sink.snapshot(), ",") != "raw,canonical" ||
				len(connection.sent) != 2 || string(connection.sent[1]) != `{"op":"ping"}` {
				t.Fatalf("recorded heartbeat calls=%v sent=%q error=%v", sink.snapshot(), connection.sent, err)
			}
		})
	}
}

type bybitScriptedTransport struct {
	fixtures map[string][]byte
	requests []*http.Request
}

func (transport *bybitScriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request.Clone(request.Context()))
	body, ok := transport.fixtures[request.URL.Path]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("missing"))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

type bybitRoundTripFunc func(*http.Request) (*http.Response, error)

func (function bybitRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type bybitFakeConnector struct {
	connection websocketConnection
	target     *url.URL
}

func (connector *bybitFakeConnector) Connect(_ context.Context, target *url.URL) (websocketConnection, error) {
	copy := *target
	connector.target = &copy
	return connector.connection, nil
}

type bybitFakeConnection struct {
	mutex  sync.Mutex
	frames [][]byte
	sent   [][]byte
	closed bool
}

func (connection *bybitFakeConnection) Receive() ([]byte, error) {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.closed || len(connection.frames) == 0 {
		return nil, io.EOF
	}
	payload := append([]byte(nil), connection.frames[0]...)
	connection.frames = connection.frames[1:]
	return payload, nil
}

func (connection *bybitFakeConnection) Send(payload []byte) error {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	connection.sent = append(connection.sent, append([]byte(nil), payload...))
	return nil
}

func (connection *bybitFakeConnection) Close() error {
	connection.mutex.Lock()
	connection.closed = true
	connection.mutex.Unlock()
	return nil
}

type bybitFrameSink struct {
	mutex   sync.Mutex
	calls   []string
	decoder []byte
}

func (sink *bybitFrameSink) RecordPublicRaw(_ context.Context, record exchangecontracts.PublicRawRecord) (exchangecontracts.StreamRecordToken, error) {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.calls = append(sink.calls, "raw")
	return exchangecontracts.StreamRecordToken{IngestOrdinal: uint64(len(sink.calls))}, nil
}

func (sink *bybitFrameSink) RecordPublicCanonical(_ context.Context, record exchangecontracts.PublicCanonicalRecord) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if record.Kind == exchangecontracts.RecordDecoderError {
		sink.calls = append(sink.calls, "decoder")
		sink.decoder = append([]byte(nil), record.Canonical...)
	} else {
		sink.calls = append(sink.calls, "canonical")
	}
	return nil
}

func (sink *bybitFrameSink) RecordSourceGap(context.Context, exchangecontracts.SourceGap) error {
	return nil
}

func (sink *bybitFrameSink) reset() {
	sink.mutex.Lock()
	sink.calls = nil
	sink.mutex.Unlock()
}

func (sink *bybitFrameSink) decoderSnapshot() []byte {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return append([]byte(nil), sink.decoder...)
}

func (sink *bybitFrameSink) snapshot() []string {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return append([]string(nil), sink.calls...)
}
