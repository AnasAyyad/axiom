package exchangecontracts

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

// RetryAction is the only permitted next step after an exchange failure.
type RetryAction string

// Retry actions are operation-specific and fail closed by default.
const (
	RetryStop      RetryAction = "stop"
	RetryRead      RetryAction = "retry_read"
	RetryReconcile RetryAction = "reconcile"
)

// RetryPolicy bounds deterministic public-read retries.
type RetryPolicy struct {
	MaximumAttempts uint32
	InitialBackoff  time.Duration
	MaximumBackoff  time.Duration
	JitterSeed      string
}

// Waiter makes retry delays replaceable by deterministic virtual time.
type Waiter interface {
	Wait(context.Context, time.Duration) error
}

// RetryDecision classifies a failure without performing an operation.
func RetryDecision(operation Operation, err error) RetryAction {
	kind := KindOf(err)
	if operation == OperationSubmission && kind == ErrorAmbiguousState {
		return RetryReconcile
	}
	if !publicRead(operation) {
		return RetryStop
	}
	switch kind {
	case ErrorRateLimit, ErrorTransient, ErrorMaintenance:
		return RetryRead
	default:
		return RetryStop
	}
}

// ExecuteRead runs one bounded, cancelable public read with deterministic backoff.
func ExecuteRead(
	ctx context.Context,
	operation Operation,
	policy RetryPolicy,
	waiter Waiter,
	read func(context.Context) error,
) error {
	if !publicRead(operation) || !validPolicy(policy) || waiter == nil || read == nil {
		return NewError(ErrorValidation, operation, 0)
	}
	for attempt := uint32(1); attempt <= policy.MaximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return NewError(ErrorCanceled, operation, 0)
		}
		err := read(ctx)
		if err == nil || RetryDecision(operation, err) != RetryRead || attempt == policy.MaximumAttempts {
			return err
		}
		delay := retryDelay(operation, policy, attempt, err)
		if err := waiter.Wait(ctx, delay); err != nil {
			return NewError(ErrorCanceled, operation, 0)
		}
	}
	return NewError(ErrorValidation, operation, 0)
}

func retryDelay(operation Operation, policy RetryPolicy, failedAttempt uint32, err error) time.Duration {
	delay := policy.InitialBackoff
	for count := uint32(1); count < failedAttempt && delay < policy.MaximumBackoff; count++ {
		if delay > policy.MaximumBackoff/2 {
			delay = policy.MaximumBackoff
			break
		}
		delay *= 2
	}
	var failure *Error
	if errors.As(err, &failure) && failure.RetryAfter > delay {
		delay = failure.RetryAfter
	}
	return addDeterministicJitter(operation, failedAttempt, delay, policy.JitterSeed)
}

func validPolicy(policy RetryPolicy) bool {
	return policy.MaximumAttempts > 0 && policy.InitialBackoff > 0 &&
		policy.MaximumBackoff >= policy.InitialBackoff && policy.JitterSeed != ""
}

func addDeterministicJitter(operation Operation, attempt uint32, base time.Duration, seed string) time.Duration {
	hash := sha256.New()
	_, _ = hash.Write([]byte(seed))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(operation))
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], attempt)
	_, _ = hash.Write(encoded[:])
	draw := binary.BigEndian.Uint64(hash.Sum(nil)[:8])
	maximumJitter := uint64(base / 4)
	if maximumJitter == 0 {
		return base
	}
	jitter := time.Duration(draw % (maximumJitter + 1))
	if base > time.Duration(^uint64(0)>>1)-jitter {
		return time.Duration(^uint64(0) >> 1)
	}
	return base + jitter
}

func publicRead(operation Operation) bool {
	switch operation {
	case OperationMetadata, OperationSnapshot, OperationTrades, OperationCandles, OperationStream:
		return true
	default:
		return false
	}
}
