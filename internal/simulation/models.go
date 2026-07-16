package simulation

import (
	"time"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

const probabilityScale = uint64(1_000_000)

// FeeModel calculates exact versioned fees or rebates.
type FeeModel struct {
	Version      string
	TakerRate    domain.Rate
	MakerRate    domain.Rate
	RebateRate   domain.Rate
	DecimalScale uint8
}

// FeeResult keeps charges and rebates separate.
type FeeResult struct {
	Charge domain.Fee
	Rebate domain.Fee
}

// Calculate returns exact fee facts for one fill notional.
func (model FeeModel) Calculate(notional domain.Notional, maker bool) (FeeResult, error) {
	if model.Version == "" || model.DecimalScale > 18 {
		return FeeResult{}, simulationError("fee_model_invalid")
	}
	rate := model.TakerRate
	if maker {
		rate = model.MakerRate
	}
	charge, err := domain.CalculateFee(notional, rate, model.DecimalScale)
	if err != nil {
		return FeeResult{}, simulationError("fee_calculation_failed")
	}
	rebate, err := domain.CalculateFee(notional, model.RebateRate, model.DecimalScale)
	if err != nil {
		return FeeResult{}, simulationError("rebate_calculation_failed")
	}
	return FeeResult{Charge: charge, Rebate: rebate}, nil
}

// PriceModel applies spread, slippage, impact, and adverse selection exactly.
type PriceModel struct {
	Version          string
	Spread           domain.Percent
	Slippage         domain.Percent
	Impact           domain.Percent
	AdverseSelection domain.Percent
	DecimalScale     uint8
}

// Apply returns a conservative effective price for one side.
func (model PriceModel) Apply(reference domain.Price, side domain.Side) (domain.Price, error) {
	if model.Version == "" || model.DecimalScale > 18 {
		return domain.Price{}, simulationError("price_model_invalid")
	}
	total, err := model.Spread.Add(model.Slippage)
	if err == nil {
		total, err = total.Add(model.Impact)
	}
	if err == nil {
		total, err = total.Add(model.AdverseSelection)
	}
	if err != nil {
		return domain.Price{}, simulationError("price_model_overflow")
	}
	price, err := domain.PriceAtSlippage(reference, total, side, model.DecimalScale)
	if err != nil {
		return domain.Price{}, simulationError("price_model_range")
	}
	return price, nil
}

// LatencyModel selects from an immutable measured/scenario distribution.
type LatencyModel struct {
	Version string
	Samples []time.Duration
}

// Sample returns one keyed duration without shared random-stream state.
func (model LatencyModel) Sample(randomness *runtimecore.Randomness, key runtimecore.RandomKey) (time.Duration, error) {
	if model.Version == "" || len(model.Samples) == 0 || randomness == nil {
		return 0, simulationError("latency_model_invalid")
	}
	for _, sample := range model.Samples {
		if sample < 0 {
			return 0, simulationError("latency_model_invalid")
		}
	}
	draw, err := randomness.Uint64(key, 0)
	if err != nil {
		return 0, simulationError("latency_draw_failed")
	}
	return model.Samples[draw%uint64(len(model.Samples))], nil
}

// FillDisposition is one deterministic probabilistic fill outcome.
type FillDisposition string

// Supported modeled fill dispositions.
const (
	FillComplete FillDisposition = "complete"
	FillPartial  FillDisposition = "partial"
	FillMissed   FillDisposition = "missed"
)

// FillModel controls missed and partial fill probabilities and partial size.
type FillModel struct {
	Version       string
	MissPPM       uint32
	PartialPPM    uint32
	PartialRatio  domain.Percent
	QuantityScale uint8
}

// Disposition returns a keyed fill outcome.
func (model FillModel) Disposition(randomness *runtimecore.Randomness, key runtimecore.RandomKey) (FillDisposition, error) {
	if model.Version == "" || uint64(model.MissPPM)+uint64(model.PartialPPM) > probabilityScale || randomness == nil {
		return "", simulationError("fill_model_invalid")
	}
	draw, err := randomness.Uint64(key, 1)
	if err != nil {
		return "", simulationError("fill_draw_failed")
	}
	value := draw % probabilityScale
	if value < uint64(model.MissPPM) {
		return FillMissed, nil
	}
	if value < uint64(model.MissPPM)+uint64(model.PartialPPM) {
		return FillPartial, nil
	}
	return FillComplete, nil
}

// Limit returns the maximum quantity allowed by one fill disposition.
func (model FillModel) Limit(requested domain.Quantity, disposition FillDisposition) (domain.Quantity, error) {
	if disposition == FillComplete {
		return requested, nil
	}
	if disposition == FillMissed {
		return domain.ParseQuantity("0")
	}
	quantity, err := domain.ScaleQuantity(requested, model.PartialRatio, model.QuantityScale)
	if err != nil {
		return domain.Quantity{}, simulationError("partial_quantity_invalid")
	}
	return quantity, nil
}

// MakerQueueModel records the conservative visible queue fraction ahead.
type MakerQueueModel struct {
	Version       string
	QueueAhead    domain.Percent
	QuantityScale uint8
}

// EligibleQuantity reduces observed contra trade quantity by modeled queue.
func (model MakerQueueModel) EligibleQuantity(traded domain.Quantity) (domain.Quantity, error) {
	if model.Version == "" {
		return domain.Quantity{}, simulationError("maker_queue_model_invalid")
	}
	one, _ := domain.ParsePercent("1")
	remaining, err := one.Subtract(model.QueueAhead)
	if err != nil {
		return domain.Quantity{}, simulationError("maker_queue_model_invalid")
	}
	return domain.ScaleQuantity(traded, remaining, model.QuantityScale)
}
