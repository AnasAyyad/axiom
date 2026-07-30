package bybit

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

type demoWebSocketPrivateConnection struct {
	connection *websocket.Conn
	closeOnce  sync.Once
}

const (
	demoPrivateHeartbeatInterval = 20 * time.Second
	demoPrivateHeartbeatRequest  = `{"req_id":"` +
		demoPrivateHeartbeatID + `","op":"ping"}`
)

// Connect opens the fixed Demo private WebSocket through its CONNECT-only
// proxy.
func (connectDemoPrivateConnector) Connect(
	ctx context.Context,
) (demoPrivateConnection, error) {
	raw, err := dialDemoPrivateProxy(ctx)
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
		ServerName: demoPrivateWebSocketHost,
	})
	if err = tlsConnection.HandshakeContext(ctx); err != nil {
		return nil, ErrDemoPrivateEvent
	}
	config, err := websocket.NewConfig(
		demoPrivateWebSocketURL, demoPrivateWebSocketOrigin,
	)
	if err != nil {
		return nil, ErrDemoPrivateEvent
	}
	connection, err := websocket.NewClient(config, tlsConnection)
	if err != nil {
		return nil, ErrDemoPrivateEvent
	}
	success = true
	return &demoWebSocketPrivateConnection{connection: connection}, nil
}

func dialDemoPrivateProxy(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	raw, err := dialer.DialContext(
		ctx,
		"tcp",
		demoPrivateWebSocketProxy,
	)
	if err != nil {
		return nil, ErrDemoPrivateEvent
	}
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	target := net.JoinHostPort(demoPrivateWebSocketHost, "443")
	request := "CONNECT " + target + " HTTP/1.1\r\nHost: " +
		target + "\r\nProxy-Connection: Keep-Alive\r\n\r\n"
	if _, err = io.WriteString(raw, request); err != nil {
		return nil, ErrDemoPrivateEvent
	}
	response, err := http.ReadResponse(
		bufio.NewReader(raw),
		&http.Request{Method: http.MethodConnect},
	)
	if err != nil {
		return nil, ErrDemoPrivateEvent
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrDemoPrivateEvent
	}
	success = true
	return raw, nil
}

// Send writes one fixed-protocol Demo private-stream request.
func (connection *demoWebSocketPrivateConnection) Send(
	ctx context.Context,
	body []byte,
) error {
	payload, err := demoPrivateTextPayload(body)
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
		return ErrDemoPrivateEvent
	case err := <-result:
		if err != nil {
			return ErrDemoPrivateEvent
		}
		return nil
	}
}

func demoPrivateTextPayload(body []byte) (string, error) {
	if !json.Valid(body) {
		return "", ErrDemoPrivateEvent
	}
	return string(body), nil
}

// Receive reads one bounded Demo private-stream response.
func (connection *demoWebSocketPrivateConnection) Receive(
	ctx context.Context,
) ([]byte, error) {
	type receiveResult struct {
		body []byte
		err  error
	}
	ticker := time.NewTicker(demoPrivateHeartbeatInterval)
	defer ticker.Stop()
	result := make(chan receiveResult, 1)
	startReceive := func() {
		go func() {
			var body []byte
			err := websocket.Message.Receive(connection.connection, &body)
			result <- receiveResult{body: body, err: err}
		}()
	}
	startReceive()
	for {
		select {
		case <-ctx.Done():
			_ = connection.Close()
			return nil, ErrDemoPrivateEvent
		case <-ticker.C:
			if err := connection.Send(
				ctx,
				[]byte(demoPrivateHeartbeatRequest),
			); err != nil {
				_ = connection.Close()
				return nil, err
			}
		case received := <-result:
			if received.err != nil || len(received.body) == 0 ||
				len(received.body) > authenticatedResponseLimit {
				return nil, ErrDemoPrivateEvent
			}
			if validDemoPrivateHeartbeat(received.body) {
				startReceive()
				continue
			}
			return received.body, nil
		}
	}
}

func validDemoPrivateHeartbeat(body []byte) bool {
	var pong struct {
		RequestID    string   `json:"req_id"`
		Operation    string   `json:"op"`
		Arguments    []string `json:"args"`
		ConnectionID string   `json:"conn_id"`
	}
	if strictDecode(body, &pong) == nil &&
		pong.RequestID == demoPrivateHeartbeatID &&
		pong.Operation == "pong" &&
		len(pong.Arguments) == 1 &&
		pong.Arguments[0] != "" &&
		pong.ConnectionID != "" {
		return true
	}
	var legacy struct {
		Success      bool   `json:"success"`
		Message      string `json:"ret_msg"`
		ConnectionID string `json:"conn_id"`
		RequestID    string `json:"req_id"`
		Operation    string `json:"op"`
	}
	return strictDecode(body, &legacy) == nil &&
		legacy.Success &&
		legacy.Message == "pong" &&
		legacy.ConnectionID != "" &&
		legacy.RequestID == demoPrivateHeartbeatID &&
		legacy.Operation == "ping"
}

// Close idempotently closes the Demo private WebSocket.
func (connection *demoWebSocketPrivateConnection) Close() error {
	var err error
	connection.closeOnce.Do(func() {
		err = connection.connection.Close()
	})
	return err
}
