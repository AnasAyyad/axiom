package binance

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const (
	publicEndpointSet        = "market-data-only-v1"
	testnetPublicEndpointSet = "testnet-market-data-only-v1"
	publicRESTOrigin         = "https://data-api.binance.vision"
	publicWSOrigin           = "wss://data-stream.binance.vision"
	testnetPublicRESTOrigin  = "https://testnet.binance.vision"
	testnetPublicWSOrigin    = "wss://stream.testnet.binance.vision"
)

type publicEndpointSetDefinition struct {
	restOrigin string
	wsOrigin   string
	host       string
}

func publicEndpoints(endpointSet string) (publicEndpointSetDefinition, error) {
	switch endpointSet {
	case publicEndpointSet:
		return publicEndpointSetDefinition{
			restOrigin: publicRESTOrigin, wsOrigin: publicWSOrigin,
			host: "data-api.binance.vision",
		}, nil
	case testnetPublicEndpointSet:
		return publicEndpointSetDefinition{
			restOrigin: testnetPublicRESTOrigin, wsOrigin: testnetPublicWSOrigin,
			host: "testnet.binance.vision",
		}, nil
	default:
		return publicEndpointSetDefinition{}, policyError(exchangecontracts.OperationCapability)
	}
}

type publicRoute string

const (
	routePing         publicRoute = "public_ping"
	routeTime         publicRoute = "public_time"
	routeMetadata     publicRoute = "public_metadata"
	routeDepth        publicRoute = "public_depth"
	routeTrades       publicRoute = "public_trades"
	routeAggregate    publicRoute = "public_aggregate_trades"
	routeCandles      publicRoute = "public_candles"
	routeMarketStream publicRoute = "public_market_stream"
)

var publicRESTPaths = map[string]publicRoute{
	"/api/v3/ping": routePing, "/api/v3/time": routeTime, "/api/v3/exchangeInfo": routeMetadata,
	"/api/v3/depth": routeDepth, "/api/v3/trades": routeTrades,
	"/api/v3/aggTrades": routeAggregate, "/api/v3/klines": routeCandles,
}

var deniedHeaders = []string{"Authorization", "Cookie", "X-Mbx-" + "Apikey"}

func validateRESTTarget(method string, target *url.URL, headers http.Header) (publicRoute, error) {
	return validateRESTTargetForHost("data-api.binance.vision", method, target, headers)
}

func validateRESTTargetForHost(host, method string, target *url.URL, headers http.Header) (publicRoute, error) {
	if method != http.MethodGet || target == nil || target.Scheme != "https" ||
		target.Hostname() != host || (target.Port() != "" && target.Port() != "443") ||
		target.User != nil || target.Fragment != "" || target.RawPath != "" || target.Path != target.EscapedPath() {
		return "", policyError(exchangecontracts.OperationCapability)
	}
	for name, values := range headers {
		canonical := http.CanonicalHeaderKey(name)
		for _, denied := range deniedHeaders {
			if canonical == http.CanonicalHeaderKey(denied) && len(values) != 0 {
				return "", policyError(exchangecontracts.OperationCapability)
			}
		}
	}
	route, ok := publicRESTPaths[target.Path]
	if !ok || !validRouteQuery(route, target.Query()) {
		return "", policyError(exchangecontracts.OperationCapability)
	}
	return route, nil
}

func validRouteQuery(route publicRoute, query url.Values) bool {
	for _, values := range query {
		if len(values) != 1 || values[0] == "" {
			return false
		}
	}
	switch route {
	case routePing, routeTime:
		return len(query) == 0
	case routeMetadata:
		return exactKeys(query, "symbol", "showPermissionSets") && validSymbol(query.Get("symbol")) &&
			query.Get("showPermissionSets") == "false"
	case routeDepth:
		return exactKeys(query, "limit", "symbol") && validSymbol(query.Get("symbol")) &&
			boundedUint(query.Get("limit"), 1, 5000)
	case routeTrades:
		return exactKeys(query, "limit", "symbol") && validSymbol(query.Get("symbol")) &&
			boundedUint(query.Get("limit"), 1, 1000)
	case routeAggregate:
		return validAggregateQuery(query)
	case routeCandles:
		return validCandleQuery(query)
	default:
		return false
	}
}

