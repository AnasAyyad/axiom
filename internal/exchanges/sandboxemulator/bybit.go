package sandboxemulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"axiom/internal/sandbox"
)

func (emulator *Emulator) bybitPublic(
	request *http.Request,
) (*http.Response, bool) {
	if emulator.config.Exchange != sandbox.ExchangeBybit ||
		request == nil || request.URL == nil ||
		request.Method != http.MethodGet ||
		request.URL.Scheme != "https" ||
		request.Header.Get("X-BAPI-API-KEY") != "" {
		return nil, false
	}
	switch request.URL.Path {
	case "/v5/market/time":
		return bybitTimeResponse(request), true
	case "/v5/market/instruments-info":
		return bybitInstrumentResponse(request), true
	case "/v5/market/orderbook":
		return bybitOrderBookResponse(request), true
	default:
		return nil, false
	}
}

func bybitTimeResponse(request *http.Request) *http.Response {
	if request.URL.Host != "api-demo.bybit.com" ||
		len(request.URL.Query()) != 0 {
		return response(http.StatusBadRequest, `{}`)
	}
	return response(http.StatusOK, bybitEnvelope(0, "OK", map[string]string{
		"timeSecond": "1700000000",
		"timeNano":   "1700000000000000000",
	}))
}

func bybitInstrumentResponse(request *http.Request) *http.Response {
	symbol := request.URL.Query().Get("symbol")
	if request.URL.Host != "api.bybit.com" ||
		request.URL.Query().Get("category") != "spot" ||
		!approvedBybitSymbol(symbol) || len(request.URL.Query()) != 2 {
		return response(http.StatusBadRequest, `{}`)
	}
	return response(http.StatusOK, bybitInstrument(symbol))
}

func bybitOrderBookResponse(request *http.Request) *http.Response {
	symbol := request.URL.Query().Get("symbol")
	if request.URL.Host != "api.bybit.com" ||
		request.URL.Query().Get("category") != "spot" ||
		request.URL.Query().Get("limit") != "1" ||
		!approvedBybitSymbol(symbol) || len(request.URL.Query()) != 3 {
		return response(http.StatusBadRequest, `{}`)
	}
	return response(http.StatusOK, bybitEnvelope(0, "OK", map[string]any{
		"s": symbol, "b": [][]string{{"99", "1"}},
		"a":  [][]string{{"101", "1"}},
		"ts": int64(1_700_000_000_000), "u": uint64(42),
		"seq": uint64(42), "cts": int64(1_700_000_000_000),
	}))
}

func (emulator *Emulator) handleBybit(
	request *http.Request,
	clientID string,
) (*http.Response, error) {
	switch request.URL.Path {
	case "/v5/user/query-api":
		return response(http.StatusOK, bybitKeyInspection(emulator.config.APIKey)), nil
	case "/v5/account/wallet-balance":
		return response(http.StatusOK, bybitWallet()), nil
	case "/v5/order/create":
		return emulator.createBybitOrder(request, clientID)
	case "/v5/order/cancel":
		return emulator.cancelBybitOrder(clientID)
	case "/v5/order/realtime":
		order := emulator.orders[clientID]
		if order == nil {
			return nil, errors.New("bybit_order_missing")
		}
		return response(
			http.StatusOK,
			bybitOrderList([]*emulatorOrder{order}),
		), nil
	case "/v5/order/history":
		orders := emulator.bybitOrders(request.URL.Query().Get("symbol"))
		if clientOrderID := request.URL.Query().Get("orderLinkId"); clientOrderID != "" {
			orders = orders[:0]
			if order := emulator.orders[clientOrderID]; order != nil {
				orders = append(orders, order)
			}
		}
		return response(
			http.StatusOK,
			bybitOrderList(orders),
		), nil
	case "/v5/execution/list":
		return response(
			http.StatusOK,
			bybitEnvelope(0, "OK", map[string]any{
				"category":       "spot",
				"nextPageCursor": "",
				"list":           []any{},
			}),
		), nil
	default:
		return nil, errors.New("bybit_operation_unsupported")
	}
}

