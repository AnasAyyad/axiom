package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"axiom/internal/sandbox"
)

type privateReceive struct {
	body []byte
	err  error
}

type fakePrivateConnection struct {
	sent     [][]byte
	received []privateReceive
	closed   bool
}

func (connection *fakePrivateConnection) Send(
	_ context.Context,
	body []byte,
) error {
	connection.sent = append(connection.sent, append([]byte(nil), body...))
	return nil
}

func (connection *fakePrivateConnection) Receive(
	context.Context,
) ([]byte, error) {
	if len(connection.received) == 0 {
		return nil, errors.New("empty")
	}
	result := connection.received[0]
	connection.received = connection.received[1:]
	return result.body, result.err
}

func (connection *fakePrivateConnection) Close() error {
	connection.closed = true
	return nil
}

type fakePrivateConnector struct {
	connections []*fakePrivateConnection
	index       int
}

func (connector *fakePrivateConnector) Connect(
	context.Context,
) (privateStreamConnection, error) {
	if connector.index >= len(connector.connections) {
		return nil, errors.New("no connection")
	}
	connection := connector.connections[connector.index]
	connector.index++
	return connection, nil
}

func TestBinancePrivateDecoderNormalizesBalanceAndExecution(t *testing.T) {
	now, submission, decoder := binancePrivateDecoderFixture(t)
	assertBinanceBalanceDecoding(t, now, submission, decoder)
	assertBinanceExecutionDecoding(t, decoder)
}

func binancePrivateDecoderFixture(
	t *testing.T,
) (time.Time, sandbox.Submission, *privateEventDecoder) {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_100).UTC()
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"),
		now.Add(-time.Second),
	)
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	decoder, err := newPrivateEventDecoder(
		submission.AccountID,
		submission.AccountEpoch,
		lookup,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	return now, submission, decoder
}

func assertBinanceBalanceDecoding(
	t *testing.T,
	now time.Time,
	submission sandbox.Submission,
	decoder *privateEventDecoder,
) {
	t.Helper()
	balanceBody := []byte(`{
	  "subscriptionId":1,
	  "event":{
	    "e":"outboundAccountPosition","E":1700000000000,"u":1700000000000,
	    "B":[{"a":"USDT","f":"100","l":"0"},{"a":"BTC","f":"1","l":"0"}]
	  }
	}`)
	var envelope privateEventEnvelope
	if decodeErr := strictDecode(balanceBody, &envelope); decodeErr != nil {
		t.Fatalf("balance envelope: %v", decodeErr)
	}
	if envelope.SubscriptionID == nil || *envelope.SubscriptionID != 1 ||
		len(envelope.Event) == 0 {
		t.Fatalf("balance envelope values: %#v", envelope)
	}
	var nativeBalance privateBalancePayload
	if decodeErr := strictDecode(envelope.Event, &nativeBalance); decodeErr != nil {
		t.Fatalf("balance payload: %v", decodeErr)
	}
	kind, decodeErr := exactPrivateEventType(envelope.Event)
	if decodeErr != nil || kind != "outboundAccountPosition" {
		t.Fatalf("balance kind=%#v err=%v", kind, decodeErr)
	}
	debugHash := hashBytes(balanceBody)
	debugEvent := sandbox.PrivateEvent{
		Identity:        "binance-balance-1-1700000000000-" + debugHash[:12],
		AccountID:       submission.AccountID,
		AccountEpoch:    submission.AccountEpoch,
		Kind:            sandbox.PrivateBalanceEvent,
		NativeOrderHash: debugHash,
		BalanceHash:     canonicalHash(nativeBalance.Balances),
		OccurredAt:      time.UnixMilli(nativeBalance.EventTime).UTC(),
		ReceivedAt:      now,
	}
	if validateErr := debugEvent.Validate(); validateErr != nil {
		t.Fatalf("constructed balance event: %#v err=%v", debugEvent, validateErr)
	}
	balance, err := decoder.decode(context.Background(), balanceBody)
	if err != nil || balance.event.Kind != sandbox.PrivateBalanceEvent ||
		balance.event.BalanceHash == "" {
		t.Fatalf("balance=%#v err=%v", balance, err)
	}
}

