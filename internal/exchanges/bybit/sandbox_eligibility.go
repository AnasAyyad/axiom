package bybit

import (
	"context"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// StartupEligibility proves the fixed Demo clock and the fresh public Spot
// book named by the instrument-scoped observation used for canary admission.
func (adapter *SandboxAdapter) StartupEligibility(
	ctx context.Context,
) (exchangecontracts.CollectorHealthSnapshot, error) {
	items, err := adapter.StrategyEligibility(ctx)
	if err != nil || len(items) == 0 {
		return exchangecontracts.CollectorHealthSnapshot{}, ErrDemoRequest
	}
	return items[0], nil
}

// StrategyEligibility returns one credential-free public-market readiness
// snapshot per approved Demo instrument. It never reads market data through
// the credential-bearing Demo client.
func (adapter *SandboxAdapter) StrategyEligibility(
	ctx context.Context,
) ([]exchangecontracts.CollectorHealthSnapshot, error) {
	if adapter == nil || adapter.client == nil || adapter.marketData == nil {
		return nil, ErrDemoRequest
	}
	if err := adapter.client.ensureDemoClock(ctx); err != nil {
		return nil, err
	}
	now := adapter.client.now().UTC()
	adapter.client.clockMutex.Lock()
	clockObservedAt := adapter.client.clockObservedAt
	clockOffset := adapter.client.clockOffset
	adapter.client.clockMutex.Unlock()
	if now.Location() != time.UTC || clockObservedAt.IsZero() ||
		clockObservedAt.Location() != time.UTC {
		return nil, ErrDemoRequest
	}
	items := make([]exchangecontracts.CollectorHealthSnapshot, 0, len(approvedInstruments()))
	for _, instrument := range approvedInstruments() {
		snapshot, err := adapter.marketData.Snapshot(ctx, exchangecontracts.SnapshotRequest{
			Instrument: instrument, Depth: 200,
		})
		if err != nil {
			return nil, err
		}
		if !validStartupSnapshot(snapshot, instrument) {
			return nil, ErrDemoRequest
		}
		items = append(items, exchangecontracts.CollectorHealthSnapshot{
			ObservedAt: now, Exchange: "bybit", Instrument: instrument.Symbol(),
			BookHealth: "healthy", BookHealthy: true, BookFresh: true,
			BookEligible: true, ClockEligible: true, ClockObservedAt: clockObservedAt,
			ClockOffset: clockOffset, ClockUncertainty: 250 * time.Millisecond,
			Eligible: true,
		})
	}
	return items, nil
}

func validStartupSnapshot(
	snapshot exchangecontracts.BookSnapshot,
	instrument domain.Instrument,
) bool {
	if snapshot.Exchange != "bybit" || snapshot.Instrument != instrument ||
		snapshot.LastSequence == 0 || snapshot.ReceivedAt.Validate() != nil ||
		len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 {
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
