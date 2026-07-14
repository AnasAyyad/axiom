package alerting

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestWebhookDeliversOnlySanitizedContract(t *testing.T) {
	const token = "webhook-canary-secret-123456"
	var body, authorization string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(request.Body)
		body, authorization = string(data), request.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	sink, err := NewWebhookSink("https://alerts.example.invalid/v1/axiom", token, []string{"alerts.example.invalid"}, client)
	if err != nil {
		t.Fatal(err)
	}
	alert := Alert{ID: "alert_1", Severity: SeverityCritical, Reason: ReasonBookUnhealthy, Component: "book", CorrelationID: "correlation-1", LastSeenAt: time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC), Occurrences: 1}
	if err := sink.Deliver(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, token) || authorization != "Bearer "+token || !strings.Contains(body, `"reason_code":"book_unhealthy"`) {
		t.Fatalf("unsafe webhook body=%s auth=%q", body, authorization)
	}
}

func TestWebhookRejectsUnsafeDestination(t *testing.T) {
	for _, endpoint := range []string{
		"http://alerts.example.invalid/hook", "https://user:pass@alerts.example.invalid/hook",
		"https://alerts.example.invalid/hook?token=value", "https://other.example.invalid/hook",
	} {
		if _, err := NewWebhookSink(endpoint, "", []string{"alerts.example.invalid"}, nil); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
}
