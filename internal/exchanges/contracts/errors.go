package exchangecontracts

import (
	"errors"
	"fmt"
	"time"
)

// ErrorKind is a stable exchange-boundary failure class.
type ErrorKind string

// Stable exchange error classes. They contain no raw payload or destination.
const (
	ErrorCapability        ErrorKind = "capability_unsupported"
	ErrorRateLimit         ErrorKind = "rate_limit"
	ErrorTransient         ErrorKind = "transient_outage"
	ErrorTimestamp         ErrorKind = "timestamp_rejected"
	ErrorFilter            ErrorKind = "filter_rejected"
	ErrorInsufficientFunds ErrorKind = "insufficient_funds"
	ErrorMaintenance       ErrorKind = "maintenance"
	ErrorValidation        ErrorKind = "validation_rejected"
	ErrorAmbiguousState    ErrorKind = "ambiguous_state"
	ErrorCanceled          ErrorKind = "operation_canceled"
)

// Error is a sanitized typed exchange failure.
type Error struct {
	Kind       ErrorKind
	Operation  Operation
	RetryAfter time.Duration
}

// Error returns a stable string without payloads, URLs, or exchange responses.
func (failure *Error) Error() string {
	if failure == nil {
		return "exchange_error"
	}
	if failure.Operation == "" {
		return string(failure.Kind)
	}
	return fmt.Sprintf("%s:%s", failure.Kind, failure.Operation)
}

// Is reports whether target has the same stable kind and optional operation.
func (failure *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && failure != nil && failure.Kind == other.Kind &&
		(other.Operation == "" || failure.Operation == other.Operation)
}

// NewError constructs a validated sanitized exchange error.
func NewError(kind ErrorKind, operation Operation, retryAfter time.Duration) error {
	if !validErrorKind(kind) || !validOperation(operation) || retryAfter < 0 {
		return &Error{Kind: ErrorValidation, Operation: OperationCapability}
	}
	if kind != ErrorRateLimit && retryAfter != 0 {
		return &Error{Kind: ErrorValidation, Operation: operation}
	}
	return &Error{Kind: kind, Operation: operation, RetryAfter: retryAfter}
}

// KindOf returns a stable kind without exposing wrapped details.
func KindOf(err error) ErrorKind {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Kind
	}
	return ErrorValidation
}

func validErrorKind(kind ErrorKind) bool {
	switch kind {
	case ErrorCapability, ErrorRateLimit, ErrorTransient, ErrorTimestamp,
		ErrorFilter, ErrorInsufficientFunds, ErrorMaintenance, ErrorValidation,
		ErrorAmbiguousState, ErrorCanceled:
		return true
	default:
		return false
	}
}
