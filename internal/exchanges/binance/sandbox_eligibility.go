package binance

import (
	"context"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// StartupEligibility proves the fixed Testnet clock, approved filters, and a
// non-crossed public Spot book before the engine may reach READY_PAUSED.
func (adapter *SandboxAdapter) StartupEligibility(
	ctx context.Context,
) (exchangecontracts.CollectorHealthSnapshot, error) {
	items, err := adapter.StrategyEligibility(ctx)
	if err != nil || len(items) == 0 {
		return exchangecontracts.CollectorHealthSnapshot{}, ErrSandboxRequest
	}
	return items[0], nil
}

// StrategyEligibility returns one credential-free public-market readiness
// snapshot per supported sandbox instrument.  Each snapshot is persisted
// separately so a BTC check can never authorize an ETH session.
func (adapter *SandboxAdapter) StrategyEligibility(
	ctx context.Context,
) ([]exchangecontracts.CollectorHealthSnapshot, error) {
	if adapter == nil || adapter.client == nil || adapter.marketData == nil {
		return nil,
			ErrSandboxRequest
	}
	if err := adapter.client.ensureClock(ctx); err != nil {
		return nil, err
	}
	now := adapter.client.now().UTC()
	clock := adapter.client.clock.Health()
	if !clock.Eligible || now.Location() != time.UTC {
		return nil, ErrSandboxRequest
	}
	items := make([]exchangecontracts.CollectorHealthSnapshot, 0, len(approvedSandboxInstruments()))
	for _, instrument := range approvedSandboxInstruments() {
		snapshot, err := adapter.marketData.Snapshot(ctx, exchangecontracts.SnapshotRequest{
			Instrument: instrument, Depth: 100,
		})
		if err != nil {
			return nil, err
		}
		if !validStartupSnapshot(snapshot, instrument) {
			return nil, ErrSandboxRequest
		}
		items = append(items, exchangecontracts.CollectorHealthSnapshot{
			ObservedAt: now, Exchange: "binance", Instrument: instrument.Symbol(),
			BookHealth: "healthy", BookHealthy: true, BookFresh: true,
			BookEligible: true, ClockEligible: true, ClockObservedAt: clock.ObservedAt,
			ClockOffset: clock.Offset, ClockUncertainty: clock.Uncertainty, Eligible: true,
		})
	}
	return items, nil
}

func validStartupSnapshot(
	snapshot exchangecontracts.BookSnapshot,
	instrument domain.Instrument,
) bool {
	if snapshot.Exchange != "binance" || snapshot.Instrument != instrument ||
		snapshot.LastSequence == 0 || snapshot.ReceivedAt.Validate() != nil || len(snapshot.Bids) == 0 ||
		len(snapshot.Asks) == 0 {
		return false
	}
	bid, ask := snapshot.Bids[0], snapshot.Asks[0]
	zeroPrice, _ := domain.ParsePrice("0")
	zeroQty, _ := domain.ParseQuantity("0")
	return bid.Price.Compare(zeroPrice) > 0 &&
		ask.Price.Compare(bid.Price) > 0 &&
		bid.Quantity.Compare(zeroQty) > 0 &&
		ask.Quantity.Compare(zeroQty) > 0
}
