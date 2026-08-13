package bybit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

const (
	demoPrivateWebSocketHost = "stream-demo.bybit.com"
	demoPrivateWebSocketPath = "/v5/private"
	demoPrivateWebSocketURL  = "wss://" +
		demoPrivateWebSocketHost +
		demoPrivateWebSocketPath
	demoPrivateWebSocketProxy  = "bybit-demo-egress:8080"
	demoPrivateWebSocketOrigin = "https://axiom.invalid"
	demoPrivateEvidencePath    = "/v5/private/auth"
	demoPrivateSubscribeID     = "axiom-private-v1"
	demoPrivateHeartbeatID     = "axiom-heartbeat-v1"
)

var demoPrivateTopics = []string{
	"order.spot",
	"execution.spot",
	"wallet",
}

type demoPrivateConnection interface {
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
	Close() error
}

type demoPrivateConnector interface {
	Connect(context.Context) (demoPrivateConnection, error)
}

type connectDemoPrivateConnector struct{}

// BybitPrivateEventSource is the fixed Demo-only private stream. It exposes
// no WebSocket order-entry operation.
type BybitPrivateEventSource struct {
	client     *SandboxClient
	adapter    *SandboxAdapter
	recovery   sandbox.SubmissionRecoveryReader
	decoder    *demoPrivateDecoder
	connector  demoPrivateConnector
	connection demoPrivateConnection
	pending    []sandbox.PrivateEvent
	closed     bool
	mutex      sync.Mutex
}

var _ sandbox.PrivateEventSource = (*BybitPrivateEventSource)(nil)

// NewPrivateEventSource opens the fixed Bybit Demo private stream and performs
// its initial deterministic backfill.
func NewPrivateEventSource(
	ctx context.Context,
	client *SandboxClient,
	adapter *SandboxAdapter,
	recovery sandbox.SubmissionRecoveryReader,
) (*BybitPrivateEventSource, error) {
	return newPrivateEventSource(
		ctx,
		client,
		adapter,
		recovery,
		connectDemoPrivateConnector{},
	)
}

func newPrivateEventSource(
	ctx context.Context,
	client *SandboxClient,
	adapter *SandboxAdapter,
	recovery sandbox.SubmissionRecoveryReader,
	connector demoPrivateConnector,
) (*BybitPrivateEventSource, error) {
	if client == nil || adapter == nil || recovery == nil ||
		connector == nil {
		return nil, ErrDemoPrivateEvent
	}
	decoder, err := newDemoPrivateDecoder(
		adapter.identity.AccountID,
		adapter.epoch,
		recovery,
		client.now,
	)
	if err != nil {
		return nil, err
	}
	source := &BybitPrivateEventSource{
		client:    client,
		adapter:   adapter,
		recovery:  recovery,
		decoder:   decoder,
		connector: connector,
	}
	if err = source.connectAndBackfill(ctx); err != nil {
		return nil, err
	}
	return source, nil
}

// Receive returns one normalized durable private event. Transport loss is
// returned as one typed, sanitized failure so the engine owns reconnect,
// backfill, reconciliation, and readiness as one bounded state machine.
func (source *BybitPrivateEventSource) Receive(
	ctx context.Context,
) (sandbox.PrivateEvent, error) {
	body, pending, err := source.receiveDemoPrivateBody(ctx)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	if pending != nil {
		return *pending, nil
	}
	decoded, err := source.decoder.decode(ctx, body)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	if !decoded.needsBackfill {
		return decoded.event, nil
	}
	events, err := source.adapter.Query(
		ctx, decoded.submission.AccountID, decoded.submission.AccountEpoch,
		decoded.submission.ClientOrderID,
	)
	if err != nil || len(events) != 1 {
		return sandbox.PrivateEvent{}, ErrDemoPrivateEvent
	}
	return events[0], nil
}

func (source *BybitPrivateEventSource) receiveDemoPrivateBody(
	ctx context.Context,
) ([]byte, *sandbox.PrivateEvent, error) {
	source.mutex.Lock()
	if source.closed {
		source.mutex.Unlock()
		return nil, nil, ErrDemoPrivateEvent
	}
	if event, ok := source.popPending(); ok {
		source.mutex.Unlock()
		return nil, &event, nil
	}
	connection := source.connection
	source.mutex.Unlock()

	body, err := connection.Receive(ctx)
	if err != nil {
		return nil, nil, bybitPrivateTransportFailure(
			"private_stream_receive_failed",
		)
	}
	return body, nil, nil
}

// Close idempotently closes the active Demo private stream.
func (source *BybitPrivateEventSource) Close() error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if source.closed {
		return nil
	}
	source.closed = true
	if source.connection == nil {
		return nil
	}
	return source.connection.Close()
}

func (source *BybitPrivateEventSource) connectAndBackfill(
	ctx context.Context,
) error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.connectAndBackfillLocked(ctx)
}

func (source *BybitPrivateEventSource) connectAndBackfillLocked(
	ctx context.Context,
) error {
	connection, err := source.connector.Connect(ctx)
	if err != nil {
		return bybitPrivateTransportFailure(
			"private_stream_connect_failed",
		)
	}
	if err = source.authenticateAndSubscribe(ctx, connection); err != nil {
		_ = connection.Close()
		return err
	}
	source.connection = connection
	if err = source.backfill(ctx); err != nil {
		_ = connection.Close()
		source.connection = nil
		return err
	}
	return nil
}

