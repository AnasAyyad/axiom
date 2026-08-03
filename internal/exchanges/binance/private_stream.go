package binance

import (
	"context"
	"sort"
	"sync"
	"time"

	"axiom/internal/sandbox"

	"golang.org/x/net/websocket"
)

const (
	sandboxWebSocketHost      = "ws-api.testnet.binance.vision"
	sandboxWebSocketPath      = "/ws-api/v3"
	sandboxWebSocketURL       = "wss://" + sandboxWebSocketHost + sandboxWebSocketPath
	sandboxWebSocketEvidence  = "/ws-api/v3/userDataStream.subscribe.signature"
	sandboxWebSocketProxy     = "binance-testnet-egress:8080"
	sandboxWebSocketOrigin    = "https://axiom.invalid"
	sandboxSubscriptionMethod = "userDataStream.subscribe.signature"
	sandboxWebSocketTimeGuard = time.Second
)

type privateStreamConnection interface {
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
	Close() error
}

type privateStreamConnector interface {
	Connect(context.Context) (privateStreamConnection, error)
}

type connectPrivateStreamConnector struct{}

type webSocketPrivateConnection struct {
	connection *websocket.Conn
	closeOnce  sync.Once
}

// BinancePrivateEventSource is one normalized, reconnecting Testnet
// user-data source. It has no WebSocket order-entry method.
type BinancePrivateEventSource struct {
	client     *SandboxClient
	adapter    *SandboxAdapter
	recovery   sandbox.SubmissionRecoveryReader
	decoder    *privateEventDecoder
	connector  privateStreamConnector
	connection privateStreamConnection
	pending    []sandbox.PrivateEvent
	closed     bool
	mutex      sync.Mutex
}

var _ sandbox.PrivateEventSource = (*BinancePrivateEventSource)(nil)

// NewPrivateEventSource opens only the fixed Testnet WebSocket API route,
// authenticates with HMAC, and performs a bounded startup backfill.
func NewPrivateEventSource(
	ctx context.Context,
	client *SandboxClient,
	adapter *SandboxAdapter,
	recovery sandbox.SubmissionRecoveryReader,
) (*BinancePrivateEventSource, error) {
	return newPrivateEventSource(
		ctx,
		client,
		adapter,
		recovery,
		connectPrivateStreamConnector{},
	)
}

func newPrivateEventSource(
	ctx context.Context,
	client *SandboxClient,
	adapter *SandboxAdapter,
	recovery sandbox.SubmissionRecoveryReader,
	connector privateStreamConnector,
) (*BinancePrivateEventSource, error) {
	if client == nil || adapter == nil || recovery == nil || connector == nil {
		return nil, ErrSandboxPrivateEvent
	}
	decoder, err := newPrivateEventDecoder(
		adapter.identity.AccountID,
		adapter.epoch,
		recovery,
		client.now,
	)
	if err != nil {
		return nil, err
	}
	source := &BinancePrivateEventSource{
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

// Receive returns one normalized event and reconnects once after a transport
// loss. Backfilled durable client IDs are emitted before new stream frames.
func (source *BinancePrivateEventSource) Receive(
	ctx context.Context,
) (sandbox.PrivateEvent, error) {
	body, pending, err := source.receivePrivateBody(ctx)
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
		return sandbox.PrivateEvent{}, ErrSandboxPrivateEvent
	}
	return events[0], nil
}

func (source *BinancePrivateEventSource) receivePrivateBody(
	ctx context.Context,
) ([]byte, *sandbox.PrivateEvent, error) {
	source.mutex.Lock()
	if source.closed {
		source.mutex.Unlock()
		return nil, nil, ErrSandboxPrivateEvent
	}
	if event, ok := source.popPending(); ok {
		source.mutex.Unlock()
		return nil, &event, nil
	}
	connection := source.connection
	source.mutex.Unlock()

	body, err := connection.Receive(ctx)
	if err != nil {
		source.mutex.Lock()
		if source.closed {
			source.mutex.Unlock()
			return nil, nil, ErrSandboxPrivateEvent
		}
		if err = source.reconnectLocked(ctx); err != nil {
			source.mutex.Unlock()
			return nil, nil, ErrSandboxPrivateEvent
		}
		if event, ok := source.popPending(); ok {
			source.mutex.Unlock()
			return nil, &event, nil
		}
		connection = source.connection
		source.mutex.Unlock()
		body, err = connection.Receive(ctx)
		if err != nil {
			return nil, nil, ErrSandboxPrivateEvent
		}
	}
	return body, nil, nil
}

// Reconnect closes any stale socket and completes a bounded backfill before
// the engine can consider the private stream healthy again.
func (source *BinancePrivateEventSource) Reconnect(ctx context.Context) error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if source.closed {
		return ErrSandboxPrivateEvent
	}
	return source.reconnectLocked(ctx)
}

// Close idempotently closes the private stream.
func (source *BinancePrivateEventSource) Close() error {
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

func (source *BinancePrivateEventSource) connectAndBackfill(
	ctx context.Context,
) error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.connectAndBackfillLocked(ctx)
}

func (source *BinancePrivateEventSource) connectAndBackfillLocked(
	ctx context.Context,
) error {
	connection, err := source.connector.Connect(ctx)
	if err != nil {
		return ErrSandboxPrivateEvent
	}
	if err = source.subscribe(ctx, connection); err != nil {
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

func (source *BinancePrivateEventSource) reconnectLocked(
	ctx context.Context,
) error {
	if source.connection != nil {
		_ = source.connection.Close()
	}
	source.connection = nil
	source.pending = nil
	return source.connectAndBackfillLocked(ctx)
}

func (source *BinancePrivateEventSource) backfill(ctx context.Context) error {
	submissions, err := source.recovery.ActiveSubmissions(
		ctx,
		source.adapter.identity.AccountID,
		source.adapter.epoch,
	)
	if err != nil || len(submissions) > 2 {
		return ErrSandboxPrivateEvent
	}
	sort.Slice(submissions, func(left, right int) bool {
		return submissions[left].ClientOrderID < submissions[right].ClientOrderID
	})
	for _, submission := range submissions {
		events, queryErr := source.adapter.Query(
			ctx,
			submission.AccountID,
			submission.AccountEpoch,
			submission.ClientOrderID,
		)
		if queryErr != nil {
			return ErrSandboxPrivateEvent
		}
		source.pending = append(source.pending, events...)
	}
	return nil
}

func (source *BinancePrivateEventSource) popPending() (
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
