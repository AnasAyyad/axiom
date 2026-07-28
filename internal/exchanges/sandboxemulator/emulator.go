// Package sandboxemulator provides deterministic authenticated exchange
// emulation without changing the qualified credential-free public collectors.
package sandboxemulator

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"axiom/internal/sandbox"
)

// Fault is one deterministic request outcome injected by the emulator.
type Fault string

// Emulator faults are a closed test-only sequence.
const (
	FaultNone    Fault = ""
	FaultTimeout Fault = "timeout"
	FaultReject  Fault = "reject"
)

// Config fixes one emulator to an exchange credential pair and fault sequence.
type Config struct {
	Exchange  sandbox.Exchange
	APIKey    string
	APISecret string
	Faults    []Fault
}

// Capture is deliberately redacted. It cannot contain headers, signatures, or
// private field values.
type Capture struct {
	Exchange    sandbox.Exchange
	Host        string
	Method      string
	Path        string
	FieldNames  []string
	RequestHash string
}

// Emulator validates signed requests and retains only redacted captures.
type Emulator struct {
	mutex      sync.Mutex
	config     Config
	captures   []Capture
	requests   int
	nativeByID map[string]string
}

// New constructs a deterministic authenticated emulator with no network I/O.
func New(config Config) (*Emulator, error) {
	if (config.Exchange != sandbox.ExchangeBinance && config.Exchange != sandbox.ExchangeBybit) ||
		config.APIKey == "" || config.APISecret == "" {
		return nil, errors.New("sandbox_emulator_configuration_invalid")
	}
	return &Emulator{config: config, nativeByID: map[string]string{}}, nil
}

// Do lets authenticated clients use the emulator as their closed test
// connector. It never opens a socket.
func (emulator *Emulator) Do(request *http.Request) (*http.Response, error) {
	emulator.mutex.Lock()
	defer emulator.mutex.Unlock()
	emulator.requests++
	fault := FaultNone
	if emulator.requests <= len(emulator.config.Faults) {
		fault = emulator.config.Faults[emulator.requests-1]
	}
	if fault == FaultTimeout {
		return nil, context.DeadlineExceeded
	}
	fields, requestHash, clientID, err := emulator.validate(request)
	if err != nil {
		return response(http.StatusForbidden, `{"code":"policy_rejected"}`), nil
	}
	emulator.captures = append(emulator.captures, Capture{
		Exchange: emulator.config.Exchange, Host: request.URL.Hostname(),
		Method: request.Method, Path: request.URL.Path, FieldNames: fields,
		RequestHash: requestHash,
	})
	if fault == FaultReject {
		return response(http.StatusTooManyRequests, `{"code":"rate_limited"}`), nil
	}
	if clientID != "" {
		if _, exists := emulator.nativeByID[clientID]; !exists {
			hash := sha256.Sum256([]byte(string(emulator.config.Exchange) + "|" + clientID))
			emulator.nativeByID[clientID] = hex.EncodeToString(hash[:16])
		}
	}
	switch {
	case emulator.config.Exchange == sandbox.ExchangeBinance && request.URL.Path == "/api/v3/account":
		return response(http.StatusOK,
			`{"canTrade":true,"accountType":"SPOT","permissions":["SPOT"],"uid":12345}`), nil
	case emulator.config.Exchange == sandbox.ExchangeBybit && request.URL.Path == "/v5/user/query-api":
		return response(http.StatusOK,
			`{"result":{"id":"demo-account","readOnly":0,"permissions":{"SpotTrade":["Trade"]}}}`), nil
	default:
		return response(http.StatusOK, `{"accepted":true}`), nil
	}
}

func (emulator *Emulator) validate(request *http.Request) ([]string, string, string, error) {
	if request == nil || request.URL == nil || request.URL.User != nil || request.URL.Fragment != "" ||
		request.URL.Scheme != "https" {
		return nil, "", "", errors.New("invalid_request")
	}
	switch emulator.config.Exchange {
	case sandbox.ExchangeBinance:
		return emulator.validateBinance(request)
	case sandbox.ExchangeBybit:
		return emulator.validateBybit(request)
	default:
		return nil, "", "", errors.New("invalid_exchange")
	}
}

