package backtest

// Error is a bounded research-platform failure without dataset paths or data.
type Error struct{ Code string }

// Error returns the stable failure code.
func (failure *Error) Error() string { return "backtest:" + failure.Code }

func backtestError(code string) error { return &Error{Code: code} }