func (source *BybitPrivateEventSource) authenticateAndSubscribe(
	ctx context.Context,
	connection demoPrivateConnection,
) error {
	if err := source.client.ensureDemoClock(ctx); err != nil {
		return ErrDemoPrivateEvent
	}
	request, evidence, err := source.authenticationRequest()
	if err != nil {
		return err
	}
	if exchangecontracts.ValidateAuthenticatedRequestEvidence(evidence) != nil {
		return ErrDemoPrivateEvent
	}
	if source.client.evidence.RecordAuthenticatedRequest(ctx, evidence) != nil {
		return ErrDemoPrivateEvent
	}
	if connection.Send(ctx, request) != nil {
		return bybitPrivateTransportFailure("private_stream_send_failed")
	}
	body, err := connection.Receive(ctx)
	if err != nil {
		return bybitPrivateTransportFailure("private_stream_receive_failed")
	}
	if !validDemoPrivateControl(body, "auth", "") {
		return ErrDemoPrivateEvent
	}
	subscribe, err := demoPrivateSubscriptionRequest()
	if err != nil {
		return ErrDemoPrivateEvent
	}
	if connection.Send(ctx, subscribe) != nil {
		return bybitPrivateTransportFailure("private_stream_send_failed")
	}
	body, err = connection.Receive(ctx)
	if err != nil {
		return bybitPrivateTransportFailure("private_stream_receive_failed")
	}
	if !validDemoPrivateControl(body, "subscribe", demoPrivateSubscribeID) {
		return ErrDemoPrivateEvent
	}
	return nil
}

func bybitPrivateTransportFailure(cause string) error {
	return exchangecontracts.NewDetailedError(
		exchangecontracts.ErrorTransient,
		exchangecontracts.OperationStream,
		0,
		0,
		cause,
	)
}

func (source *BybitPrivateEventSource) authenticationRequest() (
	[]byte,
	exchangecontracts.AuthenticatedRequestEvidence,
	error,
) {
	expires := source.client.now().UTC().
		Add(source.client.demoClockOffset()).
		Add(5 * time.Second).
		UnixMilli()
	signingInput := "GET/realtime" + strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, []byte(source.client.apiSecret))
	if _, err := mac.Write([]byte(signingInput)); err != nil {
		return nil, exchangecontracts.AuthenticatedRequestEvidence{},
			ErrDemoPrivateEvent
	}
	request := struct {
		Operation string `json:"op"`
		Arguments []any  `json:"args"`
	}{
		Operation: "auth",
		Arguments: []any{
			source.client.apiKey,
			expires,
			hex.EncodeToString(mac.Sum(nil)),
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, exchangecontracts.AuthenticatedRequestEvidence{},
			ErrDemoPrivateEvent
	}
	requestHash := sha256.Sum256([]byte(
		"WS\n" + demoPrivateWebSocketHost + "\nauth\n" +
			strconv.FormatInt(expires, 10),
	))
	evidence := exchangecontracts.AuthenticatedRequestEvidence{
		Exchange: "bybit", Host: demoPrivateWebSocketHost,
		Method: "WS", Path: demoPrivateEvidencePath,
		FieldNames:      []string{"timestamp"},
		Enumerated:      map[string]string{},
		RequestHash:     requestHash,
		ConfigurationID: source.client.configurationID,
		RecordedAt:      source.client.now().UTC(),
	}
	return body, evidence, nil
}

func demoPrivateSubscriptionRequest() ([]byte, error) {
	topics := append([]string(nil), demoPrivateTopics...)
	if !sort.StringsAreSorted(topics) {
		sort.Strings(topics)
	}
	request := struct {
		Operation string   `json:"op"`
		Arguments []string `json:"args"`
		RequestID string   `json:"req_id"`
	}{
		Operation: "subscribe",
		Arguments: topics,
		RequestID: demoPrivateSubscribeID,
	}
	return json.Marshal(request)
}

func validDemoPrivateControl(
	body []byte,
	operation string,
	requestID string,
) bool {
	var response struct {
		Success      bool   `json:"success"`
		Message      string `json:"ret_msg"`
		ConnectionID string `json:"conn_id"`
		RequestID    string `json:"req_id"`
		Operation    string `json:"op"`
	}
	return strictDecode(body, &response) == nil &&
		response.Success &&
		response.Message == "" &&
		response.ConnectionID != "" &&
		response.Operation == operation &&
		(requestID == "" || response.RequestID == requestID)
}

func (source *BybitPrivateEventSource) backfill(
	ctx context.Context,
) error {
	submissions, err := source.recovery.ActiveSubmissions(
		ctx,
		source.adapter.identity.AccountID,
		source.adapter.epoch,
	)
	if err != nil || len(submissions) > 2 {
		return ErrDemoPrivateEvent
	}
	sort.Slice(submissions, func(left, right int) bool {
		return submissions[left].ClientOrderID <
			submissions[right].ClientOrderID
	})
	for _, submission := range submissions {
		events, queryErr := source.adapter.Query(
			ctx,
			submission.AccountID,
			submission.AccountEpoch,
			submission.ClientOrderID,
		)
		if queryErr != nil {
			return ErrDemoPrivateEvent
		}
		source.pending = append(source.pending, events...)
	}
	return nil
}

func (source *BybitPrivateEventSource) popPending() (
	sandbox.PrivateEvent,
	bool,
) {
	if len(source.pending) == 0 {
		return sandbox.PrivateEvent{}, false
	}
	event := source.pending[0]
	source.pending = source.pending[1:]
	return event, true
}

func demoPrivateAuthSigningInput(expires int64) string {
	return fmt.Sprintf("GET/realtime%d", expires)
}