func (emulator *Emulator) validateBinance(request *http.Request) ([]string, string, string, error) {
	if request.URL.Host != "testnet.binance.vision" ||
		request.Header.Get("X-MBX-APIKEY") != emulator.config.APIKey ||
		!binanceRoute(request.Method, request.URL.Path) {
		return nil, "", "", errors.New("binance_policy")
	}
	values := request.URL.Query()
	signature := values.Get("signature")
	values.Del("signature")
	if signature == "" || !validHMAC(emulator.config.APISecret, values.Encode(), signature) {
		return nil, "", "", errors.New("binance_signature")
	}
	fields := valueNames(values)
	requestHash := hashRequest(request.Method, request.URL.Path, values.Encode())
	return fields, requestHash, firstNonempty(values.Get("newClientOrderId"), values.Get("origClientOrderId")), nil
}

func (emulator *Emulator) validateBybit(request *http.Request) ([]string, string, string, error) {
	if request.URL.Host != "api-demo.bybit.com" ||
		request.Header.Get("X-BAPI-API-KEY") != emulator.config.APIKey ||
		!bybitRoute(request.Method, request.URL.Path) {
		return nil, "", "", errors.New("bybit_policy")
	}
	timestamp := request.Header.Get("X-BAPI-TIMESTAMP")
	window := request.Header.Get("X-BAPI-RECV-WINDOW")
	signature := request.Header.Get("X-BAPI-SIGN")
	if timestamp == "" || window != "5000" || signature == "" {
		return nil, "", "", errors.New("bybit_headers")
	}
	var payload string
	var values map[string]string
	if request.Method == http.MethodGet {
		payload = request.URL.Query().Encode()
		values = make(map[string]string)
		for name := range request.URL.Query() {
			values[name] = request.URL.Query().Get(name)
		}
	} else {
		body, err := io.ReadAll(io.LimitReader(request.Body, 64*1024))
		if err != nil {
			return nil, "", "", err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		payload = string(body)
		if err := json.Unmarshal(body, &values); err != nil {
			return nil, "", "", err
		}
	}
	signingInput := timestamp + emulator.config.APIKey + window + payload
	if !validHMAC(emulator.config.APISecret, signingInput, signature) {
		return nil, "", "", errors.New("bybit_signature")
	}
	fields := make([]string, 0, len(values)+2)
	for name := range values {
		fields = append(fields, name)
	}
	fields = append(fields, "recvWindow", "timestamp")
	sort.Strings(fields)
	return fields, hashRequest(request.Method, request.URL.Path, payload), values["orderLinkId"], nil
}

func validHMAC(secret, payload, encoded string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.ToLower(encoded)))
}

func binanceRoute(method, path string) bool {
	allowed := map[string]map[string]bool{
		"/api/v3/account":    {http.MethodGet: true},
		"/api/v3/openOrders": {http.MethodGet: true},
		"/api/v3/allOrders":  {http.MethodGet: true},
		"/api/v3/myTrades":   {http.MethodGet: true},
		"/api/v3/order/test": {http.MethodPost: true},
		"/api/v3/order":      {http.MethodGet: true, http.MethodPost: true, http.MethodDelete: true},
	}
	return allowed[path][method]
}

func bybitRoute(method, path string) bool {
	allowed := map[string]map[string]bool{
		"/v5/user/query-api":         {http.MethodGet: true},
		"/v5/account/wallet-balance": {http.MethodGet: true},
		"/v5/order/create":           {http.MethodPost: true},
		"/v5/order/cancel":           {http.MethodPost: true},
		"/v5/order/realtime":         {http.MethodGet: true},
		"/v5/order/history":          {http.MethodGet: true},
		"/v5/execution/list":         {http.MethodGet: true},
	}
	return allowed[path][method]
}

func valueNames(values url.Values) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func hashRequest(method, path, payload string) string {
	hash := sha256.Sum256([]byte(method + "\n" + path + "\n" + payload))
	return hex.EncodeToString(hash[:])
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

// Captures returns defensive copies of all redacted accepted request shapes.
func (emulator *Emulator) Captures() []Capture {
	emulator.mutex.Lock()
	defer emulator.mutex.Unlock()
	result := make([]Capture, len(emulator.captures))
	for index, capture := range emulator.captures {
		result[index] = capture
		result[index].FieldNames = append([]string(nil), capture.FieldNames...)
	}
	return result
}

// NativeOrderCount reports unique deterministic client-order identities.
func (emulator *Emulator) NativeOrderCount() int {
	emulator.mutex.Lock()
	defer emulator.mutex.Unlock()
	return len(emulator.nativeByID)
}
