package meanreversion

import "fmt"

// Error is one stable fail-closed mean-reversion error.
type Error struct{ Code string }

// Error returns the stable fail-closed reason code with its strategy namespace.
func (err Error) Error() string { return fmt.Sprintf("meanreversion: %s", err.Code) }

func strategyError(code string) error { return Error{Code: code} }
