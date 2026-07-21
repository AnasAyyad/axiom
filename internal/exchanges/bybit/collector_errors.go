package bybit

import "errors"

type recorderFailure struct{ error }

func isRecorderFailure(err error) bool {
	var failure recorderFailure
	return errors.As(err, &failure)
}
