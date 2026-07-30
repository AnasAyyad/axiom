package binance

import (
	"context"
	"net/url"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// StartupEligibility proves the fixed Testnet clock, approved filters, and a
// non-crossed public Spot book before the engine may reach READY_PAUSED.
func (adapter *SandboxAdapter) StartupEligibility(
	ctx context.Context,
) (exchangecontracts.CollectorHealthSnapshot, error) {
	if adapter == nil || adapter.client == nil {
		return exchangecontracts.CollectorHealthSnapshot{},
			ErrSandboxRequest
	}
	if err := adapter.client.ensureClock(ctx); err != nil {
		return exchangecontracts.CollectorHealthSnapshot{}, err
	}
	for _, instrument := range approvedSandboxInstruments() {
		if err := adapter.client.validateStartupBook(ctx, instrument); err != nil {
			return exchangecontracts.CollectorHealthSnapshot{}, err
		}
	}
	now := adapter.client.now().UTC()
	clock := adapter.client.clock.Health()
	result := exchangecontracts.CollectorHealthSnapshot{
		ObservedAt:       now,
		Exchange:         "binance",
		Instrument:       approvedSandboxInstruments()[0].Symbol(),
		BookHealth:       "healthy",
		BookHealthy:      true,
		BookFresh:        true,
		BookEligible:     true,
		ClockEligible:    clock.Eligible,
		ClockObservedAt:  clock.ObservedAt,
		ClockOffset:      clock.Offset,
		ClockUncertainty: clock.Uncertainty,
		Eligible:         clock.Eligible,
	}
	if !result.Eligible || result.ObservedAt.Location() != time.UTC {
		return exchangecontracts.CollectorHealthSnapshot{},
			ErrSandboxRequest
	}
	return result, nil
}

func (client *SandboxClient) validateStartupBook(
	ctx context.Context,
	instrument domain.Instrument,
) error {
	body, err := client.executeUnsigned(
		ctx,
		"/api/v3/depth",
		url.Values{
			"limit":  {"5"},
			"symbol": {instrument.Symbol()},
		},
	)
	if err != nil {
		return err
	}
	var native struct {
		LastUpdateID uint64     `json:"lastUpdateId"`
		Bids         [][]string `json:"bids"`
		Asks         [][]string `json:"asks"`
	}
	if strictDecode(body, &native) != nil ||
		native.LastUpdateID == 0 ||
		!validTopOfBook(native.Bids, native.Asks) {
		return ErrSandboxRequest
	}
	return nil
}

func validTopOfBook(bids, asks [][]string) bool {
	if len(bids) == 0 || len(asks) == 0 ||
		len(bids[0]) != 2 || len(asks[0]) != 2 {
		return false
	}
	bid, bidErr := domain.ParsePrice(bids[0][0])
	ask, askErr := domain.ParsePrice(asks[0][0])
	bidQty, bidQtyErr := domain.ParseQuantity(bids[0][1])
	askQty, askQtyErr := domain.ParseQuantity(asks[0][1])
	zeroPrice, _ := domain.ParsePrice("0")
	zeroQty, _ := domain.ParseQuantity("0")
	return bidErr == nil && askErr == nil &&
		bidQtyErr == nil && askQtyErr == nil &&
		bid.Compare(zeroPrice) > 0 &&
		ask.Compare(bid) > 0 &&
		bidQty.Compare(zeroQty) > 0 &&
		askQty.Compare(zeroQty) > 0
}
