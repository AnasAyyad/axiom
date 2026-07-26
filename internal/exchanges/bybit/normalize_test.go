package bybit

import (
	"errors"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func TestB1BybitPublicNormalizationContract(t *testing.T) {
	clock, err := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	received := clock.Now()
	instrument := approvedInstruments()[0]
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "server time", run: func() error {
			_, normalizeErr := NormalizeServerTime([]byte(`{"retCode":0,"retMsg":"OK","result":{"timeSecond":"1784592000","timeNano":"1784592000123456789"},"retExtInfo":{},"time":1784592000123}`))
			return normalizeErr
		}},
		{name: "snapshot", run: func() error {
			_, normalizeErr := NormalizeSnapshot([]byte(`{"retCode":0,"retMsg":"OK","result":{"s":"BTCUSDT","b":[["100","2"]],"a":[["101","3"]],"ts":1784592000000,"u":42,"seq":99,"cts":1784592000000},"retExtInfo":{},"time":1784592000000}`), instrument, received)
			return normalizeErr
		}},
		{name: "ticker", run: func() error {
			_, normalizeErr := NormalizeTicker([]byte(`{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"symbol":"BTCUSDT","bid1Price":"100","bid1Size":"2","ask1Price":"101","ask1Size":"3","lastPrice":"100.5"}]},"retExtInfo":{},"time":1784592000000}`), instrument, received)
			return normalizeErr
		}},
		{name: "candles", run: func() error {
			_, normalizeErr := NormalizeCandleHistory([]byte(`{"retCode":0,"retMsg":"OK","result":{"category":"spot","symbol":"BTCUSDT","list":[["1784592000000","100","102","99","101","4","404"]]},"retExtInfo":{},"time":1784592000000}`), instrument, "1h", received)
			return normalizeErr
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestB1BybitSnapshotDeltaDeleteAndUpdateIDOneReplacement(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
	received := clock.Now()
	tickerState := make(map[string]tickerPayload)
	snapshotJSON := []byte(`{"topic":"orderbook.1000.BTCUSDT","type":"snapshot","ts":1784592000000,"data":{"s":"BTCUSDT","b":[["100","2"],["99","1"]],"a":[["101","3"]],"u":10,"seq":50,"cts":1784592000000},"cts":1784592000000}`)
	_, snapshotEvent, err := normalizeStream(snapshotJSON, received, tickerState)
	if err != nil || snapshotEvent.Snapshot == nil {
		t.Fatalf("snapshot = %#v, %v", snapshotEvent, err)
	}
	book, err := marketdata.NewBook("bybit", approvedInstruments()[0], 1000, 8192, nil)
	if err != nil || book.BeginGeneration("connection-1", 1) != nil {
		t.Fatal(err)
	}
	if err = book.ReplaceSnapshot(*snapshotEvent.Snapshot, testObservation(clock, received, 1, 10)); err != nil {
		t.Fatal(err)
	}
	deltaJSON := []byte(`{"topic":"orderbook.1000.BTCUSDT","type":"delta","ts":1784592000010,"data":{"s":"BTCUSDT","b":[["100","0"],["98","5"]],"a":[["101","4"]],"u":11,"seq":51,"cts":1784592000010},"cts":1784592000010}`)
	_, deltaEvent, err := normalizeStream(deltaJSON, clock.Now(), tickerState)
	if err != nil || deltaEvent.Depth == nil {
		t.Fatalf("delta = %#v, %v", deltaEvent, err)
	}
	if err = book.Apply(marketdata.DepthEvent{Update: *deltaEvent.Depth,
		Observation: testObservation(clock, deltaEvent.Depth.ReceivedAt, 2, 11)}); err != nil {
		t.Fatal(err)
	}
	if bids := book.View().Bids(); len(bids) != 2 || bids[0].Price.String() != "99" {
		t.Fatalf("delete semantics = %#v", bids)
	}
	resetJSON := []byte(`{"topic":"orderbook.1000.BTCUSDT","type":"delta","ts":1784592000020,"data":{"s":"BTCUSDT","b":[["97","7"]],"a":[["103","8"]],"u":1,"seq":52,"cts":1784592000020},"cts":1784592000020}`)
	_, resetEvent, err := normalizeStream(resetJSON, clock.Now(), tickerState)
	if err != nil || resetEvent.Snapshot == nil {
		t.Fatalf("u=1 reset = %#v, %v", resetEvent, err)
	}
	if err = book.ReplaceSnapshot(*resetEvent.Snapshot,
		testObservation(clock, resetEvent.Snapshot.ReceivedAt, 3, 1)); err != nil {
		t.Fatal(err)
	}
	if bids := book.View().Bids(); len(bids) != 1 || bids[0].Price.String() != "97" || book.View().Sequence() != 1 {
		t.Fatalf("replacement = %#v sequence=%d", bids, book.View().Sequence())
	}
}

func TestB1BybitLifecycleTickerMergeAndUnknownFieldsFailClosed(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
	state := make(map[string]tickerPayload)
	_, event, err := normalizeStream([]byte(`{"success":true,"ret_msg":"subscribe","conn_id":"public-1","req_id":"","op":"subscribe"}`), clock.Now(), state)
	if err != nil || event.Lifecycle == nil || event.Lifecycle.State != "SUBSCRIBED" {
		t.Fatalf("lifecycle = %#v, %v", event, err)
	}
	_, event, err = normalizeStream([]byte(`{"success":true,"ret_msg":"pong","conn_id":"public-1","op":"ping"}`), clock.Now(), state)
	if err != nil || event.Lifecycle == nil || event.Lifecycle.State != "HEALTHY" ||
		event.Lifecycle.Reason != "heartbeat_pong" {
		t.Fatalf("Spot heartbeat = %#v, %v", event, err)
	}
	_, event, err = normalizeStream([]byte(`{"success":true,"ret_msg":"","conn_id":"public-1","op":"subscribe"}`), clock.Now(), state)
	if err != nil || event.Lifecycle == nil || event.Lifecycle.State != "SUBSCRIBED" {
		t.Fatalf("Spot subscription = %#v, %v", event, err)
	}
	_, _, err = normalizeStream([]byte(`{"success":true,"ret_msg":"unexpected","conn_id":"public-1","op":"ping"}`), clock.Now(), state)
	var failure *exchangecontracts.Error
	if !errors.As(err, &failure) || failure.Cause != "heartbeat_response_invalid" {
		t.Fatalf("heartbeat failure = %#v, %v", failure, err)
	}
	_, event, err = normalizeStream([]byte(`{"topic":"tickers.BTCUSDT","type":"snapshot","ts":1784592000000,"cs":1,"data":{"symbol":"BTCUSDT","bid1Price":"100","bid1Size":"2","ask1Price":"101","ask1Size":"3","lastPrice":"100.5"}}`), clock.Now(), state)
	if err != nil || event.Ticker == nil {
		t.Fatalf("ticker snapshot = %#v, %v", event, err)
	}
	_, event, err = normalizeStream([]byte(`{"topic":"tickers.BTCUSDT","type":"delta","ts":1784592000010,"cs":2,"data":{"symbol":"BTCUSDT","lastPrice":"100.75"}}`), clock.Now(), state)
	if err != nil || event.Ticker == nil || event.Ticker.BidPrice.String() != "100" || event.Ticker.LastPrice.String() != "100.75" {
		t.Fatalf("ticker delta = %#v, %v", event, err)
	}
	if _, _, err = normalizeStream([]byte(`{"topic":"tickers.BTCUSDT","type":"delta","ts":1784592000010,"cs":3,"data":{"symbol":"BTCUSDT","lastPrice":"100.75","unexpected":true}}`), clock.Now(), state); err == nil {
		t.Fatal("unknown field accepted")
	}
	_, event, err = normalizeStream([]byte(`{"topic":"tickers.BTCUSDT","type":"snapshot","ts":1784592000020,"cs":4,"data":{"symbol":"BTCUSDT","lastPrice":"100.8","highPrice24h":"102","lowPrice24h":"98","prevPrice24h":"99","volume24h":"10","turnover24h":"1000","price24hPcnt":"0.01","usdIndexPrice":"100.7"}}`), clock.Now(), make(map[string]tickerPayload))
	if err != nil || event.Ticker == nil || event.Ticker.BestQuotePresent || event.Ticker.LastPrice.String() != "100.8" {
		t.Fatalf("spot market-statistics ticker = %#v, %v", event, err)
	}
}

func TestB1BybitTradeBatchPreservesEveryDocumentedTradeAndSequence(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
	payload := []byte(`{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1784592000000,"data":[{"T":1784592000000,"s":"BTCUSDT","S":"Buy","v":"0.1","p":"100","L":"PlusTick","i":"trade-1","BT":false,"RPI":true,"seq":10},{"T":1784592000001,"s":"BTCUSDT","S":"Sell","v":"0.2","p":"101","L":"MinusTick","i":"trade-2","BT":true,"RPI":false,"seq":11}]}`)
	_, event, err := normalizeStream(payload, clock.Now(), make(map[string]tickerPayload))
	if err != nil || event.Trade != nil || len(event.Trades) != 2 ||
		event.Trades[0].SourceSequence != 10 || event.Trades[1].NativeID != "trade-2" ||
		!event.Trades[1].BuyerIsMaker {
		t.Fatalf("trade batch=%#v error=%v", event, err)
	}
	if sequence, exchangeTime := canonicalStreamEvidence(event); sequence != "10:11" || exchangeTime == nil {
		t.Fatalf("trade batch evidence=%q/%v", sequence, exchangeTime)
	}
}

func TestB1BybitTradeDecoderRetainsBoundedFailureCause(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
	for _, test := range []struct {
		name    string
		payload string
		cause   string
	}{
		{name: "unknown field", cause: "public_trade_schema_rejected", payload: `{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1784592000000,"data":[{"T":1784592000000,"s":"BTCUSDT","S":"Buy","v":"0.1","p":"100","L":"PlusTick","i":"trade-1","BT":false,"RPI":false,"seq":10,"unexpected":true}]}`},
		{name: "zero sequence", cause: "public_trade_identity_invalid", payload: `{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1784592000000,"data":[{"T":1784592000000,"s":"BTCUSDT","S":"Buy","v":"0.1","p":"100","L":"PlusTick","i":"trade-1","BT":false,"RPI":false,"seq":0}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := normalizeStream([]byte(test.payload), clock.Now(), make(map[string]tickerPayload))
			cause, _, _, _ := exchangecontracts.DiagnosticOf(err)
			if cause != test.cause {
				t.Fatalf("cause=%q want=%q error=%v", cause, test.cause, err)
			}
		})
	}
}

func TestB1BybitRecentTradesAcceptsDocumentedClassificationFlags(t *testing.T) {
	clock, _ := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
	instrument := approvedInstruments()[0]
	payload := []byte(`{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"execId":"trade-1","symbol":"BTCUSDT","price":"100","size":"0.1","side":"Buy","time":"1784592000000","isBlockTrade":true,"isRPITrade":false},{"execId":"trade-2","symbol":"BTCUSDT","price":"101","size":"0.2","side":"Sell","time":"1784592000001","isBlockTrade":false,"isRPITrade":true}]},"retExtInfo":{},"time":1784592000000}`)
	trades, err := NormalizeTrades(payload, instrument, clock.Now())
	if err != nil || len(trades) != 2 || trades[0].NativeID != "trade-1" || !trades[1].BuyerIsMaker {
		t.Fatalf("trades=%#v error=%v", trades, err)
	}
}

func testObservation(
	clock domain.Clock,
	received domain.EventTime,
	ordinal uint64,
	sequence uint64,
) marketdata.Observation {
	processed := clock.Now()
	published := clock.Now()
	return marketdata.Observation{ReceivedAt: received, ProcessedAt: processed, PublishedAt: published,
		ConnectionID: "connection-1", ConnectionGeneration: 1, SourceSequence: sequence,
		IngestOrdinal: ordinal, ReceivedOffsetNanos: ordinal, ProcessedOffsetNanos: ordinal,
		PublishedOffsetNanos: ordinal}
}

func TestB1BybitCapabilitiesRemainCredentialFree(t *testing.T) {
	descriptor, err := Capabilities(time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC))
	if err != nil || descriptor.Exchange != "bybit" || descriptor.AccountMode != exchangecontracts.AccountModePublicOnly {
		t.Fatalf("descriptor = %#v, %v", descriptor, err)
	}
	for _, capability := range descriptor.Capabilities {
		if (capability.Feature == exchangecontracts.FeaturePrivateData ||
			capability.Feature == exchangecontracts.FeatureOrders) && capability.Support != exchangecontracts.Unsupported {
			t.Fatalf("private capability enabled: %#v", capability)
		}
	}
}

func FuzzNormalizeBybitPublicStream(f *testing.F) {
	f.Add([]byte(`{"success":true,"ret_msg":"subscribe","conn_id":"public-1","op":"subscribe"}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		clock, _ := domain.NewReplayClock(time.Date(2026, 7, 21, 0, 0, 1, 0, time.UTC))
		_, _, _ = normalizeStream(payload, clock.Now(), make(map[string]tickerPayload))
	})
}
