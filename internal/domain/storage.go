package domain

import "database/sql/driver"

// Value emits canonical decimal text for exact database storage.
func (value decimalValue) Value() (driver.Value, error) { return value.String(), nil }

func scanText(source any, operation string) (string, error) {
	switch value := source.(type) {
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return "", domainError(CodeInvalidDecimal, operation)
	}
}

// Scan reads canonical decimal text from a database value.
func (value *Price) Scan(source any) error {
	text, err := scanText(source, "price_scan")
	if err == nil {
		*value, err = ParsePrice(text)
	}
	return err
}

// UnmarshalText parses canonical price text.
func (value *Price) UnmarshalText(text []byte) error {
	parsed, err := ParsePrice(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Quantity) Scan(source any) error {
	text, err := scanText(source, "quantity_scan")
	if err == nil {
		*value, err = ParseQuantity(text)
	}
	return err
}

// UnmarshalText parses canonical quantity text.
func (value *Quantity) UnmarshalText(text []byte) error {
	parsed, err := ParseQuantity(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Money) Scan(source any) error {
	text, err := scanText(source, "money_scan")
	if err == nil {
		*value, err = ParseMoney(text)
	}
	return err
}

// UnmarshalText parses canonical money text.
func (value *Money) UnmarshalText(text []byte) error {
	parsed, err := ParseMoney(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Rate) Scan(source any) error {
	text, err := scanText(source, "rate_scan")
	if err == nil {
		*value, err = ParseRate(text)
	}
	return err
}

// UnmarshalText parses canonical rate text.
func (value *Rate) UnmarshalText(text []byte) error {
	parsed, err := ParseRate(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Percent) Scan(source any) error {
	text, err := scanText(source, "percent_scan")
	if err == nil {
		*value, err = ParsePercent(text)
	}
	return err
}

// UnmarshalText parses canonical percentage text.
func (value *Percent) UnmarshalText(text []byte) error {
	parsed, err := ParsePercent(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Fee) Scan(source any) error {
	text, err := scanText(source, "fee_scan")
	if err == nil {
		*value, err = ParseFee(text)
	}
	return err
}

// UnmarshalText parses canonical fee text.
func (value *Fee) UnmarshalText(text []byte) error {
	parsed, err := ParseFee(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Notional) Scan(source any) error {
	text, err := scanText(source, "notional_scan")
	if err == nil {
		*value, err = ParseNotional(text)
	}
	return err
}

// UnmarshalText parses canonical notional text.
func (value *Notional) UnmarshalText(text []byte) error {
	parsed, err := ParseNotional(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *Balance) Scan(source any) error {
	text, err := scanText(source, "balance_scan")
	if err == nil {
		*value, err = ParseBalance(text)
	}
	return err
}

// UnmarshalText parses canonical balance text.
func (value *Balance) UnmarshalText(text []byte) error {
	parsed, err := ParseBalance(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}

// Scan reads canonical decimal text from a database value.
func (value *PnL) Scan(source any) error {
	text, err := scanText(source, "pnl_scan")
	if err == nil {
		*value, err = ParsePnL(text)
	}
	return err
}

// UnmarshalText parses canonical profit-and-loss text.
func (value *PnL) UnmarshalText(text []byte) error {
	parsed, err := ParsePnL(string(text))
	if err == nil {
		*value = parsed
	}
	return err
}
