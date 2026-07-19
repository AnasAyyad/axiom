package authentication

import "errors"

// Stable authentication boundary errors deliberately carry no sensitive detail.
var (
	ErrAuthenticationFailed = errors.New("authentication_failed")
	ErrRateLimited          = errors.New("authentication_rate_limited")
	ErrSessionInvalid       = errors.New("session_invalid")
	ErrCSRFInvalid          = errors.New("csrf_invalid")
	ErrForbidden            = errors.New("forbidden")
	ErrBootstrapRequired    = errors.New("authentication_bootstrap_required")
	ErrConfiguration        = errors.New("authentication_configuration_invalid")
)
