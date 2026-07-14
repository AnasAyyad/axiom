package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"
)

const maximumWebhookResponseBytes = 64 * 1024

// WebhookSink sends a closed, sanitized payload to one explicitly allowlisted
// HTTPS origin. Redirects are rejected so authorization cannot cross origins.
type WebhookSink struct {
	endpoint      *url.URL
	authorization string
	client        *http.Client
}

// NewWebhookSink validates one allowlisted HTTPS destination and isolated client.
func NewWebhookSink(endpoint, authorization string, allowedHosts []string, client *http.Client) (*WebhookSink, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!slices.Contains(allowedHosts, parsed.Host) || len(allowedHosts) == 0 {
		return nil, fmt.Errorf("alert_webhook_endpoint_rejected")
	}
	if authorization != "" && (len(authorization) < 16 || len(authorization) > 4096) {
		return nil, fmt.Errorf("alert_webhook_secret_rejected")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	isolated := *client
	if isolated.Timeout <= 0 || isolated.Timeout > 30*time.Second {
		isolated.Timeout = 10 * time.Second
	}
	isolated.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("alert_webhook_redirect_rejected") }
	copyURL := *parsed
	return &WebhookSink{endpoint: &copyURL, authorization: authorization, client: &isolated}, nil
}

// Name returns the stable durable sink identity.
func (*WebhookSink) Name() string { return "webhook" }

// Deliver posts the closed sanitized alert schema without following redirects.
func (sink *WebhookSink) Deliver(ctx context.Context, alert Alert) error {
	payload := struct {
		SchemaVersion string   `json:"schema_version"`
		AlertID       string   `json:"alert_id"`
		Severity      Severity `json:"severity"`
		ReasonCode    Reason   `json:"reason_code"`
		Component     string   `json:"component"`
		CorrelationID string   `json:"correlation_id"`
		OccurredAt    string   `json:"occurred_at"`
		Occurrences   uint64   `json:"occurrences"`
	}{"axiom.alert.v1", alert.ID, alert.Severity, alert.Reason, alert.Component, alert.CorrelationID, alert.LastSeenAt.Format(time.RFC3339Nano), alert.Occurrences}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("alert_webhook_encode_failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sink.endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("alert_webhook_request_failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "axiom-alert-sink/1")
	if sink.authorization != "" {
		request.Header.Set("Authorization", "Bearer "+sink.authorization)
	}
	response, err := sink.client.Do(request)
	if err != nil {
		return fmt.Errorf("alert_webhook_unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumWebhookResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("alert_webhook_rejected")
	}
	return nil
}
