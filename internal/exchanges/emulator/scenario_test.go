package emulator

import (
	"io"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestFaultMatrixReproducesDeterministically(t *testing.T) {
	const expected = "e838f0921541040852261302b4749a26da850530dc1eb8d8693df0d4825c52dd"
	first := runFaultMatrix(t)
	second := runFaultMatrix(t)
	if first != second {
		t.Fatalf("fault transcript changed: %s != %s", first, second)
	}
	if first != expected {
		t.Fatalf("fault transcript changed from golden: %s", first)
	}
	t.Logf("fault transcript hash: %s", first)
}

func TestScenarioRejectsUnsafeOrAmbiguousScripts(t *testing.T) {
	t.Parallel()
	cases := []Scenario{
		{},
		{Name: "unsafe", Seed: "v1", REST: []RESTStep{{Method: "POST", Path: "/api/v3/depth", Status: 200}}},
		{Name: "unsafe", Seed: "v1", REST: []RESTStep{{Method: "GET", Path: "/api/v3/depth", Status: 200,
			Headers: []Header{{Name: "Authorization", Value: "canary"}}}}},
		{Name: "unsafe", Seed: "v1", StreamSessions: []StreamSession{{Path: "/../escape", Frames: []StreamFrame{{Body: []byte(`{}`)}}}}},
	}
	for _, scenario := range cases {
		if err := scenario.Validate(); err == nil {
			t.Fatalf("unsafe scenario accepted: %+v", scenario)
		}
	}
}

func runFaultMatrix(t *testing.T) string {
	t.Helper()
	scenario := faultScenario()
	server, err := NewServer(scenario)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	for _, target := range []string{
		server.URL() + "/api/v3/depth?symbol=BTCUSDT",
		server.URL() + "/api/v3/depth?limit=100&symbol=BTCUSDT",
		server.URL() + "/api/v3/exchangeInfo?symbol=BTCUSDT",
		server.URL() + "/emulator/state",
	} {
		response, requestErr := client.Get(target)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	for connection := 0; connection < 2; connection++ {
		consumeStream(t, server.WebSocketURL()+"/ws/conformance", server.URL())
	}
	if !server.Complete() {
		t.Fatal("fault scenario was not fully consumed")
	}
	hash, err := server.TranscriptHash()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func consumeStream(t *testing.T, target, origin string) {
	t.Helper()
	connection, err := websocket.Dial(target, "", origin)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	for {
		var body []byte
		if err := websocket.Message.Receive(connection, &body); err != nil {
			return
		}
	}
}

func faultScenario() Scenario {
	return Scenario{
		Name: "complete-fault-matrix", Seed: "fault-matrix-v1",
		REST: []RESTStep{
			{Method: "GET", Path: "/api/v3/depth", RawQuery: "symbol=BTCUSDT", Status: 429,
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}, {Name: "Retry-After", Value: "1"}}, Body: []byte(`{"code":"rate_limited"}`)},
			{Method: "GET", Path: "/api/v3/depth", RawQuery: "limit=100&symbol=BTCUSDT", Status: 200,
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(`{"lastUpdateId":10,"bids":[],"asks":[]}`), Delay: time.Millisecond},
			{Method: "GET", Path: "/api/v3/exchangeInfo", RawQuery: "symbol=BTCUSDT", Status: 200,
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(`{"filter_version":2,"schema_change":true}`)},
			{Method: "GET", Path: "/emulator/state", Status: 200,
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(`{"reconciliation_snapshot":"reset_epoch_2"}`)},
		},
		StreamSessions: []StreamSession{
			{Path: "/ws/conformance", Frames: []StreamFrame{
				{Body: []byte(`{"kind":"snapshot","sequence":100}`)},
				{Body: []byte(`{"kind":"duplicate","sequence":100}`)},
				{Body: []byte(`{"kind":"gap","sequence":103}`)},
				{Body: []byte(`{"kind":"out_of_order","sequence":99}`)},
				{Body: []byte(`not-json`)},
				{Body: []byte(`{"kind":"stale","event_time":1}`)},
				{Body: []byte(`{"kind":"schema_change","unexpected":true}`)},
				{Close: true},
			}},
			{Path: "/ws/conformance", Frames: []StreamFrame{
				{Body: []byte(`{"kind":"asynchronous_acknowledgement","state":"accepted"}`)},
				{Body: []byte(`{"kind":"partial_fill","cumulative":"0.1"}`)},
				{Body: []byte(`{"kind":"unknown_state"}`)},
				{Body: []byte(`{"kind":"late_fill","cumulative":"0.2"}`)},
				{Body: []byte(`{"kind":"account_reset","epoch":2}`)},
				{Body: []byte(`{"kind":"reconciliation_snapshot","epoch":2}`)},
				{Close: true},
			}},
		},
	}
}
