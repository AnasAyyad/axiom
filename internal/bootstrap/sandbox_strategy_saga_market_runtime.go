package bootstrap

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/arbitrage"
)

// sandboxSagaPublicData is the common credential-free surface implemented by
// both approved public clients. It deliberately excludes every account,
// private-stream, cancel, and order-submission operation.
type sandboxSagaPublicData interface {
	sandbox.StrategyMarketData
	SampleServerTime(context.Context) (exchangecontracts.ClockHealth, error)
}

func newSandboxEngineSagaMarketRuntime(
	ctx context.Context,
	adapter sandboxEngineAdapter,
	market sandbox.StrategyMarketData,
	clock domain.Clock,
	product config.Configuration,
	exchange sandbox.Exchange,
) (*SandboxSagaMarketCache, *SandboxSagaMarketCacheSet, error) {
	public, ok := market.(sandboxSagaPublicData)
	if ctx == nil || adapter == nil || !ok || public == nil || clock == nil {
		return nil, nil, fmt.Errorf("sandbox_engine_saga_market_runtime_invalid")
	}
	rules, err := sandboxEngineSagaInstrumentRules(adapter, product, exchange)
	if err != nil {
		return nil, nil, err
	}
	monotonic := exchangecontracts.NewProcessMonotonicSource()
	current, err := NewSandboxSagaMarketCache(public, clock, monotonic,
		public, string(exchange), rules)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_saga_market_runtime_invalid")
	}
	caches := []*SandboxSagaMarketCache{current}
	if exchange == sandbox.ExchangeBinance {
		peerCache, peerErr := newBybitSandboxSagaMarketCache(ctx, clock, monotonic, product)
		if peerErr != nil {
			return nil, nil, peerErr
		}
		caches = append(caches, peerCache)
	}
	set, err := NewSandboxSagaMarketCacheSet(caches, monotonic)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox_engine_saga_market_runtime_invalid")
	}
	return current, set, nil
}

func newBybitSandboxSagaMarketCache(ctx context.Context, clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource, product config.Configuration,
) (*SandboxSagaMarketCache, error) {
	peer, err := bybit.NewMarketPublicClient(clock)
	if err != nil {
		return nil, fmt.Errorf("sandbox_engine_saga_peer_market_invalid")
	}
	instruments := make([]domain.Instrument, 0, 2)
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT"} {
		instrument, instrumentErr := sandboxSagaInstrument(symbol)
		if instrumentErr != nil {
			return nil, fmt.Errorf("sandbox_engine_saga_peer_market_invalid")
		}
		instruments = append(instruments, instrument)
	}
	nativeRules, rulesErr := peer.StrategyInstrumentRules(ctx, instruments)
	fee, feeErr := publicShadowFeeRate(product.Models.Fee)
	if rulesErr != nil || feeErr != nil {
		return nil, fmt.Errorf("sandbox_engine_saga_peer_rules_unavailable")
	}
	peerRules, rulesErr := bybitSandboxSagaRules(nativeRules, product.Models.Fee, fee)
	if rulesErr != nil {
		return nil, fmt.Errorf("sandbox_engine_saga_peer_rules_unavailable")
	}
	cache, err := NewSandboxSagaMarketCache(peer, clock, monotonic, peer,
		string(sandbox.ExchangeBybit), peerRules)
	if err != nil {
		return nil, fmt.Errorf("sandbox_engine_saga_peer_market_invalid")
	}
	return cache, nil
}