func assertBinanceExecutionDecoding(
	t *testing.T,
	decoder *privateEventDecoder,
) {
	t.Helper()
	execution, err := decoder.decode(context.Background(), []byte(`{
	  "subscriptionId":1,
	  "event":{
	    "e":"executionReport","E":1700000000000,"s":"BTCUSDT",
	    "c":"ax-00000001","S":"BUY","o":"LIMIT","f":"GTC",
	    "q":"0.1","p":"100","P":"0","F":"0","g":-1,"C":"",
	    "x":"NEW","X":"NEW","r":"NONE","i":42,"l":"0","z":"0",
	    "L":"0","n":"0","N":null,"T":1700000000000,"t":-1,
	    "I":5,"w":true,"m":false,"M":false,"O":1700000000000,
	    "Z":"0","Y":"0","Q":"0","W":1700000000000,"V":"NONE"
	  }
	}`))
	if err != nil || execution.needsBackfill ||
		execution.event.OrderEvent == nil ||
		execution.event.OrderEvent.ExchangeStatus != "NEW" {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
}

func TestBinancePrivateDecoderRequiresRESTBackfillForCumulativeFill(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_100).UTC()
	submission := completeSandboxSubmission(
		sandboxSubmission(t, "100", "0.1", "10"),
		now.Add(-time.Second),
	)
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{
		submission.ClientOrderID: submission,
	}}
	decoder, err := newPrivateEventDecoder(
		submission.AccountID,
		submission.AccountEpoch,
		lookup,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decoder.decode(context.Background(), []byte(`{
	  "subscriptionId":1,
	  "event":{
	    "e":"executionReport","E":1700000000000,"s":"BTCUSDT",
	    "c":"ax-00000001","S":"BUY","o":"LIMIT","f":"GTC",
	    "q":"0.1","p":"100","P":"0","F":"0","g":-1,"C":"",
	    "x":"TRADE","X":"FILLED","r":"NONE","i":42,"l":"0.1","z":"0.1",
	    "L":"100","n":"0.01","N":"USDT","T":1700000000000,"t":7,
	    "I":6,"w":false,"m":false,"M":false,"O":1700000000000,
	    "Z":"10","Y":"10","Q":"0","W":1700000000000,"V":"NONE"
	  }
	}`))
	if err != nil || !decoded.needsBackfill ||
		decoded.submission.ClientOrderID != submission.ClientOrderID {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}
}

func TestBinancePrivateStreamSignsSubscribesAndBackfillsAfterReconnect(t *testing.T) {
	fixture := newBinancePrivateStreamFixture(t)
	defer fixture.source.Close()
	event, err := fixture.source.Receive(context.Background())
	if err != nil || event.Kind != sandbox.PrivateBalanceEvent ||
		fixture.connector.index != 2 {
		t.Fatalf(
			"event=%#v reconnects=%d err=%v",
			event, fixture.connector.index, err,
		)
	}
	if len(fixture.first.sent) != 1 || len(fixture.second.sent) != 1 ||
		len(fixture.evidence.records) != 2 {
		t.Fatalf(
			"sent=(%d,%d) evidence=%d",
			len(fixture.first.sent), len(fixture.second.sent),
			len(fixture.evidence.records),
		)
	}
	assertSubscriptionSignature(t, fixture.first.sent[0])
	assertBinancePrivateEvidence(t, fixture.evidence)
}

func TestBinancePrivateStreamExplicitReconnectCompletesWithoutEvent(
	t *testing.T,
) {
	fixture := newBinancePrivateStreamFixture(t)
	defer fixture.source.Close()
	if _, err := fixture.source.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	ack := []byte(`{"id":"axiom-1699999999100","status":200,"result":{"subscriptionId":0},"rateLimits":[]}`)
	third := &fakePrivateConnection{
		received: []privateReceive{{body: ack}},
	}
	fixture.connector.connections = append(
		fixture.connector.connections,
		third,
	)
	if err := fixture.source.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fixture.second.closed || fixture.connector.index != 3 ||
		len(third.sent) != 1 || len(fixture.evidence.records) != 3 {
		t.Fatalf(
			"closed=%t reconnects=%d sent=%d evidence=%d",
			fixture.second.closed, fixture.connector.index,
			len(third.sent), len(fixture.evidence.records),
		)
	}
}

type binancePrivateStreamFixture struct {
	source    *BinancePrivateEventSource
	evidence  *captureEvidence
	connector *fakePrivateConnector
	first     *fakePrivateConnection
	second    *fakePrivateConnection
}

