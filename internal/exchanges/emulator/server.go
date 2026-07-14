package emulator

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// Server runs one deterministic local conformance scenario.
type Server struct {
	mutex        sync.Mutex
	scenario     Scenario
	http         *httptest.Server
	restIndex    int
	sessionIndex map[string]int
	connections  uint64
	transcript   []TranscriptEntry
}

// NewServer validates and starts a loopback-only REST/WebSocket emulator.
func NewServer(scenario Scenario) (*Server, error) {
	if err := scenario.Validate(); err != nil {
		return nil, err
	}
	server := &Server{scenario: scenario, sessionIndex: make(map[string]int)}
	handler := http.HandlerFunc(server.handle)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, scenarioError("listen")
	}
	server.http = httptest.NewUnstartedServer(handler)
	server.http.Listener = listener
	server.http.Start()
	return server, nil
}

// URL returns the test-only loopback HTTP origin.
func (server *Server) URL() string { return server.http.URL }

// WebSocketURL returns the test-only loopback WebSocket origin.
func (server *Server) WebSocketURL() string {
	return "ws" + strings.TrimPrefix(server.http.URL, "http")
}

// Close stops the local server.
func (server *Server) Close() {
	if server != nil && server.http != nil {
		server.http.Close()
	}
}

// Transcript returns a defensive ordered copy.
func (server *Server) Transcript() []TranscriptEntry {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	result := make([]TranscriptEntry, len(server.transcript))
	copy(result, server.transcript)
	return result
}

// TranscriptHash returns the canonical ordered interaction hash.
func (server *Server) TranscriptHash() (string, error) {
	return transcriptHash(server.Transcript())
}

// Complete reports whether every scripted REST response and stream session ran.
func (server *Server) Complete() bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if server.restIndex != len(server.scenario.REST) {
		return false
	}
	counts := make(map[string]int)
	for _, session := range server.scenario.StreamSessions {
		counts[session.Path]++
	}
	for path, expected := range counts {
		if server.sessionIndex[path] != expected {
			return false
		}
	}
	return true
}

func (server *Server) handle(writer http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/ws/") {
		websocket.Handler(server.handleStream).ServeHTTP(writer, request)
		return
	}
	server.handleREST(writer, request)
}

func (server *Server) handleREST(writer http.ResponseWriter, request *http.Request) {
	step, ok := server.nextREST(request)
	if !ok {
		http.Error(writer, "scenario_mismatch", http.StatusConflict)
		return
	}
	if !wait(request.Context(), step.Delay) {
		return
	}
	for _, header := range step.Headers {
		writer.Header().Set(header.Name, header.Value)
	}
	writer.WriteHeader(step.Status)
	_, _ = writer.Write(step.Body)
	server.record("rest", "response", step.Path, 0, step.Status, step.Body)
}

func (server *Server) nextREST(request *http.Request) (RESTStep, bool) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if server.restIndex >= len(server.scenario.REST) {
		return RESTStep{}, false
	}
	step := server.scenario.REST[server.restIndex]
	if request.Method != step.Method || request.URL.Path != step.Path || request.URL.RawQuery != step.RawQuery {
		return RESTStep{}, false
	}
	server.restIndex++
	server.appendLocked("rest", "request", step.Path, 0, 0, nil)
	return step, true
}

func (server *Server) handleStream(connection *websocket.Conn) {
	path := connection.Request().URL.Path
	session, connectionID, ok := server.nextSession(path)
	if !ok {
		_ = connection.Close()
		return
	}
	server.record("websocket", "connect", path, connectionID, 0, nil)
	for _, frame := range session.Frames {
		if !wait(connection.Request().Context(), frame.Delay) || frame.Close {
			_ = connection.Close()
			server.record("websocket", "close", path, connectionID, 0, nil)
			return
		}
		if err := websocket.Message.Send(connection, frame.Body); err != nil {
			return
		}
		server.record("websocket", "frame", path, connectionID, 0, frame.Body)
	}
	_ = connection.Close()
	server.record("websocket", "close", path, connectionID, 0, nil)
}

func (server *Server) nextSession(path string) (StreamSession, uint64, bool) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	match := server.sessionIndex[path]
	seen := 0
	for _, session := range server.scenario.StreamSessions {
		if session.Path != path {
			continue
		}
		if seen == match {
			server.sessionIndex[path]++
			server.connections++
			return session, server.connections, true
		}
		seen++
	}
	return StreamSession{}, 0, false
}

func (server *Server) record(transport, direction, path string, connection uint64, status int, body []byte) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.appendLocked(transport, direction, path, connection, status, body)
}

func (server *Server) appendLocked(transport, direction, path string, connection uint64, status int, body []byte) {
	server.transcript = append(server.transcript, TranscriptEntry{
		Ordinal: uint64(len(server.transcript) + 1), Transport: transport, Direction: direction,
		Path: path, Connection: connection, Status: status, PayloadHash: bodyHash(body),
	})
}

func wait(ctx context.Context, duration time.Duration) bool {
	if duration == 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
