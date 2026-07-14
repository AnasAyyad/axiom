package api

import (
	"net/http"

	"axiom/internal/api/health"
	staticassets "axiom/internal/api/static"
)

// NewRouter composes A1 health routes and the embedded React application.
func NewRouter(options health.Options) http.Handler {
	mux := http.NewServeMux()
	health.Register(mux, options)
	mux.Handle("/", staticassets.Handler())
	return securityHeaders(mux)
}

// NewHealthRouter composes only process health for non-API roles.
func NewHealthRouter(options health.Options) http.Handler {
	mux := http.NewServeMux()
	health.Register(mux, options)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; style-src 'self'; script-src 'self'")
		next.ServeHTTP(writer, request)
	})
}
