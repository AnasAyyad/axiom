package sandboxemulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"

	"axiom/internal/sandbox"
)

func (emulator *Emulator) binancePublic(
	request *http.Request,
) (*http.Response, bool) {
	if emulator.config.Exchange != sandbox.ExchangeBinance ||
		request == nil || request.URL == nil ||
		request.Method != http.MethodGet ||
		request.URL.Scheme != "https" ||
		request.URL.Host != "testnet.binance.vision" ||
		request.Header.Get("X-MBX-APIKEY") != "" {
		return nil, false
	}
	symbol := request.URL.Query().Get("symbol")
	switch request.URL.Path {
	case "/api/v3/time":
		if symbol != "" || len(request.URL.Query()) != 0 {
			return response(http.StatusBadRequest, `{"code":-1102}`), true
		}
		return response(
			http.StatusOK,
			`{"serverTime":1700000000000}`,
		), true
	case "/api/v3/exchangeInfo":
		if !approvedBinanceSymbol(symbol) {
			return response(http.StatusBadRequest, `{"code":-1121}`), true
		}
		return response(http.StatusOK, binanceExchangeInfo(symbol)), true
	case "/api/v3/avgPrice":
		if !approvedBinanceSymbol(symbol) {
			return response(http.StatusBadRequest, `{"code":-1121}`), true
		}
		return response(
			http.StatusOK,
			`{"mins":5,"price":"100","closeTime":1700000000000}`,
		), true
	case "/api/v3/depth":
		if !approvedBinanceSymbol(symbol) ||
			request.URL.Query().Get("limit") != "5" ||
			len(request.URL.Query()) != 2 {
			return response(http.StatusBadRequest, `{"code":-1121}`), true
		}
		return response(
			http.StatusOK,
			`{"lastUpdateId":42,"bids":[["99","1"]],"asks":[["101","1"]]}`,
		), true
	default:
		return nil, false
	}
}

func (emulator *Emulator) handleBinance(
	request *http.Request,
	clientID string,
) (*http.Response, error) {
	switch request.URL.Path {
	case "/api/v3/account":
		return response(http.StatusOK, binanceAccount()), nil
	case "/api/v3/openOrders":
		return response(http.StatusOK, emulator.binanceOrders(true, request.URL.Query().Get("symbol"))), nil
	case "/api/v3/allOrders":
		return response(http.StatusOK, emulator.binanceOrders(false, request.URL.Query().Get("symbol"))), nil
	case "/api/v3/myTrades":
		return response(http.StatusOK, `[]`), nil
	case "/api/v3/order/test":
		return response(http.StatusOK, `{}`), nil
	case "/api/v3/order":
		switch request.Method {
		case http.MethodPost:
			return emulator.createBinanceOrder(request.URL.Query())
		case http.MethodGet:
			order := emulator.orders[clientID]
			if order == nil {
				return response(http.StatusBadRequest, `{"code":-2013}`), nil
			}
			return response(http.StatusOK, marshalBinanceOrder(order)), nil
		case http.MethodDelete:
			order := emulator.orders[clientID]
			if order == nil {
				return response(http.StatusBadRequest, `{"code":-2011}`), nil
			}
			if order.Status != "FILLED" {
				order.Status = "CANCELED"
				order.UpdatedAt++
				emulator.appendBinanceExecution(order, "CANCELED")
			}
			return response(http.StatusOK, marshalBinanceOrder(order)), nil
		}
	}
	return nil, errors.New("binance_operation_unsupported")
}

func (emulator *Emulator) createBinanceOrder(
	values url.Values,
) (*http.Response, error) {
	clientID := values.Get("newClientOrderId")
	if clientID == "" || emulator.orders[clientID] != nil {
		return nil, errors.New("binance_duplicate_order")
	}
	timeInForce := values.Get("timeInForce")
	if values.Get("type") == "LIMIT_MAKER" {
		timeInForce = "GTC"
	}
	order := &emulatorOrder{
		ClientOrderID: clientID,
		OrderID:       emulator.nextOrderID,
		Symbol:        values.Get("symbol"),
		Price:         values.Get("price"),
		Quantity:      values.Get("quantity"),
		Side:          values.Get("side"),
		Type:          values.Get("type"),
		TimeInForce:   timeInForce,
		Status:        "NEW",
		CreatedAt:     1_700_000_000_000 + int64(emulator.nextOrderID),
		UpdatedAt:     1_700_000_000_000 + int64(emulator.nextOrderID),
	}
	emulator.nextOrderID++
	emulator.orders[clientID] = order
	hash := sha256.Sum256([]byte(string(emulator.config.Exchange) + "|" + clientID))
	emulator.nativeByID[clientID] = hex.EncodeToString(hash[:16])
	emulator.appendBinanceExecution(order, "NEW")
	acknowledgement := map[string]any{
		"symbol":        order.Symbol,
		"orderId":       order.OrderID,
		"orderListId":   -1,
		"clientOrderId": order.ClientOrderID,
		"transactTime":  order.CreatedAt,
	}
	body, _ := json.Marshal(acknowledgement)
	return response(http.StatusOK, string(body)), nil
}

func (emulator *Emulator) binanceOrders(
	openOnly bool,
	symbol string,
) string {
	ids := make([]string, 0, len(emulator.orders))
	for clientID := range emulator.orders {
		ids = append(ids, clientID)
	}
	sort.Strings(ids)
	orders := make([]json.RawMessage, 0, len(ids))
	for _, clientID := range ids {
		order := emulator.orders[clientID]
		if symbol != "" && order.Symbol != symbol {
			continue
		}
		if openOnly && order.Status != "NEW" &&
			order.Status != "PARTIALLY_FILLED" {
			continue
		}
		orders = append(orders, json.RawMessage(marshalBinanceOrder(order)))
	}
	body, _ := json.Marshal(orders)
	return string(body)
}

