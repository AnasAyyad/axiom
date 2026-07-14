package exchangecontracts

import (
	"errors"
	"testing"
	"time"
)

func TestErrorTaxonomyIsStableAndSanitized(t *testing.T) {
	t.Parallel()
	kinds := []ErrorKind{
		ErrorCapability, ErrorRateLimit, ErrorTransient, ErrorTimestamp, ErrorFilter,
		ErrorInsufficientFunds, ErrorMaintenance, ErrorValidation, ErrorAmbiguousState, ErrorCanceled,
	}
	for _, kind := range kinds {
		retryAfter := time.Duration(0)
		if kind == ErrorRateLimit {
			retryAfter = 2 * time.Second
		}
		err := NewError(kind, OperationSnapshot, retryAfter)
		var failure *Error
		if !errors.As(err, &failure) || failure.Kind != kind || KindOf(err) != kind ||
			failure.Error() != string(kind)+":"+string(OperationSnapshot) {
			t.Fatalf("unstable error for %s: %#v", kind, err)
		}
	}
}

func TestInvalidErrorConstructionFailsClosed(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		NewError("unknown", OperationSnapshot, 0),
		NewError(ErrorTransient, "unknown", 0),
		NewError(ErrorTransient, OperationSnapshot, time.Second),
	} {
		if KindOf(err) != ErrorValidation {
			t.Fatalf("expected validation error, got %v", err)
		}
	}
	if !errors.Is(NewError(ErrorRateLimit, OperationSnapshot, 0), &Error{Kind: ErrorRateLimit}) {
		t.Fatal("errors.Is did not match stable kind")
	}
}
