package egressproxy

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type fixedResolver struct {
	addresses []netip.Addr
	err       error
}

func (resolver fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), resolver.addresses...), resolver.err
}

type recordingDialer struct{ address string }

func (dialer *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	dialer.address = address
	return nil, proxyError("test_stop")
}

func TestPoliciesExposeOnlyExactReviewedHosts(t *testing.T) {
	binance, err := Hosts(PolicyBinanceTestnet)
	if err != nil || strings.Join(binance, ",") !=
		"stream.testnet.binance.vision,testnet.binance.vision,ws-api.testnet.binance.vision" {
		t.Fatalf("Binance hosts = %#v, %v", binance, err)
	}
	bybit, err := Hosts(PolicyBybitDemo)
	if err != nil || strings.Join(bybit, ",") !=
		"api-demo.bybit.com,api.bybit.com,stream-demo.bybit.com,stream.bybit.com" {
		t.Fatalf("Bybit hosts = %#v, %v", bybit, err)
	}
}

func TestValidatedTargetRejectsPortsSuffixesIPAndURLTricks(t *testing.T) {
	valid := requestFor("testnet.binance.vision:443")
	if host, err := validatedTarget(PolicyBinanceTestnet, valid); err != nil ||
		host != "testnet.binance.vision" {
		t.Fatalf("valid target = %q, %v", host, err)
	}
	for _, target := range []string{
		"testnet.binance.vision:80",
		"api.binance.com:443",
		"testnet.binance.vision.example:443",
		"127.0.0.1:443",
		"[::1]:443",
		"TESTNET.BINANCE.VISION:443",
		"testnet.binance.vision:444",
	} {
		if _, err := validatedTarget(PolicyBinanceTestnet, requestFor(target)); err == nil {
			t.Fatalf("target %q accepted", target)
		}
	}
	trick := requestFor("testnet.binance.vision:443")
	trick.URL.RawQuery = "next=api.binance.com"
	if _, err := validatedTarget(PolicyBinanceTestnet, trick); err == nil {
		t.Fatal("query-bearing CONNECT target accepted")
	}
}

func TestResolutionRejectsEveryNonPublicOrMixedAnswerAndDialsValidatedIP(t *testing.T) {
	rejected := [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("10.0.0.1")},
		{netip.MustParseAddr("169.254.169.254")},
		{netip.MustParseAddr("224.0.0.1")},
		{netip.MustParseAddr("::1")},
		{netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("10.0.0.1")},
	}
	for _, addresses := range rejected {
		server, err := newWithNetwork(PolicyBinanceTestnet, fixedResolver{addresses: addresses}, &recordingDialer{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = server.resolve(context.Background(), "testnet.binance.vision"); err == nil {
			t.Fatalf("addresses %#v accepted", addresses)
		}
	}
	dialer := &recordingDialer{}
	server, err := newWithNetwork(PolicyBinanceTestnet,
		fixedResolver{addresses: []netip.Addr{
			netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1"),
		}}, dialer)
	if err != nil {
		t.Fatal(err)
	}
	address, err := server.resolve(context.Background(), "testnet.binance.vision")
	if err != nil || address != "1.1.1.1:443" {
		t.Fatalf("resolved address = %q, %v", address, err)
	}
}

func requestFor(target string) *http.Request {
	request, _ := http.NewRequest(http.MethodConnect, "http://"+target, nil)
	request.Host = target
	request.RequestURI = target
	request.URL.Path = ""
	return request
}
