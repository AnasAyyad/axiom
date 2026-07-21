package bybit

import (
	"sort"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func requestedTopics(request exchangecontracts.StreamRequest) (
	map[string]exchangecontracts.StreamKind,
	[]string,
	error,
) {
	expected := make(map[string]exchangecontracts.StreamKind)
	for _, kind := range request.Kinds {
		topics, err := topicsForKind(request, kind)
		if err != nil {
			return nil, nil, err
		}
		for _, topic := range topics {
			if _, duplicate := expected[topic]; duplicate {
				return nil, nil, streamError()
			}
			expected[topic] = kind
		}
	}
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 || len(names) > 10 {
		return nil, nil, streamError()
	}
	return expected, names, nil
}

func topicsForKind(
	request exchangecontracts.StreamRequest,
	kind exchangecontracts.StreamKind,
) ([]string, error) {
	symbol := request.Instrument.Symbol()
	switch kind {
	case exchangecontracts.StreamDepth:
		return []string{"orderbook.1000." + symbol}, nil
	case exchangecontracts.StreamTrades:
		return []string{"publicTrade." + symbol}, nil
	case exchangecontracts.StreamTicker:
		return []string{"tickers." + symbol}, nil
	case exchangecontracts.StreamCandle:
		return candleTopics(symbol, request.CandleIntervals)
	default:
		return nil, streamError()
	}
}

func candleTopics(symbol string, intervals []string) ([]string, error) {
	if len(intervals) == 0 {
		intervals = []string{"4h"}
	}
	if len(intervals) > 3 {
		return nil, streamError()
	}
	topics := make([]string, 0, len(intervals))
	seen := make(map[string]struct{}, len(intervals))
	for _, interval := range intervals {
		native, ok := intervalNative(interval)
		if !ok {
			return nil, streamError()
		}
		if _, duplicate := seen[native]; duplicate {
			return nil, streamError()
		}
		seen[native] = struct{}{}
		topics = append(topics, "kline."+native+"."+symbol)
	}
	return topics, nil
}
