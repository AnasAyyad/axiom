package accounting

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/runtime"
)

func TestJournalBalancesEveryAssetAndRebuildsIdentically(t *testing.T) {
	journal := NewMemoryJournal()
	transaction := journalTransaction(t)
	if err := journal.Append(transaction); err != nil {
		t.Fatal(err)
	}
	first, firstHash, err := journal.Rebuild()
	if err != nil || len(first) != 4 || firstHash == "" {
		t.Fatalf("rebuild = %#v, %q, %v", first, firstHash, err)
	}
	second, secondHash, _ := journal.Rebuild()
	if secondHash != firstHash || len(second) != len(first) {
		t.Fatal("journal rebuild was not deterministic")
	}
	copy := journal.Transactions()
	copy[0].Lines[0].Account.Owner = "mutated"
	if journal.Transactions()[0].Lines[0].Account.Owner == "mutated" {
		t.Fatal("journal mutated through returned history")
	}
}

func TestJournalRejectsCrossCommodityAndDuplicateBalancing(t *testing.T) {
	journal := NewMemoryJournal()
	transaction := journalTransaction(t)
	transaction.Lines[1].Account.Asset = "BTC"
	if err := journal.Append(transaction); err == nil {
		t.Fatal("USDT debit was balanced numerically against BTC credit")
	}
	transaction = journalTransaction(t)
	if err := journal.Append(transaction); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(transaction); err == nil {
		t.Fatal("duplicate immutable transaction accepted")
	}
}

func TestJournalReversalMustExactlyOpposeOneExistingTransaction(t *testing.T) {
	journal := NewMemoryJournal()
	original := journalTransaction(t)
	if err := journal.Append(original); err != nil {
		t.Fatal(err)
	}
	reversal := journalTransaction(t)
	reversal.ID, _ = domain.NewJournalTransactionID("journal-reversal")
	reversal.Type = "reversal"
	reversalID := original.ID
	reversal.ReversalOf = &reversalID
	for index := range reversal.Lines {
		if reversal.Lines[index].Direction == Debit {
			reversal.Lines[index].Direction = Credit
		} else {
			reversal.Lines[index].Direction = Debit
		}
	}
	if err := journal.Append(reversal); err != nil {
		t.Fatal(err)
	}
	other, _ := domain.NewJournalTransactionID("caller-mutated-target")
	reversalID = other
	if journal.Transactions()[1].ReversalOf.String() != original.ID.String() {
		t.Fatal("journal retained caller-owned reversal pointer")
	}
	projections, _, err := journal.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	for _, projection := range projections {
		if projection.Debits.Compare(projection.Credits) != 0 {
			t.Fatalf("reversal did not neutralize account: %#v", projection)
		}
	}
	duplicate := reversal
	duplicate.ID, _ = domain.NewJournalTransactionID("journal-reversal-duplicate")
	if err := journal.Append(duplicate); err == nil {
		t.Fatal("second reversal of the same transaction accepted")
	}
	missing := reversal
	missing.ID, _ = domain.NewJournalTransactionID("journal-reversal-missing")
	missingTarget, _ := domain.NewJournalTransactionID("journal-missing")
	missing.ReversalOf = &missingTarget
	if err := journal.Append(missing); err == nil {
		t.Fatal("reversal of missing transaction accepted")
	}
}

func TestJournalRejectsBalancedButUnrelatedReversal(t *testing.T) {
	journal := NewMemoryJournal()
	original := journalTransaction(t)
	if err := journal.Append(original); err != nil {
		t.Fatal(err)
	}
	reversal := journalTransaction(t)
	reversal.ID, _ = domain.NewJournalTransactionID("journal-bad-reversal")
	target := original.ID
	reversal.ReversalOf = &target
	if err := journal.Append(reversal); err == nil {
		t.Fatal("balanced but non-opposite reversal accepted")
	}
}

func FuzzJournalPerAssetBalance(f *testing.F) {
	f.Add("1.25", "0.001")
	f.Add("500", "2")
	f.Fuzz(func(t *testing.T, quoteText, baseText string) {
		quote, quoteErr := domain.ParseBalance(quoteText)
		base, baseErr := domain.ParseBalance(baseText)
		if quoteErr != nil || baseErr != nil || !positive(quote) || !positive(base) {
			t.Skip()
		}
		transaction := journalTransaction(t)
		transaction.Lines[0].Quantity, transaction.Lines[1].Quantity = quote, quote
		transaction.Lines[2].Quantity, transaction.Lines[3].Quantity = base, base
		if err := ValidateTransaction(transaction); err != nil {
			t.Fatal(err)
		}
		transaction.Lines[3].Quantity = quote
		if quote.Compare(base) != 0 && ValidateTransaction(transaction) == nil {
			t.Fatal("unbalanced base commodity accepted")
		}
	})
}

