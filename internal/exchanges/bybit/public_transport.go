package bybit

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

func (client *PublicClient) get(
	ctx context.Context,
	path string,
	query url.Values,
	operation exchangecontracts.Operation,
	weight uint64,
) ([]byte, domain.EventTime, error) {
	if err := client.acquire(operation, weight); err != nil {
		return nil, domain.EventTime{}, err
	}
	target := *client.restOrigin
	target.Path = path
	if query != nil {
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, domain.EventTime{}, validationError(operation)
	}
	request.Header.Set("Accept", "application/json")
	if _, err = client.validateREST(request.Method, request.URL, request.Header); err != nil {
		return nil, domain.EventTime{}, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, domain.EventTime{}, exchangecontracts.NewError(
				exchangecontracts.ErrorCanceled, operation, 0)
		}
		return nil, domain.EventTime{}, exchangecontracts.NewDetailedError(
			exchangecontracts.ErrorTransient, operation, 0, 0, transportFailureCause(err))
	}
	defer response.Body.Close()
	if err = responseError(response, operation); err != nil {
		return nil, domain.EventTime{}, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, publicBodyLimit+1))
	if err != nil || len(body) == 0 || len(body) > publicBodyLimit {
		return nil, domain.EventTime{}, validationError(operation)
	}
	return body, client.clock.Now(), nil
}

func (client *PublicClient) acquire(operation exchangecontracts.Operation, weight uint64) error {
	decision, err := client.budget.TryAcquire(client.monotonic(), exchangecontracts.BudgetPublic, weight)
	client.telemetryMutex.Lock()
	client.budgetTelemetry = RateBudgetTelemetry{Remaining: decision.Remaining,
		RetryAfter: decision.RetryAfter, Granted: decision.Granted}
	client.telemetryMutex.Unlock()
	if err != nil {
		return err
	}
	if !decision.Granted {
		return exchangecontracts.NewError(exchangecontracts.ErrorRateLimit, operation, decision.RetryAfter)
	}
	return nil
}

func responseError(response *http.Response, operation exchangecontracts.Operation) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode == http.StatusTooManyRequests {
		retry := time.Duration(0)
		if raw := response.Header.Get("Retry-After"); raw != "" && !strings.ContainsAny(raw, "., ") {
			if seconds, err := strconv.ParseUint(raw, 10, 32); err == nil && seconds <= 300 {
				retry = time.Duration(seconds) * time.Second
			}
		}
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorRateLimit, operation,
			retry, response.StatusCode, "http_rate_limit")
	}
	if response.StatusCode >= 500 {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient, operation,
			0, response.StatusCode, "http_server_error")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorCapability, operation,
			0, response.StatusCode, "http_redirect")
	}
	return exchangecontracts.NewDetailedError(exchangecontracts.ErrorValidation, operation,
		0, response.StatusCode, "http_client_error")
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
