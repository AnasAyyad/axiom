package binance

import (
	"context"
	"sort"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const maximumCombinedObservedInstruments = 3

// SubscribeCombinedObserved opens one credential-free WebSocket carrying two
// or three approved instruments. Each frame remains an independent exchange
// event; this method does not claim that Binance publishes an atomic snapshot.
func (client *PublicClient) SubscribeCombinedObserved(
	ctx context.Context,
	requests []exchangecontracts.StreamRequest,
) (ObservedStream, error) {
	if len(requests) < 2 || len(requests) > maximumCombinedObservedInstruments {
		return nil, streamError()
	}
	expected := make(map[string]exchangecontracts.StreamKind)
	names := make([]string, 0, len(requests)*4)
	instruments := make(map[domain.Instrument]struct{}, len(requests))
	for _, request := range requests {
		if !approvedInstrument(request.Instrument) || len(request.Kinds) == 0 || len(request.Kinds) > 4 {
			return nil, streamError()
		}
		if _, duplicate := instruments[request.Instrument]; duplicate {
			return nil, streamError()
		}
		instruments[request.Instrument] = struct{}{}
		requestExpected, requestNames, err := requestedStreams(request)
		if err != nil {
			return nil, err
		}
		for name, kind := range requestExpected {
			if _, duplicate := expected[name]; duplicate {
				return nil, streamError()
			}
			expected[name] = kind
		}
		names = append(names, requestNames...)
	}
	sort.Strings(names)
	return client.openPublicStream(ctx, expected, names, domain.Instrument{}, nil)
}
