package bybit

import (
	"github.com/cockroachdb/apd/v3"

	"axiom/internal/domain"
)

func positiveDecimal(value string) bool {
	decimal, _, err := apd.NewFromString(value)
	return err == nil && decimal.Form == apd.Finite && decimal.Sign() > 0
}

func positiveQuantity(value domain.Quantity) bool {
	zero, _ := domain.ParseQuantity("0")
	return value.Compare(zero) > 0
}

func positivePrice(value domain.Price) bool {
	zero, _ := domain.ParsePrice("0")
	return value.Compare(zero) > 0
}

func positiveNotional(value domain.Notional) bool {
	zero, _ := domain.ParseNotional("0")
	return value.Compare(zero) > 0
}
