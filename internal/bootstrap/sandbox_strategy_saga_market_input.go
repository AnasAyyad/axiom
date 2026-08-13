package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

// SandboxSagaMarketMember is one credential-free collector view plus its
// public clock and immutable filter/fee facts. It contains no account data,
// adapter, endpoint override, or order capability.
type SandboxSagaMarketMember struct {
	View              marketdata.BookView
	Clock             exchangecontracts.ClockHealth
	Rules             arbitrage.InstrumentRules
	CollectorInstance string
	CollectorRegion   string
}

// SandboxSagaMarketViewSet is one atomic collector capture. Trigger is the
// deterministic as-of boundary; FirstDetectedOffset starts the short candidate
// lifetime and may never be moved backwards by the strategy runtime.
type SandboxSagaMarketViewSet struct {
	Trigger             runtimecore.AsOfTrigger
	FirstDetectedOffset uint64
	Members             []SandboxSagaMarketMember
}

// SandboxSagaMarketViewSource supplies already normalized public collector
// views. Implementations may be in-process collectors or a verified local
// projection, but must not be authenticated exchange adapters.
type SandboxSagaMarketViewSource interface {
	CaptureSandboxSagaMarketViews(
		context.Context,
		[]runtimecore.MarketKey,
		time.Time,
	) (SandboxSagaMarketViewSet, error)
}

// SandboxTriangularMarketInput binds one coherent three-market generation.
type SandboxTriangularMarketInput struct {
	Markets              []triangular.MarketInput
	Trigger              runtimecore.AsOfTrigger
	FirstDetectedOffset  uint64
	CoherentViewID       string
	InstrumentMetadataID string
}

// SandboxCrossExchangeMarketInput binds one coherent two-venue generation.
type SandboxCrossExchangeMarketInput struct {
	Markets                   []crossarb.MarketInput
	Coherent                  crossarb.CoherentViewInput
	Trigger                   runtimecore.AsOfTrigger
	InstrumentMetadataSetHash string
}

// RiskMarket returns the exact primary-instrument public view used by the
// account-level central-risk projector. Arbitrage strategies intentionally
// carry no fabricated candle series.
func (input SandboxTriangularMarketInput) RiskMarket(
	work sandbox.StrategySessionWork,
) (sandbox.StrategyMarketInput, error) {
	if work.Strategy != sandbox.StrategyTriangular {
		return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_triangular_risk_market_invalid")
	}
	return sandboxSagaRiskMarket(input.Markets, work, input.Trigger)
}

// RiskMarket selects one venue's exact side of the coherent pair for its
// independent account valuation. It does not merge balances or peer leases.
func (input SandboxCrossExchangeMarketInput) RiskMarket(
	work sandbox.StrategySessionWork,
) (sandbox.StrategyMarketInput, error) {
	if work.Strategy != sandbox.StrategyCrossExchangeArbitrage {
		return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_cross_exchange_risk_market_invalid")
	}
	markets := make([]triangular.MarketInput, 0, len(input.Markets))
	for _, market := range input.Markets {
		markets = append(markets, triangular.MarketInput{Snapshot: market.Snapshot,
			Observation: market.Observation, Rules: market.Rules})
	}
	return sandboxSagaRiskMarket(markets, work, input.Trigger)
}

// SandboxSagaMarketInputReader verifies synchronized public books before a
// strategy can construct a candidate. All coherence and clock checks happen
// against the same immutable capture.
type SandboxSagaMarketInputReader struct{ source SandboxSagaMarketViewSource }

// NewSandboxSagaMarketInputReader validates coherent cached market views.
func NewSandboxSagaMarketInputReader(
	source SandboxSagaMarketViewSource,
) (*SandboxSagaMarketInputReader, error) {
	if source == nil {
		return nil, fmt.Errorf("sandbox_saga_market_input_reader_invalid")
	}
	return &SandboxSagaMarketInputReader{source: source}, nil
}

