package binance

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/websocket"
)

// Connect opens the fixed Testnet private WebSocket through its CONNECT-only
// proxy.
func (connectPrivateStreamConnector) Connect(
	ctx context.Context,
) (privateStreamConnection, error) {
	raw, err := dialPrivateStreamProxy(ctx)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	tlsConnection := tls.Client(raw, &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: sandboxWebSocketHost,
	})
	if err = tlsConnection.HandshakeContext(ctx); err != nil {
		return nil, ErrSandboxPrivateEvent
	}
	config, err := websocket.NewConfig(
		sandboxWebSocketURL, sandboxWebSocketOrigin,
	)
	if err != nil {
		return nil, ErrSandboxPrivateEvent
	}
	connection, err := websocket.NewClient(config, tlsConnection)
	if err != nil {
		return nil, ErrSandboxPrivateEvent
	}
	success = true
	return &webSocketPrivateConnection{connection: connection}, nil
}

func dialPrivateStreamProxy(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", sandboxWebSocketProxy)
	if err != nil {
		return nil, ErrSandboxPrivateEvent
	}
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	target := net.JoinHostPort(sandboxWebSocketHost, "443")
	request := "CONNECT " + target + " HTTP/1.1\r\nHost: " +
		target + "\r\nProxy-Connection: Keep-Alive\r\n\r\n"
	if _, err = io.WriteString(raw, request); err != nil {
		return nil, ErrSandboxPrivateEvent
	}
	response, err := http.ReadResponse(
		bufio.NewReader(raw),
		&http.Request{Method: http.MethodConnect},
	)
	if err != nil {
		return nil, ErrSandboxPrivateEvent
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrSandboxPrivateEvent
	}
	success = true
	return raw, nil
}

// Send writes one fixed-protocol private-stream request.
func (connection *webSocketPrivateConnection) Send(
	ctx context.Context,
	body []byte,
) error {
	payload, err := privateTextPayload(body)
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		result <- websocket.Message.Send(connection.connection, payload)
	}()
	select {
	case <-ctx.Done():
		_ = connection.Close()
		return ErrSandboxPrivateEvent
	case err := <-result:
		if err != nil {
			return ErrSandboxPrivateEvent
		}
		return nil
	}
}

func privateTextPayload(body []byte) (string, error) {
	if !json.Valid(body) {
		return "", ErrSandboxPrivateEvent
	}
	return string(body), nil
}

// Receive reads one private-stream response under caller cancellation.
func (connection *webSocketPrivateConnection) Receive(
	ctx context.Context,
) ([]byte, error) {
	type receiveResult struct {
		body []byte
		err  error
	}
	result := make(chan receiveResult, 1)
	go func() {
		var body []byte
		err := websocket.Message.Receive(connection.connection, &body)
		result <- receiveResult{body: body, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = connection.Close()
		return nil, ErrSandboxPrivateEvent
	case received := <-result:
		if received.err != nil {
			return nil, ErrSandboxPrivateEvent
		}
		return received.body, nil
	}
}

// Close idempotently closes the private WebSocket.
func (connection *webSocketPrivateConnection) Close() error {
	var err error
	connection.closeOnce.Do(func() {
		err = connection.connection.Close()
	})
	return err
}