func sandboxEngineSagaInstrumentRules(
	adapter sandboxEngineAdapter,
	product config.Configuration,
	exchange sandbox.Exchange,
) ([]arbitrage.InstrumentRules, error) {
	fee, err := publicShadowFeeRate(product.Models.Fee)
	if err != nil {
		return nil, fmt.Errorf("sandbox_engine_saga_fee_model_invalid")
	}
	switch typed := adapter.(type) {
	case *binance.SandboxAdapter:
		if exchange != sandbox.ExchangeBinance {
			return nil, fmt.Errorf("sandbox_engine_saga_rules_invalid")
		}
		values, loadErr := typed.StrategyInstrumentRules()
		if loadErr != nil {
			return nil, fmt.Errorf("sandbox_engine_saga_rules_unavailable")
		}
		return binanceSandboxSagaRules(values, product.Models.Fee, fee)
	case *bybit.SandboxAdapter:
		if exchange != sandbox.ExchangeBybit {
			return nil, fmt.Errorf("sandbox_engine_saga_rules_invalid")
		}
		values, loadErr := typed.StrategyInstrumentRules()
		if loadErr != nil {
			return nil, fmt.Errorf("sandbox_engine_saga_rules_unavailable")
		}
		return bybitSandboxSagaRules(values, product.Models.Fee, fee)
	default:
		return nil, fmt.Errorf("sandbox_engine_saga_rules_invalid")
	}
}

func binanceSandboxSagaRules(
	values []binance.SandboxInstrumentRules,
	feeVersion string,
	fee domain.Rate,
) ([]arbitrage.InstrumentRules, error) {
	result := make([]arbitrage.InstrumentRules, 0, len(values))
	for _, value := range values {
		version, err := sandboxSagaSourceVersion(value.SourceHash)
		if err != nil {
			return nil, fmt.Errorf("sandbox_engine_saga_rules_invalid")
		}
		result = append(result, arbitrage.InstrumentRules{Exchange: string(sandbox.ExchangeBinance),
			Metadata: domain.InstrumentMetadata{Instrument: value.Instrument, Version: version,
				EffectiveAt: value.ObservedAt, PriceTick: value.PriceTick,
				QuantityStep: value.QuantityStep, MinimumQuantity: value.MinimumQuantity,
				MinimumNotional: value.MinimumNotional}, MaximumQuantity: value.MaximumQuantity,
			Fee:    arbitrage.FeeSchedule{Version: feeVersion, Rate: fee, Asset: value.Instrument.Quote},
			Active: true, ObservedAt: value.ObservedAt})
	}
	return result, nil
}

func bybitSandboxSagaRules(
	values []bybit.DemoInstrumentRules,
	feeVersion string,
	fee domain.Rate,
) ([]arbitrage.InstrumentRules, error) {
	result := make([]arbitrage.InstrumentRules, 0, len(values))
	for _, value := range values {
		version, err := sandboxSagaSourceVersion(value.SourceHash)
		if err != nil {
			return nil, fmt.Errorf("sandbox_engine_saga_rules_invalid")
		}
		result = append(result, arbitrage.InstrumentRules{Exchange: string(sandbox.ExchangeBybit),
			Metadata: domain.InstrumentMetadata{Instrument: value.Instrument, Version: version,
				EffectiveAt: value.ObservedAt, PriceTick: value.PriceTick,
				QuantityStep: value.QuantityStep, MinimumQuantity: value.MinimumQuantity,
				MinimumNotional: value.MinimumOrderAmount}, MaximumQuantity: value.MaximumQuantity,
			Fee:    arbitrage.FeeSchedule{Version: feeVersion, Rate: fee, Asset: value.Instrument.Quote},
			Active: true, ObservedAt: value.ObservedAt})
	}
	return result, nil
}

func sandboxSagaSourceVersion(sourceHash string) (uint64, error) {
	if len(sourceHash) != 64 {
		return 0, fmt.Errorf("sandbox_engine_saga_rules_invalid")
	}
	decoded, err := hex.DecodeString(sourceHash)
	if err != nil {
		return 0, fmt.Errorf("sandbox_engine_saga_rules_invalid")
	}
	version := binary.BigEndian.Uint64(decoded[:8])
	if version == 0 {
		version = 1
	}
	return version, nil
}

var _ sandboxSagaPublicData = (*binance.PublicClient)(nil)
var _ sandboxSagaPublicData = (*bybit.PublicClient)(nil)
