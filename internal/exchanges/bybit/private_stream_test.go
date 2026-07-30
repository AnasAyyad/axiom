package bybit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"axiom/internal/sandbox"
)

type demoPrivateReceive struct {
	body []byte
	err  error
}

type fakeDemoPrivateConnection struct {
	sent     [][]byte
	received []demoPrivateReceive
	closed   bool
}

func (connection *fakeDemoPrivateConnection) Send(
	_ context.Context,
	body []byte,
) error {
	connection.sent = append(
		connection.sent,
		append([]byte(nil), body...),
	)
	return nil
}

func (connection *fakeDemoPrivateConnection) Receive(
	context.Context,
) ([]byte, error) {
	if len(connection.received) == 0 {
		return nil, errors.New("empty")
	}
	result := connection.received[0]
	connection.received = connection.received[1:]
	return result.body, result.err
}

func (connection *fakeDemoPrivateConnection) Close() error {
	connection.closed = true
	return nil
}

type fakeDemoPrivateConnector struct {
	connections []*fakeDemoPrivateConnection
	index       int
}

func (connector *fakeDemoPrivateConnector) Connect(
	context.Context,
) (demoPrivateConnection, error) {
	if connector.index >= len(connector.connections) {
		return nil, errors.New("no connection")
	}
	connection := connector.connections[connector.index]
	connector.index++
	return connection, nil
}

func TestBybitPrivateStreamAuthenticatesSubscribesAndReconnects(t *testing.T) {
	fixture := newBybitPrivateStreamFixture(t)
	defer fixture.source.Close()
	event, err := fixture.source.Receive(context.Background())
	if err != nil || event.Kind != sandbox.PrivateBalanceEvent ||
		fixture.connector.index != 2 {
		t.Fatalf(
			"event=%#v reconnects=%d err=%v",
			event, fixture.connector.index, err,
		)
	}
	if len(fixture.first.sent) != 2 || len(fixture.second.sent) != 2 ||
		len(fixture.evidence.records) != 2 {
		t.Fatalf(
			"sent=(%d,%d) evidence=%d",
			len(fixture.first.sent), len(fixture.second.sent),
			len(fixture.evidence.records),
		)
	}
	assertDemoPrivateAuth(t, fixture.first.sent[0])
	assertDemoPrivateTopics(t, fixture.first.sent[1])
	assertBybitPrivateEvidence(t, fixture.evidence)
}

func TestBybitPrivateStreamExplicitReconnectCompletesWithoutEvent(
	t *testing.T,
) {
	fixture := newBybitPrivateStreamFixture(t)
	defer fixture.source.Close()
	if _, err := fixture.source.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	auth := []byte(`{"success":true,"ret_msg":"","conn_id":"demo-2","req_id":"","op":"auth"}`)
	subscribe := []byte(`{"success":true,"ret_msg":"","conn_id":"demo-2","req_id":"axiom-private-v1","op":"subscribe"}`)
	third := &fakeDemoPrivateConnection{
		received: []demoPrivateReceive{{body: auth}, {body: subscribe}},
	}
	fixture.connector.connections = append(
		fixture.connector.connections,
		third,
	)
	if err := fixture.source.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fixture.second.closed || fixture.connector.index != 3 ||
		len(third.sent) != 2 || len(fixture.evidence.records) != 3 {
		t.Fatalf(
			"closed=%t reconnects=%d sent=%d evidence=%d",
			fixture.second.closed, fixture.connector.index,
			len(third.sent), len(fixture.evidence.records),
		)
	}
}

type bybitPrivateStreamFixture struct {
	source    *BybitPrivateEventSource
	evidence  *captureEvidence
	connector *fakeDemoPrivateConnector
	first     *fakeDemoPrivateConnection
	second    *fakeDemoPrivateConnection
}

func newBybitPrivateStreamFixture(t *testing.T) bybitPrivateStreamFixture {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_100).UTC()
	evidence := &captureEvidence{}
	client, adapter, lookup := newBybitPrivateStreamDependencies(
		t, now, evidence,
	)
	auth := []byte(`{"success":true,"ret_msg":"","conn_id":"demo-1","req_id":"","op":"auth"}`)
	subscribe := []byte(`{"success":true,"ret_msg":"","conn_id":"demo-1","req_id":"axiom-private-v1","op":"subscribe"}`)
	first := &fakeDemoPrivateConnection{received: []demoPrivateReceive{
		{body: auth},
		{body: subscribe},
		{err: errors.New("disconnect")},
	}}
	second := &fakeDemoPrivateConnection{received: []demoPrivateReceive{
		{body: auth},
		{body: subscribe},
		{body: []byte(`{
		  "id":"wallet-1","topic":"wallet","creationTime":1700000000000,
		  "data":[{
		    "accountType":"UNIFIED",
		    "coin":[{
		      "coin":"USDT","walletBalance":"100","locked":"0",
		      "spotBorrow":"0","borrowAmount":"0","accruedInterest":"0",
		      "totalPositionIM":"0","totalPositionMM":"0","spotHedgingQty":"0"
		    }]
		  }]
		}`)},
	}}
	connector := &fakeDemoPrivateConnector{
		connections: []*fakeDemoPrivateConnection{first, second},
	}
	source, err := newPrivateEventSource(
		context.Background(),
		client,
		adapter,
		lookup,
		connector,
	)
	if err != nil {
		t.Fatal(err)
	}
	return bybitPrivateStreamFixture{
		source: source, evidence: evidence, connector: connector,
		first: first, second: second,
	}
}

