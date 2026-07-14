package domain

import "fmt"

// ErrorCode is a stable machine-readable domain failure code.
type ErrorCode string

// Stable domain error codes returned by checked value operations.
const (
	CodeInvalidDecimal    ErrorCode = "invalid_decimal"
	CodeDecimalRange      ErrorCode = "decimal_range"
	CodeArithmetic        ErrorCode = "arithmetic_rejected"
	CodeNegativeValue     ErrorCode = "negative_value"
	CodeInvalidScale      ErrorCode = "invalid_scale"
	CodeInvalidIdentifier ErrorCode = "invalid_identifier"
	CodeInvalidInstrument ErrorCode = "invalid_instrument"
	CodeInvalidTimestamp  ErrorCode = "invalid_timestamp"
)

// Error describes a stable fail-closed domain validation or arithmetic error.
type Error struct {
	Code      ErrorCode
	Operation string
}

// Error returns a stable string that does not expose input data.
func (e *Error) Error() string {
	if e.Operation == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s:%s", e.Code, e.Operation)
}

func domainError(code ErrorCode, operation string) error {
	return &Error{Code: code, Operation: operation}
}