func validAggregateQuery(query url.Values) bool {
	allowed := map[string]bool{"symbol": true, "fromId": true, "startTime": true, "endTime": true, "limit": true}
	if !onlyKeys(query, allowed) || !validSymbol(query.Get("symbol")) ||
		!boundedUint(query.Get("limit"), 1, 1000) {
		return false
	}
	return optionalUint(query.Get("fromId")) && optionalMillis(query.Get("startTime")) &&
		optionalMillis(query.Get("endTime")) && validRange(query.Get("startTime"), query.Get("endTime"))
}

func validCandleQuery(query url.Values) bool {
	if !exactKeys(query, "endTime", "interval", "limit", "startTime", "symbol", "timeZone") ||
		!validSymbol(query.Get("symbol")) || !supportedCandleInterval(query.Get("interval")) || query.Get("timeZone") != "0" ||
		!boundedUint(query.Get("limit"), 1, 1000) || !optionalMillis(query.Get("startTime")) ||
		!optionalMillis(query.Get("endTime")) {
		return false
	}
	return validRange(query.Get("startTime"), query.Get("endTime"))
}

func validateWebSocketTarget(target *url.URL) (publicRoute, error) {
	return validateWebSocketTargetForHost("data-stream.binance.vision", target)
}

func validateWebSocketTargetForHost(host string, target *url.URL) (publicRoute, error) {
	if target == nil || target.Scheme != "wss" || target.Hostname() != host ||
		(target.Port() != "" && target.Port() != "443") || target.User != nil || target.Fragment != "" ||
		target.RawPath != "" || target.Path != target.EscapedPath() {
		return "", policyError(exchangecontracts.OperationStream)
	}
	query := target.Query()
	if strings.HasPrefix(target.Path, "/ws/") && len(query) == 0 && validStreamName(strings.TrimPrefix(target.Path, "/ws/")) {
		return routeMarketStream, nil
	}
	if target.Path == "/stream" && exactKeys(query, "streams") && len(query["streams"]) == 1 {
		streams := strings.Split(query.Get("streams"), "/")
		if len(streams) == 0 || len(streams) > 6 {
			return "", policyError(exchangecontracts.OperationStream)
		}
		seen := make(map[string]struct{}, len(streams))
		for _, stream := range streams {
			if !validStreamName(stream) {
				return "", policyError(exchangecontracts.OperationStream)
			}
			if _, duplicate := seen[stream]; duplicate {
				return "", policyError(exchangecontracts.OperationStream)
			}
			seen[stream] = struct{}{}
		}
		return routeMarketStream, nil
	}
	return "", policyError(exchangecontracts.OperationStream)
}

func validStreamName(stream string) bool {
	parts := strings.Split(stream, "@")
	if len(parts) < 2 || (parts[0] != "btcusdt" && parts[0] != "ethusdt" && parts[0] != "ethbtc") {
		return false
	}
	suffix := strings.Join(parts[1:], "@")
	switch suffix {
	case "depth", "depth@100ms", "trade", "aggTrade", "bookTicker", "kline_15m", "kline_1h", "kline_4h":
		return true
	default:
		return false
	}
}

func exactKeys(query url.Values, keys ...string) bool {
	if len(query) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, exists := query[key]; !exists {
			return false
		}
	}
	return true
}

func onlyKeys(query url.Values, allowed map[string]bool) bool {
	for key := range query {
		if !allowed[key] {
			return false
		}
	}
	return true
}

func validSymbol(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT" || symbol == "ETHBTC"
}

func boundedUint(value string, minimum, maximum uint64) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && parsed >= minimum && parsed <= maximum && strconv.FormatUint(parsed, 10) == value
}

func optionalUint(value string) bool { return value == "" || boundedUint(value, 1, ^uint64(0)) }

func optionalMillis(value string) bool { return value == "" || boundedUint(value, 1, 1<<62) }

func validRange(start, end string) bool {
	if start == "" || end == "" {
		return true
	}
	left, leftErr := strconv.ParseUint(start, 10, 64)
	right, rightErr := strconv.ParseUint(end, 10, 64)
	return leftErr == nil && rightErr == nil && left <= right
}

func policyError(operation exchangecontracts.Operation) error {
	return exchangecontracts.NewError(exchangecontracts.ErrorCapability, operation, 0)
}

type publicResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

const (
	publicSetupDeadline   = 5 * time.Second
	maximumDialCandidates = 4
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

func newPublicHTTPClientFor(host string) *http.Client {
	dialer := &publicDialer{host: host, resolver: net.DefaultResolver,
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
	if !ok || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
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
