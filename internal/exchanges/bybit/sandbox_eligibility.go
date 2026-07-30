package bybit

import (
	"context"
	"net/url"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// StartupEligibility proves the fixed Demo clock and the fresh public Spot
// book named by the instrument-scoped observation used for canary admission.
func (adapter *SandboxAdapter) StartupEligibility(
	ctx context.Context,
) (exchangecontracts.CollectorHealthSnapshot, error) {
	if adapter == nil || adapter.client == nil {
		return exchangecontracts.CollectorHealthSnapshot{}, ErrDemoRequest
	}
	if err := adapter.client.ensureDemoClock(ctx); err != nil {
		return exchangecontracts.CollectorHealthSnapshot{}, err
	}
	instrument := approvedInstruments()[0]
	if err := adapter.client.validateStartupBook(ctx, instrument); err != nil {
		return exchangecontracts.CollectorHealthSnapshot{}, err
	}
	now := adapter.client.now().UTC()
	adapter.client.clockMutex.Lock()
	clockObservedAt := adapter.client.clockObservedAt
	clockOffset := adapter.client.clockOffset
	adapter.client.clockMutex.Unlock()
	result := exchangecontracts.CollectorHealthSnapshot{
		ObservedAt:       now,
		Exchange:         "bybit",
		Instrument:       instrument.Symbol(),
		BookHealth:       "healthy",
		BookHealthy:      true,
		BookFresh:        true,
		BookEligible:     true,
		ClockEligible:    true,
		ClockObservedAt:  clockObservedAt,
		ClockOffset:      clockOffset,
		ClockUncertainty: 250 * time.Millisecond,
		Eligible:         true,
	}
	if result.ClockObservedAt.IsZero() ||
		result.ClockObservedAt.Location() != time.UTC {
		return exchangecontracts.CollectorHealthSnapshot{}, ErrDemoRequest
	}
	return result, nil
}

func (client *SandboxClient) validateStartupBook(
	ctx context.Context,
	instrument domain.Instrument,
) error {
	body, err := client.executePublicUnsigned(
		ctx,
		"/v5/market/orderbook",
		url.Values{
			"category": {"spot"},
			"limit":    {"1"},
			"symbol":   {instrument.Symbol()},
		},
	)
	if err != nil {
		return err
	}
	var envelope responseEnvelope[orderBookResult]
	if strictDecode(body, &envelope) != nil ||
		envelope.RetCode != 0 ||
		envelope.RetMsg != "OK" ||
		envelope.Time <= 0 {
		return ErrDemoRequest
	}
	result := envelope.Result
	serverAt := time.UnixMilli(envelope.Time).UTC()
	bookAt := time.UnixMilli(result.Timestamp).UTC()
	if result.Symbol != instrument.Symbol() ||
		result.UpdateID == 0 ||
		result.CrossSequence == 0 ||
		result.Timestamp <= 0 ||
		result.MatchingTime <= 0 ||
		bookAt.After(serverAt.Add(time.Second)) ||
		serverAt.Sub(bookAt) > 2*time.Second ||
		!validDemoTopOfBook(result.Bids, result.Asks) {
		return ErrDemoRequest
	}
	return nil
}

func validDemoTopOfBook(bids, asks [][]string) bool {
	if len(bids) != 1 || len(asks) != 1 ||
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
