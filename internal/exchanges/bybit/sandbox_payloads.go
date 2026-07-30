package bybit

import "encoding/json"

type keyInspectionResult struct {
	ID           string              `json:"id"`
	Note         string              `json:"note"`
	APIKey       string              `json:"apiKey"`
	ReadOnly     int                 `json:"readOnly"`
	Secret       string              `json:"secret"`
	Permissions  map[string][]string `json:"permissions"`
	IPs          []string            `json:"ips"`
	Type         int                 `json:"type"`
	DeadlineDay  int64               `json:"deadlineDay"`
	ExpiredAt    string              `json:"expiredAt"`
	CreatedAt    string              `json:"createdAt"`
	UTA          int                 `json:"uta"`
	UserID       uint64              `json:"userID"`
	InviterID    uint64              `json:"inviterID"`
	VIPLevel     string              `json:"vipLevel"`
	MarketMaker  string              `json:"mktMakerLevel"`
	AffiliateID  uint64              `json:"affiliateID"`
	RSAPublicKey string              `json:"rsaPublicKey"`
	IsMaster     bool                `json:"isMaster"`
	ParentUID    string              `json:"parentUid"`
	KYCLevel     string              `json:"kycLevel"`
	KYCRegion    string              `json:"kycRegion"`
	Unified      int                 `json:"unified"`
}

type demoResponseCode struct {
	RetCode int64 `json:"retCode"`
}

type walletBalanceResult struct {
	List []walletAccountPayload `json:"list"`
}

type walletAccountPayload struct {
	AccountType           string              `json:"accountType"`
	AccountIMRate         string              `json:"accountIMRate"`
	AccountIMRateByMP     string              `json:"accountIMRateByMp"`
	AccountMMRate         string              `json:"accountMMRate"`
	AccountMMRateByMP     string              `json:"accountMMRateByMp"`
	AccountLTV            string              `json:"accountLTV"`
	TotalEquity           string              `json:"totalEquity"`
	TotalWalletBalance    string              `json:"totalWalletBalance"`
	TotalMarginBalance    string              `json:"totalMarginBalance"`
	TotalAvailableBalance string              `json:"totalAvailableBalance"`
	TotalPerpetualUPL     string              `json:"totalPerpUPL"`
	TotalInitialMargin    string              `json:"totalInitialMargin"`
	TotalInitialMarginMP  string              `json:"totalInitialMarginByMp"`
	TotalMaintenance      string              `json:"totalMaintenanceMargin"`
	TotalMaintenanceMP    string              `json:"totalMaintenanceMarginByMp"`
	Coin                  []walletCoinPayload `json:"coin"`
}

type walletCoinPayload struct {
	Coin                  string `json:"coin"`
	Equity                string `json:"equity"`
	USDValue              string `json:"usdValue"`
	WalletBalance         string `json:"walletBalance"`
	Free                  string `json:"free"`
	Locked                string `json:"locked"`
	SpotBorrow            string `json:"spotBorrow"`
	BorrowAmount          string `json:"borrowAmount"`
	AvailableToWithdraw   string `json:"availableToWithdraw"`
	AvailableToBorrow     string `json:"availableToBorrow"`
	AccruedInterest       string `json:"accruedInterest"`
	TotalOrderIM          string `json:"totalOrderIM"`
	TotalPositionIM       string `json:"totalPositionIM"`
	TotalPositionMM       string `json:"totalPositionMM"`
	UnrealisedPNL         string `json:"unrealisedPnl"`
	CumulativeRealised    string `json:"cumRealisedPnl"`
	Bonus                 string `json:"bonus"`
	MarginCollateral      bool   `json:"marginCollateral"`
	CollateralSwitch      bool   `json:"collateralSwitch"`
	SpotHedgingQuantity   string `json:"spotHedgingQty"`
	CollateralRestriction string `json:"colRes"`
}

type orderListResult struct {
	Category       string             `json:"category"`
	NextPageCursor string             `json:"nextPageCursor"`
	List           []demoOrderPayload `json:"list"`
}

type orderAcknowledgementResult struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
}

