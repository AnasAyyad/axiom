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

// FailureMetadata is bounded transport evidence safe for logs and
// qualification artifacts. It intentionally excludes URLs, addresses,
// headers, response bodies, and arbitrary exchange text.
type FailureMetadata struct {
	RequestDuration        time.Duration `json:"request_duration_nanos,omitempty"`
	ResponseHeaderDuration time.Duration `json:"response_header_duration_nanos,omitempty"`
	ResponseBodyDuration   time.Duration `json:"response_body_duration_nanos,omitempty"`
	ResponseBytes          uint64        `json:"response_bytes,omitempty"`
	ContentLengthBytes     uint64        `json:"content_length_bytes,omitempty"`
	ContentLengthKnown     bool          `json:"content_length_known,omitempty"`
	BodyLimitBytes         uint64        `json:"body_limit_bytes,omitempty"`
	DNSDuration            time.Duration `json:"dns_duration_nanos,omitempty"`
	TCPDuration            time.Duration `json:"tcp_duration_nanos,omitempty"`
	TLSDuration            time.Duration `json:"tls_duration_nanos,omitempty"`
	UpgradeDuration        time.Duration `json:"upgrade_duration_nanos,omitempty"`
	WriteDuration          time.Duration `json:"write_duration_nanos,omitempty"`
	CandidateCount         uint32        `json:"candidate_count,omitempty"`
	AttemptCount           uint32        `json:"attempt_count,omitempty"`
	AddressFamily          string        `json:"address_family,omitempty"`
	SetupStage             string        `json:"setup_stage,omitempty"`
}

// Error is a sanitized typed exchange failure.
type Error struct {
	Kind       ErrorKind
	Operation  Operation
	RetryAfter time.Duration
	HTTPStatus int
	Cause      string
	Metadata   FailureMetadata
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

// NewDetailedError adds only bounded diagnostic metadata. Cause must be a
// short lowercase code and must never contain an address, URL, or raw message.
func NewDetailedError(
	kind ErrorKind,
	operation Operation,
	retryAfter time.Duration,
	httpStatus int,
	cause string,
	metadata ...FailureMetadata,
) error {
	err := NewError(kind, operation, retryAfter)
	failure, ok := err.(*Error)
	if !ok || (httpStatus != 0 && (httpStatus < 100 || httpStatus > 599)) ||
		!validDiagnosticCause(cause) || len(metadata) > 1 ||
		(len(metadata) == 1 && !validFailureMetadata(metadata[0])) {
		return &Error{Kind: ErrorValidation, Operation: operation}
	}
	failure.HTTPStatus, failure.Cause = httpStatus, cause
	if len(metadata) == 1 {
		failure.Metadata = metadata[0]
	}
	return failure
}

func validDiagnosticCause(cause string) bool {
	if len(cause) == 0 || len(cause) > 64 {
		return false
	}
	for _, value := range cause {
		if (value < 'a' || value > 'z') && value != '_' && (value < '0' || value > '9') {
			return false
		}
	}
	return true
}

func validFailureMetadata(metadata FailureMetadata) bool {
	const maximumDiagnosticDuration = 10 * time.Minute
	if metadata.RequestDuration < 0 || metadata.ResponseHeaderDuration < 0 ||
		metadata.ResponseBodyDuration < 0 || metadata.RequestDuration > maximumDiagnosticDuration ||
		metadata.ResponseHeaderDuration > maximumDiagnosticDuration ||
		metadata.ResponseBodyDuration > maximumDiagnosticDuration ||
		metadata.BodyLimitBytes > 64*1024*1024 ||
		metadata.DNSDuration < 0 || metadata.DNSDuration > maximumDiagnosticDuration ||
		metadata.TCPDuration < 0 || metadata.TCPDuration > maximumDiagnosticDuration ||
		metadata.TLSDuration < 0 || metadata.TLSDuration > maximumDiagnosticDuration ||
		metadata.UpgradeDuration < 0 || metadata.UpgradeDuration > maximumDiagnosticDuration ||
		metadata.WriteDuration < 0 || metadata.WriteDuration > maximumDiagnosticDuration ||
		metadata.CandidateCount > 64 || metadata.AttemptCount > 64 ||
		!validAddressFamily(metadata.AddressFamily) ||
		(metadata.SetupStage != "" && !validDiagnosticCause(metadata.SetupStage)) {
		return false
	}
	return metadata.BodyLimitBytes == 0 || metadata.ResponseBytes <= metadata.BodyLimitBytes+1
}

func validAddressFamily(family string) bool {
	return family == "" || family == "ipv4" || family == "ipv6" || family == "mixed"
}

// KindOf returns a stable kind without exposing wrapped details.
func KindOf(err error) ErrorKind {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Kind
	}
	return ErrorValidation
}

// DiagnosticOf returns only bounded metadata from a typed exchange failure.
func DiagnosticOf(err error) (string, int, time.Duration, FailureMetadata) {
	var failure *Error
	if !errors.As(err, &failure) || failure == nil {
		return "", 0, 0, FailureMetadata{}
	}
	return failure.Cause, failure.HTTPStatus, failure.RetryAfter, failure.Metadata
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
