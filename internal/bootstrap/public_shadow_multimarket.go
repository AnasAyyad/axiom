package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
)

// publicShadowSagaMarketSource adapts the already-running credential-free public
// collectors to the shared coherent multi-market reader. It performs no REST
// request and has no authenticated exchange capability.
type publicShadowSagaMarketSource struct {
	claimID   string
	exchange  string
	monotonic interface{ MonotonicOffset() uint64 }
	clock     interface {
		ClockHealth() exchangecontracts.ClockHealth
	}
	collectors      map[domain.Instrument]shadowPublicCollector
	metadata        map[domain.Instrument]domain.InstrumentMetadata
	maximumQuantity map[domain.Instrument]domain.Quantity
	feeVersion      string
	feeRate         domain.Rate
	collectorRegion string
}

func newPublicShadowSagaMarketSource(session *ownerConsoleLiveShadowSession) (*publicShadowSagaMarketSource, error) {
	if session == nil || session.claim.ID == "" || session.claim.ExchangeID == "" || session.client == nil ||
		len(session.collectors) != 3 || len(session.metadata) != 3 || len(session.maximumQuantity) != 3 {
		return nil, fmt.Errorf("shadow_saga_market_source_invalid")
	}
	fee, err := publicShadowFeeRate(session.claim.Configuration.Models.Fee)
	if err != nil {
		return nil, err
	}
	clock, ok := session.client.(interface {
		ClockHealth() exchangecontracts.ClockHealth
	})
	if !ok {
		return nil, fmt.Errorf("shadow_saga_market_clock_unavailable")
	}
	return &publicShadowSagaMarketSource{claimID: session.claim.ID, exchange: session.claim.ExchangeID,
		monotonic: session.client, clock: clock, collectors: session.collectors, metadata: session.metadata,
		maximumQuantity: session.maximumQuantity, feeVersion: session.claim.Configuration.Models.Fee,
		feeRate: fee, collectorRegion: "engine-local"}, nil
}

// CaptureSandboxSagaMarketViews returns one complete coherent public generation.
func (source *publicShadowSagaMarketSource) CaptureSandboxSagaMarketViews(
	ctx context.Context,
	keys []runtimecore.MarketKey,
	now time.Time,
) (SandboxSagaMarketViewSet, error) {
	if !source.validCaptureRequest(ctx, keys, now) {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("shadow_saga_market_capture_invalid")
	}
	logical := source.monotonic.MonotonicOffset()
	clock := source.clock.ClockHealth()
	if logical == 0 || !clock.Eligible {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("shadow_saga_market_capture_invalid")
	}
	views, health, firstDetected, err := source.captureShadowSagaViews(keys, clock)
	if err != nil {
		return SandboxSagaMarketViewSet{}, err
	}
	if firstDetected == 0 || firstDetected > logical {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("shadow_saga_market_capture_invalid")
	}
	result := SandboxSagaMarketViewSet{Trigger: runtimecore.AsOfTrigger{
		MonotonicNanos: logical, IngestOrdinal: logical, UTC: now,
	}, FirstDetectedOffset: firstDetected, Members: make([]SandboxSagaMarketMember, 0, len(keys))}
	for _, key := range keys {
		member, memberErr := source.shadowSagaMember(key, views, health)
		if memberErr != nil {
			return SandboxSagaMarketViewSet{}, memberErr
		}
		result.Members = append(result.Members, member)
	}
	return result, nil
}

func (source *publicShadowSagaMarketSource) validCaptureRequest(ctx context.Context,
	keys []runtimecore.MarketKey, now time.Time,
) bool {
	return source != nil && ctx != nil && source.monotonic != nil && source.claimID != "" &&
		source.clock != nil && source.exchange != "" && len(keys) == 3 && len(source.collectors) == 3 &&
		!now.IsZero() && now.Location() == time.UTC
}

func (source *publicShadowSagaMarketSource) captureShadowSagaViews(keys []runtimecore.MarketKey,
	clock exchangecontracts.ClockHealth,
) (
	map[domain.Instrument]marketdata.BookView,
	map[domain.Instrument]exchangecontracts.CollectorHealthSnapshot,
	uint64,
	error,
) {
	views := make(map[domain.Instrument]marketdata.BookView, len(keys))
	health := make(map[domain.Instrument]exchangecontracts.CollectorHealthSnapshot, len(keys))
	firstDetected := uint64(0)
	for _, key := range keys {
		view, snapshot, err := source.captureShadowSagaView(key)
		if err != nil {
			return nil, nil, 0, err
		}
		snapshot.ClockObservedAt = clock.ObservedAt
		snapshot.ClockOffset = clock.Offset
		snapshot.ClockUncertainty = clock.Uncertainty
		snapshot.ClockEligible = clock.Eligible
		snapshot.Eligible = snapshot.BookEligible && clock.Eligible
		views[key.Instrument], health[key.Instrument] = view, snapshot
		if published := view.Observation().PublishedOffsetNanos; published > firstDetected {
			firstDetected = published
		}
	}
	return views, health, firstDetected, nil
}

