package binance

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

// ConnectionPolicy fixes bounded reconnect and scheduled renewal behavior.
type ConnectionPolicy struct {
	MinimumBackoff time.Duration
	MaximumBackoff time.Duration
	Renewal        time.Duration
	Seed           string
}

// Validate rejects unbounded or exchange-unsafe lifecycle settings.
func (policy ConnectionPolicy) Validate() error {
	if policy.MinimumBackoff <= 0 || policy.MaximumBackoff < policy.MinimumBackoff ||
		policy.MaximumBackoff > 5*time.Minute || policy.Renewal <= 0 || policy.Renewal > 24*time.Hour ||
		policy.Seed == "" {
		return exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
	}
	return nil
}

// Backoff returns capped exponential delay with deterministic keyed jitter.
func (policy ConnectionPolicy) Backoff(attempt uint32) (time.Duration, error) {
	if policy.Validate() != nil || attempt == 0 {
		return 0, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
	}
	delay := policy.MinimumBackoff
	for count := uint32(1); count < attempt && delay < policy.MaximumBackoff; count++ {
		if delay > policy.MaximumBackoff/2 {
			delay = policy.MaximumBackoff
			break
		}
		delay *= 2
	}
	if delay > policy.MaximumBackoff {
		delay = policy.MaximumBackoff
	}
	hash := sha256.Sum256(append([]byte(policy.Seed), byte(attempt>>24), byte(attempt>>16), byte(attempt>>8), byte(attempt)))
	maximumJitter := uint64(delay / 4)
	if maximumJitter == 0 {
		return delay, nil
	}
	jitter := time.Duration(binary.BigEndian.Uint64(hash[:8]) % (maximumJitter + 1))
	if delay+jitter > policy.MaximumBackoff {
		return policy.MaximumBackoff, nil
	}
	return delay + jitter, nil
}

// RenewalDue reports whether one connection reached its scheduled lifetime.
func (policy ConnectionPolicy) RenewalDue(elapsed time.Duration) bool {
	return policy.Validate() == nil && elapsed >= policy.Renewal
}
