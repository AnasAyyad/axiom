package emulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const maximumPayloadBytes = 2 * 1024 * 1024

// Header is one deterministic response header.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RESTStep is one exact expected request and scripted response.
type RESTStep struct {
	Method   string        `json:"method"`
	Path     string        `json:"path"`
	RawQuery string        `json:"raw_query"`
	Status   int           `json:"status"`
	Headers  []Header      `json:"headers"`
	Body     []byte        `json:"body"`
	Delay    time.Duration `json:"delay"`
}

// StreamFrame is one text frame or deterministic connection close.
type StreamFrame struct {
	Body  []byte        `json:"body"`
	Delay time.Duration `json:"delay"`
	Close bool          `json:"close"`
}

// StreamSession is one expected WebSocket connection generation.
type StreamSession struct {
	Path   string        `json:"path"`
	Frames []StreamFrame `json:"frames"`
}

// Scenario is an immutable deterministic emulator script.
type Scenario struct {
	Name           string          `json:"name"`
	Seed           string          `json:"seed"`
	REST           []RESTStep      `json:"rest"`
	StreamSessions []StreamSession `json:"stream_sessions"`
}

// Validate rejects ambiguous, unsafe, or unbounded scenario scripts.
func (scenario Scenario) Validate() error {
	if scenario.Name == "" || scenario.Seed == "" {
		return scenarioError("identity")
	}
	for _, step := range scenario.REST {
		if step.Method != http.MethodGet || !validPath(step.Path) || step.Status < 100 ||
			step.Status > 599 || step.Delay < 0 || len(step.Body) > maximumPayloadBytes ||
			!validHeaders(step.Headers) {
			return scenarioError("rest")
		}
	}
	for _, session := range scenario.StreamSessions {
		if !validPath(session.Path) || len(session.Frames) == 0 {
			return scenarioError("stream")
		}
		for _, frame := range session.Frames {
			if frame.Delay < 0 || len(frame.Body) > maximumPayloadBytes || (frame.Close && len(frame.Body) != 0) {
				return scenarioError("frame")
			}
		}
	}
	return nil
}

// Hash returns the canonical SHA-256 identity of a valid scenario.
func (scenario Scenario) Hash() (string, error) {
	if err := scenario.Validate(); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(scenario)
	if err != nil {
		return "", scenarioError("encoding")
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func validPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.Contains(path, "..") && !strings.ContainsAny(path, "?#")
}

func validHeaders(headers []Header) bool {
	previous := ""
	for _, header := range headers {
		canonical := http.CanonicalHeaderKey(header.Name)
		if canonical == "" || canonical <= previous || header.Value == "" ||
			strings.EqualFold(canonical, "Authorization") || strings.EqualFold(canonical, "Cookie") {
			return false
		}
		previous = canonical
	}
	return true
}

type scriptError struct{ scope string }

// Error returns a stable emulator failure without request or payload details.
func (failure *scriptError) Error() string { return "emulator_scenario_rejected:" + failure.scope }

func scenarioError(scope string) error { return &scriptError{scope: scope} }
