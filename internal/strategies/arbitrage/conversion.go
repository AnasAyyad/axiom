package arbitrage

import (
	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Convert consumes executable depth with exact filters, side-specific
// rounding, fee-asset handling, and explicit source dust.
func Convert(request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	instrument := request.Rules.Metadata.Instrument
	switch {
	case request.Source == instrument.Quote && request.Target == instrument.Base:
		return convertBuy(request)
	case request.Source == instrument.Base && request.Target == instrument.Quote:
		return convertSell(request)
	default:
		return Result{}, conversionError("orientation_unavailable")
	}
}

func convertBuy(request Request) (Result, error) {
	input, _ := parseNumber(request.Input.String())
	rate, _ := parseNumber(request.Rules.Fee.Rate.String())
	tradeBudget := input
	if request.Rules.Fee.Asset == request.Source {
		one, _ := parseNumber("1")
		divisor, err := one.add(rate, apd.RoundCeiling)
		if err != nil {
			return Result{}, err
		}
		tradeBudget, err = input.divide(divisor, apd.RoundFloor)
		if err != nil {
			return Result{}, err
		}
	}
	rawQuantity, err := affordableBase(request.Book.Asks(), tradeBudget)
	if err != nil {
		return Result{}, err
	}
	step, _ := parseNumber(request.Rules.Metadata.QuantityStep.String())
	quantity, err := rawQuantity.floorMultiple(step)
	if err != nil {
		return Result{}, err
	}
	return finalizeBuy(request, input, quantity, rate)
}

func finalizeBuy(request Request, input, quantity, rate number) (Result, error) {
	if err := validateQuantity(request.Rules, quantity); err != nil {
		return Result{}, err
	}
	parsedQuantity, _ := domain.ParseQuantity(quantity.text())
	vwap, notional, err := request.Book.VWAPToBuyBase(parsedQuantity, 18)
	if err != nil {
		return Result{}, conversionError("depth_insufficient")
	}
	if err = validateNotional(request.Rules, notional); err != nil {
		return Result{}, err
	}
	notionalNumber, _ := parseNumber(notional.String())
	feeQuantity, feeQuote, err := calculateFee(request, quantity, notionalNumber, rate)
	if err != nil {
		return Result{}, err
	}
	output := quantity
	if request.Rules.Fee.Asset == request.Target {
		output, err = output.subtract(feeQuantity, false, apd.RoundFloor)
		if err != nil {
			return Result{}, conversionError("fee_exceeds_output")
		}
	}
	sourceSpent := notionalNumber
	if request.Rules.Fee.Asset == request.Source {
		sourceSpent, err = sourceSpent.add(feeQuantity, apd.RoundCeiling)
		if err != nil {
			return Result{}, err
		}
	}
	dust, err := input.subtract(sourceSpent, false, apd.RoundFloor)
	if err != nil {
		return Result{}, conversionError("fee_exceeds_input")
	}
	spread, err := buySpreadCost(request.Book.Asks(), quantity, notionalNumber)
	if err != nil {
		return Result{}, err
	}
	return buildResult(request, domain.SideBuy, quantity, quantity, output, dust,
		feeQuantity, feeQuote, notional, vwap, spread)
}

func convertSell(request Request) (Result, error) {
	input, _ := parseNumber(request.Input.String())
	rate, _ := parseNumber(request.Rules.Fee.Rate.String())
	quantity, err := sellQuantity(request, input, rate)
	if err != nil {
		return Result{}, err
	}
	return finalizeSell(request, input, quantity, rate)
}

func sellQuantity(request Request, input, rate number) (number, error) {
	tradeInput := input
	if request.Rules.Fee.Asset == request.Source {
		one, _ := parseNumber("1")
		divisor, err := one.add(rate, apd.RoundCeiling)
		if err != nil {
			return number{}, err
		}
		tradeInput, err = input.divide(divisor, apd.RoundFloor)
		if err != nil {
			return number{}, err
		}
	}
	step, _ := parseNumber(request.Rules.Metadata.QuantityStep.String())
	return tradeInput.floorMultiple(step)
}

func finalizeSell(request Request, input, quantity, rate number) (Result, error) {
	if err := validateQuantity(request.Rules, quantity); err != nil {
		return Result{}, err
	}
	parsedQuantity, _ := domain.ParseQuantity(quantity.text())
	vwap, notional, err := request.Book.VWAPToSellBase(parsedQuantity, 18)
	if err != nil {
		return Result{}, conversionError("depth_insufficient")
	}
	if err = validateNotional(request.Rules, notional); err != nil {
		return Result{}, err
	}
	notionalNumber, _ := parseNumber(notional.String())
	feeQuantity, feeQuote, err := calculateFee(request, quantity, notionalNumber, rate)
	if err != nil {
		return Result{}, err
	}
	output := notionalNumber
	if request.Rules.Fee.Asset == request.Target {
		output, err = output.subtract(feeQuantity, false, apd.RoundFloor)
		if err != nil {
			return Result{}, conversionError("fee_exceeds_output")
		}
	}
	sourceSpent := quantity
	if request.Rules.Fee.Asset == request.Source {
		sourceSpent, err = sourceSpent.add(feeQuantity, apd.RoundCeiling)
		if err != nil {
			return Result{}, err
		}
	}
	dust, err := input.subtract(sourceSpent, false, apd.RoundFloor)
	if err != nil {
		return Result{}, conversionError("fee_exceeds_input")
	}
	spread, err := sellSpreadCost(request.Book.Bids(), quantity, notionalNumber)
	if err != nil {
		return Result{}, err
	}
	return buildResult(request, domain.SideSell, quantity, notionalNumber, output, dust,
		feeQuantity, feeQuote, notional, vwap, spread)
}

func affordableBase(levels []exchangecontracts.PriceLevel, quoteBudget number) (number, error) {
	if len(levels) == 0 || quoteBudget.sign() <= 0 {
		return number{}, conversionError("depth_insufficient")
	}
	remaining := quoteBudget
	total, _ := parseNumber("0")
	for _, level := range levels {
		price, _ := parseNumber(level.Price.String())
		quantity, _ := parseNumber(level.Quantity.String())
		levelCost, err := price.multiply(quantity, apd.RoundCeiling)
		if err != nil {
			return number{}, err
		}
		take := quantity
		if levelCost.compare(remaining) > 0 {
			take, err = remaining.divide(price, apd.RoundFloor)
			if err != nil {
				return number{}, err
			}
			levelCost, err = price.multiply(take, apd.RoundCeiling)
			if err != nil {
				return number{}, err
			}
		}
		total, err = total.add(take, apd.RoundFloor)
		if err != nil {
			return number{}, err
		}
		remaining, err = remaining.subtract(levelCost, false, apd.RoundFloor)
		if err != nil {
			return number{}, err
		}
		if remaining.sign() == 0 || take.compare(quantity) < 0 {
			break
		}
	}
	if total.sign() <= 0 {
		return number{}, conversionError("depth_insufficient")
	}
	return total, nil
}

func calculateFee(request Request, baseQuantity, quoteNotional, rate number) (number, number, error) {
	zero, _ := parseNumber("0")
	if rate.sign() == 0 {
		return zero, zero, nil
	}
	feeQuote, err := quoteNotional.multiply(rate, apd.RoundCeiling)
	if err != nil {
		return number{}, number{}, err
	}
	switch request.Rules.Fee.Asset {
	case request.Rules.Metadata.Instrument.Quote:
		return feeQuote, feeQuote, nil
	case request.Rules.Metadata.Instrument.Base:
		feeBase, feeErr := baseQuantity.multiply(rate, apd.RoundCeiling)
		return feeBase, feeQuote, feeErr
	default:
		mark, markErr := parseNumber(request.Rules.Fee.ThirdAssetPriceInQuote.String())
		if markErr != nil || mark.sign() <= 0 {
			return number{}, number{}, conversionError("third_asset_fee_mark_missing")
		}
		feeThird, feeErr := feeQuote.divide(mark, apd.RoundCeiling)
		return feeThird, feeQuote, feeErr
	}
}

func validateRequest(request Request) error {
	rules := request.Rules
	if request.Source == "" || request.Target == "" || request.Source == request.Target ||
		request.Input.String() == "0" || rules.Exchange == "" || !rules.Active ||
		rules.Metadata.Validate() != nil || rules.Metadata.Instrument != request.Book.Instrument() ||
		rules.Exchange != request.Book.Exchange() || request.Book.Health() != "HEALTHY" ||
		request.Book.Generation() == 0 || request.Book.Version() == 0 ||
		rules.MaximumQuantity.String() == "0" || rules.Fee.Version == "" ||
		rules.ObservedAt.IsZero() || rules.ObservedAt.Location() != rules.ObservedAt.UTC().Location() {
		return conversionError("request_invalid")
	}
	if _, err := domain.ParseAssetSymbol(string(rules.Fee.Asset)); err != nil {
		return conversionError("fee_asset_invalid")
	}
	if !bookPricesMatchTick(request.Book.Bids(), rules.Metadata.PriceTick) ||
		!bookPricesMatchTick(request.Book.Asks(), rules.Metadata.PriceTick) {
		return conversionError("price_tick_invalid")
	}
	return nil
}

func bookPricesMatchTick(levels []exchangecontracts.PriceLevel, tick domain.Price) bool {
	increment, err := parseNumber(tick.String())
	if err != nil || len(levels) == 0 {
		return false
	}
	for _, level := range levels {
		value, valueErr := parseNumber(level.Price.String())
		if valueErr != nil || !value.multipleOf(increment) {
			return false
		}
	}
	return true
}

func validateQuantity(rules InstrumentRules, quantity number) error {
	minimum, _ := parseNumber(rules.Metadata.MinimumQuantity.String())
	maximum, _ := parseNumber(rules.MaximumQuantity.String())
	step, _ := parseNumber(rules.Metadata.QuantityStep.String())
	if quantity.compare(minimum) < 0 || quantity.compare(maximum) > 0 || !quantity.multipleOf(step) {
		return conversionError("quantity_filter_invalid")
	}
	return nil
}

func validateNotional(rules InstrumentRules, notional domain.Notional) error {
	value, _ := parseNumber(notional.String())
	minimum, _ := parseNumber(rules.Metadata.MinimumNotional.String())
	if value.compare(minimum) < 0 {
		return conversionError("minimum_notional")
	}
	return nil
}

func buySpreadCost(levels []exchangecontracts.PriceLevel, quantity, notional number) (number, error) {
	best, _ := parseNumber(levels[0].Price.String())
	bestCost, err := best.multiply(quantity, apd.RoundHalfEven)
	if err != nil {
		return number{}, err
	}
	return notional.subtract(bestCost, false, apd.RoundCeiling)
}

func sellSpreadCost(levels []exchangecontracts.PriceLevel, quantity, notional number) (number, error) {
	best, _ := parseNumber(levels[0].Price.String())
	bestProceeds, err := best.multiply(quantity, apd.RoundHalfEven)
	if err != nil {
		return number{}, err
	}
	return bestProceeds.subtract(notional, false, apd.RoundCeiling)
}

func buildResult(
	request Request,
	side domain.Side,
	tradeQuantity, gross, net, dust, feeQuantity, feeQuote number,
	notional domain.Notional,
	vwap domain.Price,
	spread number,
) (Result, error) {
	grossOutput, grossErr := domain.ParseQuantity(gross.text())
	netOutput, netErr := domain.ParseQuantity(net.text())
	sourceDust, dustErr := domain.ParseQuantity(dust.text())
	fee, feeErr := domain.ParseQuantity(feeQuantity.text())
	feeMoney, feeMoneyErr := domain.ParseMoney(feeQuote.text())
	spreadMoney, spreadErr := domain.ParseMoney(spread.text())
	quantity, quantityErr := domain.ParseQuantity(tradeQuantity.text())
	if grossErr != nil || netErr != nil || dustErr != nil || feeErr != nil ||
		feeMoneyErr != nil || spreadErr != nil || quantityErr != nil {
		return Result{}, conversionError("result_invalid")
	}
	return Result{
		Exchange: request.Rules.Exchange, Instrument: request.Rules.Metadata.Instrument,
		Side: side, Source: request.Source, Target: request.Target, Input: request.Input,
		TradeQuantity: quantity, GrossOutput: grossOutput, NetOutput: netOutput,
		SourceDust: sourceDust, FeeAsset: request.Rules.Fee.Asset, FeeQuantity: fee,
		FeeQuoteEquivalent: feeMoney, Notional: notional, VWAP: vwap, SpreadCost: spreadMoney,
		MetadataVersion: request.Rules.Metadata.Version, FeeVersion: request.Rules.Fee.Version,
		BookVersion: request.Book.Version(), Generation: request.Book.Generation(),
	}, nil
}