func (emulator *Emulator) createBybitOrder(
	request *http.Request,
	clientID string,
) (*http.Response, error) {
	if clientID == "" || emulator.orders[clientID] != nil {
		return nil, errors.New("bybit_duplicate_order")
	}
	var values map[string]string
	if json.NewDecoder(request.Body).Decode(&values) != nil {
		return nil, errors.New("bybit_body_invalid")
	}
	order := &emulatorOrder{
		ClientOrderID: clientID,
		OrderID:       emulator.nextOrderID,
		Symbol:        values["symbol"],
		Price:         values["price"],
		Quantity:      values["qty"],
		Side:          values["side"],
		Type:          values["orderType"],
		TimeInForce:   values["timeInForce"],
		Status:        "New",
		CreatedAt:     1_700_000_000_000 + int64(emulator.nextOrderID),
		UpdatedAt:     1_700_000_000_000 + int64(emulator.nextOrderID),
	}
	emulator.nextOrderID++
	emulator.orders[clientID] = order
	hash := sha256.Sum256([]byte(string(emulator.config.Exchange) + "|" + clientID))
	emulator.nativeByID[clientID] = hex.EncodeToString(hash[:16])
	emulator.appendBybitOrder(order)
	return response(
		http.StatusOK,
		bybitEnvelope(0, "OK", map[string]string{
			"orderId":     strconv.FormatUint(order.OrderID, 10),
			"orderLinkId": order.ClientOrderID,
		}),
	), nil
}

func (emulator *Emulator) cancelBybitOrder(
	clientID string,
) (*http.Response, error) {
	order := emulator.orders[clientID]
	if order == nil {
		return nil, errors.New("bybit_order_missing")
	}
	if order.Status != "Filled" {
		order.Status = "Cancelled"
		order.UpdatedAt++
		emulator.appendBybitOrder(order)
	}
	return response(
		http.StatusOK,
		bybitEnvelope(0, "OK", map[string]string{
			"orderId":     strconv.FormatUint(order.OrderID, 10),
			"orderLinkId": order.ClientOrderID,
		}),
	), nil
}

func (emulator *Emulator) bybitOrders(
	symbol string,
) []*emulatorOrder {
	ids := make([]string, 0, len(emulator.orders))
	for clientID := range emulator.orders {
		ids = append(ids, clientID)
	}
	sort.Strings(ids)
	orders := make([]*emulatorOrder, 0, len(ids))
	for _, clientID := range ids {
		order := emulator.orders[clientID]
		if symbol == "" || order.Symbol == symbol {
			orders = append(orders, order)
		}
	}
	return orders
}

func (emulator *Emulator) appendBybitOrder(
	order *emulatorOrder,
) {
	frame, _ := json.Marshal(map[string]any{
		"id":           fmt.Sprintf("bybit-order-%d-%d", order.OrderID, order.UpdatedAt),
		"topic":        "order.spot",
		"creationTime": order.UpdatedAt,
		"data":         []any{bybitOrderObject(order)},
	})
	emulator.privateFrames = append(emulator.privateFrames, frame)
}

func bybitOrderList(orders []*emulatorOrder) string {
	list := make([]any, 0, len(orders))
	for _, order := range orders {
		list = append(list, bybitOrderObject(order))
	}
	return bybitEnvelope(0, "OK", map[string]any{
		"category":       "spot",
		"nextPageCursor": "",
		"list":           list,
	})
}

func bybitOrderObject(order *emulatorOrder) map[string]any {
	return map[string]any{
		"category":       "spot",
		"orderId":        strconv.FormatUint(order.OrderID, 10),
		"orderLinkId":    order.ClientOrderID,
		"symbol":         order.Symbol,
		"price":          order.Price,
		"qty":            order.Quantity,
		"side":           order.Side,
		"isLeverage":     "0",
		"positionIdx":    0,
		"orderStatus":    order.Status,
		"orderFilter":    "Order",
		"avgPrice":       "",
		"leavesQty":      order.Quantity,
		"leavesValue":    "0",
		"cumExecQty":     "0",
		"cumExecValue":   "0",
		"cumExecFee":     "0",
		"timeInForce":    order.TimeInForce,
		"orderType":      "Limit",
		"stopOrderType":  "",
		"marketUnit":     "",
		"triggerPrice":   "",
		"takeProfit":     "",
		"stopLoss":       "",
		"reduceOnly":     false,
		"closeOnTrigger": false,
		"createdTime":    strconv.FormatInt(order.CreatedAt, 10),
		"updatedTime":    strconv.FormatInt(order.UpdatedAt, 10),
	}
}

