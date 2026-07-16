package reconciliation

// Error is one stable reconciliation or recovery failure.
type Error struct{ Code string }

// Error returns the stable failure code.
func (failure *Error) Error() string { return failure.Code }

func reconciliationError(code string) error { return &Error{Code: code} }