func (emulator *Emulator) appendBinanceExecution(
	order *emulatorOrder,
	executionType string,
) {
	event := map[string]any{
		"e": "executionReport",
		"E": order.UpdatedAt,
		"s": order.Symbol,
		"c": order.ClientOrderID,
		"S": order.Side,
		"o": order.Type,
		"f": order.TimeInForce,
		"q": order.Quantity,
		"p": order.Price,
		"P": "0",
		"F": "0",
		"g": -1,
		"C": "",
		"x": executionType,
		"X": order.Status,
		"r": "NONE",
		"i": order.OrderID,
		"l": "0",
		"z": "0",
		"L": "0",
		"n": "0",
		"N": nil,
		"T": order.UpdatedAt,
		"t": -1,
		"I": order.OrderID,
		"w": order.Status == "NEW",
		"m": false,
		"M": false,
		"O": order.CreatedAt,
		"Z": "0",
		"Y": "0",
		"Q": "0",
		"W": order.UpdatedAt,
		"V": "NONE",
	}
	envelope, _ := json.Marshal(map[string]any{
		"subscriptionId": 1,
		"event":          event,
	})
	emulator.privateFrames = append(emulator.privateFrames, envelope)
}

func marshalBinanceOrder(order *emulatorOrder) string {
	body, _ := json.Marshal(map[string]any{
		"symbol":                  order.Symbol,
		"orderId":                 order.OrderID,
		"orderListId":             -1,
		"clientOrderId":           order.ClientOrderID,
		"price":                   order.Price,
		"origQty":                 order.Quantity,
		"executedQty":             "0",
		"cummulativeQuoteQty":     "0",
		"status":                  order.Status,
		"timeInForce":             order.TimeInForce,
		"type":                    order.Type,
		"side":                    order.Side,
		"stopPrice":               "0",
		"icebergQty":              "0",
		"time":                    order.CreatedAt,
		"updateTime":              order.UpdatedAt,
		"isWorking":               order.Status == "NEW",
		"workingTime":             order.CreatedAt,
		"origQuoteOrderQty":       "0",
		"selfTradePreventionMode": "NONE",
	})
	return string(body)
}

func binanceAccount() string {
	return `{
	  "makerCommission":0,"takerCommission":0,
	  "buyerCommission":0,"sellerCommission":0,
	  "commissionRates":{"maker":"0","taker":"0","buyer":"0","seller":"0"},
	  "canTrade":true,"canWithdraw":false,"canDeposit":true,
	  "brokered":false,"requireSelfTradePrevention":false,"preventSor":false,
	  "updateTime":1700000000000,"accountType":"SPOT",
	  "balances":[
	    {"asset":"BTC","free":"1","locked":"0"},
	    {"asset":"ETH","free":"1","locked":"0"},
	    {"asset":"USDT","free":"100","locked":"0"}
	  ],
	  "permissions":["SPOT"],"uid":12345
	}`
}

func binanceExchangeInfo(symbol string) string {
	base, quote := "BTC", "USDT"
	switch symbol {
	case "ETHUSDT":
		base = "ETH"
	case "ETHBTC":
		base, quote = "ETH", "BTC"
	}
	body := map[string]any{
		"timezone":        "UTC",
		"serverTime":      int64(1_700_000_000_000),
		"rateLimits":      []any{},
		"exchangeFilters": []any{},
		"symbols":         []any{binanceSymbolDefinition(symbol, base, quote)},
	}
	encoded, _ := json.Marshal(body)
	return string(encoded)
}

func binanceSymbolDefinition(symbol, base, quote string) map[string]any {
	return map[string]any{
		"symbol":                          symbol,
		"status":                          "TRADING",
		"baseAsset":                       base,
		"baseAssetPrecision":              8,
		"quoteAsset":                      quote,
		"quotePrecision":                  8,
		"quoteAssetPrecision":             8,
		"baseCommissionPrecision":         8,
		"quoteCommissionPrecision":        8,
		"orderTypes":                      []string{"LIMIT", "LIMIT_MAKER"},
		"icebergAllowed":                  false,
		"ocoAllowed":                      false,
		"otoAllowed":                      false,
		"opoAllowed":                      false,
		"quoteOrderQtyMarketAllowed":      false,
		"allowTrailingStop":               false,
		"cancelReplaceAllowed":            false,
		"amendAllowed":                    false,
		"pegInstructionsAllowed":          false,
		"isSpotTradingAllowed":            true,
		"isMarginTradingAllowed":          false,
		"filters":                         binanceSymbolFilters(),
		"permissions":                     []string{"SPOT"},
		"permissionSets":                  []any{},
		"defaultSelfTradePreventionMode":  "NONE",
		"allowedSelfTradePreventionModes": []string{"NONE"},
	}
}

func binanceSymbolFilters() []any {
	return []any{
		map[string]any{
			"filterType": "PRICE_FILTER", "minPrice": "0.00000001",
			"maxPrice": "1000000", "tickSize": "0.00000001",
		},
		map[string]any{
			"filterType": "LOT_SIZE", "minQty": "0.00000001",
			"maxQty": "1000", "stepSize": "0.00000001",
		},
		map[string]any{
			"filterType": "NOTIONAL", "minNotional": "0.00000001",
			"applyMinToMarket": false, "maxNotional": "1000000",
			"applyMaxToMarket": false,
		},
		map[string]any{
			"filterType": "PERCENT_PRICE", "multiplierUp": "5",
			"multiplierDown": "0.2", "avgPriceMins": 5,
		},
	}
}

func approvedBinanceSymbol(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT" || symbol == "ETHBTC"
}