func bybitEnvelope(code int64, message string, result any) string {
	body, _ := json.Marshal(map[string]any{
		"retCode":    code,
		"retMsg":     message,
		"result":     result,
		"retExtInfo": map[string]any{},
		"time":       int64(1_700_000_000_000),
	})
	return string(body)
}

func bybitKeyInspection(apiKey string) string {
	return bybitEnvelope(0, "OK", map[string]any{
		"id":       "demo-account",
		"note":     "",
		"apiKey":   apiKey,
		"readOnly": 0,
		"secret":   "",
		"permissions": map[string]any{
			"ContractTrade": []string{},
			"Spot":          []string{"SpotTrade"},
			"Wallet":        []string{},
			"Options":       []string{},
			"Derivatives":   []string{},
		},
		"deadlineDay":   0,
		"expiredAt":     "",
		"createdAt":     "",
		"uta":           1,
		"userID":        uint64(12345),
		"inviterID":     uint64(0),
		"vipLevel":      "",
		"mktMakerLevel": "",
		"affiliateID":   uint64(0),
		"rsaPublicKey":  "",
		"isMaster":      true,
		"parentUid":     "",
		"kycLevel":      "",
		"kycRegion":     "",
		"unified":       1,
	})
}

func bybitWallet() string {
	coins := []any{
		bybitCoin("BTC", "1"),
		bybitCoin("ETH", "1"),
		bybitCoin("USDT", "100"),
	}
	return bybitEnvelope(0, "OK", map[string]any{
		"list": []any{map[string]any{
			"accountType":                "UNIFIED",
			"accountIMRate":              "0",
			"accountIMRateByMp":          "0",
			"accountMMRate":              "0",
			"accountMMRateByMp":          "0",
			"accountLTV":                 "0",
			"totalEquity":                "102",
			"totalWalletBalance":         "102",
			"totalMarginBalance":         "102",
			"totalAvailableBalance":      "102",
			"totalPerpUPL":               "0",
			"totalInitialMargin":         "0",
			"totalInitialMarginByMp":     "0",
			"totalMaintenanceMargin":     "0",
			"totalMaintenanceMarginByMp": "0",
			"coin":                       coins,
		}},
	})
}

func bybitCoin(symbol, balance string) map[string]any {
	return map[string]any{
		"coin":                symbol,
		"equity":              balance,
		"usdValue":            balance,
		"walletBalance":       balance,
		"locked":              "0",
		"spotBorrow":          "0",
		"borrowAmount":        "0",
		"availableToWithdraw": balance,
		"availableToBorrow":   "",
		"accruedInterest":     "0",
		"totalOrderIM":        "0",
		"totalPositionIM":     "0",
		"totalPositionMM":     "0",
		"unrealisedPnl":       "0",
		"cumRealisedPnl":      "0",
		"bonus":               "0",
		"marginCollateral":    false,
		"collateralSwitch":    false,
		"spotHedgingQty":      "0",
	}
}

func bybitInstrument(symbol string) string {
	base, quote := "BTC", "USDT"
	if symbol == "ETHUSDT" {
		base = "ETH"
	}
	if symbol == "ETHBTC" {
		base, quote = "ETH", "BTC"
	}
	return bybitEnvelope(0, "OK", map[string]any{
		"category":       "spot",
		"nextPageCursor": "",
		"list": []any{map[string]any{
			"symbolId":      uint64(1),
			"symbol":        symbol,
			"baseCoin":      base,
			"quoteCoin":     quote,
			"innovation":    "0",
			"status":        "Trading",
			"marginTrading": "none",
			"stTag":         "0",
			"lotSizeFilter": map[string]any{
				"basePrecision":             "0.00000001",
				"quotePrecision":            "0.00000001",
				"maxOrderQty":               "1000",
				"minOrderQty":               "0.00000001",
				"minOrderAmt":               "0.00000001",
				"maxOrderAmt":               "1000000",
				"maxLimitOrderQty":          "1000",
				"maxMarketOrderQty":         "1000",
				"postOnlyMaxLimitOrderSize": "1000",
			},
			"priceFilter": map[string]any{
				"tickSize": "0.00000001",
			},
			"riskParameters":   map[string]any{},
			"symbolType":       "",
			"xstockMultiplier": "",
		}},
	})
}

func approvedBybitSymbol(symbol string) bool {
	return symbol == "BTCUSDT" ||
		symbol == "ETHUSDT" ||
		symbol == "ETHBTC"
}
