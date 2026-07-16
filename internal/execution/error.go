package execution

// Error is one bounded execution invariant failure.
type Error struct{ Code string }

// Error returns the stable execution failure code.
func (failure *Error) Error() string { return "execution:" + failure.Code }

func executionError(code string) error { return &Error{Code: code} }
