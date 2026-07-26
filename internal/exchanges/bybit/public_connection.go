package bybit

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type publicResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

const (
	publicSetupDeadline   = 5 * time.Second
	maximumDialCandidates = 4
	publicWriteDeadline   = 2 * time.Second
)

type publicDialer struct {
	host     string
	resolver publicResolver
	dialer   net.Dialer
	dial     func(context.Context, string, string) (net.Conn, error)
}

// DialContext revalidates and pins one exact public DNS result per connection.
func (dialer *publicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	connection, _, err := dialer.dialValidated(ctx, network, address)
	return connection, err
}

func (dialer *publicDialer) dialValidated(ctx context.Context, network, address string) (
	net.Conn, exchangecontracts.FailureMetadata, error,
) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != dialer.host || port != "443" {
		return nil, exchangecontracts.FailureMetadata{}, policyError(exchangecontracts.OperationCapability)
	}
	setupContext, cancel := context.WithTimeout(ctx, publicSetupDeadline)
	defer cancel()
	dnsStarted := time.Now()
	addresses, err := dialer.resolver.LookupIPAddr(setupContext, host)
	metadata := exchangecontracts.FailureMetadata{DNSDuration: time.Since(dnsStarted), SetupStage: "dns"}
	if err != nil || len(addresses) == 0 {
		return nil, metadata, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationCapability, 0, 0, "dns_failure", metadata)
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return nil, metadata, policyError(exchangecontracts.OperationCapability)
		}
	}
	candidates := orderPublicCandidates(addresses)
	metadata.CandidateCount = uint32(len(candidates))
	return dialer.dialCandidates(setupContext, network, port, candidates, metadata)
}

func (dialer *publicDialer) dialCandidates(
	setupContext context.Context,
	network, port string,
	candidates []net.IPAddr,
	metadata exchangecontracts.FailureMetadata,
) (net.Conn, exchangecontracts.FailureMetadata, error) {
	dialContext := dialer.dial
	if dialContext == nil {
		dialContext = dialer.dialer.DialContext
	}
	tcpStarted := time.Now()
	for index, candidate := range candidates {
		metadata.AttemptCount = uint32(index + 1)
		metadata.AddressFamily = addressFamily(candidate.IP)
		remaining := time.Until(deadlineOf(setupContext))
		if remaining <= 0 {
			break
		}
		attemptBudget := min(publicSetupDeadline/maximumDialCandidates, remaining)
		attemptContext, attemptCancel := context.WithTimeout(setupContext, attemptBudget)
		connection, dialErr := dialContext(attemptContext, network,
			net.JoinHostPort(candidate.IP.String(), port))
		attemptCancel()
		if dialErr == nil && connection != nil {
			metadata.TCPDuration, metadata.SetupStage = time.Since(tcpStarted), "tcp"
			return connection, metadata, nil
		}
		if connection != nil {
			_ = connection.Close()
		}
	}
	metadata.TCPDuration, metadata.SetupStage = time.Since(tcpStarted), "tcp"
	cause := "tcp_connect_failure"
	if errors.Is(setupContext.Err(), context.DeadlineExceeded) {
		cause = "network_timeout"
	}
	return nil, metadata, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
		exchangecontracts.OperationCapability, 0, 0, cause, metadata)
}

func deadlineOf(ctx context.Context) time.Time {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Now().Add(publicSetupDeadline)
	}
	return deadline
}

func orderPublicCandidates(addresses []net.IPAddr) []net.IPAddr {
	var ipv4, ipv6 []net.IPAddr
	for _, address := range addresses {
		if address.IP.To4() != nil {
			ipv4 = append(ipv4, address)
		} else {
			ipv6 = append(ipv6, address)
		}
	}
	ordered := make([]net.IPAddr, 0, min(len(addresses), maximumDialCandidates))
	preferIPv6 := len(addresses) != 0 && addresses[0].IP.To4() == nil
	for len(ordered) < maximumDialCandidates && (len(ipv4) != 0 || len(ipv6) != 0) {
		if preferIPv6 && len(ipv6) != 0 {
			ordered, ipv6 = append(ordered, ipv6[0]), ipv6[1:]
		} else if !preferIPv6 && len(ipv4) != 0 {
			ordered, ipv4 = append(ordered, ipv4[0]), ipv4[1:]
		} else if len(ipv6) != 0 {
			ordered, ipv6 = append(ordered, ipv6[0]), ipv6[1:]
		} else {
			ordered, ipv4 = append(ordered, ipv4[0]), ipv4[1:]
		}
		preferIPv6 = !preferIPv6
	}
	return ordered
}

func addressFamily(ip net.IP) string {
	if ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
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