// ReadTriangular constructs an exchange-local triangular input.
func (reader *SandboxSagaMarketInputReader) ReadTriangular(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (SandboxTriangularMarketInput, error) {
	if reader == nil || reader.source == nil || ctx == nil || work.ValidAt(now) != nil ||
		work.Strategy != sandbox.StrategyTriangular {
		return SandboxTriangularMarketInput{}, fmt.Errorf("sandbox_triangular_market_input_invalid")
	}
	keys := make([]runtimecore.MarketKey, 0, 3)
	for _, symbol := range []string{"BTCUSDT", "ETHBTC", "ETHUSDT"} {
		instrument, err := sandboxSagaInstrument(symbol)
		if err != nil {
			return SandboxTriangularMarketInput{}, fmt.Errorf("sandbox_triangular_market_input_invalid")
		}
		keys = append(keys, runtimecore.MarketKey{Exchange: string(work.Account.Exchange), Instrument: instrument})
	}
	capture, err := reader.captureTriangular(ctx, keys, now, triangular.DefaultConfiguration().MaximumBookAge)
	if err != nil {
		return SandboxTriangularMarketInput{}, fmt.Errorf("sandbox_triangular_market_input_unavailable")
	}
	markets := make([]triangular.MarketInput, 0, len(capture.members))
	for _, member := range capture.members {
		markets = append(markets, triangular.MarketInput{Snapshot: member.snapshot,
			Observation: member.view.Observation(), Rules: member.rules})
	}
	metadataID := sandboxSagaMarketEvidenceHash(capture.coherent.Identity(), capture.rules)
	return SandboxTriangularMarketInput{Markets: markets, Trigger: capture.trigger,
		FirstDetectedOffset: capture.firstDetected, CoherentViewID: capture.coherent.Identity(),
		InstrumentMetadataID: metadataID}, nil
}

// ReadCrossExchange constructs a paired Binance and Bybit input.
func (reader *SandboxSagaMarketInputReader) ReadCrossExchange(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (SandboxCrossExchangeMarketInput, error) {
	if reader == nil || reader.source == nil || ctx == nil || work.ValidAt(now) != nil ||
		work.Strategy != sandbox.StrategyCrossExchangeArbitrage ||
		work.Account.Exchange != sandbox.ExchangeBinance {
		return SandboxCrossExchangeMarketInput{}, fmt.Errorf("sandbox_cross_exchange_market_input_invalid")
	}
	instrument, err := sandboxSagaInstrument(work.Instrument)
	if err != nil {
		return SandboxCrossExchangeMarketInput{}, fmt.Errorf("sandbox_cross_exchange_market_input_invalid")
	}
	keys := []runtimecore.MarketKey{
		{Exchange: string(sandbox.ExchangeBinance), Instrument: instrument},
		{Exchange: string(sandbox.ExchangeBybit), Instrument: instrument},
	}
	capture, err := reader.capture(ctx, keys, now)
	if err != nil {
		return SandboxCrossExchangeMarketInput{}, fmt.Errorf("sandbox_cross_exchange_market_input_unavailable")
	}
	markets := make([]crossarb.MarketInput, 0, len(capture.members))
	for _, member := range capture.members {
		markets = append(markets, crossarb.MarketInput{Snapshot: member.snapshot,
			Observation: member.view.Observation(), Rules: member.rules})
	}
	view := capture.coherent
	return SandboxCrossExchangeMarketInput{Markets: markets,
		Coherent: crossarb.CoherentViewInput{Identity: view.Identity(), Policy: view.Policy(),
			Trigger: view.Trigger(), Members: view.Members()}, Trigger: capture.trigger,
		InstrumentMetadataSetHash: sandboxSagaMarketEvidenceHash(view.Identity(), capture.rules)}, nil
}

type validatedSandboxSagaMarketCapture struct {
	trigger       runtimecore.AsOfTrigger
	firstDetected uint64
	coherent      runtimecore.CoherentView
	members       []validatedSandboxSagaMarketMember
	rules         []arbitrage.InstrumentRules
}

type validatedSandboxSagaMarketMember struct {
	view     marketdata.BookView
	snapshot exchangecontracts.BookSnapshot
	rules    arbitrage.InstrumentRules
}

func (reader *SandboxSagaMarketInputReader) capture(
	ctx context.Context,
	keys []runtimecore.MarketKey,
	now time.Time,
) (validatedSandboxSagaMarketCapture, error) {
	policy := runtimecore.InitialCoherentMarketDataCoherentPolicy()
	return reader.captureWithPolicy(ctx, keys, now, policy.MaximumBookAge, false)
}

func (reader *SandboxSagaMarketInputReader) captureTriangular(
	ctx context.Context,
	keys []runtimecore.MarketKey,
	now time.Time,
	maximumBookAge time.Duration,
) (validatedSandboxSagaMarketCapture, error) {
	return reader.captureWithPolicy(ctx, keys, now, maximumBookAge, true)
}

func (reader *SandboxSagaMarketInputReader) captureWithPolicy(
	ctx context.Context,
	keys []runtimecore.MarketKey,
	now time.Time,
	maximumBookAge time.Duration,
	sameExchangeTriangular bool,
) (validatedSandboxSagaMarketCapture, error) {
	requested := append([]runtimecore.MarketKey(nil), keys...)
	sort.Slice(requested, func(left, right int) bool {
		if requested[left].Exchange != requested[right].Exchange {
			return requested[left].Exchange < requested[right].Exchange
		}
		return requested[left].Instrument.Symbol() < requested[right].Instrument.Symbol()
	})
	set, err := reader.source.CaptureSandboxSagaMarketViews(ctx, requested, now)
	if err != nil || !validSandboxSagaCaptureSet(set, requested, now, maximumBookAge) {
		return validatedSandboxSagaMarketCapture{}, fmt.Errorf("sandbox_saga_market_capture_invalid")
	}
	views := runtimecore.NewMarketViews()
	validated := make([]validatedSandboxSagaMarketMember, 0, len(requested))
	rules := make([]arbitrage.InstrumentRules, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, member := range set.Members {
		item, memberErr := validateSandboxSagaCaptureMember(
			views, requested, set.Trigger, member, seen, now, maximumBookAge)
		if memberErr != nil {
			return validatedSandboxSagaMarketCapture{}, memberErr
		}
		validated, rules = append(validated, item), append(rules, member.Rules)
	}
	var coherent runtimecore.CoherentView
	if sameExchangeTriangular {
		coherent, err = views.SameExchangeTriangularAsOf(requested, set.Trigger, maximumBookAge)
	} else {
		coherent, err = views.CoherentAsOf(requested, set.Trigger,
			runtimecore.InitialCoherentMarketDataCoherentPolicy())
	}
	if err != nil {
		return validatedSandboxSagaMarketCapture{}, fmt.Errorf("sandbox_saga_market_coherence_invalid")
	}
	sortValidatedSandboxSagaMarket(validated, rules)
	return validatedSandboxSagaMarketCapture{trigger: set.Trigger,
		firstDetected: set.FirstDetectedOffset, coherent: coherent, members: validated, rules: rules}, nil
}

func validSandboxSagaCaptureSet(set SandboxSagaMarketViewSet, requested []runtimecore.MarketKey,
	now time.Time, maximumBookAge time.Duration,
) bool {
	return maximumBookAge > 0 && maximumBookAge <= 250*time.Millisecond &&
		set.Trigger.MonotonicNanos != 0 && set.Trigger.IngestOrdinal != 0 &&
		!set.Trigger.UTC.IsZero() && set.Trigger.UTC.Location() == time.UTC &&
		!set.Trigger.UTC.After(now) && now.Sub(set.Trigger.UTC) <= maximumBookAge &&
		set.FirstDetectedOffset != 0 && set.FirstDetectedOffset <= set.Trigger.MonotonicNanos &&
		set.Trigger.MonotonicNanos-set.FirstDetectedOffset <= uint64(maximumBookAge.Nanoseconds()) &&
		len(set.Members) == len(requested)
}

func validateSandboxSagaCaptureMember(views *runtimecore.MarketViews,
	requested []runtimecore.MarketKey, trigger runtimecore.AsOfTrigger,
	member SandboxSagaMarketMember, seen map[string]struct{}, now time.Time, maximumBookAge time.Duration,
) (validatedSandboxSagaMarketMember, error) {
	view := member.View
	key := runtimecore.MarketKey{Exchange: view.Exchange(), Instrument: view.Instrument()}
	identity := key.Exchange + "\x00" + key.Instrument.Symbol()
	if _, duplicate := seen[identity]; duplicate || !containsSandboxSagaMarketKey(requested, key) ||
		!view.Eligible(trigger.MonotonicNanos, maximumBookAge) ||
		!validSandboxSagaInstrumentRules(member.Rules, key, now) ||
		member.CollectorInstance == "" || member.CollectorRegion == "" {
		return validatedSandboxSagaMarketMember{}, fmt.Errorf("sandbox_saga_market_member_invalid")
	}
	coherentInput, err := marketdata.CoherentInput(
		view, member.Clock, member.CollectorInstance, member.CollectorRegion)
	if err != nil || coherentInput.Key != key || views.ActivateGeneration(key, view.Generation()) != nil {
		return validatedSandboxSagaMarketMember{}, fmt.Errorf("sandbox_saga_market_member_invalid")
	}
	published, err := views.Publish(coherentInput)
	if err != nil || published.Version() != view.Version() {
		return validatedSandboxSagaMarketMember{}, fmt.Errorf("sandbox_saga_market_member_invalid")
	}
	snapshot := exchangecontracts.BookSnapshot{Exchange: exchangecontracts.ExchangeID(view.Exchange()),
		Instrument: view.Instrument(), LastSequence: view.Sequence(), ReceivedAt: view.Observation().ReceivedAt,
		Bids: view.Bids(), Asks: view.Asks(), RawPayloadHash: coherentInput.StateHash}
	seen[identity] = struct{}{}
	return validatedSandboxSagaMarketMember{view: view, snapshot: snapshot, rules: member.Rules}, nil
}

func sortValidatedSandboxSagaMarket(validated []validatedSandboxSagaMarketMember,
	rules []arbitrage.InstrumentRules,
) {
	sort.Slice(validated, func(left, right int) bool {
		if validated[left].view.Exchange() != validated[right].view.Exchange() {
			return validated[left].view.Exchange() < validated[right].view.Exchange()
		}
		return validated[left].view.Instrument().Symbol() < validated[right].view.Instrument().Symbol()
	})
	sort.Slice(rules, func(left, right int) bool {
		if rules[left].Exchange != rules[right].Exchange {
			return rules[left].Exchange < rules[right].Exchange
		}
		return rules[left].Metadata.Instrument.Symbol() < rules[right].Metadata.Instrument.Symbol()
	})
}

func containsSandboxSagaMarketKey(values []runtimecore.MarketKey, wanted runtimecore.MarketKey) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validSandboxSagaInstrumentRules(
	rules arbitrage.InstrumentRules,
	key runtimecore.MarketKey,
	now time.Time,
) bool {
	zeroQuantity, quantityErr := domain.ParseQuantity("0")
	zeroRate, rateErr := domain.ParseRate("0")
	_, assetErr := domain.ParseAssetSymbol(string(rules.Fee.Asset))
	return quantityErr == nil && rateErr == nil && assetErr == nil && rules.Active &&
		rules.Exchange == key.Exchange && rules.Metadata.Instrument == key.Instrument &&
		rules.Metadata.Validate() == nil && !rules.Metadata.EffectiveAt.After(now) &&
		rules.MaximumQuantity.Compare(zeroQuantity) > 0 && rules.Fee.Version != "" &&
		rules.Fee.Rate.Compare(zeroRate) >= 0 && !rules.ObservedAt.IsZero() &&
		rules.ObservedAt.Location() == time.UTC && !rules.ObservedAt.After(now)
}

func sandboxSagaInstrument(symbol string) (domain.Instrument, error) {
	switch symbol {
	case "BTCUSDT":
		return domain.NewSpotInstrument("BTC", "USDT")
	case "ETHUSDT":
		return domain.NewSpotInstrument("ETH", "USDT")
	case "ETHBTC":
		return domain.NewSpotInstrument("ETH", "BTC")
	default:
		return domain.Instrument{}, fmt.Errorf("sandbox_saga_instrument_invalid")
	}
}

func sandboxSagaMarketEvidenceHash(identity string, rules []arbitrage.InstrumentRules) string {
	encoded, _ := json.Marshal(struct {
		Identity string                      `json:"coherent_view_id"`
		Rules    []arbitrage.InstrumentRules `json:"rules"`
	}{Identity: identity, Rules: rules})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func sandboxSagaRiskMarket(
	markets []triangular.MarketInput,
	work sandbox.StrategySessionWork,
	trigger runtimecore.AsOfTrigger,
) (sandbox.StrategyMarketInput, error) {
	if work.ValidAt(trigger.UTC) != nil || trigger.MonotonicNanos == 0 || trigger.IngestOrdinal == 0 {
		return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_invalid")
	}
	for _, market := range markets {
		if string(market.Snapshot.Exchange) != string(work.Account.Exchange) ||
			market.Snapshot.Instrument.Symbol() != work.Instrument ||
			market.Observation.Validate() != nil ||
			!validSandboxSagaInstrumentRules(market.Rules, runtimecore.MarketKey{
				Exchange: string(work.Account.Exchange), Instrument: market.Snapshot.Instrument,
			}, trigger.UTC) {
			continue
		}
		encoded, err := json.Marshal(market.Rules)
		if err != nil {
			return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_invalid")
		}
		digest := sha256.Sum256(encoded)
		metadataHash := hex.EncodeToString(digest[:])
		result := sandbox.StrategyMarketInput{Instrument: market.Snapshot.Instrument,
			Metadata: exchangecontracts.InstrumentRecord{Exchange: market.Snapshot.Exchange,
				NativeSymbol: market.Snapshot.Instrument.Symbol(), NativeStatus: "Trading",
				Metadata: market.Rules.Metadata, MaximumQuantity: market.Rules.MaximumQuantity,
				RawPayloadHash: metadataHash},
			Book: market.Snapshot, ObservedAt: domain.EventTime{UTC: trigger.UTC,
				Sequence: trigger.IngestOrdinal}}
		if sandbox.StrategyMarketEvidenceHash(result) == "" {
			return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_invalid")
		}
		return result, nil
	}
	return sandbox.StrategyMarketInput{}, fmt.Errorf("sandbox_saga_risk_market_unavailable")
}
