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

type publicWebsocketEndpoint struct {
	host   string
	origin string
}

type websocketEndpointConnector func(
	context.Context,
	*url.URL,
	publicWebsocketEndpoint,
) (websocketConnection, error)

type secureWebsocketConnector struct {
	endpoints []publicWebsocketEndpoint
	connect   websocketEndpointConnector
}

func newSecureWebsocketConnector() *secureWebsocketConnector {
	return &secureWebsocketConnector{
		endpoints: []publicWebsocketEndpoint{
			{host: "data-stream.binance.vision", origin: "https://data-stream.binance.vision"},
			{host: "stream.binance.com", origin: "https://stream.binance.com"},
		},
		connect: connectPublicWebsocketEndpoint,
	}
}

// Connect opens one validated public-market WebSocket. The two code-owned
// endpoints expose the same public streams, and share one five-second setup
// budget so failover cannot extend the qualification recovery deadline.
func (connector *secureWebsocketConnector) Connect(ctx context.Context, target *url.URL) (websocketConnection, error) {
	if _, err := validateWebSocketTarget(target); err != nil {
		return nil, err
	}
	if connector == nil || len(connector.endpoints) == 0 || connector.connect == nil {
		return nil, policyError(exchangecontracts.OperationStream)
	}
	setupContext, cancel := context.WithTimeout(ctx, publicSetupDeadline)
	defer cancel()
	var lastErr error
	for index, endpoint := range connector.endpoints {
		candidate := *target
		candidate.Host = endpoint.host
		if _, err := validateWebSocketTargetForHost(endpoint.host, &candidate); err != nil {
			return nil, err
		}
		remaining := time.Until(deadlineOf(setupContext))
		if remaining <= 0 {
			break
		}
		attemptBudget := remaining / time.Duration(len(connector.endpoints)-index)
		attemptContext, attemptCancel := context.WithTimeout(setupContext, attemptBudget)
		connection, err := connector.connect(attemptContext, &candidate, endpoint)
		attemptCancel()
		if err == nil && connection != nil {
			return connection, nil
		}
		if connection != nil {
			_ = connection.Close()
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
		exchangecontracts.OperationStream, 0, 0, "websocket_endpoints_unavailable")
}

func connectPublicWebsocketEndpoint(
	ctx context.Context,
	target *url.URL,
	endpoint publicWebsocketEndpoint,
) (websocketConnection, error) {
	dialer := &publicDialer{host: endpoint.host, resolver: net.DefaultResolver,
		dialer: net.Dialer{Timeout: publicSetupDeadline, KeepAlive: 30 * time.Second}}
	raw, metadata, err := dialer.dialValidated(ctx, "tcp", net.JoinHostPort(endpoint.host, "443"))
	if err != nil {
		return nil, remapTransportError(exchangecontracts.OperationStream, err, metadata)
	}
	tlsConnection := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.host})
	tlsStarted := time.Now()
	if err = tlsConnection.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		metadata.TLSDuration, metadata.SetupStage = time.Since(tlsStarted), "tls"
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "tls_handshake_failure", metadata)
	}
	metadata.TLSDuration = time.Since(tlsStarted)
	configuration, err := websocket.NewConfig(target.String(), endpoint.origin)
	if err != nil {
		_ = tlsConnection.Close()
		return nil, policyError(exchangecontracts.OperationStream)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConnection.SetDeadline(deadline)
	}
	upgradeStarted := time.Now()
	connection, err := websocket.NewClient(configuration, tlsConnection)
	if err != nil {
		_ = tlsConnection.Close()
		metadata.UpgradeDuration, metadata.SetupStage = time.Since(upgradeStarted), "upgrade"
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "websocket_upgrade_failure", metadata)
	}
	_ = tlsConnection.SetDeadline(time.Time{})
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
