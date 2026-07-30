package bybit

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func newSandboxPublicHTTPClient(proxyURL *url.URL) sandboxDoer {
	publicHost := strings.TrimPrefix(publicRESTOrigin, "https://")
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: publicHost,
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect_rejected")
		},
	}
}

// executePublicUnsigned is the only production-host path reachable from the
// Demo engine. It is credential-free, GET-only, and constrained by the
// existing B1 production-public route policy.
func (client *SandboxClient) executePublicUnsigned(
	ctx context.Context,
	path string,
	query url.Values,
) ([]byte, error) {
	if client == nil || client.publicDoer == nil {
		return nil, ErrDemoRequest
	}
	target, err := url.Parse(publicRESTOrigin + path)
	if err != nil {
		return nil, ErrDemoRequest
	}
	target.RawQuery = query.Encode()
	headers := make(http.Header)
	if _, err = validateRESTTarget(http.MethodGet, target, headers); err != nil {
		return nil, ErrDemoRequest
	}
	if err = client.allowDemoRequest(); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		target.String(),
		nil,
	)
	if err != nil {
		return nil, ErrDemoRequest
	}
	response, err := client.publicDoer.Do(request)
	if err != nil {
		return nil, ErrDemoRequest
	}
	return client.readDemoUnsignedResponse(response)
}
