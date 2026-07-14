package binance

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func TestPublicRESTPolicyAcceptsOnlyCompiledRoutes(t *testing.T) {
	allowed := []string{
		publicRESTOrigin + "/api/v3/ping",
		publicRESTOrigin + "/api/v3/time",
		publicRESTOrigin + "/api/v3/exchangeInfo?showPermissionSets=false&symbol=BTCUSDT",
		publicRESTOrigin + "/api/v3/depth?limit=1000&symbol=ETHUSDT",
		publicRESTOrigin + "/api/v3/trades?limit=100&symbol=BTCUSDT",
		publicRESTOrigin + "/api/v3/aggTrades?endTime=2&limit=100&startTime=1&symbol=BTCUSDT",
		publicRESTOrigin + "/api/v3/klines?endTime=2&interval=4h&limit=100&startTime=1&symbol=ETHUSDT&timeZone=0",
	}
	for _, raw := range allowed {
		target, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = validateRESTTarget(http.MethodGet, target, nil); err != nil {
			t.Fatalf("allowed target rejected: %s: %v", raw, err)
		}
	}
}

func TestPublicRESTPolicyRejectsNearMissesBeforeNetwork(t *testing.T) {
	denied := []string{
		"http://data-api.binance.vision/api/v3/time",
		"https://data-api.binance.vision.evil.invalid/api/v3/time",
		"https://127.0.0.1/api/v3/time",
		publicRESTOrigin + "/api/v3/account",
		publicRESTOrigin + "/api/v3/order",
		publicRESTOrigin + "/api/v3/depth?limit=1000&symbol=BNBUSDT",
		publicRESTOrigin + "/api/v3/depth?limit=1000&limit=5000&symbol=BTCUSDT",
		publicRESTOrigin + "/api/v3/klines?endTime=1&interval=1m&limit=100&startTime=2&symbol=BTCUSDT&timeZone=0",
		publicRESTOrigin + "/api/v3/%2e%2e/account",
	}
	for _, raw := range denied {
		target, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if _, err = validateRESTTarget(http.MethodGet, target, nil); err == nil {
			t.Fatalf("denied target accepted: %s", raw)
		}
	}
	target, _ := url.Parse(publicRESTOrigin + "/api/v3/time")
	for _, headers := range []http.Header{{"Authorization": {"canary"}}, {"Cookie": {"canary"}}, {"X-MBX-APIKEY": {"canary"}}} {
		if _, err := validateRESTTarget(http.MethodGet, target, headers); err == nil {
			t.Fatalf("credential-bearing request accepted: %v", headers)
		}
	}
}

func TestPublicWebSocketPolicyAcceptsBoundedMarketStreams(t *testing.T) {
	allowed := []string{
		publicWSOrigin + "/ws/btcusdt@depth@100ms",
		publicWSOrigin + "/ws/ethusdt@trade",
		publicWSOrigin + "/stream?streams=btcusdt@depth@100ms/ethusdt@kline_4h",
	}
	for _, raw := range allowed {
		target, _ := url.Parse(raw)
		if _, err := validateWebSocketTarget(target); err != nil {
			t.Fatalf("allowed stream rejected: %s: %v", raw, err)
		}
	}
	denied := []string{
		"ws://data-stream.binance.vision/ws/btcusdt@depth",
		publicWSOrigin + "/ws/btcusdt@userData",
		publicWSOrigin + "/ws/listen-key",
		publicWSOrigin + "/stream?streams=btcusdt@depth/btcusdt@depth",
		publicWSOrigin + "/stream?streams=btcusdt@depth&streams=ethusdt@depth",
		"wss://stream.binance.com/ws/btcusdt@depth",
	}
	for _, raw := range denied {
		target, _ := url.Parse(raw)
		if _, err := validateWebSocketTarget(target); err == nil {
			t.Fatalf("denied stream accepted: %s", raw)
		}
	}
}

func TestPublicDialerRejectsAnyNonPublicResolution(t *testing.T) {
	resolver := staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("127.0.0.1")}}}
	dialer := &publicDialer{host: "data-api.binance.vision", resolver: resolver}
	if _, err := dialer.DialContext(context.Background(), "tcp", "data-api.binance.vision:443"); err == nil {
		t.Fatal("mixed public/private resolution accepted")
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "192.0.2.1", "::1", "2001:db8::1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("non-public address accepted: %s", raw)
		}
	}
	if !publicIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("public address rejected")
	}
}

type staticResolver struct{ addresses []net.IPAddr }

func (resolver staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.addresses, nil
}
