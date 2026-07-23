package binance

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const unknownContentLength int64 = -1

func (client *PublicClient) get(
	ctx context.Context,
	path string,
	query url.Values,
	operation exchangecontracts.Operation,
	weight uint64,
) ([]byte, domain.EventTime, error) {
	started := time.Now()
	if err := client.acquirePublicBudget(operation, weight); err != nil {
		return nil, domain.EventTime{}, err
	}
	target := *client.restOrigin
	target.Path, target.RawQuery = path, query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, domain.EventTime{}, exchangecontracts.NewDetailedError(
			exchangecontracts.ErrorValidation, operation, 0, 0, "request_build_failed")
	}
	request.Header.Set("Accept", "application/json")
	if _, err = client.validateREST(request.Method, request.URL, request.Header); err != nil {
		return nil, domain.EventTime{}, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		completed := time.Now()
		if ctx.Err() != nil {
			return nil, domain.EventTime{}, exchangecontracts.NewDetailedError(exchangecontracts.ErrorCanceled,
				operation, 0, 0, "context_canceled", requestFailureMetadata(started, completed, completed, nil, 0))
		}
		return nil, domain.EventTime{}, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			operation, 0, 0, transportFailureCause(err),
			requestFailureMetadata(started, completed, completed, nil, 0))
	}
	headersAt := time.Now()
	if err = responseError(response, operation,
		requestFailureMetadata(started, headersAt, headersAt, response, 0)); err != nil {
		_ = response.Body.Close()
		return nil, domain.EventTime{}, err
	}
	body, err := readSuccessfulResponse(response, operation, started, headersAt)
	if err != nil {
		return nil, domain.EventTime{}, err
	}
	return body, client.clock.Now(), nil
}

func (client *PublicClient) acquirePublicBudget(operation exchangecontracts.Operation, weight uint64) error {
	decision, err := client.budget.TryAcquire(
		client.monotonic(), exchangecontracts.BudgetPublic, weight)
	if err != nil {
		return exchangecontracts.NewDetailedError(
			exchangecontracts.KindOf(err), operation, 0, 0, "rate_budget_failure")
	}
	if !decision.Granted {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorRateLimit,
			operation, decision.RetryAfter, 0, "rate_budget_exhausted")
	}
	return nil
}

func readSuccessfulResponse(
	response *http.Response,
	operation exchangecontracts.Operation,
	started, headersAt time.Time,
) ([]byte, error) {
	bodyStarted := time.Now()
	body, err := io.ReadAll(io.LimitReader(response.Body, publicBodyLimit+1))
	completed := time.Now()
	closeErr := response.Body.Close()
	metadata := requestFailureMetadata(started, headersAt, completed, response, len(body))
	metadata.ResponseBodyDuration = completed.Sub(bodyStarted)
	if err != nil {
		cause := "response_body_read_failed"
		var network net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
			cause = "response_body_timeout"
		}
		return nil, exchangecontracts.NewDetailedError(
			exchangecontracts.ErrorTransient, operation, 0, response.StatusCode, cause, metadata)
	}
	if closeErr != nil {
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			operation, 0, response.StatusCode, "response_body_close_failed", metadata)
	}
	if len(body) == 0 {
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			operation, 0, response.StatusCode, "response_body_empty", metadata)
	}
	if len(body) > publicBodyLimit {
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorValidation,
			operation, 0, response.StatusCode, "response_body_too_large", metadata)
	}
	return body, nil
}

func requestFailureMetadata(
	started, headersAt, completed time.Time,
	response *http.Response,
	responseBytes int,
) exchangecontracts.FailureMetadata {
	metadata := exchangecontracts.FailureMetadata{RequestDuration: completed.Sub(started),
		ResponseHeaderDuration: headersAt.Sub(started), ResponseBytes: uint64(responseBytes),
		BodyLimitBytes: publicBodyLimit}
	if response != nil && response.ContentLength != unknownContentLength {
		metadata.ContentLengthKnown = true
		if response.ContentLength > 0 {
			metadata.ContentLengthBytes = uint64(response.ContentLength)
		}
	}
	return metadata
}

func responseError(response *http.Response, operation exchangecontracts.Operation,
	metadata ...exchangecontracts.FailureMetadata) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusTeapot {
		retry := time.Duration(0)
		if raw := response.Header.Get("Retry-After"); raw != "" && !strings.ContainsAny(raw, "., ") {
			if seconds, err := strconv.ParseUint(raw, 10, 32); err == nil && seconds <= 300 {
				retry = time.Duration(seconds) * time.Second
			}
		}
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorRateLimit, operation, retry,
			response.StatusCode, "http_rate_limit", metadata...)
	}
	if response.StatusCode >= 500 {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient, operation, 0,
			response.StatusCode, "http_server_error", metadata...)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorCapability, operation, 0,
			response.StatusCode, "http_redirect", metadata...)
	}
	return exchangecontracts.NewDetailedError(exchangecontracts.ErrorValidation, operation, 0,
		response.StatusCode, "http_client_error", metadata...)
}

func transportFailureCause(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns_failure"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "network_timeout"
	}
	var operation *net.OpError
	if errors.As(err, &operation) {
		switch operation.Op {
		case "dial":
			return "tcp_connect_failure"
		case "read", "write":
			return "network_io_failure"
		}
	}
	return "transport_failure"
}

func approvedInstrument(instrument domain.Instrument) bool {
	return instrument.Product == domain.ProductSpot &&
		((instrument.Quote == "USDT" && (instrument.Base == "BTC" || instrument.Base == "ETH")) ||
			(instrument.Base == "ETH" && instrument.Quote == "BTC"))
}

func validSnapshotDepth(depth uint32) bool {
	return depth == 100 || depth == 500 || depth == 1000 || depth == 5000
}

func snapshotWeight(depth uint32) uint64 {
	switch depth {
	case 100:
		return 5
	case 500:
		return 25
	case 1000:
		return 50
	default:
		return 250
	}
}
