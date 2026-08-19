package binance

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"
)

type failoverWebsocketConnection struct{}

func (*failoverWebsocketConnection) Receive() ([]byte, error) {
	return nil, errors.New("fixture closed")
}
func (*failoverWebsocketConnection) Close() error { return nil }

func TestPublicWebsocketFailoverUsesOnlyCompiledMarketEndpoints(t *testing.T) {
	target, err := url.Parse(publicWSOrigin + "/stream?streams=btcusdt@depth@100ms/ethusdt@kline_4h")
	if err != nil {
		t.Fatal(err)
	}
	wantConnection := &failoverWebsocketConnection{}
	var hosts []string
	connector := &secureWebsocketConnector{
		endpoints: []publicWebsocketEndpoint{
			{host: "data-stream.binance.vision", origin: "https://data-stream.binance.vision"},
			{host: "stream.binance.com", origin: "https://stream.binance.com"},
		},
		connect: func(_ context.Context, candidate *url.URL, endpoint publicWebsocketEndpoint) (websocketConnection, error) {
			hosts = append(hosts, candidate.Hostname())
			if candidate.Hostname() != endpoint.host {
				t.Fatalf("candidate host=%q endpoint=%q", candidate.Hostname(), endpoint.host)
			}
			if len(hosts) == 1 {
				return nil, errors.New("bounded primary endpoint failure")
			}
			return wantConnection, nil
		},
	}
	connection, err := connector.Connect(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if connection != wantConnection || !reflect.DeepEqual(hosts,
		[]string{"data-stream.binance.vision", "stream.binance.com"}) {
		t.Fatalf("connection=%T hosts=%v", connection, hosts)
	}
}

func TestPublicWebsocketFailoverRejectsNonMarketTargetBeforeNetwork(t *testing.T) {
	target, err := url.Parse(publicWSFallbackOrigin + "/ws/btcusdt@userData")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	connector := newSecureWebsocketConnector()
	connector.connect = func(context.Context, *url.URL, publicWebsocketEndpoint) (websocketConnection, error) {
		calls++
		return nil, errors.New("must not run")
	}
	if _, err = connector.Connect(context.Background(), target); err == nil || calls != 0 {
		t.Fatalf("invalid fallback target reached network: calls=%d err=%v", calls, err)
	}
}

func TestFallbackHostPolicyAcceptsOnlyBoundedPublicMarketStreams(t *testing.T) {
	allowed, _ := url.Parse(publicWSFallbackOrigin + "/ws/btcusdt@depth@100ms")
	if _, err := validateWebSocketTargetForHost("stream.binance.com", allowed); err != nil {
		t.Fatalf("compiled fallback market stream rejected: %v", err)
	}
	denied, _ := url.Parse(publicWSFallbackOrigin + "/ws/btcusdt@userData")
	if _, err := validateWebSocketTargetForHost("stream.binance.com", denied); err == nil {
		t.Fatal("fallback user-data stream accepted")
	}
}
