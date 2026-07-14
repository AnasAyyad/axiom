package exchangecontracts

import (
	"context"
	"testing"
	"time"
)

type recordingWaiter struct {
	delays []time.Duration
	err    error
}

func (waiter *recordingWaiter) Wait(_ context.Context, delay time.Duration) error {
	waiter.delays = append(waiter.delays, delay)
	return waiter.err
}

func TestExecuteReadUsesBoundedDeterministicBackoff(t *testing.T) {
	t.Parallel()
	waiter := &recordingWaiter{}
	attempts := 0
	err := ExecuteRead(
		context.Background(),
		OperationSnapshot,
		RetryPolicy{MaximumAttempts: 4, InitialBackoff: 10 * time.Millisecond, MaximumBackoff: 25 * time.Millisecond, JitterSeed: "retry-test"},
		waiter,
		func(context.Context) error {
			attempts++
			if attempts < 4 {
				return NewError(ErrorTransient, OperationSnapshot, 0)
			}
			return nil
		},
	)
	if err != nil || attempts != 4 {
		t.Fatalf("unexpected retry result: attempts=%d err=%v", attempts, err)
	}
	bases := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 25 * time.Millisecond}
	if len(waiter.delays) != len(bases) {
		t.Fatalf("unexpected delays: %v", waiter.delays)
	}
	for index := range bases {
		if waiter.delays[index] < bases[index] || waiter.delays[index] > bases[index]+bases[index]/4 {
			t.Fatalf("delay %d outside deterministic jitter range: %s", index, waiter.delays[index])
		}
	}
}

func TestRetryHonorsRateHeaderAndCancellation(t *testing.T) {
	t.Parallel()
	waiter := &recordingWaiter{}
	attempts := 0
	err := ExecuteRead(
		context.Background(), OperationTrades,
		RetryPolicy{MaximumAttempts: 2, InitialBackoff: time.Second, MaximumBackoff: 5 * time.Second, JitterSeed: "rate-test"},
		waiter,
		func(context.Context) error {
			attempts++
			return NewError(ErrorRateLimit, OperationTrades, 3*time.Second)
		},
	)
	if KindOf(err) != ErrorRateLimit || attempts != 2 || len(waiter.delays) != 1 ||
		waiter.delays[0] < 3*time.Second || waiter.delays[0] > 3750*time.Millisecond {
		t.Fatalf("retry-after mismatch: attempts=%d delays=%v err=%v", attempts, waiter.delays, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = ExecuteRead(ctx, OperationTrades,
		RetryPolicy{MaximumAttempts: 2, InitialBackoff: time.Second, MaximumBackoff: time.Second, JitterSeed: "cancel-test"},
		&recordingWaiter{}, func(context.Context) error { return nil })
	if KindOf(err) != ErrorCanceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestPrivateAmbiguityNeverBlindlyRetries(t *testing.T) {
	t.Parallel()
	ambiguous := NewError(ErrorAmbiguousState, OperationSubmission, 0)
	if RetryDecision(OperationSubmission, ambiguous) != RetryReconcile {
		t.Fatal("submission ambiguity must reconcile")
	}
	if RetryDecision(OperationCancel, NewError(ErrorTransient, OperationCancel, 0)) != RetryStop {
		t.Fatal("non-read operation must not retry")
	}
	if RetryDecision(OperationSnapshot, NewError(ErrorFilter, OperationSnapshot, 0)) != RetryStop {
		t.Fatal("permanent public validation must not retry")
	}
}

func TestRetryJitterIsKeyedAndRepeatable(t *testing.T) {
	t.Parallel()
	policy := RetryPolicy{
		MaximumAttempts: 3, InitialBackoff: time.Second, MaximumBackoff: 4 * time.Second, JitterSeed: "stable-seed",
	}
	failure := NewError(ErrorTransient, OperationSnapshot, 0)
	first := retryDelay(OperationSnapshot, policy, 2, failure)
	if first != retryDelay(OperationSnapshot, policy, 2, failure) {
		t.Fatal("same retry identity changed jitter")
	}
	if first == retryDelay(OperationTrades, policy, 2, failure) {
		t.Fatal("operation identity did not separate jitter streams")
	}
}
