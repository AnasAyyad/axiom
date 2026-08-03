package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func classifySandboxResponse(
	route authenticatedRoute,
	status int,
	body []byte,
) error {
	var response struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(body, &response)
	if route == authenticatedQuery && response.Code == -2013 {
		return ErrSandboxOrderNotFound
	}
	if response.Code == -1021 {
		return fmt.Errorf(
			"%w: status_%d_code_%d",
			ErrSandboxTimestamp,
			status,
			response.Code,
		)
	}
	if response.Code == -1007 ||
		(status >= http.StatusInternalServerError &&
			routeCanChangeOrder(route)) {
		return ErrSandboxAmbiguous
	}
	if status == http.StatusTooManyRequests || status == http.StatusTeapot {
		return ErrSandboxRateLimited
	}
	if status >= http.StatusBadRequest &&
		status < http.StatusInternalServerError {
		return fmt.Errorf(
			"%w: status_%d_code_%d",
			ErrSandboxRejected,
			status,
			response.Code,
		)
	}
	if status >= http.StatusInternalServerError {
		return fmt.Errorf(
			"%w: status_%d_code_%d",
			ErrSandboxRequest,
			status,
			response.Code,
		)
	}
	return ErrSandboxRequest
}
