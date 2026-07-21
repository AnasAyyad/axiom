package replay

// Error is one bounded replay-control failure.
type Error struct{ Code string }

// Error returns the stable replay failure code.
func (failure *Error) Error() string { return "replay:" + failure.Code }

func replayError(code string) error { return &Error{Code: code} }
