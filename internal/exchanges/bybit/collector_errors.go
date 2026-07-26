package bybit

import "errors"

type recorderFailure struct{ error }

// Unwrap preserves the bounded recorder cause for qualification evidence.
func (failure recorderFailure) Unwrap() error { return failure.error }

func isRecorderFailure(err error) bool {
	var failure recorderFailure
	return errors.As(err, &failure)
}
