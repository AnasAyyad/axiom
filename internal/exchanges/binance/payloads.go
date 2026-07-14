package binance

import "encoding/json"

type serverTimePayload struct {
	ServerTime int64 `json:"serverTime"`
}

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

type combinedStreamPayload struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type streamTradePayload struct {
	EventType     string `json:"e"`
	EventTime     int64  `json:"E"`
	Symbol        string `json:"s"`
	TradeID       uint64 `json:"t"`
	Price         string `json:"p"`
	Quantity      string `json:"q"`
	BuyerOrderID  uint64 `json:"b"`
	SellerOrderID uint64 `json:"a"`
	TradeTime     int64  `json:"T"`
	BuyerIsMaker  bool   `json:"m"`
	BestMatch     bool   `json:"M"`
}

type candleBodyPayload struct {
	OpenTime      int64  `json:"t"`
	CloseTime     int64  `json:"T"`
	Symbol        string `json:"s"`
	Interval      string `json:"i"`
	FirstTradeID  uint64 `json:"f"`
	LastTradeID   uint64 `json:"L"`
	Open          string `json:"o"`
	Close         string `json:"c"`
	High          string `json:"h"`
	Low           string `json:"l"`
	Volume        string `json:"v"`
	TradeCount    uint64 `json:"n"`
	Closed        bool   `json:"x"`
	QuoteVolume   string `json:"q"`
	TakerBuyBase  string `json:"V"`
	TakerBuyQuote string `json:"Q"`
	Unused        string `json:"B"`
}

type exchangeInfoPayload struct {
	Timezone       string                      `json:"timezone"`
	ServerTime     int64                       `json:"serverTime"`
	RateLimits     []rateLimitPayload          `json:"rateLimits"`
	ExchangeFilter []json.RawMessage           `json:"exchangeFilters"`
	Symbols        []exchangeInstrumentPayload `json:"symbols"`
}

type exchangeInstrumentPayload struct {
	Symbol                          string          `json:"symbol"`
	Status                          string          `json:"status"`
	BaseAsset                       string          `json:"baseAsset"`
	BaseAssetPrecision              uint8           `json:"baseAssetPrecision"`
	QuoteAsset                      string          `json:"quoteAsset"`
	QuotePrecision                  uint8           `json:"quotePrecision"`
	QuoteAssetPrecision             uint8           `json:"quoteAssetPrecision"`
	BaseCommissionPrecision         uint8           `json:"baseCommissionPrecision"`
	QuoteCommissionPrecision        uint8           `json:"quoteCommissionPrecision"`
	OrderTypes                      []string        `json:"orderTypes"`
	IcebergAllowed                  bool            `json:"icebergAllowed"`
	OCOAllowed                      bool            `json:"ocoAllowed"`
	OTOAllowed                      bool            `json:"otoAllowed"`
	OPOAllowed                      bool            `json:"opoAllowed"`
	QuoteOrderQtyMarketAllowed      bool            `json:"quoteOrderQtyMarketAllowed"`
	AllowTrailingStop               bool            `json:"allowTrailingStop"`
	CancelReplaceAllowed            bool            `json:"cancelReplaceAllowed"`
	AmendAllowed                    bool            `json:"amendAllowed"`
	PegInstructionsAllowed          bool            `json:"pegInstructionsAllowed"`
	SpotTradingAllowed              bool            `json:"isSpotTradingAllowed"`
	MarginTradingAllowed            bool            `json:"isMarginTradingAllowed"`
	Filters                         []filterPayload `json:"filters"`
	Permissions                     []string        `json:"permissions"`
	PermissionSets                  [][]string      `json:"permissionSets"`
	DefaultSelfTradePreventionMode  string          `json:"defaultSelfTradePreventionMode"`
	AllowedSelfTradePreventionModes []string        `json:"allowedSelfTradePreventionModes"`
}

type filterPayload struct {
	Type                  string `json:"filterType"`
	MinimumPrice          string `json:"minPrice,omitempty"`
	MaximumPrice          string `json:"maxPrice,omitempty"`
	TickSize              string `json:"tickSize,omitempty"`
	MinimumQty            string `json:"minQty,omitempty"`
	MaximumQty            string `json:"maxQty,omitempty"`
	StepSize              string `json:"stepSize,omitempty"`
	Limit                 uint64 `json:"limit,omitempty"`
	MinimumTrailingAbove  uint64 `json:"minTrailingAboveDelta,omitempty"`
	MaximumTrailingAbove  uint64 `json:"maxTrailingAboveDelta,omitempty"`
	MinimumTrailingBelow  uint64 `json:"minTrailingBelowDelta,omitempty"`
	MaximumTrailingBelow  uint64 `json:"maxTrailingBelowDelta,omitempty"`
	BidMultiplierUp       string `json:"bidMultiplierUp,omitempty"`
	BidMultiplierDown     string `json:"bidMultiplierDown,omitempty"`
	AskMultiplierUp       string `json:"askMultiplierUp,omitempty"`
	AskMultiplierDown     string `json:"askMultiplierDown,omitempty"`
	MultiplierUp          string `json:"multiplierUp,omitempty"`
	MultiplierDown        string `json:"multiplierDown,omitempty"`
	AveragePriceMinutes   uint64 `json:"avgPriceMins,omitempty"`
	MinimumNotional       string `json:"minNotional,omitempty"`
	ApplyMinimumToMarket  bool   `json:"applyMinToMarket,omitempty"`
	ApplyToMarket         bool   `json:"applyToMarket,omitempty"`
	MaximumNotional       string `json:"maxNotional,omitempty"`
	ApplyMaximumToMarket  bool   `json:"applyMaxToMarket,omitempty"`
	MaximumOrders         uint64 `json:"maxNumOrders,omitempty"`
	MaximumOrderLists     uint64 `json:"maxNumOrderLists,omitempty"`
	MaximumAlgorithmic    uint64 `json:"maxNumAlgoOrders,omitempty"`
	MaximumOrderAmendment uint64 `json:"maxNumOrderAmends,omitempty"`
}

type rateLimitPayload struct {
	Type     string `json:"rateLimitType"`
	Interval string `json:"interval"`
	Number   uint64 `json:"intervalNum"`
	Limit    uint64 `json:"limit"`
}
