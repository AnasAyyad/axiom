package emulator

import (
	"context"
	"sync"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	exchangecontracts "axiom/internal/exchanges/contracts"

	"golang.org/x/net/websocket"
)

type stream struct {
	connection *websocket.Conn
	clock      domain.Clock
	kind       exchangecontracts.StreamKind
	closeOnce  sync.Once
}

type receiveResult struct {
	body []byte
	err  error
}

// Receive waits for and immediately normalizes one public emulator frame.
func (source *stream) Receive(ctx context.Context) (exchangecontracts.StreamEvent, error) {
	result := make(chan receiveResult, 1)
	go func() {
		var body []byte
		err := websocket.Message.Receive(source.connection, &body)
		result <- receiveResult{body: body, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = source.Close()
		return exchangecontracts.StreamEvent{}, exchangecontracts.NewError(
			exchangecontracts.ErrorCanceled, exchangecontracts.OperationStream, 0,
		)
	case received := <-result:
		if received.err != nil {
			return exchangecontracts.StreamEvent{}, exchangecontracts.NewError(
				exchangecontracts.ErrorTransient, exchangecontracts.OperationStream, 0,
			)
		}
		return source.normalize(received.body)
	}
}

// Close idempotently closes the local emulator stream.
func (source *stream) Close() error {
	var err error
	source.closeOnce.Do(func() { err = source.connection.Close() })
	return err
}

func (source *stream) normalize(body []byte) (exchangecontracts.StreamEvent, error) {
	switch source.kind {
	case exchangecontracts.StreamDepth:
		depth, err := binance.NormalizeDepth(body, source.clock.Now())
		if err != nil {
			return exchangecontracts.StreamEvent{}, err
		}
		return exchangecontracts.StreamEvent{Kind: source.kind, Depth: &depth}, nil
	default:
		return exchangecontracts.StreamEvent{}, exchangecontracts.NewError(
			exchangecontracts.ErrorCapability, exchangecontracts.OperationStream, 0,
		)
	}
}
