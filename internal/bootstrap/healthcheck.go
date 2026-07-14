package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

func validateHealthURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return fmt.Errorf("healthcheck_url_rejected")
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if host != "localhost" && (address == nil || !address.IsLoopback()) {
		return fmt.Errorf("healthcheck_url_rejected")
	}
	if parsed.Path != "/health/live" && parsed.Path != "/health/ready" {
		return fmt.Errorf("healthcheck_url_rejected")
	}
	return nil
}

func runHealthcheck(ctx context.Context, target string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("healthcheck_redirect_rejected")
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("healthcheck_request_invalid")
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("healthcheck_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck_unhealthy")
	}
	return nil
}
