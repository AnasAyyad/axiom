package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestNewServesIndexForClientRoute(t *testing.T) {
	handler := New(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("axiom-health")},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health-ui", nil))
	if response.Code != http.StatusOK || response.Body.String() != "axiom-health" {
		t.Fatalf("unexpected response: %d %q", response.Code, response.Body.String())
	}
}
