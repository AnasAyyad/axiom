package bybit

import (
	"context"
	"net/url"

	exchangecontracts "axiom/internal/exchanges/contracts"

	"golang.org/x/net/websocket"
)

type websocketConnection interface {
	Receive() ([]byte, error)
	Send([]byte) error
	Close() error
}

type websocketConnector interface {
	Connect(context.Context, *url.URL) (websocketConnection, error)
}

type secureWebsocketConnector struct{}

func newSecureWebsocketConnector() *secureWebsocketConnector { return &secureWebsocketConnector{} }

// Connect opens one validated TLS WebSocket to the compiled public host.
func (connector *secureWebsocketConnector) Connect(
	ctx context.Context,
	target *url.URL,
) (websocketConnection, error) {
	if _, err := validateWebSocketTarget(target); err != nil {
		return nil, err
	}
	tlsConnection, err := websocketTLS(ctx, target)
	if err != nil {
		return nil, err
	}
	configuration, err := websocket.NewConfig(target.String(), "https://stream.bybit.com")
	if err != nil {
		_ = tlsConnection.Close()
		return nil, policyError(exchangecontracts.OperationStream)
	}
	connection, err := websocket.NewClient(configuration, tlsConnection)
	if err != nil {
		_ = tlsConnection.Close()
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0)
	}
	connection.MaxPayloadBytes = publicBodyLimit
	return &xNetConnection{connection: connection}, nil
}

type xNetConnection struct{ connection *websocket.Conn }

// Receive reads one bounded text payload.
func (connection *xNetConnection) Receive() ([]byte, error) {
	var payload []byte
	if err := websocket.Message.Receive(connection.connection, &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > publicBodyLimit {
		return nil, websocket.ErrFrameTooLarge
	}
	return payload, nil
}

// Send writes one bounded public subscription or heartbeat frame.
func (connection *xNetConnection) Send(payload []byte) error {
	if len(payload) == 0 || len(payload) > 4096 {
		return websocket.ErrFrameTooLarge
	}
	return websocket.Message.Send(connection.connection, payload)
}

// Close terminates the underlying public WebSocket.
func (connection *xNetConnection) Close() error { return connection.connection.Close() }
