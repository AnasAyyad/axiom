package simulation

// Error is one bounded simulation failure.
type Error struct{ Code string }

// Error returns the stable simulation failure code.
func (failure *Error) Error() string { return "simulation:" + failure.Code }

func simulationError(code string) error { return &Error{Code: code} }
