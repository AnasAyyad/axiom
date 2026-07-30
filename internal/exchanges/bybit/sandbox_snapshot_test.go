package bybit

import (
	"context"
	"errors"
	"testing"
)

func TestRetryDemoSnapshotReadRecoversOneTransportFailure(t *testing.T) {
	attempts := 0
	value, err := retryDemoSnapshotRead(context.Background(), func() (string, error) {
		attempts++
		if attempts == 1 {
			return "", ErrDemoRequest
		}
		return "ok", nil
	})
	if err != nil || value != "ok" || attempts != 2 {
		t.Fatalf("value=%q error=%v attempts=%d", value, err, attempts)
	}
}

func TestRetryDemoSnapshotReadDoesNotRetryRejection(t *testing.T) {
	attempts := 0
	_, err := retryDemoSnapshotRead(context.Background(), func() (string, error) {
		attempts++
		return "", ErrDemoRejected
	})
	if !errors.Is(err, ErrDemoRejected) || attempts != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}

func TestRetryDemoSnapshotReadFailsClosedAfterThirdFailure(t *testing.T) {
	attempts := 0
	_, err := retryDemoSnapshotRead(context.Background(), func() (string, error) {
		attempts++
		return "", ErrDemoRequest
	})
	if !errors.Is(err, ErrDemoRequest) || attempts != 3 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}
