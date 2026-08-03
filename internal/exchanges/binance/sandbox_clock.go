package binance

import (
	"context"
	"net/url"
	"time"
)

const (
	sandboxClockSampleLifetime = 30 * time.Second
	sandboxClockWarmupAttempts = 8
	sandboxRESTTimeGuard       = time.Second
)

func (client *SandboxClient) ensureClock(ctx context.Context) error {
	client.clockMutex.Lock()
	defer client.clockMutex.Unlock()
	health := client.clock.Health()
	now := client.now().UTC()
	if client.clockValidated && health.Eligible &&
		!now.Before(health.ObservedAt) &&
		now.Sub(health.ObservedAt) <= sandboxClockSampleLifetime {
		return nil
	}
	for attempt := 0; attempt < sandboxClockWarmupAttempts; attempt++ {
		if ctx.Err() != nil {
			client.clockValidated = false
			return ErrSandboxRequest
		}
		if client.observeSandboxClockSample(ctx) {
			client.clockValidated = true
			return nil
		}
		client.clockValidated = false
	}
	client.clockValidated = false
	return ErrSandboxRequest
}

func (client *SandboxClient) conservativeServerNowFor(
	ctx context.Context,
	observedAt time.Time,
) (time.Time, error) {
	if observedAt.IsZero() || observedAt.Location() != time.UTC {
		return time.Time{}, ErrSandboxRequest
	}
	client.clockMutex.Lock()
	defer client.clockMutex.Unlock()
	for attempt := 0; attempt <= sandboxClockWarmupAttempts; attempt++ {
		health := client.clock.Health()
		now := client.now().UTC()
		if client.clockValidated && health.Eligible &&
			!now.Before(health.ObservedAt) &&
			now.Sub(health.ObservedAt) <= sandboxClockSampleLifetime {
			observedThrough := now.
				Add(health.Offset).
				Add(health.Uncertainty)
			if !observedAt.After(observedThrough) {
				return observedThrough, nil
			}
		}
		if attempt == sandboxClockWarmupAttempts || ctx.Err() != nil {
			break
		}
		client.clockValidated = client.observeSandboxClockSample(ctx)
	}
	client.clockValidated = false
	return time.Time{}, ErrSandboxRequest
}

func (client *SandboxClient) invalidateClock() {
	client.clockMutex.Lock()
	client.clockValidated = false
	client.clockMutex.Unlock()
}

func (client *SandboxClient) observeSandboxClockSample(
	ctx context.Context,
) bool {
	sentAt := client.now().UTC()
	body, err := client.executeUnsigned(ctx, "/api/v3/time", url.Values{})
	receivedAt := client.now().UTC()
	if err != nil || receivedAt.Before(sentAt) {
		return false
	}
	var native struct {
		ServerTime int64 `json:"serverTime"`
	}
	if strictDecode(body, &native) != nil || native.ServerTime <= 0 {
		return false
	}
	roundTrip := receivedAt.Sub(sentAt)
	if err = client.clock.Observe(
		sentAt,
		receivedAt,
		time.UnixMilli(native.ServerTime).UTC(),
		0,
		roundTrip,
	); err != nil {
		return false
	}
	return client.clock.Health().Eligible
}
