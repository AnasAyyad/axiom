package bybit

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type publicResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type publicDialer struct {
	host     string
	resolver publicResolver
	dialer   net.Dialer
}

// DialContext pins one validated public DNS answer for a single connection.
func (dialer *publicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != dialer.host || port != "443" {
		return nil, policyError(exchangecontracts.OperationCapability)
	}
	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationCapability, 0)
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return nil, policyError(exchangecontracts.OperationCapability)
		}
	}
	return dialer.dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func newPublicHTTPClient() *http.Client {
	dialer := &publicDialer{host: "api.bybit.com", resolver: net.DefaultResolver,
		dialer: net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second, IdleConnTimeout: 60 * time.Second,
		MaxIdleConns: 8, MaxIdleConnsPerHost: 4, DisableCompression: true}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: rejectPublicRedirect}
}

func rejectPublicRedirect(_ *http.Request, _ []*http.Request) error {
	return policyError(exchangecontracts.OperationCapability)
}

var rejectedPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32"),
}

func publicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() {
		return false
	}
	address = address.Unmap()
	for _, prefix := range rejectedPublicRanges {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func websocketTLS(ctx context.Context, target *url.URL) (net.Conn, error) {
	if _, err := validateWebSocketTarget(target); err != nil {
		return nil, err
	}
	dialer := &publicDialer{host: "stream.bybit.com", resolver: net.DefaultResolver,
		dialer: net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}}
	raw, err := dialer.DialContext(ctx, "tcp", "stream.bybit.com:443")
	if err != nil {
		return nil, err
	}
	tlsConnection := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "stream.bybit.com"})
	if err = tlsConnection.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0)
	}
	return tlsConnection, nil
}
