package config

// Error is a stable fail-closed configuration error without input disclosure.
type Error struct {
	Code  string
	Field string
}

// Error returns the stable code and field.
func (e *Error) Error() string { return e.Code + ":" + e.Field }

func configError(code, field string) error { return &Error{Code: code, Field: field} }
