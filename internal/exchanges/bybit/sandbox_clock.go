package bybit

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	demoClockSampleLifetime = 30 * time.Second
	demoClockWarmupAttempts = 12
)

func (client *SandboxClient) ensureDemoClock(ctx context.Context) error {
	client.clockMutex.Lock()
	defer client.clockMutex.Unlock()
	now := client.now().UTC()
	if client.clockValidated &&
		!now.Before(client.clockObservedAt) &&
		now.Sub(client.clockObservedAt) <= demoClockSampleLifetime {
		return nil
	}
	for attempt := 0; attempt < demoClockWarmupAttempts; attempt++ {
		sentAt := client.now().UTC()
		body, err := client.executeDemoUnsigned(ctx, "/v5/market/time", url.Values{})
		receivedAt := client.now().UTC()
		if err != nil {
			client.clockValidated = false
			return err
		}
		if receivedAt.Before(sentAt) {
			client.clockValidated = false
			return ErrDemoRequest
		}
		result, err := decodeDemoResult[serverTimeResult](body)
		if err != nil {
			client.clockValidated = false
			return ErrDemoRequest
		}
		nanoseconds, err := strconv.ParseInt(result.TimeNano, 10, 64)
		if err != nil || nanoseconds <= 0 {
			client.clockValidated = false
			return ErrDemoRequest
		}
		serverAt := time.Unix(0, nanoseconds).UTC()
		roundTrip := receivedAt.Sub(sentAt)
		if roundTrip/2 > 250*time.Millisecond {
			continue
		}
		midpoint := sentAt.Add(roundTrip / 2)
		client.clockOffset = serverAt.Sub(midpoint)
		client.clockObservedAt = receivedAt
		client.clockValidated = true
		return nil
	}
	client.clockValidated = false
	return ErrDemoRequest
}

func (client *SandboxClient) demoClockOffset() time.Duration {
	client.clockMutex.Lock()
	defer client.clockMutex.Unlock()
	return client.clockOffset
}

func (client *SandboxClient) invalidateDemoClock() {
	client.clockMutex.Lock()
	defer client.clockMutex.Unlock()
	client.clockValidated = false
}

func (client *SandboxClient) executeDemoUnsigned(
	ctx context.Context,
	path string,
	query url.Values,
) ([]byte, error) {
	if !validDemoUnsignedRoute(path, query) {
		return nil, ErrDemoRequest
	}
	if err := client.allowDemoRequest(); err != nil {
		return nil, err
	}
	target := demoRESTOrigin + path
	if len(query) != 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		return nil, ErrDemoRequest
	}
	response, err := client.doer.Do(request)
	if err != nil {
		return nil, ErrDemoRequest
	}
	return client.readDemoUnsignedResponse(response)
}

func (client *SandboxClient) readDemoUnsignedResponse(
	response *http.Response,
) ([]byte, error) {
	defer response.Body.Close()
	client.observeDemoRateLimit(response)
	body, err := io.ReadAll(
		io.LimitReader(response.Body, authenticatedResponseLimit+1),
	)
	if err != nil || len(body) > authenticatedResponseLimit {
		return nil, ErrDemoRequest
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, ErrDemoRateLimited
	}
	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrDemoRequest
	}
	if err = classifyDemoEnvelope(0, body); err != nil {
		if err == ErrDemoRateLimited {
			client.blockDemoRateLimit(response)
		}
		return nil, err
	}
	return body, nil
}

func validDemoUnsignedRoute(path string, query url.Values) bool {
	return path == "/v5/market/time" && len(query) == 0
}
