package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"axiom/internal/api/console"
	"axiom/internal/api/health"
	staticassets "axiom/internal/api/static"
)

// NewRouter composes A1 health routes and the embedded React application.
func NewRouter(options health.Options, consoleOptions ...console.Options) http.Handler {
	mux := http.NewServeMux()
	if len(consoleOptions) > 0 {
		options.OmitStatus = true
		console.Register(mux, consoleOptions[0])
	}
	health.Register(mux, options)
	mux.Handle("/", staticassets.Handler())
	return recoverPanics(securityHeaders(mux))
}

// NewHealthRouter composes only process health for non-API roles.
func NewHealthRouter(options health.Options) http.Handler {
	mux := http.NewServeMux()
	health.Register(mux, options)
	return recoverPanics(securityHeaders(mux))
}

// NewOperationalRouter composes health and the internal metrics endpoint. It is
// intended only for the Compose-internal metrics network.
func NewOperationalRouter(options health.Options, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	health.Register(mux, options)
	mux.Handle("/metrics", metrics)
	return recoverPanics(securityHeaders(mux))
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() == nil {
				return
			}
			correlation := request.Header.Get("X-Correlation-ID")
			if correlation == "" || len(correlation) > 128 || strings.ContainsAny(correlation, "\r\n") {
				value := make([]byte, 16)
				_, _ = rand.Read(value)
				correlation = "request-" + hex.EncodeToString(value)
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Cache-Control", "no-store")
			writer.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"code": "internal_error", "message": "Request could not be completed", "correlation_id": correlation,
			})
		}()
		next.ServeHTTP(writer, request)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; style-src 'self'; script-src 'self'")
		next.ServeHTTP(writer, request)
	})
}
