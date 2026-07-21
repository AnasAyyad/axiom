package console

import (
	"errors"
	"net"
	"net/http"
	"strconv"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
)

func (handler *handler) login(writer http.ResponseWriter, request *http.Request) {
	if !handler.validOrigin(request) {
		handler.writeError(writer, request, http.StatusForbidden, "origin_invalid", "Request origin rejected")
		return
	}
	if handler.options.Authentication == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication unavailable")
		return
	}
	var body generated.LoginRequest
	if !handler.decode(writer, request, &body) || body.Password == nil {
		return
	}
	result, err := handler.options.Authentication.Login(request.Context(), string(body.Email), *body.Password,
		loginSourceScope(request.RemoteAddr), randomCorrelationID())
	if err != nil {
		if errors.Is(err, authentication.ErrRateLimited) {
			writer.Header().Set("Retry-After", "900")
			handler.writeError(writer, request, http.StatusTooManyRequests, "authentication_rate_limited", "Authentication temporarily unavailable")
			return
		}
		handler.writeError(writer, request, http.StatusUnauthorized, "authentication_failed", "Email or password is incorrect")
		return
	}
	handler.setCookies(writer, result)
	handler.writeJSON(writer, http.StatusCreated, generated.LoginResponse{CsrfToken: result.CSRFToken,
		ExpiresAt: result.ExpiresAt, User: sessionUser(result.Principal)})
}

func loginSourceScope(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	if net.ParseIP(remoteAddress) != nil {
		return remoteAddress
	}
	return "invalid-source"
}

func (handler *handler) logout(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
	_ = handler.options.Authentication.Logout(request.Context(), principal.SessionID)
	handler.clearCookies(writer)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (handler *handler) me(writer http.ResponseWriter, _ *http.Request, principal authentication.Principal) {
	handler.writeJSON(writer, http.StatusOK, generated.SessionMe{User: sessionUser(principal), SessionId: principal.SessionID,
		SessionRevision: strconv.FormatInt(principal.SessionRevision, 10), ReauthenticatedAt: principal.ReauthenticatedAt})
}

func (handler *handler) setCookies(writer http.ResponseWriter, result authentication.LoginResult) {
	maximumAge := int(authentication.AbsoluteLifetime.Seconds())
	http.SetCookie(writer, &http.Cookie{Name: sessionCookieName, Value: result.SessionToken, Path: "/", HttpOnly: true,
		Secure: handler.options.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: maximumAge, Expires: result.ExpiresAt})
	http.SetCookie(writer, &http.Cookie{Name: csrfCookieName, Value: result.CSRFToken, Path: "/", HttpOnly: false,
		Secure: handler.options.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: maximumAge, Expires: result.ExpiresAt})
}

func (handler *handler) clearCookies(writer http.ResponseWriter) {
	for _, cookie := range []*http.Cookie{
		{Name: sessionCookieName, Path: "/", HttpOnly: true},
		{Name: csrfCookieName, Path: "/", HttpOnly: false},
	} {
		cookie.Secure = handler.options.SecureCookies
		cookie.SameSite = http.SameSiteStrictMode
		cookie.MaxAge = -1
		cookie.Value = ""
		http.SetCookie(writer, cookie)
	}
}