func TestReservationsRejectConcurrentDoubleSpend(t *testing.T) {
	ledger := NewReservationLedger()
	account, _ := domain.NewVirtualAccountID("account-a")
	key := BalanceKey{Account: account, Asset: "USDT"}
	available, _ := domain.ParseBalance("500")
	quantity, _ := domain.ParseBalance("400")
	if err := ledger.OpenBalance(key, available); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var group sync.WaitGroup
	for _, value := range []string{"reservation-a", "reservation-b"} {
		value := value
		group.Add(1)
		go func() {
			defer group.Done()
			id, _ := domain.NewReservationID(value)
			if _, err := ledger.Reserve(id, key, quantity, 1); err == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful reservations = %d", successes.Load())
	}
	balance, _ := ledger.Balance(key)
	wantAvailable, _ := domain.ParseBalance("100")
	if balance.Available.Compare(wantAvailable) != 0 || balance.Reserved.Compare(quantity) != 0 {
		t.Fatalf("balance = %#v", balance)
	}
}

func TestReservationLifecycleRequiresRevisionAndFence(t *testing.T) {
	ledger := NewReservationLedger()
	account, _ := domain.NewVirtualAccountID("account-a")
	key := BalanceKey{Account: account, Asset: "BTC"}
	available, _ := domain.ParseBalance("2")
	quantity, _ := domain.ParseBalance("1")
	_ = ledger.OpenBalance(key, available)
	id, _ := domain.NewReservationID("reservation-a")
	reservation, _ := ledger.Reserve(id, key, quantity, runtimecore.FencingToken(7))
	if err := ledger.Release(id, reservation.Revision, 8); err == nil {
		t.Fatal("wrong fencing token released ownership")
	}
	if err := ledger.Release(id, reservation.Revision, 7); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Consume(id, reservation.Revision, 7); err == nil {
		t.Fatal("closed reservation was consumed")
	}
	balance, _ := ledger.Balance(key)
	zero, _ := domain.ParseBalance("0")
	if balance.Available.Compare(available) != 0 || balance.Reserved.Compare(zero) != 0 {
		t.Fatalf("released balance = %#v", balance)
	}
}

func TestReservationQuarantineKeepsUncertainOwnershipReserved(t *testing.T) {
	ledger := NewReservationLedger()
	account, _ := domain.NewVirtualAccountID("account-a")
	key := BalanceKey{Account: account, Asset: "USDT"}
	available, _ := domain.ParseBalance("500")
	quantity, _ := domain.ParseBalance("400")
	_ = ledger.OpenBalance(key, available)
	id, _ := domain.NewReservationID("reservation-a")
	reservation, _ := ledger.Reserve(id, key, quantity, 9)
	if err := ledger.Quarantine(id, reservation.Revision, 9); err != nil {
		t.Fatal(err)
	}
	balance, _ := ledger.Balance(key)
	wantAvailable, _ := domain.ParseBalance("100")
	if balance.Available.Compare(wantAvailable) != 0 || balance.Reserved.Compare(quantity) != 0 {
		t.Fatalf("quarantine released uncertain funds: %#v", balance)
	}
	if err := ledger.Release(id, reservation.Revision+1, 9); err == nil {
		t.Fatal("quarantined ownership released without reconciliation")
	}
}

func TestWeightedAverageCostAndRealizedPnLStaySeparated(t *testing.T) {
	basis := NewCostBasis()
	firstQuantity, _ := domain.ParseBalance("2")
	firstPrice, _ := domain.ParsePrice("100")
	firstFee, _ := domain.ParseFee("2")
	var err error
	basis, err = basis.Buy(firstQuantity, firstPrice, firstFee, 8)
	if err != nil {
		t.Fatal(err)
	}
	secondQuantity, _ := domain.ParseBalance("2")
	secondPrice, _ := domain.ParsePrice("200")
	zeroFee, _ := domain.ParseFee("0")
	basis, err = basis.Buy(secondQuantity, secondPrice, zeroFee, 8)
	if err != nil {
		t.Fatal(err)
	}
	wantAverage, _ := domain.ParsePrice("150.5")
	if basis.AveragePrice.Compare(wantAverage) != 0 {
		t.Fatalf("weighted average = %s", basis.AveragePrice.String())
	}
	sellQuantity, _ := domain.ParseBalance("1")
	proceeds, _ := domain.ParseMoney("175")
	sellFee, _ := domain.ParseFee("1")
	remaining, pnl, err := basis.Sell(sellQuantity, proceeds, sellFee, 8)
	if err != nil {
		t.Fatal(err)
	}
	wantPnL, _ := domain.ParsePnL("23.5")
	wantRemaining, _ := domain.ParseBalance("3")
	if pnl.String() != wantPnL.String() || remaining.Quantity.Compare(wantRemaining) != 0 ||
		remaining.AveragePrice.Compare(wantAverage) != 0 {
		t.Fatalf("remaining=%#v pnl=%s", remaining, pnl.String())
	}
}

func TestWeightedAverageCostRejectsOversell(t *testing.T) {
	basis := NewCostBasis()
	quantity, _ := domain.ParseBalance("1")
	price, _ := domain.ParsePrice("10")
	fee, _ := domain.ParseFee("0")
	basis, _ = basis.Buy(quantity, price, fee, 8)
	tooMuch, _ := domain.ParseBalance("2")
	proceeds, _ := domain.ParseMoney("20")
	if _, _, err := basis.Sell(tooMuch, proceeds, fee, 8); err == nil {
		t.Fatal("owned inventory oversell accepted")
	}
}

func TestPartialSaleDoesNotReaverageRoundingResidual(t *testing.T) {
	basis := NewCostBasis()
	quantity, _ := domain.ParseBalance("2")
	price, _ := domain.ParsePrice("1")
	fee, _ := domain.ParseFee("0.01")
	basis, err := basis.Buy(quantity, price, fee, 2)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := domain.ParseBalance("1")
	proceeds, _ := domain.ParseMoney("1")
	zeroFee, _ := domain.ParseFee("0")
	remaining, _, err := basis.Sell(one, proceeds, zeroFee, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantCost, _ := domain.ParseMoney("1.01")
	if remaining.AveragePrice.Compare(basis.AveragePrice) != 0 || remaining.TotalCost.Compare(wantCost) != 0 {
		t.Fatalf("partial sale re-averaged residual: before=%#v after=%#v", basis, remaining)
	}
	final, _, err := remaining.Sell(one, proceeds, zeroFee, 2)
	if err != nil {
		t.Fatal(err)
	}
	zero := NewCostBasis()
	if final.Quantity.Compare(zero.Quantity) != 0 || final.TotalCost.Compare(zero.TotalCost) != 0 {
		t.Fatalf("final sale retained residual: %#v", final)
	}
}

func journalTransaction(t testing.TB) Transaction {
	t.Helper()
	id, _ := domain.NewJournalTransactionID("journal-a")
	runID, _ := domain.NewRunID("run-a")
	portfolioID, _ := domain.NewPortfolioID("portfolio-a")
	eventID, _ := domain.NewEventID("event-a")
	usdt, _ := domain.ParseBalance("100")
	btc, _ := domain.ParseBalance("0.002")
	return Transaction{
		ID: id, Type: "virtual_buy", RunID: runID, PortfolioID: portfolioID,
		ConfigurationHash: runtimecore.PayloadDigest([]byte("configuration")), CausationID: eventID,
		RecordedAt: domain.EventTime{UTC: time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC), Sequence: 1}, IngestOrdinal: 1,
		Lines: []Line{
			{Account: AccountKey{Class: TradeCostProceeds, Asset: "USDT", Owner: "trade"}, Direction: Debit, Quantity: usdt},
			{Account: AccountKey{Class: AvailableAsset, Asset: "USDT", Owner: "account"}, Direction: Credit, Quantity: usdt},
			{Account: AccountKey{Class: StrategyInventory, Asset: "BTC", Owner: "strategy"}, Direction: Debit, Quantity: btc},
			{Account: AccountKey{Class: ExternalEquity, Asset: "BTC", Owner: "market"}, Direction: Credit, Quantity: btc},
		},
	}
}