type demoOrderPayload struct {
	Category            string          `json:"category"`
	OrderID             string          `json:"orderId"`
	OrderLinkID         string          `json:"orderLinkId"`
	ParentOrderLinkID   string          `json:"parentOrderLinkId"`
	BlockTradeID        string          `json:"blockTradeId"`
	Symbol              string          `json:"symbol"`
	Price               string          `json:"price"`
	Quantity            string          `json:"qty"`
	Side                string          `json:"side"`
	IsLeverage          string          `json:"isLeverage"`
	PositionIndex       int64           `json:"positionIdx"`
	OrderStatus         string          `json:"orderStatus"`
	CreateType          string          `json:"createType"`
	OrderFilter         string          `json:"orderFilter"`
	CancelType          string          `json:"cancelType"`
	RejectReason        string          `json:"rejectReason"`
	AveragePrice        string          `json:"avgPrice"`
	LeavesQuantity      string          `json:"leavesQty"`
	LeavesValue         string          `json:"leavesValue"`
	CumulativeQuantity  string          `json:"cumExecQty"`
	CumulativeValue     string          `json:"cumExecValue"`
	CumulativeFee       string          `json:"cumExecFee"`
	CumulativeFeeDetail json.RawMessage `json:"cumFeeDetail"`
	TimeInForce         string          `json:"timeInForce"`
	OrderType           string          `json:"orderType"`
	StopOrderType       string          `json:"stopOrderType"`
	OrderIV             string          `json:"orderIv"`
	MarketUnit          string          `json:"marketUnit"`
	SlippageType        string          `json:"slippageToleranceType"`
	SlippageTolerance   string          `json:"slippageTolerance"`
	TriggerPrice        string          `json:"triggerPrice"`
	ActivationPrice     string          `json:"activationPrice"`
	TrailingPercentage  string          `json:"trailingPercentage"`
	TrailingValue       string          `json:"trailingValue"`
	TakeProfit          string          `json:"takeProfit"`
	StopLoss            string          `json:"stopLoss"`
	TPSLMode            string          `json:"tpslMode"`
	OCOTriggerBy        string          `json:"ocoTriggerBy"`
	TPLimitPrice        string          `json:"tpLimitPrice"`
	SLLimitPrice        string          `json:"slLimitPrice"`
	TPTriggerBy         string          `json:"tpTriggerBy"`
	SLTriggerBy         string          `json:"slTriggerBy"`
	PlaceType           string          `json:"placeType"`
	SMPType             string          `json:"smpType"`
	SMPGroup            int64           `json:"smpGroup"`
	SMPOrderID          string          `json:"smpOrderId"`
	TriggerDirection    int64           `json:"triggerDirection"`
	TriggerBy           string          `json:"triggerBy"`
	LastPriceOnCreated  string          `json:"lastPriceOnCreated"`
	BasePrice           string          `json:"basePrice"`
	ReduceOnly          bool            `json:"reduceOnly"`
	CloseOnTrigger      bool            `json:"closeOnTrigger"`
	CreatedTime         string          `json:"createdTime"`
	UpdatedTime         string          `json:"updatedTime"`
	ExtraFees           json.RawMessage `json:"extraFees"`
	RPITakerAccess      bool            `json:"rpiTakerAccess"`
	RPIMatchedQuantity  string          `json:"rpiMatchedQty"`
}

type executionListResult struct {
	Category       string                 `json:"category"`
	NextPageCursor string                 `json:"nextPageCursor"`
	List           []demoExecutionPayload `json:"list"`
}

type demoExecutionPayload struct {
	Category        string          `json:"category"`
	Symbol          string          `json:"symbol"`
	OrderID         string          `json:"orderId"`
	OrderLinkID     string          `json:"orderLinkId"`
	Side            string          `json:"side"`
	OrderPrice      string          `json:"orderPrice"`
	OrderQuantity   string          `json:"orderQty"`
	LeavesQuantity  string          `json:"leavesQty"`
	OrderType       string          `json:"orderType"`
	StopOrderType   string          `json:"stopOrderType"`
	ExecutionFee    string          `json:"execFee"`
	ExecutionFeeV2  string          `json:"execFeeV2"`
	ExecutionID     string          `json:"execId"`
	ExecutionPrice  string          `json:"execPrice"`
	ExecutionQty    string          `json:"execQty"`
	ExecutionPNL    string          `json:"execPnl"`
	ExecutionType   string          `json:"execType"`
	ExecutionValue  string          `json:"execValue"`
	ExecutionTime   string          `json:"execTime"`
	IsMaker         bool            `json:"isMaker"`
	FeeRate         string          `json:"feeRate"`
	FeeCurrency     string          `json:"feeCurrency"`
	MarketUnit      string          `json:"marketUnit"`
	IsLeverage      string          `json:"isLeverage"`
	Sequence        int64           `json:"seq"`
	ClosedSize      string          `json:"closedSize"`
	IndexPrice      string          `json:"indexPrice"`
	MarkPrice       string          `json:"markPrice"`
	MarkIV          string          `json:"markIv"`
	IV              string          `json:"iv"`
	TradeIV         string          `json:"tradeIv"`
	BlockTradeID    string          `json:"blockTradeId"`
	MarkPriceAtFill string          `json:"markPriceAtFill"`
	UnderlyingPrice string          `json:"underlyingPrice"`
	ExtraFees       json.RawMessage `json:"extraFees"`
}

func bindDemoOrderCategories(result *orderListResult) bool {
	if result == nil || result.Category != "spot" {
		return false
	}
	for index := range result.List {
		if result.List[index].Category != "" &&
			result.List[index].Category != result.Category {
			return false
		}
		result.List[index].Category = result.Category
	}
	return true
}

func bindDemoExecutionCategories(result *executionListResult) bool {
	if result == nil || result.Category != "spot" {
		return false
	}
	for index := range result.List {
		if result.List[index].Category != "" &&
			result.List[index].Category != result.Category {
			return false
		}
		result.List[index].Category = result.Category
	}
	return true
}