func newBybitPrivateStreamDependencies(
	t *testing.T,
	now time.Time,
	evidence *captureEvidence,
) (*SandboxClient, *SandboxAdapter, *demoLookup) {
	t.Helper()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("REST not expected")
		}),
		sandbox.CredentialPair{
			APIKey: "test-key", APISecret: "test-secret",
		},
		evidence, "cfg", func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	lookup := &demoLookup{submissions: map[string]sandbox.Submission{}}
	adapter, err := newSandboxAdapterForTest(
		client, demoIdentity(now), 1, lookup,
		&demoExpectations{}, demoRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	return client, adapter, lookup
}

func assertBybitPrivateEvidence(
	t *testing.T,
	evidence *captureEvidence,
) {
	t.Helper()
	for _, record := range evidence.records {
		if record.Host != demoPrivateWebSocketHost ||
			record.Path != demoPrivateEvidencePath ||
			record.Method != "WS" ||
			strings.Contains(
				strings.Join(record.FieldNames, ","),
				"apiKey",
			) {
			t.Fatalf("unsafe private evidence: %#v", record)
		}
	}
}

func assertDemoPrivateAuth(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Operation string            `json:"op"`
		Arguments []json.RawMessage `json:"args"`
	}
	if json.Unmarshal(body, &request) != nil ||
		request.Operation != "auth" ||
		len(request.Arguments) != 3 {
		t.Fatalf("invalid auth: %s", body)
	}
	var apiKey string
	var expires int64
	var signature string
	_ = json.Unmarshal(request.Arguments[0], &apiKey)
	_ = json.Unmarshal(request.Arguments[1], &expires)
	_ = json.Unmarshal(request.Arguments[2], &signature)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(demoPrivateAuthSigningInput(expires)))
	if apiKey != "test-key" ||
		signature != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("invalid auth signature: %s", body)
	}
}

func assertDemoPrivateTopics(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Operation string   `json:"op"`
		Arguments []string `json:"args"`
		RequestID string   `json:"req_id"`
	}
	if json.Unmarshal(body, &request) != nil ||
		request.Operation != "subscribe" ||
		request.RequestID != demoPrivateSubscribeID ||
		strings.Join(request.Arguments, ",") !=
			"execution.spot,order.spot,wallet" {
		t.Fatalf("invalid private topics: %s", body)
	}
}

func TestBybitPrivateTransportUsesOnlyJSONTextPayloads(t *testing.T) {
	payload, err := demoPrivateTextPayload([]byte(`{"op":"auth"}`))
	if err != nil || payload != `{"op":"auth"}` {
		t.Fatalf("payload=%q error=%v", payload, err)
	}
	if _, err = demoPrivateTextPayload([]byte{0xff}); err == nil {
		t.Fatal("invalid JSON private payload accepted")
	}
}

func TestBybitPrivateHeartbeatResponsesAreClosed(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"req_id":"axiom-heartbeat-v1","op":"pong","args":["1700000000000"],"conn_id":"demo-1"}`),
		[]byte(`{"success":true,"ret_msg":"pong","conn_id":"demo-1","req_id":"axiom-heartbeat-v1","op":"ping"}`),
	} {
		if !validDemoPrivateHeartbeat(body) {
			t.Fatalf("documented heartbeat rejected: %s", body)
		}
	}
	for _, body := range [][]byte{
		[]byte(`{"req_id":"other","op":"pong","args":["1700000000000"],"conn_id":"demo-1"}`),
		[]byte(`{"req_id":"axiom-heartbeat-v1","op":"pong","args":[],"conn_id":"demo-1"}`),
		[]byte(`{"req_id":"axiom-heartbeat-v1","op":"pong","args":["1700000000000"],"conn_id":"demo-1","unknown":true}`),
	} {
		if validDemoPrivateHeartbeat(body) {
			t.Fatalf("unsafe heartbeat accepted: %s", body)
		}
	}
}

func FuzzBybitDemoPrivateDecoder(f *testing.F) {
	f.Add([]byte(`{
	  "id":"wallet-1","topic":"wallet","creationTime":1700000000000,
	  "data":[{
	    "accountType":"UNIFIED",
	    "coin":[{
	      "coin":"USDT","walletBalance":"100","locked":"0",
	      "spotBorrow":"0","borrowAmount":"0","accruedInterest":"0",
	      "totalPositionIM":"0","totalPositionMM":"0","spotHedgingQty":"0"
	    }]
	  }]
	}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		now := time.UnixMilli(1_700_000_001_000).UTC()
		identity := demoIdentity(now)
		submission := demoSubmission(t, identity, now)
		lookup := &demoLookup{submissions: map[string]sandbox.Submission{
			submission.ClientOrderID: submission,
		}}
		decoder, err := newDemoPrivateDecoder(
			identity.AccountID,
			1,
			lookup,
			func() time.Time { return now },
		)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decoder.decode(context.Background(), body)
		if err != nil {
			return
		}
		if decoded.needsBackfill {
			if decoded.submission.ClientOrderID !=
				submission.ClientOrderID {
				t.Fatal("accepted backfill lost immutable identity")
			}
			return
		}
		if decoded.event.Validate() != nil ||
			decoded.event.AccountID != identity.AccountID ||
			decoded.event.AccountEpoch != 1 {
			t.Fatalf("unsafe private event: %#v", decoded.event)
		}
	})
}
