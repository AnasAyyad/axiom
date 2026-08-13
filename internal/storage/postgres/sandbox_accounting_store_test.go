package postgres

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func TestSandboxAccountingTransactionBalancesBuySellFeeAndRebateByAsset(t *testing.T) {
	for _, side := range []domain.Side{domain.SideBuy, domain.SideSell} {
		t.Run(string(side), func(t *testing.T) {
			submission, event, fill := sandboxAccountingFixture(t, side)
			posting, err := newSandboxAccountingTransaction(
				sandboxAccountingTestScope(), submission, event, fill,
			)
			if err != nil {
				t.Fatal(err)
			}
			if posting.Notional != "20" || posting.Fee != "0.1" ||
				posting.Rebate != "0.02" || posting.FeeAsset != "USDT" ||
				len(posting.Entries) != 8 || len(posting.EvidenceHash) != 64 {
				t.Fatalf("posting = %#v", posting)
			}
			assertSandboxAccountingBalanced(t, posting.Entries)
			assertSandboxAccountingOwnershipLines(t, side, posting.Entries)
		})
	}
}

func TestSandboxAccountingTransactionRejectsMissingFeeAssetAndMismatchedTime(t *testing.T) {
	submission, event, fill := sandboxAccountingFixture(t, domain.SideBuy)
	fill.FeeAsset = ""
	if _, err := newSandboxAccountingTransaction(sandboxAccountingTestScope(), submission, event, fill); err == nil {
		t.Fatal("positive fee without an asset was accepted")
	}
	_, event, fill = sandboxAccountingFixture(t, domain.SideBuy)
	event.OrderEvent.OccurredAt = event.ReceivedAt.Add(time.Second)
	if _, err := newSandboxAccountingTransaction(sandboxAccountingTestScope(), submission, event, fill); err == nil {
		t.Fatal("fill observed before its canonical occurrence was accepted")
	}
	_, event, fill = sandboxAccountingFixture(t, domain.SideBuy)
	event.OrderEvent.ClientOrderID = "different-client-order"
	if _, err := newSandboxAccountingTransaction(sandboxAccountingTestScope(), submission, event, fill); err == nil {
		t.Fatal("fill bound to a different canonical order was accepted")
	}
}

func sandboxAccountingTestScope() sandboxAccountingScope {
	return sandboxAccountingScope{
		StrategySessionID: "strategy-session-one",
		ConfigurationID:   "configuration-one",
		Exchange:          sandbox.ExchangeBinance,
		Environment:       sandbox.EnvironmentBinanceSpotTestnet,
	}
}

func sandboxAccountingFixture(
	t *testing.T,
	side domain.Side,
) (sandbox.Submission, sandbox.PrivateEvent, execution.FillFact) {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("plan-strategy-one")
	orderID, _ := domain.NewVirtualOrderID("order-strategy-one")
	strategyID, _ := domain.NewStrategyID("strategy:trend")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("2")
	price, _ := domain.ParsePrice("10")
	notional, _ := domain.ParseNotional("20")
	fillID, _ := domain.NewVirtualFillID("fill-strategy-one")
	fee, _ := domain.ParseFee("0.1")
	rebate, _ := domain.ParseFee("0.02")
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	submission := sandbox.Submission{PlanID: planID, OrderID: orderID,
		AccountID: "account-one", AccountEpoch: 1, ClientOrderID: "client-order-one",
		StrategyID: strategyID, Instrument: instrument, Side: side,
		Quantity: quantity, LimitPrice: price, Notional: notional,
		Style: sandbox.OrderStyleLimitGTC, Action: sandbox.IntentEntry,
		RequestHash: strings.Repeat("a", 64), PolicyHash: strings.Repeat("b", 64),
		ApprovedAt: now.Add(-time.Second)}
	fill := execution.FillFact{ID: fillID, Quantity: quantity, Price: price,
		Fee: fee, Rebate: rebate, FeeAsset: "USDT", Ordinal: 7}
	orderEvent := execution.OrderEvent{ID: "fill-event-one", OrderID: orderID,
		ClientOrderID: submission.ClientOrderID, State: execution.OrderFilled,
		ExchangeStatus: "FILLED", CumulativeQuantity: quantity,
		Fills: []execution.FillFact{fill}, OccurredAt: now, Ordinal: 7}
	event := sandbox.PrivateEvent{Identity: "private-fill-one",
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateFillEvent, OrderID: orderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: strings.Repeat("c", 64), NativeFillHash: strings.Repeat("d", 64),
		OrderEvent: &orderEvent, OccurredAt: now, ReceivedAt: now.Add(time.Millisecond)}
	return submission, event, fill
}

func assertSandboxAccountingBalanced(t *testing.T, entries []sandboxAccountingEntry) {
	t.Helper()
	type totals struct{ debit, credit domain.Balance }
	values := make(map[domain.AssetSymbol]totals)
	zero, _ := domain.ParseBalance("0")
	for _, entry := range entries {
		quantity, err := domain.ParseBalance(entry.Quantity)
		if err != nil || quantity.Compare(zero) <= 0 {
			t.Fatalf("invalid entry = %#v", entry)
		}
		value := values[entry.Asset]
		if value.debit.String() == "" {
			value.debit, value.credit = zero, zero
		}
		if entry.Direction == "debit" {
			value.debit, err = value.debit.Add(quantity)
		} else if entry.Direction == "credit" {
			value.credit, err = value.credit.Add(quantity)
		} else {
			t.Fatalf("invalid direction = %q", entry.Direction)
		}
		if err != nil {
			t.Fatal(err)
		}
		values[entry.Asset] = value
	}
	for asset, value := range values {
		if value.debit.Compare(value.credit) != 0 {
			t.Fatalf("unbalanced %s = %s/%s", asset, value.debit.String(), value.credit.String())
		}
	}
}

func assertSandboxAccountingOwnershipLines(
	t *testing.T,
	side domain.Side,
	entries []sandboxAccountingEntry,
) {
	t.Helper()
	wantDirection := "debit"
	if side == domain.SideSell {
		wantDirection = "credit"
	}
	for _, entry := range entries {
		if entry.AccountClass == "strategy_inventory" && entry.Asset == "BTC" {
			if entry.Direction != wantDirection {
				t.Fatalf("strategy inventory direction = %s, want %s", entry.Direction, wantDirection)
			}
			return
		}
	}
	t.Fatal("strategy inventory line is missing")
}
