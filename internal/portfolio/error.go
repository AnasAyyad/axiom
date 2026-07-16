package portfolio

// Error is one stable fail-closed portfolio or allocation failure.
type Error struct{ Code string }

// Error returns the stable failure code.
func (failure *Error) Error() string { return failure.Code }

func portfolioError(code string) error { return &Error{Code: code} }
