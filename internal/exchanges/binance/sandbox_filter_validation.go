package binance

import (
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/cockroachdb/apd/v3"
)

func (rules SandboxInstrumentRules) validateSubmission(
	submission sandbox.Submission,
	owned domain.Balance,
	reference sandboxAveragePrice,
	now time.Time,
) error {
	if rules.validateSubmissionContext(submission, reference, now) != nil ||
		rules.validateSubmissionQuantity(submission, owned) != nil ||
		rules.validateSubmissionPriceNotional(submission) != nil {
		return ErrSandboxFilter
	}
	return rules.validateDynamicPrice(
		submission.Side, submission.LimitPrice, reference.Price,
	)
}

func (rules SandboxInstrumentRules) validateSubmissionContext(
	submission sandbox.Submission,
	reference sandboxAveragePrice,
	now time.Time,
) error {
	if submission.Instrument != rules.Instrument || now.IsZero() ||
		now.Location() != time.UTC || reference.ObservedAt.IsZero() ||
		reference.ObservedAt.Location() != time.UTC ||
		reference.ObservedAt.After(now) ||
		now.Sub(reference.ObservedAt) > averagePriceFreshness(rules.AveragePriceMinutes) ||
		(rules.AveragePriceMinutes != 0 &&
			reference.Minutes != rules.AveragePriceMinutes) {
		return ErrSandboxFilter
	}
	if fractionalDigits(submission.Quantity.String()) > int(rules.BasePrecision) ||
		fractionalDigits(submission.LimitPrice.String()) > int(rules.QuotePrecision) {
		return ErrSandboxFilter
	}
	return nil
}

func (rules SandboxInstrumentRules) validateSubmissionQuantity(
	submission sandbox.Submission,
	owned domain.Balance,
) error {
	roundedQuantity, err := domain.RoundBuyQuantity(
		submission.Quantity,
		rules.QuantityStep,
	)
	if err != nil || roundedQuantity.Compare(submission.Quantity) != 0 ||
		submission.Quantity.Compare(rules.MinimumQuantity) < 0 ||
		(nonzeroQuantity(rules.MaximumQuantity) &&
			submission.Quantity.Compare(rules.MaximumQuantity) > 0) {
		return ErrSandboxFilter
	}
	if submission.Side == domain.SideSell {
		ownedQuantity, parseErr := domain.ParseQuantity(owned.String())
		if parseErr != nil || submission.Quantity.Compare(ownedQuantity) > 0 {
			return ErrSandboxFilter
		}
	}
	return nil
}

func (rules SandboxInstrumentRules) validateSubmissionPriceNotional(
	submission sandbox.Submission,
) error {
	roundedPrice, err := domain.RoundLimitPrice(
		submission.Side,
		submission.LimitPrice,
		rules.PriceTick,
	)
	if err != nil || roundedPrice.Compare(submission.LimitPrice) != 0 ||
		submission.LimitPrice.Compare(rules.MinimumPrice) < 0 ||
		(nonzeroPrice(rules.MaximumPrice) &&
			submission.LimitPrice.Compare(rules.MaximumPrice) > 0) {
		return ErrSandboxFilter
	}
	nativeNotional, err := domain.CalculateNotional(
		submission.LimitPrice,
		submission.Quantity,
		18,
	)
	if err != nil || nativeNotional.Compare(rules.MinimumNotional) < 0 ||
		(nonzeroNotional(rules.MaximumNotional) &&
			nativeNotional.Compare(rules.MaximumNotional) > 0) {
		return ErrSandboxFilter
	}
	if submission.Instrument.Quote == "USDT" &&
		nativeNotional.Compare(submission.Notional) != 0 {
		return ErrSandboxFilter
	}
	return nil
}

func (rules SandboxInstrumentRules) validateDynamicPrice(
	side domain.Side,
	price domain.Price,
	reference domain.Price,
) error {
	upper, lower := rules.BidMultiplierUp, rules.BidMultiplierDown
	if side == domain.SideSell {
		upper, lower = rules.AskMultiplierUp, rules.AskMultiplierDown
	} else if side != domain.SideBuy {
		return ErrSandboxFilter
	}
	priceValue, _, priceErr := apd.NewFromString(price.String())
	referenceValue, _, referenceErr := apd.NewFromString(reference.String())
	upperValue, _, upperErr := apd.NewFromString(upper)
	lowerValue, _, lowerErr := apd.NewFromString(lower)
	if priceErr != nil || referenceErr != nil || upperErr != nil || lowerErr != nil {
		return ErrSandboxFilter
	}
	context := apd.BaseContext.WithPrecision(38)
	var maximum, minimum apd.Decimal
	if _, err := context.Mul(&maximum, referenceValue, upperValue); err != nil {
		return ErrSandboxFilter
	}
	if _, err := context.Mul(&minimum, referenceValue, lowerValue); err != nil {
		return ErrSandboxFilter
	}
	if priceValue.Cmp(&minimum) < 0 || priceValue.Cmp(&maximum) > 0 {
		return ErrSandboxFilter
	}
	return nil
}

func averagePriceFreshness(minutes uint64) time.Duration {
	if minutes == 0 {
		return time.Minute
	}
	if minutes > 60 {
		return 0
	}
	return time.Duration(minutes) * time.Minute
}

func fractionalDigits(value string) int {
	for index, character := range value {
		if character == '.' {
			return len(value) - index - 1
		}
	}
	return 0
}

func nonzeroPrice(value domain.Price) bool {
	zero, _ := domain.ParsePrice("0")
	return value.Compare(zero) != 0
}

func nonzeroQuantity(value domain.Quantity) bool {
	zero, _ := domain.ParseQuantity("0")
	return value.Compare(zero) != 0
}

func nonzeroNotional(value domain.Notional) bool {
	zero, _ := domain.ParseNotional("0")
	return value.Compare(zero) != 0
}
