package binance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type privateSubscriptionResponse struct {
	ID     string `json:"id"`
	Status int    `json:"status"`
	Result *struct {
		SubscriptionID *uint64 `json:"subscriptionId"`
	} `json:"result"`
	Error *struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"error"`
	RateLimits []struct {
		Type       string `json:"rateLimitType"`
		Interval   string `json:"interval"`
		IntervalNo uint64 `json:"intervalNum"`
		Limit      uint64 `json:"limit"`
		Count      uint64 `json:"count"`
	} `json:"rateLimits"`
}

func (source *BinancePrivateEventSource) subscribe(
	ctx context.Context,
	connection privateStreamConnection,
) error {
	if err := source.client.ensureClock(ctx); err != nil {
		return fmt.Errorf("%w: subscription_clock", ErrSandboxPrivateEvent)
	}
	request, requestID, evidence, err := source.subscriptionRequest()
	if err != nil {
		return fmt.Errorf("%w: subscription_request", ErrSandboxPrivateEvent)
	}
	if err = exchangecontracts.ValidateAuthenticatedRequestEvidence(evidence); err != nil {
		return fmt.Errorf("%w: subscription_evidence", ErrSandboxPrivateEvent)
	}
	if err = source.client.evidence.RecordAuthenticatedRequest(ctx, evidence); err != nil {
		return fmt.Errorf(
			"%w: subscription_evidence_persistence",
			ErrSandboxPrivateEvent,
		)
	}
	if err = connection.Send(ctx, request); err != nil {
		return fmt.Errorf("%w: subscription_send", ErrSandboxPrivateEvent)
	}
	body, err := connection.Receive(ctx)
	if err != nil {
		return fmt.Errorf("%w: subscription_receive", ErrSandboxPrivateEvent)
	}
	return validateSubscriptionResponse(body, requestID)
}

func validateSubscriptionResponse(body []byte, requestID string) error {
	var response privateSubscriptionResponse
	if strictDecode(body, &response) != nil {
		return fmt.Errorf(
			"%w: subscription_response_shape",
			ErrSandboxPrivateEvent,
		)
	}
	if response.ID != requestID {
		return fmt.Errorf(
			"%w: subscription_response_id",
			ErrSandboxPrivateEvent,
		)
	}
	if response.Status != http.StatusOK {
		return subscriptionStatusError(response)
	}
	if response.Error != nil || response.Result == nil ||
		response.Result.SubscriptionID == nil {
		return fmt.Errorf(
			"%w: subscription_response_result",
			ErrSandboxPrivateEvent,
		)
	}
	return nil
}

func subscriptionStatusError(response privateSubscriptionResponse) error {
	code := 0
	if response.Error != nil {
		code = response.Error.Code
	}
	return fmt.Errorf(
		"%w: subscription_status_%d_code_%d_%s",
		ErrSandboxPrivateEvent,
		response.Status,
		code,
		subscriptionResponseReason(response),
	)
}

func subscriptionResponseReason(response privateSubscriptionResponse) string {
	if response.Error == nil {
		return "none"
	}
	message := strings.ToLower(response.Error.Msg)
	switch {
	case strings.Contains(message, "ahead"):
		return "timestamp_ahead"
	case strings.Contains(message, "outside"):
		return "timestamp_outside_window"
	default:
		return "other"
	}
}

func (source *BinancePrivateEventSource) subscriptionRequest() (
	[]byte,
	string,
	exchangecontracts.AuthenticatedRequestEvidence,
	error,
) {
	health := source.client.clock.Health()
	timestamp := source.client.now().UTC().
		Add(health.Offset).
		Add(-health.Uncertainty).
		Add(-sandboxWebSocketTimeGuard).
		UnixMilli()
	encoded, requestID, canonical, err := source.subscriptionPayload(timestamp)
	if err != nil {
		return nil, "", exchangecontracts.AuthenticatedRequestEvidence{},
			ErrSandboxPrivateEvent
	}
	requestHash := sha256.Sum256([]byte(
		"WS\n" + sandboxWebSocketHost + "\n" +
			sandboxSubscriptionMethod + "\n" + canonical,
	))
	evidence := exchangecontracts.AuthenticatedRequestEvidence{
		Exchange: "binance", Host: sandboxWebSocketHost,
		Method: "WS", Path: sandboxWebSocketEvidence,
		FieldNames: []string{"recvWindow", "timestamp"},
		Enumerated: map[string]string{}, RequestHash: requestHash,
		ConfigurationID: source.client.configurationID,
		RecordedAt:      source.client.now().UTC(),
	}
	return encoded, requestID, evidence, nil
}

func (source *BinancePrivateEventSource) subscriptionPayload(
	timestamp int64,
) ([]byte, string, string, error) {
	params := url.Values{
		"apiKey":     {source.client.apiKey},
		"recvWindow": {sandboxReceiveWindow},
		"timestamp":  {strconv.FormatInt(timestamp, 10)},
	}
	canonical := params.Encode()
	signature, err := hmacSHA256Hex(source.client.apiSecret, canonical)
	if err != nil {
		return nil, "", "", ErrSandboxPrivateEvent
	}
	requestID := fmt.Sprintf("axiom-%d", timestamp)
	request := newPrivateSubscriptionRequest(
		requestID,
		source.client.apiKey,
		timestamp,
		signature,
	)
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, "", "", ErrSandboxPrivateEvent
	}
	return encoded, requestID, canonical, nil
}

func newPrivateSubscriptionRequest(
	requestID string,
	apiKey string,
	timestamp int64,
	signature string,
) any {
	request := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			APIKey     string `json:"apiKey"`
			RecvWindow uint64 `json:"recvWindow"`
			Timestamp  int64  `json:"timestamp"`
			Signature  string `json:"signature"`
		} `json:"params"`
	}{
		ID:     requestID,
		Method: sandboxSubscriptionMethod,
	}
	request.Params.APIKey = apiKey
	request.Params.RecvWindow = 5000
	request.Params.Timestamp = timestamp
	request.Params.Signature = signature
	return request
}
