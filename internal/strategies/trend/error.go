package trend

import "fmt"

// Error is one stable fail-closed Trend error.
type Error struct{ Code string }

// Error returns only the safe stable code.
func (err Error) Error() string { return fmt.Sprintf("trend: %s", err.Code) }

func trendError(code string) error { return Error{Code: code} }
