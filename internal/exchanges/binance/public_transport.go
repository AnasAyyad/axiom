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

func (client *PublicClient) get(
	ctx context.Context,
	path string,
	query url.Values,
	operation exchangecontracts.Operation,
	weight uint64,
) ([]byte, domain.EventTime, error) {
	now := client.monotonic()
	decision, err := client.budget.TryAcquire(now, exchangecontracts.BudgetPublic, weight)
	if err != nil {
		return nil, domain.EventTime{}, err
	}
	if !decision.Granted {
		return nil, domain.EventTime{}, exchangecontracts.NewError(exchangecontracts.ErrorRateLimit, operation, decision.RetryAfter)
	}
	target := *client.restOrigin
	target.Path, target.RawQuery = path, query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, domain.EventTime{}, exchangecontracts.NewError(exchangecontracts.ErrorValidation, operation, 0)
	}
	request.Header.Set("Accept", "application/json")
	if _, err = client.validateREST(request.Method, request.URL, request.Header); err != nil {
		return nil, domain.EventTime{}, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, domain.EventTime{}, exchangecontracts.NewError(exchangecontracts.ErrorCanceled, operation, 0)
		}
		return nil, domain.EventTime{}, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			operation, 0, 0, transportFailureCause(err))
	}
	defer response.Body.Close()
	if err = responseError(response, operation); err != nil {
		return nil, domain.EventTime{}, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, publicBodyLimit+1))
	if err != nil || len(body) == 0 || len(body) > publicBodyLimit {
		return nil, domain.EventTime{}, exchangecontracts.NewError(exchangecontracts.ErrorValidation, operation, 0)
	}
	return body, client.clock.Now(), nil
}

func responseError(response *http.Response, operation exchangecontracts.Operation) error {
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
			response.StatusCode, "http_rate_limit")
	}
	if response.StatusCode >= 500 {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient, operation, 0,
			response.StatusCode, "http_server_error")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorCapability, operation, 0,
			response.StatusCode, "http_redirect")
	}
	return exchangecontracts.NewDetailedError(exchangecontracts.ErrorValidation, operation, 0,
		response.StatusCode, "http_client_error")
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
	return instrument.Product == domain.ProductSpot && instrument.Quote == "USDT" &&
		(instrument.Base == "BTC" || instrument.Base == "ETH")
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
