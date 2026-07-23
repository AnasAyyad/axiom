package config

// DefaultV1BConfiguration returns the current credential-free public exchange,
// B3, B4, and B5 strategy graphs while preserving earlier compatibility projections.
func DefaultV1BConfiguration() Configuration {
	configuration := DefaultConfiguration()
	configuration.SchemaVersion = SchemaVersionV1BB5
	configuration.Revision = 1
	instruments := []Instrument{
		{Base: "BTC", Quote: "USDT", Product: "spot"},
		{Base: "ETH", Quote: "USDT", Product: "spot"},
		{Base: "ETH", Quote: "BTC", Product: "spot"},
	}
	intervals := []string{"15m", "1h", "4h"}
	configuration.Exchanges = []ExchangeConfiguration{
		{ID: "binance", EndpointSet: "market-data-only-v1",
			REST: "https://data-api.binance.vision", WebSocket: "wss://data-stream.binance.vision",
			Instruments: append([]Instrument(nil), instruments...), CandleIntervals: append([]string(nil), intervals...)},
		{ID: "bybit", EndpointSet: "bybit-public-v1",
			REST: "https://api.bybit.com", WebSocket: "wss://stream.bybit.com/v5/public/spot",
			Instruments: append([]Instrument(nil), instruments...), CandleIntervals: append([]string(nil), intervals...)},
	}
	configuration.MeanReversion = defaultMeanReversionConfiguration()
	configuration.Triangular = defaultTriangularConfiguration()
	configuration.CrossExchange = defaultCrossExchangeConfiguration()
	return configuration
}

// PublicExchanges returns the ordered recorder graph, projecting legacy V1A
// configuration into one Binance public exchange without mutating it.
func (configuration Configuration) PublicExchanges() []ExchangeConfiguration {
	if len(configuration.Exchanges) != 0 {
		return cloneConfiguration(configuration).Exchanges
	}
	return []ExchangeConfiguration{{ID: "binance", EndpointSet: configuration.Endpoint.Set,
		REST: configuration.Endpoint.REST, WebSocket: configuration.Endpoint.WebSocket,
		Instruments:     append([]Instrument(nil), configuration.Instruments...),
		CandleIntervals: []string{"4h"}}}
}