func (source *publicShadowSagaMarketSource) captureShadowSagaView(key runtimecore.MarketKey) (
	marketdata.BookView, exchangecontracts.CollectorHealthSnapshot, error,
) {
	collector := source.collectors[key.Instrument]
	metadata, metadataFound := source.metadata[key.Instrument]
	_, maximumFound := source.maximumQuantity[key.Instrument]
	view, err := collectorBook(collector, key)
	snapshot := exchangecontracts.CollectorHealthSnapshot{}
	if collector != nil {
		snapshot = collector.HealthSnapshot()
	}
	if err != nil || key.Exchange != source.exchange || !metadataFound || !maximumFound ||
		metadata.Instrument != key.Instrument || snapshot.Exchange != key.Exchange ||
		snapshot.Instrument != key.Instrument.Symbol() || !snapshot.Eligible {
		return marketdata.BookView{}, exchangecontracts.CollectorHealthSnapshot{},
			fmt.Errorf("shadow_saga_market_member_unavailable")
	}
	return view, snapshot, nil
}

func (source *publicShadowSagaMarketSource) shadowSagaMember(key runtimecore.MarketKey,
	views map[domain.Instrument]marketdata.BookView,
	health map[domain.Instrument]exchangecontracts.CollectorHealthSnapshot,
) (SandboxSagaMarketMember, error) {
	view, snapshot := views[key.Instrument], health[key.Instrument]
	fee := arbitrage.FeeSchedule{Version: source.feeVersion, Rate: source.feeRate, Asset: "USDT"}
	if key.Instrument.Quote != "USDT" {
		mark, err := publicShadowSettlementFeeMark(key.Instrument.Quote, views)
		if err != nil {
			return SandboxSagaMarketMember{}, err
		}
		fee.ThirdAssetPriceInQuote = mark
	}
	return SandboxSagaMarketMember{View: view,
		Clock: exchangecontracts.ClockHealth{ObservedAt: snapshot.ClockObservedAt,
			Offset: snapshot.ClockOffset, Uncertainty: snapshot.ClockUncertainty,
			Eligible: snapshot.ClockEligible},
		Rules: arbitrage.InstrumentRules{Exchange: source.exchange, Metadata: source.metadata[key.Instrument],
			MaximumQuantity: source.maximumQuantity[key.Instrument], Fee: fee,
			Active: true, ObservedAt: source.metadata[key.Instrument].EffectiveAt},
		CollectorInstance: "owner_console-shadow-" + source.claimID + "-" + source.exchange,
		CollectorRegion:   source.collectorRegion}, nil
}

func collectorBook(
	collector shadowPublicCollector,
	key runtimecore.MarketKey,
) (marketdata.BookView, error) {
	if collector == nil || collector.Views() == nil {
		return marketdata.BookView{}, fmt.Errorf("shadow_saga_market_member_unavailable")
	}
	return collector.Views().Book(key.Exchange, key.Instrument)
}

// publicShadowSettlementFeeMark returns quote units per USDT using the executable
// USDT market's best ask. Flooring the reciprocal is conservative because the
// downstream fee conversion divides by this mark.
func publicShadowSettlementFeeMark(
	quote domain.AssetSymbol,
	views map[domain.Instrument]marketdata.BookView,
) (domain.Price, error) {
	settlement, _ := domain.ParseAssetSymbol("USDT")
	reference, err := domain.NewSpotInstrument(quote, settlement)
	if err != nil {
		return domain.Price{}, fmt.Errorf("shadow_saga_fee_mark_unavailable")
	}
	view, exists := views[reference]
	if !exists || len(view.Asks()) == 0 {
		return domain.Price{}, fmt.Errorf("shadow_saga_fee_mark_unavailable")
	}
	mark, err := domain.CalculateReciprocalPriceFloor(view.Asks()[0].Price, 18)
	if err != nil {
		return domain.Price{}, fmt.Errorf("shadow_saga_fee_mark_invalid")
	}
	return mark, nil
}

var _ SandboxSagaMarketViewSource = (*publicShadowSagaMarketSource)(nil)
