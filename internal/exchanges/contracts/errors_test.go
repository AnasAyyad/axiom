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

func TestDetailedErrorRetainsOnlyBoundedDiagnosticMetadata(t *testing.T) {
	t.Parallel()
	err := NewDetailedError(ErrorRateLimit, OperationSnapshot, 3*time.Second, 429, "http_rate_limit")
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != ErrorRateLimit || failure.Operation != OperationSnapshot ||
		failure.RetryAfter != 3*time.Second || failure.HTTPStatus != 429 || failure.Cause != "http_rate_limit" {
		t.Fatalf("detailed error=%#v", err)
	}
	for _, invalid := range []error{
		NewDetailedError(ErrorTransient, OperationSnapshot, 0, 99, "http_server_error"),
		NewDetailedError(ErrorTransient, OperationSnapshot, 0, 600, "http_server_error"),
		NewDetailedError(ErrorTransient, OperationSnapshot, 0, 500, "contains space"),
		NewDetailedError(ErrorTransient, OperationSnapshot, 0, 500, "https://example.invalid"),
		NewDetailedError(ErrorTransient, OperationSnapshot, 0, 500, ""),
	} {
		if KindOf(invalid) != ErrorValidation {
			t.Fatalf("unsafe diagnostic accepted: %#v", invalid)
		}
		if errors.As(invalid, &failure) && (failure.HTTPStatus != 0 || failure.Cause != "") {
			t.Fatalf("unsafe metadata retained: %#v", failure)
		}
	}
}
