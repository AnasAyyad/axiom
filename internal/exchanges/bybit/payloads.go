package bybit

import "encoding/json"

type responseEnvelope[T any] struct {
	RetCode    int64           `json:"retCode"`
	RetMsg     string          `json:"retMsg"`
	Result     T               `json:"result"`
	RetExtInfo json.RawMessage `json:"retExtInfo"`
	Time       int64           `json:"time"`
}

type serverTimeResult struct {
	TimeSecond string `json:"timeSecond"`
	TimeNano   string `json:"timeNano"`
}

type orderBookResult struct {
	Symbol        string     `json:"s"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
	Timestamp     int64      `json:"ts"`
	UpdateID      uint64     `json:"u"`
	CrossSequence uint64     `json:"seq"`
	MatchingTime  int64      `json:"cts"`
}

type instrumentsResult struct {
	Category       string              `json:"category"`
	NextPageCursor string              `json:"nextPageCursor"`
	List           []instrumentPayload `json:"list"`
}

type instrumentPayload struct {
	SymbolID        uint64          `json:"symbolId"`
	Symbol          string          `json:"symbol"`
	BaseCoin        string          `json:"baseCoin"`
	QuoteCoin       string          `json:"quoteCoin"`
	Innovation      string          `json:"innovation"`
	Status          string          `json:"status"`
	MarginTrading   string          `json:"marginTrading"`
	SpecialTag      string          `json:"stTag"`
	LotSizeFilter   lotSizeFilter   `json:"lotSizeFilter"`
	PriceFilter     priceFilter     `json:"priceFilter"`
	RiskParameters  json.RawMessage `json:"riskParameters"`
	SymbolType      string          `json:"symbolType"`
	StockMultiplier string          `json:"xstockMultiplier"`
}

type lotSizeFilter struct {
	BasePrecision      string `json:"basePrecision"`
	QuotePrecision     string `json:"quotePrecision"`
	MaximumOrderQty    string `json:"maxOrderQty"`
	MinimumOrderQty    string `json:"minOrderQty"`
	MinimumOrderAmount string `json:"minOrderAmt"`
	MaximumOrderAmount string `json:"maxOrderAmt"`
	MaximumLimitQty    string `json:"maxLimitOrderQty"`
	MaximumMarketQty   string `json:"maxMarketOrderQty"`
	PostOnlyMaximumQty string `json:"postOnlyMaxLimitOrderSize"`
}

type priceFilter struct {
	TickSize string `json:"tickSize"`
}

type tradesResult struct {
	Category string         `json:"category"`
	List     []tradePayload `json:"list"`
}

type tradePayload struct {
	ExecutionID string `json:"execId"`
	Symbol      string `json:"symbol"`
	Price       string `json:"price"`
	Size        string `json:"size"`
	Side        string `json:"side"`
	Time        string `json:"time"`
	BlockTrade  bool   `json:"isBlockTrade"`
	RPITrade    bool   `json:"isRPITrade"`
}

type candlesResult struct {
	Category string     `json:"category"`
	Symbol   string     `json:"symbol"`
	List     [][]string `json:"list"`
}

type tickersResult struct {
	Category string          `json:"category"`
	List     []tickerPayload `json:"list"`
}

type tickerPayload struct {
	Symbol          string `json:"symbol"`
	BidPrice        string `json:"bid1Price"`
	BidSize         string `json:"bid1Size"`
	AskPrice        string `json:"ask1Price"`
	AskSize         string `json:"ask1Size"`
	LastPrice       string `json:"lastPrice"`
	PreviousPrice   string `json:"prevPrice24h"`
	PriceChangeRate string `json:"price24hPcnt"`
	HighPrice       string `json:"highPrice24h"`
	LowPrice        string `json:"lowPrice24h"`
	Turnover        string `json:"turnover24h"`
	Volume          string `json:"volume24h"`
	USDIndexPrice   string `json:"usdIndexPrice"`
}

type streamEnvelope struct {
	Topic         string          `json:"topic"`
	Type          string          `json:"type"`
	TS            int64           `json:"ts"`
	Data          json.RawMessage `json:"data"`
	CTS           int64           `json:"cts"`
	CrossSequence uint64          `json:"cs"`
	Success       *bool           `json:"success"`
	RetMsg        string          `json:"ret_msg"`
	ConnID        string          `json:"conn_id"`
	RequestID     string          `json:"req_id"`
	Op            string          `json:"op"`
}

type streamTradePayload struct {
	Timestamp     int64  `json:"T"`
	Symbol        string `json:"s"`
	Side          string `json:"S"`
	Size          string `json:"v"`
	Price         string `json:"p"`
	Direction     string `json:"L"`
	TradeID       string `json:"i"`
	CrossSequence uint64 `json:"seq"`
	BlockTrade    bool   `json:"BT"`
	RPITrade      bool   `json:"RPI"`
}

type streamCandlePayload struct {
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
	Interval  string `json:"interval"`
	Open      string `json:"open"`
	Close     string `json:"close"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Volume    string `json:"volume"`
	Turnover  string `json:"turnover"`
	Confirm   bool   `json:"confirm"`
	Timestamp int64  `json:"timestamp"`
}
