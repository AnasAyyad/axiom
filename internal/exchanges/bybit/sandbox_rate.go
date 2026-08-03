package bybit

import (
	"net/http"
	"strconv"
	"time"
)

func (client *SandboxClient) allowDemoRequest() error {
	now := client.now().UTC()
	client.rateMutex.Lock()
	defer client.rateMutex.Unlock()
	if !client.rateBlockedUntil.IsZero() && now.Before(client.rateBlockedUntil) {
		return ErrDemoRateLimited
	}
	if !client.rateBlockedUntil.IsZero() {
		client.rateBlockedUntil = time.Time{}
	}
	return nil
}

func (client *SandboxClient) observeDemoRateLimit(response *http.Response) {
	if response == nil || response.StatusCode != http.StatusTooManyRequests {
		return
	}
	client.blockDemoRateLimit(response)
}

func (client *SandboxClient) blockDemoRateLimit(response *http.Response) {
	if response == nil {
		return
	}
	wait := 72 * time.Hour
	if seconds, err := strconv.ParseUint(
		response.Header.Get("Retry-After"),
		10,
		32,
	); err == nil && seconds > 0 {
		wait = time.Duration(seconds) * time.Second
	} else if resetAt, err := strconv.ParseInt(
		response.Header.Get("X-Bapi-Limit-Reset-Timestamp"),
		10,
		64,
	); err == nil && resetAt > client.now().UTC().UnixMilli() {
		wait = time.UnixMilli(resetAt).UTC().Sub(client.now().UTC())
	}
	blockedUntil := client.now().UTC().Add(wait)
	client.rateMutex.Lock()
	if blockedUntil.After(client.rateBlockedUntil) {
		client.rateBlockedUntil = blockedUntil
	}
	client.rateMutex.Unlock()
}
