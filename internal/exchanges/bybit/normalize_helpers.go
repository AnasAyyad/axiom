package bybit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func strictDecode(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return streamError()
	}
	return nil
}

func unwrap[T any](payload []byte) (T, int64, error) {
	var envelope responseEnvelope[T]
	if err := strictDecode(payload, &envelope); err != nil || envelope.RetCode != 0 ||
		envelope.RetMsg != "OK" || envelope.Time <= 0 {
		var zero T
		return zero, 0, validationError(exchangecontracts.OperationMetadata)
	}
	return envelope.Result, envelope.Time, nil
}

func normalizeLevels(native [][]string, allowZero bool) ([]exchangecontracts.PriceLevel, error) {
	levels := make([]exchangecontracts.PriceLevel, 0, len(native))
	for _, item := range native {
		if len(item) != 2 {
			return nil, streamError()
		}
		price, priceErr := domain.ParsePrice(item[0])
		quantity, quantityErr := domain.ParseQuantity(item[1])
		if priceErr != nil || quantityErr != nil || price.String() == "0" ||
			(!allowZero && quantity.String() == "0") {
			return nil, streamError()
		}
		levels = append(levels, exchangecontracts.PriceLevel{Price: price, Quantity: quantity})
	}
	return levels, nil
}

func instrumentForSymbol(symbol string) (domain.Instrument, error) {
	for _, candidate := range approvedInstruments() {
		if candidate.Symbol() == symbol {
			return candidate, nil
		}
	}
	return domain.Instrument{}, validationError(exchangecontracts.OperationMetadata)
}

func approvedInstruments() []domain.Instrument {
	values := [][2]string{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}}
	result := make([]domain.Instrument, 0, len(values))
	for _, value := range values {
		base, _ := domain.ParseAssetSymbol(value[0])
		quote, _ := domain.ParseAssetSymbol(value[1])
		instrument, _ := domain.NewSpotInstrument(base, quote)
		result = append(result, instrument)
	}
	return result
}

func approvedInstrument(instrument domain.Instrument) bool {
	for _, candidate := range approvedInstruments() {
		if candidate == instrument {
			return true
		}
	}
	return false
}

func intervalNative(interval string) (string, bool) {
	switch interval {
	case "15m":
		return "15", true
	case "1h":
		return "60", true
	case "4h":
		return "240", true
	default:
		return "", false
	}
}

func intervalCanonical(interval string) (string, bool) {
	switch interval {
	case "15":
		return "15m", true
	case "60":
		return "1h", true
	case "240":
		return "4h", true
	default:
		return "", false
	}
}

func millisecondString(value string) (time.Time, error) {
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 || strconv.FormatInt(milliseconds, 10) != value {
		return time.Time{}, validationError(exchangecontracts.OperationTrades)
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func topicParts(topic, prefix string) (string, bool) {
	if !strings.HasPrefix(topic, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(topic, prefix)
	return value, value != "" && !strings.Contains(value, ".")
}

func payloadHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validationError(operation exchangecontracts.Operation) error {
	return exchangecontracts.NewError(exchangecontracts.ErrorValidation, operation, 0)
}

func streamError() error { return validationError(exchangecontracts.OperationStream) }

func streamValidation(cause string) error {
	return exchangecontracts.NewDetailedError(exchangecontracts.ErrorValidation,
		exchangecontracts.OperationStream, 0, 0, cause)
}
