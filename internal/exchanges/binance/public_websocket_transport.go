package binance

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"

	"golang.org/x/net/websocket"
)

type websocketConnection interface {
	Receive() ([]byte, error)
	Close() error
}

type websocketConnector interface {
	Connect(context.Context, *url.URL) (websocketConnection, error)
}

type secureWebsocketConnector struct{ dialer *publicDialer }

func newSecureWebsocketConnector() *secureWebsocketConnector {
	return &secureWebsocketConnector{dialer: &publicDialer{host: "data-stream.binance.vision", resolver: net.DefaultResolver,
		dialer: net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}}}
}

// Connect opens one validated TLS WebSocket to the compiled public host.
func (connector *secureWebsocketConnector) Connect(ctx context.Context, target *url.URL) (websocketConnection, error) {
	if _, err := validateWebSocketTarget(target); err != nil {
		return nil, err
	}
	raw, err := connector.dialer.DialContext(ctx, "tcp", "data-stream.binance.vision:443")
	if err != nil {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient, exchangecontracts.OperationStream, 0)
	}
	tlsConnection := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "data-stream.binance.vision"})
	if err = tlsConnection.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient, exchangecontracts.OperationStream, 0)
	}
	configuration, err := websocket.NewConfig(target.String(), "https://data-stream.binance.vision")
	if err != nil {
		_ = tlsConnection.Close()
		return nil, policyError(exchangecontracts.OperationStream)
	}
	connection, err := websocket.NewClient(configuration, tlsConnection)
	if err != nil {
		_ = tlsConnection.Close()
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient, exchangecontracts.OperationStream, 0)
	}
	connection.MaxPayloadBytes = publicBodyLimit
	return &xNetConnection{connection: connection}, nil
}

type xNetConnection struct{ connection *websocket.Conn }

// Receive reads one bounded binary/text payload.
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

// Close terminates the underlying public WebSocket.
func (connection *xNetConnection) Close() error { return connection.connection.Close() }
