package binance

type depthSnapshotPayload struct {
	LastUpdateID uint64     `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

type depthUpdatePayload struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	Symbol        string     `json:"s"`
	FirstSequence uint64     `json:"U"`
	LastSequence  uint64     `json:"u"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
}

type tradePayload struct {
	ID            uint64 `json:"id"`
	Price         string `json:"price"`
	Quantity      string `json:"qty"`
	QuoteQuantity string `json:"quoteQty"`
	Time          int64  `json:"time"`
	BuyerIsMaker  bool   `json:"isBuyerMaker"`
	BestMatch     bool   `json:"isBestMatch"`
}

type candlePayload struct {
	EventType string            `json:"e"`
	EventTime int64             `json:"E"`
	Symbol    string            `json:"s"`
	Candle    candleBodyPayload `json:"k"`
}

type candleBodyPayload struct {
	OpenTime  int64  `json:"t"`
	CloseTime int64  `json:"T"`
	Symbol    string `json:"s"`
	Interval  string `json:"i"`
	Open      string `json:"o"`
	Close     string `json:"c"`
	High      string `json:"h"`
	Low       string `json:"l"`
	Volume    string `json:"v"`
	Closed    bool   `json:"x"`
}

type exchangeInfoPayload struct {
	Timezone   string                      `json:"timezone"`
	ServerTime int64                       `json:"serverTime"`
	Symbols    []exchangeInstrumentPayload `json:"symbols"`
}

type exchangeInstrumentPayload struct {
	Symbol     string          `json:"symbol"`
	Status     string          `json:"status"`
	BaseAsset  string          `json:"baseAsset"`
	QuoteAsset string          `json:"quoteAsset"`
	Filters    []filterPayload `json:"filters"`
}

type filterPayload struct {
	Type            string `json:"filterType"`
	TickSize        string `json:"tickSize,omitempty"`
	MinimumQty      string `json:"minQty,omitempty"`
	StepSize        string `json:"stepSize,omitempty"`
	MinimumNotional string `json:"minNotional,omitempty"`
}
