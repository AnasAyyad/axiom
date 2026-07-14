package domain

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestFinancialParsingCanonicalizesExactText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "0", want: "0"},
		{input: "12.3400", want: "12.34"},
		{input: "999999999999999999.000000000000000001", want: "999999999999999999.000000000000000001"},
	}
	for _, test := range tests {
		value, err := ParsePrice(test.input)
		if err != nil {
			t.Fatalf("ParsePrice(%q): %v", test.input, err)
		}
		if value.String() != test.want {
			t.Fatalf("ParsePrice(%q) = %q, want %q", test.input, value.String(), test.want)
		}
	}
}

func TestFinancialParsingRejectsUnsafeForms(t *testing.T) {
	inputs := []string{"", "+1", "01", ".1", "1e2", "NaN", "Infinity", "-0", "-0.0", "-1", "0.0000000000000000001", "123456789012345678901234567890123456789"}
	for _, input := range inputs {
		if _, err := ParseMoney(input); err == nil {
			t.Fatalf("ParseMoney(%q) succeeded", input)
		}
	}
	pnl, err := ParsePnL("-0.1")
	if err != nil || pnl.String() != "-0.1" {
		t.Fatalf("signed PnL parse = %q, %v", pnl.String(), err)
	}
}

func TestCheckedArithmeticAndTraps(t *testing.T) {
	left := mustQuantity(t, "10.25")
	right := mustQuantity(t, "2.5")
	sum, err := left.Add(right)
	if err != nil || sum.String() != "12.75" {
		t.Fatalf("sum = %q, %v", sum.String(), err)
	}
	if _, err := right.Subtract(left); errorCode(err) != CodeNegativeValue {
		t.Fatalf("negative subtraction error = %v", err)
	}
	one, _ := ParseMoney("1")
	three, _ := ParseMoney("3")
	if _, err := one.Divide(three); errorCode(err) != CodeArithmetic {
		t.Fatalf("inexact division error = %v", err)
	}
	maximum := mustQuantity(t, "99999999999999999999999999999999999999")
	if _, err := maximum.Add(mustQuantity(t, "1")); errorCode(err) != CodeArithmetic {
		t.Fatalf("overflowing precision error = %v", err)
	}
}

func TestSerializationAndDatabaseRoundTrip(t *testing.T) {
	original := mustPrice(t, "123.4500")
	encoded, err := json.Marshal(original)
	if err != nil || string(encoded) != `"123.45"` {
		t.Fatalf("marshal = %s, %v", encoded, err)
	}
	var decoded Price
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Compare(original) != 0 {
		t.Fatalf("unmarshal = %q, %v", decoded.String(), err)
	}
	databaseValue, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}
	var scanned Price
	if err := scanned.Scan(databaseValue); err != nil || scanned.Compare(original) != 0 {
		t.Fatalf("scan = %q, %v", scanned.String(), err)
	}
	if err := scanned.Scan(123.45); err == nil {
		t.Fatal("binary float database value was accepted")
	}
}

func TestPostgreSQLNumericCodecRoundTrip(t *testing.T) {
	original := mustPrice(t, "999999999999999999.000000000000000001")
	mapping := pgtype.NewMap()
	encoded, err := mapping.Encode(pgtype.NumericOID, pgtype.TextFormatCode, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Price
	if err := mapping.Scan(pgtype.NumericOID, pgtype.TextFormatCode, encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Compare(original) != 0 || decoded.String() != original.String() {
		t.Fatalf("numeric codec round trip = %q", decoded.String())
	}
}

func TestQuantityAdditionProperties(t *testing.T) {
	for left := int64(0); left < 100; left++ {
		for right := int64(0); right < 20; right++ {
			a := mustQuantity(t, strconv.FormatInt(left, 10))
			b := mustQuantity(t, strconv.FormatInt(right, 10))
			ab, err := a.Add(b)
			if err != nil {
				t.Fatal(err)
			}
			ba, err := b.Add(a)
			if err != nil || ab.Compare(ba) != 0 {
				t.Fatalf("addition is not commutative for %d, %d", left, right)
			}
		}
	}
}

func FuzzParseFinancial(f *testing.F) {
	for _, seed := range []string{"0", "1.2300", "-5.5", "1e3", "NaN"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		value, err := ParsePnL(input)
		if err != nil {
			return
		}
		reparsed, err := ParsePnL(value.String())
		if err != nil || reparsed.String() != value.String() {
			t.Fatalf("canonical round trip failed for %q", input)
		}
	})
}

func BenchmarkFinancialArithmetic(b *testing.B) {
	price := mustPrice(b, "65231.125")
	quantity := mustQuantity(b, "0.015")
	for b.Loop() {
		if _, err := CalculateNotional(price, quantity, 8); err != nil {
			b.Fatal(err)
		}
	}
}

func mustPrice(tb testing.TB, text string) Price {
	tb.Helper()
	value, err := ParsePrice(text)
	if err != nil {
		tb.Fatal(err)
	}
	return value
}

func mustQuantity(tb testing.TB, text string) Quantity {
	tb.Helper()
	value, err := ParseQuantity(text)
	if err != nil {
		tb.Fatal(err)
	}
	return value
}

func errorCode(err error) ErrorCode {
	var domainFailure *Error
	if errors.As(err, &domainFailure) {
		return domainFailure.Code
	}
	return ""
}