func newBinancePrivateStreamFixture(
	t *testing.T,
) binancePrivateStreamFixture {
	t.Helper()
	now := time.UnixMilli(1_700_000_000_100).UTC()
	evidence := &captureEvidence{}
	client, adapter, lookup := newBinancePrivateStreamDependencies(
		t, now, evidence,
	)
	ack := []byte(`{"id":"axiom-1699999999100","status":200,"result":{"subscriptionId":0},"rateLimits":[]}`)
	first := &fakePrivateConnection{received: []privateReceive{
		{body: ack},
		{err: errors.New("disconnect")},
	}}
	second := &fakePrivateConnection{received: []privateReceive{
		{body: ack},
		{body: []byte(`{
		  "subscriptionId":0,
		  "event":{
		    "e":"outboundAccountPosition","E":1700000000000,"u":1700000000000,
		    "B":[{"a":"USDT","f":"100","l":"0"}]
		  }
		}`)},
	}}
	connector := &fakePrivateConnector{
		connections: []*fakePrivateConnection{first, second},
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
	return binancePrivateStreamFixture{
		source: source, evidence: evidence, connector: connector,
		first: first, second: second,
	}
}

func newBinancePrivateStreamDependencies(
	t *testing.T,
	now time.Time,
	evidence *captureEvidence,
) (*SandboxClient, *SandboxAdapter, *sandboxLookup) {
	t.Helper()
	client, err := newSandboxClientForTest(
		authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("REST not expected")
		}),
		sandbox.CredentialPair{APIKey: "test-key", APISecret: "test-secret"},
		evidence, "cfg", func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	lookup := &sandboxLookup{submissions: map[string]sandbox.Submission{}}
	adapter, err := newSandboxAdapterForTest(
		client, sandboxIdentity(now), 1, lookup,
		&sandboxExpectations{}, sandboxRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	return client, adapter, lookup
}

func assertBinancePrivateEvidence(
	t *testing.T,
	evidence *captureEvidence,
) {
	t.Helper()
	for _, record := range evidence.records {
		if record.Host != sandboxWebSocketHost ||
			record.Path != sandboxWebSocketEvidence ||
			record.Method != "WS" ||
			strings.Contains(strings.Join(record.FieldNames, ","), "apiKey") ||
			strings.Contains(strings.Join(record.FieldNames, ","), "signature") {
			t.Fatalf("unsafe stream evidence: %#v", record)
		}
	}
}

func assertSubscriptionSignature(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Method string `json:"method"`
		Params struct {
			APIKey     string `json:"apiKey"`
			RecvWindow uint64 `json:"recvWindow"`
			Timestamp  int64  `json:"timestamp"`
			Signature  string `json:"signature"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"apiKey":     {request.Params.APIKey},
		"recvWindow": {strconv.FormatUint(request.Params.RecvWindow, 10)},
		"timestamp":  {strconv.FormatInt(request.Params.Timestamp, 10)},
	}
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(values.Encode()))
	if request.Method != sandboxSubscriptionMethod ||
		request.Params.Timestamp != 1_699_999_999_100 ||
		request.Params.Signature != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("invalid subscription request: %s", body)
	}
}

func TestBinancePrivateTransportUsesOnlyJSONTextPayloads(t *testing.T) {
	payload, err := privateTextPayload([]byte(`{"op":"subscribe"}`))
	if err != nil || payload != `{"op":"subscribe"}` {
		t.Fatalf("payload=%q error=%v", payload, err)
	}
	if _, err = privateTextPayload([]byte{0xff}); err == nil {
		t.Fatal("invalid JSON private payload accepted")
	}
}

func FuzzBinancePrivateEventDecoder(f *testing.F) {
	f.Add([]byte(`{
	  "subscriptionId":1,
	  "event":{
	    "e":"outboundAccountPosition","E":1700000000000,"u":1700000000000,
	    "B":[{"a":"USDT","f":"100","l":"0"}]
	  }
	}`))
	f.Add([]byte(`{
	  "subscriptionId":1,
	  "event":{
	    "e":"executionReport","E":1700000000000,"s":"BTCUSDT",
	    "c":"ax-00000001","S":"BUY","o":"LIMIT","f":"GTC",
	    "q":"0.1","p":"100","x":"NEW","X":"NEW","i":42,
	    "l":"0","z":"0","T":1700000000000,"I":5
	  }
	}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		assertFuzzedBinancePrivateEvent(t, body)
	})
}

func assertFuzzedBinancePrivateEvent(t *testing.T, body []byte) {
	t.Helper()
	_, submission, decoder := binancePrivateDecoderFixture(t)
	decoded, err := decoder.decode(context.Background(), body)
	if err != nil {
		return
	}
	if decoded.needsBackfill {
		if decoded.submission.ClientOrderID != submission.ClientOrderID {
			t.Fatal("accepted backfill lost immutable client identity")
		}
		return
	}
	if decoded.event.Validate() != nil ||
		decoded.event.AccountID != submission.AccountID ||
		decoded.event.AccountEpoch != submission.AccountEpoch {
		t.Fatalf("unsafe accepted private event: %#v", decoded.event)
	}
}
