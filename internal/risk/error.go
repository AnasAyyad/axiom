package risk

// Error is one stable risk configuration or transition failure.
type Error struct{ Code string }

// Error returns the stable failure code.
func (failure *Error) Error() string { return failure.Code }

func riskError(code string) error { return &Error{Code: code} }
