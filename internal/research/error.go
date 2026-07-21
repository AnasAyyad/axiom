package research

import "fmt"

// Error is one stable research-validation failure.
type Error struct{ Code string }

// Error emits only the safe stable code.
func (err Error) Error() string { return fmt.Sprintf("research: %s", err.Code) }

func researchError(code string) error { return Error{Code: code} }
