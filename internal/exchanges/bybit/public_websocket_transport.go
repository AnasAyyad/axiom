package bybit

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
	Send([]byte) error
	Close() error
}

type websocketConnector interface {
	Connect(context.Context, *url.URL) (websocketConnection, error)
}

type secureWebsocketConnector struct{ dialer *publicDialer }

func newSecureWebsocketConnector() *secureWebsocketConnector {
	return &secureWebsocketConnector{dialer: &publicDialer{host: "stream.bybit.com",
		resolver: net.DefaultResolver, dialer: net.Dialer{Timeout: publicSetupDeadline, KeepAlive: 30 * time.Second}}}
}

// Connect opens one validated TLS WebSocket to the compiled public host.
func (connector *secureWebsocketConnector) Connect(
	ctx context.Context,
	target *url.URL,
) (websocketConnection, error) {
	if _, err := validateWebSocketTarget(target); err != nil {
		return nil, err
	}
	setupContext, cancel := context.WithTimeout(ctx, publicSetupDeadline)
	defer cancel()
	raw, metadata, err := connector.dialer.dialValidated(setupContext, "tcp", "stream.bybit.com:443")
	if err != nil {
		return nil, remapTransportError(exchangecontracts.OperationStream, err, metadata)
	}
	tlsConnection := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "stream.bybit.com"})
	tlsStarted := time.Now()
	if err = tlsConnection.HandshakeContext(setupContext); err != nil {
		_ = raw.Close()
		metadata.TLSDuration, metadata.SetupStage = time.Since(tlsStarted), "tls"
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "tls_handshake_failure", metadata)
	}
	metadata.TLSDuration = time.Since(tlsStarted)
	configuration, err := websocket.NewConfig(target.String(), "https://stream.bybit.com")
	if err != nil {
		_ = tlsConnection.Close()
		return nil, policyError(exchangecontracts.OperationStream)
	}
	upgradeStarted := time.Now()
	connection, err := websocket.NewClient(configuration, tlsConnection)
	if err != nil {
		_ = tlsConnection.Close()
		metadata.UpgradeDuration, metadata.SetupStage = time.Since(upgradeStarted), "upgrade"
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "websocket_upgrade_failure", metadata)
	}
	metadata.UpgradeDuration, metadata.SetupStage = time.Since(upgradeStarted), "upgrade"
	connection.MaxPayloadBytes = publicBodyLimit
	return &xNetConnection{connection: connection, metadata: metadata}, nil
}

type xNetConnection struct {
	connection *websocket.Conn
	metadata   exchangecontracts.FailureMetadata
}

// TransportMetadata returns the bounded setup-stage facts for this connection.
func (connection *xNetConnection) TransportMetadata() exchangecontracts.FailureMetadata {
	return connection.metadata
}

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
	started := time.Now()
	if err := connection.connection.SetWriteDeadline(time.Now().Add(publicWriteDeadline)); err != nil {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "write_deadline_failed",
			exchangecontracts.FailureMetadata{SetupStage: "write"})
	}
	err := websocket.Message.Send(connection.connection, payload)
	_ = connection.connection.SetWriteDeadline(time.Time{})
	if err != nil {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "websocket_send_failure",
			exchangecontracts.FailureMetadata{WriteDuration: time.Since(started), SetupStage: "write"})
	}
	return nil
}

// Close terminates the underlying public WebSocket.
func (connection *xNetConnection) Close() error { return connection.connection.Close() }
