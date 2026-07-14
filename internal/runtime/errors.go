package runtimecore

// Error is a stable runtime failure that contains no event payload.
type Error struct {
	Code  string
	Scope string
}

// Error returns the stable failure code and bounded scope.
func (failure *Error) Error() string { return failure.Code + ":" + failure.Scope }

func runtimeError(code, scope string) error { return &Error{Code: code, Scope: scope} }
