package binance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestTestnetPublicClientUsesOnlyCompiledCredentialFreeTestnetHosts(t *testing.T) {
	clock, err := domain.NewReplayClock(time.UnixMilli(1_700_000_000_000).UTC())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewTestnetPublicClientWithMonotonic(clock, func() time.Duration { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedTransport{fixtures: map[string][]byte{
		"/api/v3/depth": fixture(t, "depth-snapshot.json"),
	}}
	client.httpClient = &http.Client{Transport: transport, CheckRedirect: rejectPublicRedirect}
	instrument := approvedBTC(t)
	if _, err = client.Snapshot(context.Background(), exchangecontracts.SnapshotRequest{
		Instrument: instrument, Depth: 100,
	}); err != nil {
		t.Fatal(err)
	}
	for _, request := range transport.requests {
		if request.URL.Hostname() != "testnet.binance.vision" ||
			request.Method != http.MethodGet ||
			request.Header.Get("Authorization") != "" ||
			request.Header.Get("Cookie") != "" ||
			request.Header.Get("X-MBX-APIKEY") != "" {
			t.Fatalf("unsafe Testnet public request: %s %s", request.Method, request.URL)
		}
	}
	connection := &fakeConnection{}
	client.connector = &fakeConnector{connection: connection}
	stream, err := client.SubscribeObserved(context.Background(), exchangecontracts.StreamRequest{
		Instrument: instrument,
		Kinds:      []exchangecontracts.StreamKind{exchangecontracts.StreamDepth},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	if client.connector.(*fakeConnector).target == nil ||
		client.connector.(*fakeConnector).target.Hostname() != "stream.testnet.binance.vision" {
		t.Fatalf("Testnet public stream target=%v", client.connector.(*fakeConnector).target)
	}
}
