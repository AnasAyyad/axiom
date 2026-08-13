package binance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

type sandboxMarketData struct {
	snapshots map[string]exchangecontracts.BookSnapshot
	err       error
	calls     int
}

func (source *sandboxMarketData) Snapshot(
	_ context.Context,
	request exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	source.calls++
	if source.err != nil {
		return exchangecontracts.BookSnapshot{}, source.err
	}
	snapshot, found := source.snapshots[request.Instrument.Symbol()]
	if !found {
		return exchangecontracts.BookSnapshot{}, errors.New("sandbox_market_snapshot_missing")
	}
	return snapshot, nil
}

func (*sandboxMarketData) Subscribe(
	context.Context,
	exchangecontracts.StreamRequest,
) (exchangecontracts.Stream, error) {
	return nil, errors.New("sandbox_market_stream_not_configured")
}

func TestSandboxEligibilityUsesCredentialFreeTestnetMarketData(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	privateCalls := 0
	client := sandboxTestClient(t, authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		privateCalls++
		return nil, errors.New("credential_client_not_expected")
	}), now)
	adapter, err := newSandboxAdapterForTest(
		client, sandboxIdentity(now), 1, &sandboxLookup{},
		&sandboxExpectations{}, sandboxRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	market := &sandboxMarketData{snapshots: testnetStartupSnapshots(t)}
	adapter.marketData = market
	eligibilities, err := adapter.StrategyEligibility(context.Background())
	if err != nil || len(eligibilities) != len(approvedSandboxInstruments()) ||
		market.calls != len(approvedSandboxInstruments()) || privateCalls != 0 {
		t.Fatalf("eligibilities=%#v market_calls=%d private_calls=%d error=%v", eligibilities, market.calls, privateCalls, err)
	}
	for index, instrument := range approvedSandboxInstruments() {
		if !eligibilities[index].Eligible || eligibilities[index].Instrument != instrument.Symbol() {
			t.Fatalf("eligibility[%d]=%#v", index, eligibilities[index])
		}
	}
}

func TestSandboxEligibilityRejectsMalformedCredentialFreeBook(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000).UTC()
	client := sandboxTestClient(t, authenticatedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("credential_client_not_expected")
	}), now)
	adapter, err := newSandboxAdapterForTest(
		client, sandboxIdentity(now), 1, &sandboxLookup{},
		&sandboxExpectations{}, sandboxRules(t, now),
	)
	if err != nil {
		t.Fatal(err)
	}
	market := &sandboxMarketData{snapshots: testnetStartupSnapshots(t)}
	broken := market.snapshots["BTCUSDT"]
	broken.Asks[0].Price = broken.Bids[0].Price
	market.snapshots["BTCUSDT"] = broken
	adapter.marketData = market
	if _, err = adapter.StartupEligibility(context.Background()); err == nil {
		t.Fatal("crossed Testnet market snapshot accepted")
	}
}

func testnetStartupSnapshots(t *testing.T) map[string]exchangecontracts.BookSnapshot {
	t.Helper()
	bid, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	ask, err := domain.ParsePrice("101")
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.ParseQuantity("1")
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]exchangecontracts.BookSnapshot, len(approvedSandboxInstruments()))
	for index, instrument := range approvedSandboxInstruments() {
		result[instrument.Symbol()] = exchangecontracts.BookSnapshot{
			Exchange: "binance", Instrument: instrument, LastSequence: uint64(index + 1),
			ReceivedAt: domain.EventTime{UTC: time.UnixMilli(1_700_000_000_000).UTC(), Sequence: uint64(index + 1)},
			Bids:       []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
			Asks:       []exchangecontracts.PriceLevel{{Price: ask, Quantity: quantity}},
		}
	}
	return result
}
