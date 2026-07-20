package console

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

const (
	sessionCookieName = "axiom_session"
	csrfCookieName    = "axiom_csrf"
)

// Options are immutable A11 HTTP dependencies.
type Options struct {
	Authentication *authentication.Service
	AllowedOrigins []string
	SecureCookies  bool
	Read           ReadService
	Commands       CommandService
	Streams        StreamService
}

// Register installs all authenticated A11 routes on one mux.
func Register(mux *http.ServeMux, options Options) {
	handler := &handler{options: options, origins: make(map[string]struct{})}
	for _, origin := range options.AllowedOrigins {
		handler.origins[origin] = struct{}{}
	}
	mux.HandleFunc("POST /api/v1/session/login", handler.login)
	mux.HandleFunc("POST /api/v1/session/logout", handler.authorizedMutation(handler.logout, ""))
	mux.HandleFunc("GET /api/v1/session/me", handler.authorized(handler.me, ""))
	handler.registerReads(mux)
	handler.registerCommands(mux)
	mux.HandleFunc("GET /api/v1/stream", handler.authorized(handler.stream, "operations.read"))
}

type handler struct {
	options Options
	origins map[string]struct{}
}

type authenticatedHandler func(http.ResponseWriter, *http.Request, authentication.Principal)

func (handler *handler) authorized(next authenticatedHandler, permission string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := handler.principal(writer, request)
		if !ok {
			return
		}
		if permission != "" && authentication.RequirePermission(principal, permission) != nil {
			handler.writeError(writer, request, http.StatusForbidden, "forbidden", "Permission denied")
			return
		}
		next(writer, request, principal)
	}
}

func (handler *handler) authorizedMutation(next authenticatedHandler, permission string) http.HandlerFunc {
	return handler.authorized(func(writer http.ResponseWriter, request *http.Request, principal authentication.Principal) {
		if !handler.validOrigin(request) {
			handler.writeError(writer, request, http.StatusForbidden, "origin_invalid", "Request origin rejected")
			return
		}
		session, sessionErr := request.Cookie(sessionCookieName)
		csrf, csrfErr := request.Cookie(csrfCookieName)
		if sessionErr != nil || csrfErr != nil || handler.options.Authentication.ValidateRequestCSRF(
			request.Context(), session.Value, csrf.Value, request.Header.Get("X-CSRF-Token"),
		) != nil {
			handler.writeError(writer, request, http.StatusForbidden, "csrf_invalid", "Request verification failed")
			return
		}
		next(writer, request, principal)
	}, permission)
}

func (handler *handler) principal(writer http.ResponseWriter, request *http.Request) (authentication.Principal, bool) {
	if handler.options.Authentication == nil {
		handler.writeError(writer, request, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication unavailable")
		return authentication.Principal{}, false
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		handler.writeError(writer, request, http.StatusUnauthorized, "session_invalid", "Authentication required")
		return authentication.Principal{}, false
	}
	principal, err := handler.options.Authentication.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		handler.clearCookies(writer)
		handler.writeError(writer, request, http.StatusUnauthorized, "session_invalid", "Authentication required")
		return authentication.Principal{}, false
	}
	return principal, true
}

func (handler *handler) validOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	_, ok := handler.origins[origin]
	return ok
}

// validEventStreamOrigin accepts the explicit Origin header when present. A
// same-origin EventSource GET may omit Origin, so that browser shape is accepted
// only when Fetch Metadata says same-origin and the request Host is allowlisted.
func (handler *handler) validEventStreamOrigin(request *http.Request) bool {
	if request.Header.Get("Origin") != "" {
		return handler.validOrigin(request)
	}
	if request.Header.Get("Sec-Fetch-Site") != "same-origin" || request.Header.Get("Sec-Fetch-Mode") != "cors" {
		return false
	}
	for origin := range handler.origins {
		parsed, err := url.Parse(origin)
		if err == nil && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" &&
			(parsed.Path == "" || parsed.Path == "/") && strings.EqualFold(parsed.Host, request.Host) {
			return true
		}
	}
	return false
}

func (handler *handler) decode(writer http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		handler.writeError(writer, request, http.StatusBadRequest, "content_type_invalid", "Content-Type must be application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || decoder.Decode(&struct{}{}) == nil {
		handler.writeError(writer, request, http.StatusBadRequest, "invalid_request", "Request body is invalid")
		return false
	}
	return true
}

func (handler *handler) writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func (handler *handler) writeError(writer http.ResponseWriter, request *http.Request, status int, code, message string) {
	correlation := request.Header.Get("X-Correlation-ID")
	if correlation == "" || len(correlation) > 128 || strings.ContainsAny(correlation, "\r\n") {
		correlation = randomCorrelationID()
	}
	handler.writeJSON(writer, status, generated.Error{Code: code, Message: message, CorrelationId: correlation})
}

func randomCorrelationID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "correlation-unavailable"
	}
	return "request-" + hex.EncodeToString(value)
}

func pageSize(request *http.Request) (int, error) {
	raw := request.URL.Query().Get("page_size")
	if raw == "" {
		return 50, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 200 {
		return 0, errors.New("invalid_page_size")
	}
	return value, nil
}

func sessionUser(principal authentication.Principal) generated.SessionUser {
	return generated.SessionUser{Id: principal.UserID, Email: openapi_types.Email(principal.Email),
		Roles: append([]string(nil), principal.Roles...), Permissions: append([]string(nil), principal.Permissions...)}
}
