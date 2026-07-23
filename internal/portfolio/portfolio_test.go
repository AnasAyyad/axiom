package portfolio

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/simulation"
)

func TestV1ATrendInitializationIsExactOwnedAndBalanced(t *testing.T) {
	portfolio, journal := initializedPortfolio(t)
	snapshot := portfolio.Snapshot()
	if snapshot.Ownership.Strategy != V1AStrategy || snapshot.Ownership.Exchange != V1AExchange ||
		snapshot.Numeraire != V1ANumeraire || snapshot.Balances["USDT"].Available.String() != "500" ||
		snapshot.Balances["BTC"].Available.String() != "0" || snapshot.Balances["ETH"].Available.String() != "0" {
		t.Fatalf("initial snapshot = %#v", snapshot)
	}
	projections, hash, err := journal.Rebuild()
	if err != nil || len(projections) != 2 || len(hash) != 64 || len(journal.Transactions()) != 1 {
		t.Fatalf("initial journal = %d %s %v", len(projections), hash, err)
	}
}

func TestB3MeanReversionOwnershipRejectsCrossStrategyAndAveragingDownAcrossRestart(t *testing.T) {
	runID, _ := domain.NewRunID("run-b3")
	portfolioID, _ := domain.NewPortfolioID("mean-reversion-one")
	accountID, _ := domain.NewVirtualAccountID("mean-reversion-binance")
	journal := accounting.NewMemoryJournal()
	capital, _ := domain.ParseBalance("500")
	owned, err := InitializeMeanReversion(runID, portfolioID, accountID, strings.Repeat("b", 64), capital,
		journal, domain.EventTime{UTC: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), Sequence: 1})
	if err != nil || owned.Snapshot().Ownership.Strategy != V1BMeanReversionStrategy {
		t.Fatalf("mean-reversion ownership = %#v, %v", owned.Snapshot().Ownership, err)
	}
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("3")
	if err = pool.Open("combined-book", quantity); err != nil {
		t.Fatal(err)
	}
	allocator, _ := NewAllocator(owned, NewAssetRegistry(), pool)
	wrongOwner := buyCandidate(t, 70)
	if _, err = allocator.Allocate([]Candidate{wrongOwner}); err == nil {
		t.Fatal("Trend candidate entered mean-reversion portfolio")
	}
	entry := buyCandidate(t, 71)
	entry.Strategy = V1BMeanReversionStrategy
	allocation := mustAllocate(t, allocator, entry)
	if err = allocator.Settle(allocation, fillFact(t, "b3-entry", "1", "100", "0.1")); err != nil {
		t.Fatal(err)
	}
	second := buyCandidate(t, 72)
	second.Strategy = V1BMeanReversionStrategy
	if _, err = allocator.Allocate([]Candidate{second}); err == nil {
		t.Fatal("averaging down allocated despite owned position")
	}
	protected := owned.ProtectedState()
	restored, err := Restore(protected)
	if err != nil || restored.ProtectedState().CanonicalHash() != protected.CanonicalHash() ||
		restored.Snapshot().Ownership.Strategy != V1BMeanReversionStrategy {
		t.Fatalf("B3 restart state mismatch: %v", err)
	}
}

func TestAllocatorPreventsConcurrentCashInventoryAndLiquidityOwnership(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	registry := NewAssetRegistry()
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("1")
	if err := pool.Open("combined-book", quantity); err != nil {
		t.Fatal(err)
	}
	allocator, _ := NewAllocator(portfolio, registry, pool)
	var successes atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if allocations, err := allocator.Allocate([]Candidate{buyCandidate(t, index)}); err == nil && len(allocations) == 1 {
				successes.Add(1)
			}
		}(index)
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful allocations = %d", successes.Load())
	}
	key, _ := portfolio.BalanceKey("USDT")
	balance, _ := portfolio.Ledger().Balance(key)
	remaining, _ := pool.Available("combined-book")
	if balance.Available.String() != "399.9" || balance.Reserved.String() != "100.1" || remaining.String() != "0" {
		t.Fatalf("ownership = %s/%s/%s", balance.Available, balance.Reserved, remaining)
	}
}

func TestAllocatorRejectsUnownedSellAndReserveViolation(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("10")
	_ = pool.Open("combined-book", quantity)
	allocator, _ := NewAllocator(portfolio, NewAssetRegistry(), pool)
	sell := buyCandidate(t, 1)
	sell.Side = domain.SideSell
	if _, err := allocator.Allocate([]Candidate{sell}); err == nil {
		t.Fatal("unowned sell was allocated")
	}
	buy := buyCandidate(t, 2)
	buy.Notional, _ = domain.ParseMoney("150.01")
	if _, err := allocator.Allocate([]Candidate{buy}); err == nil {
		t.Fatal("trade budget violation was allocated")
	}
}

func TestPortfolioSettlesBuySellFeesAndCostBasisExactly(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	registry := NewAssetRegistry()
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("2")
	_ = pool.Open("combined-book", quantity)
	allocator, _ := NewAllocator(portfolio, registry, pool)
	buy := buyCandidate(t, 1)
	allocation := mustAllocate(t, allocator, buy)
	if err := allocator.Settle(allocation, fillFact(t, "buy-fill", "1", "100", "0.1")); err != nil {
		t.Fatal(err)
	}
	sell := buyCandidate(t, 2)
	sell.Side, sell.Notional = domain.SideSell, mustMoney("110")
	allocation = mustAllocate(t, allocator, sell)
	if err := allocator.Settle(allocation, fillFact(t, "sell-fill", "1", "110", "0.1")); err != nil {
		t.Fatal(err)
	}
	snapshot := portfolio.Snapshot()
	if snapshot.Balances["USDT"].Available.String() != "509.8" || snapshot.Balances["BTC"].Available.String() != "0" ||
		len(snapshot.Positions) != 1 || snapshot.Positions[0].RealizedPnL.String() != "9.8" ||
		snapshot.Positions[0].Quantity.String() != "0" {
		t.Fatalf("settled snapshot = %#v", snapshot)
	}
}

func TestPortfolioRetainsExactClaimsAcrossPartialFills(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("1")
	if err := pool.Open("combined-book", quantity); err != nil {
		t.Fatal(err)
	}
	allocator, _ := NewAllocator(portfolio, NewAssetRegistry(), pool)
	allocation := mustAllocate(t, allocator, buyCandidate(t, 50))

	allocation, err := allocator.ApplyFill(allocation, fillFact(t, "partial-fill-one", "0.4", "100", "0.04"), false)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Funds.State != accounting.ReservationActive || allocation.Funds.Remaining.String() != "60.06" ||
		allocation.Funds.Revision != 2 || allocation.Liquidity.State != "active" ||
		allocation.Liquidity.Remaining.String() != "0.6" || allocation.Liquidity.Revision != 2 {
		t.Fatalf("partial claims = %#v %#v", allocation.Funds, allocation.Liquidity)
	}
	protected := portfolio.ProtectedState()
	restored, restoreErr := Restore(protected)
	if restoreErr != nil || restored.ProtectedState().CanonicalHash() != protected.CanonicalHash() {
		t.Fatalf("partial ownership restart failed: %v", restoreErr)
	}
	liquidityState := pool.State()
	restoredLiquidity, liquidityRestoreErr := RestoreLiquidityPool(liquidityState)
	if liquidityRestoreErr != nil || !reflect.DeepEqual(restoredLiquidity.State(), liquidityState) {
		t.Fatalf("partial liquidity restart failed: %v", liquidityRestoreErr)
	}

	allocation, err = allocator.ApplyFill(allocation, fillFact(t, "partial-fill-two", "0.6", "100", "0.06"), true)
	if err != nil {
		t.Fatal(err)
	}
	remainingDepth, _ := pool.Available("combined-book")
	snapshot := portfolio.Snapshot()
	if allocation.Funds.State != accounting.ReservationConsumed || allocation.Funds.Remaining.String() != "0" ||
		allocation.Liquidity.State != "consumed" || allocation.Liquidity.Remaining.String() != "0" ||
		remainingDepth.String() != "0" || snapshot.Balances["USDT"].Available.String() != "399.9" ||
		snapshot.Balances["USDT"].Reserved.String() != "0" || snapshot.Balances["BTC"].Available.String() != "1" ||
		len(snapshot.Positions) != 1 || snapshot.Positions[0].Cost.String() != "100.1" {
		t.Fatalf("final partial settlement = %#v %#v %#v", allocation, snapshot, remainingDepth)
	}
}

func TestPortfolioMaintainsExactInventoryExposureAndReserve(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("1")
	_ = pool.Open("combined-book", quantity)
	allocator, _ := NewAllocator(portfolio, NewAssetRegistry(), pool)
	allocation := mustAllocate(t, allocator, buyCandidate(t, 30))
	if err := allocator.Settle(allocation, fillFact(t, "exposure-fill", "1", "100", "0.1")); err != nil {
		t.Fatal(err)
	}
	mark, _ := domain.ParsePrice("110")
	exposure, err := portfolio.Exposure(map[domain.AssetSymbol]domain.Price{"BTC": mark})
	if err != nil {
		t.Fatal(err)
	}
	if exposure.Equity.String() != "509.9" || exposure.Inventory["BTC"].String() != "1" ||
		exposure.Inventory["ETH"].String() != "0" || exposure.AssetExposure["BTC"].String() != "0.215728574230241224" ||
		exposure.Combined.String() != exposure.Exchange.String() || exposure.Reserve.String() != "0.784271425769758776" ||
		exposure.ReservedCapital.String() != "0" {
		t.Fatalf("exact exposure = %#v", exposure)
	}
	if _, err = portfolio.Exposure(nil); err == nil {
		t.Fatal("owned volatile inventory accepted without mark")
	}
}

func TestProtectedPortfolioAndJournalStateAreIdenticalAcrossRestart(t *testing.T) {
	portfolio, journal := initializedPortfolio(t)
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("2")
	_ = pool.Open("combined-book", quantity)
	allocator, _ := NewAllocator(portfolio, NewAssetRegistry(), pool)
	first := mustAllocate(t, allocator, buyCandidate(t, 40))
	if err := allocator.Settle(first, fillFact(t, "restart-fill", "1", "100", "0.1")); err != nil {
		t.Fatal(err)
	}
	second := buyCandidate(t, 41)
	second.Instrument, _ = domain.NewSpotInstrument("ETH", "USDT")
	second.LiquidityDomain = "combined-book"
	_ = mustAllocate(t, allocator, second)
	protected := portfolio.ProtectedState()
	beforeSnapshot := portfolio.Snapshot().CanonicalHash()
	_, beforeJournal, err := journal.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(protected)
	if err != nil {
		t.Fatal(err)
	}
	replayedJournal := accounting.NewMemoryJournal()
	for _, transaction := range journal.Transactions() {
		if err = replayedJournal.Append(transaction); err != nil {
			t.Fatal(err)
		}
	}
	_, afterJournal, err := replayedJournal.Rebuild()
	if err != nil || restored.ProtectedState().CanonicalHash() != protected.CanonicalHash() ||
		restored.Snapshot().CanonicalHash() != beforeSnapshot || afterJournal != beforeJournal {
		t.Fatalf("restart mismatch portfolio=%s/%s journal=%s/%s err=%v",
			restored.ProtectedState().CanonicalHash(), protected.CanonicalHash(), afterJournal, beforeJournal, err)
	}
	corrupt := protected
	corrupt.Ledger.Reservations = append([]accounting.Reservation(nil), protected.Ledger.Reservations...)
	last := len(corrupt.Ledger.Reservations) - 1
	corrupt.Ledger.Reservations[last].Revision++
	corrupt.Ledger.Reservations[last].State = accounting.ReservationReleased
	if _, err = Restore(corrupt); err == nil {
		t.Fatal("inconsistent reserved ownership restored")
	}
}

func TestAllocatorClosesBothClaimsWithCASAndFence(t *testing.T) {
	states := []accounting.ReservationState{accounting.ReservationReleased, accounting.ReservationExpired,
		accounting.ReservationQuarantined}
	for index, state := range states {
		t.Run(string(state), func(t *testing.T) {
			portfolio, _ := initializedPortfolio(t)
			pool := NewLiquidityPool()
			quantity, _ := domain.ParseQuantity("1")
			_ = pool.Open("combined-book", quantity)
			allocator, _ := NewAllocator(portfolio, NewAssetRegistry(), pool)
			allocation := mustAllocate(t, allocator, buyCandidate(t, index+10))
			stale := allocation
			stale.Funds.Revision++
			if err := allocator.Close(stale, state); err == nil {
				t.Fatal("stale allocation closed")
			}
			if err := allocator.Close(allocation, state); err != nil {
				t.Fatal(err)
			}
			funds, _ := portfolio.Ledger().Reservation(allocation.Funds.ID)
			liquidity, _ := pool.Reservation(allocation.Liquidity.ID)
			remaining, _ := pool.Available("combined-book")
			if funds.State != state || liquidity.State != string(state) || funds.Revision != 2 || liquidity.Revision != 2 {
				t.Fatalf("closed claims = %#v %#v", funds, liquidity)
			}
			if state == accounting.ReservationQuarantined && remaining.String() != "0" {
				t.Fatal("quarantined displayed liquidity was released")
			}
			if state != accounting.ReservationQuarantined && remaining.String() != "1" {
				t.Fatal("closed displayed liquidity was not released")
			}
			if err := allocator.Close(allocation, state); err == nil {
				t.Fatal("closed allocation transitioned twice")
			}
		})
	}
}

func TestAllocatorRejectsSettlementBeforeEitherClaimChanges(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("1")
	_ = pool.Open("combined-book", quantity)
	allocator, _ := NewAllocator(portfolio, NewAssetRegistry(), pool)
	allocation := mustAllocate(t, allocator, buyCandidate(t, 20))
	stale := allocation
	stale.Liquidity.Revision++
	before := portfolio.Snapshot().CanonicalHash()
	if err := allocator.Settle(stale, fillFact(t, "stale-fill", "1", "100", "0.1")); err == nil {
		t.Fatal("stale liquidity claim settled")
	}
	if after := portfolio.Snapshot().CanonicalHash(); after != before {
		t.Fatal("rejected allocation settlement changed portfolio")
	}
	funds, _ := portfolio.Ledger().Reservation(allocation.Funds.ID)
	liquidity, _ := pool.Reservation(allocation.Liquidity.ID)
	if funds.State != accounting.ReservationActive || liquidity.State != "active" {
		t.Fatal("rejected settlement changed reservation lifecycle")
	}
}

func TestAssetEligibilityRechecksAllocationPlanAndBrokerBoundaries(t *testing.T) {
	portfolio, _ := initializedPortfolio(t)
	registry := NewAssetRegistry()
	pool := NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("2")
	_ = pool.Open("combined-book", quantity)
	allocator, _ := NewAllocator(portfolio, registry, pool)
	if err := registry.Set("BTC", domain.AssetBlocked, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Allocate([]Candidate{buyCandidate(t, 1)}); err == nil {
		t.Fatal("status change bypassed allocator")
	}
	plannerBoundaryTest(t)
	guard, _ := NewBrokerGuard(portfolio, registry)
	if _, err := guard.Authorize(plannedLeg(t), simulation.BookState{}); err == nil {
		t.Fatal("status change bypassed broker")
	}
}

func plannerBoundaryTest(t *testing.T) {
	registry := NewAssetRegistry()
	vault := NewApprovalVault()
	decisionID, _ := domain.NewDecisionID("decision-one")
	record := ApprovalRecord{DecisionID: decisionID, PolicyHash: strings.Repeat("a", 64),
		Assets: []AssetEligibility{{Asset: "BTC", Status: domain.AssetApproved, Version: 1},
			{Asset: "USDT", Status: domain.AssetApproved, Version: 1}}}
	intent, err := vault.Issue(record)
	if err != nil {
		t.Fatal(err)
	}
	planner, _ := NewEligibilityPlanner(staticPlanner{plan: plannedPlan(t, intent)}, vault, registry)
	_ = registry.Set("BTC", domain.AssetScanOnly, 2)
	if _, err = planner.Plan(context.Background(), intent); err == nil {
		t.Fatal("status change bypassed planner")
	}
}

func initializedPortfolio(t *testing.T) (*Portfolio, *accounting.MemoryJournal) {
	t.Helper()
	runID, _ := domain.NewRunID("run-one")
	portfolioID, _ := domain.NewPortfolioID("trend-one")
	accountID, _ := domain.NewVirtualAccountID("trend-binance")
	journal := accounting.NewMemoryJournal()
	portfolio, err := InitializeV1ATrend(runID, portfolioID, accountID, strings.Repeat("a", 64), journal,
		domain.EventTime{UTC: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	return portfolio, journal
}

func buyCandidate(t *testing.T, index int) Candidate {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	notional, _ := domain.ParseMoney("100.1")
	score, _ := domain.ParsePnL("1")
	funds, _ := domain.NewReservationID("funds-" + decimal(index))
	liquidity, _ := domain.NewReservationID("liquidity-" + decimal(index))
	return Candidate{ID: "candidate-" + decimal(index), Strategy: V1AStrategy,
		Instrument: instrument, Side: domain.SideBuy,
		Quantity: quantity, Notional: notional, Score: score, ScoreComponents: []ScoreComponent{{Name: "worst_case", Value: score}},
		BaseEligibility: 1, QuoteEligibility: 1, LiquidityDomain: "combined-book",
		LiquidityReservation: liquidity, FundsReservation: funds, Fence: 1}
}

func fillFact(t *testing.T, id, quantityValue, priceValue, feeValue string) execution.FillFact {
	t.Helper()
	idValue, _ := domain.NewVirtualFillID(id)
	quantity, _ := domain.ParseQuantity(quantityValue)
	price, _ := domain.ParsePrice(priceValue)
	fee, _ := domain.ParseFee(feeValue)
	rebate, _ := domain.ParseFee("0")
	asset, _ := domain.ParseAssetSymbol("USDT")
	return execution.FillFact{ID: idValue, Quantity: quantity, Price: price, Fee: fee,
		Rebate: rebate, FeeAsset: asset, Ordinal: 1}
}

func mustAllocate(t *testing.T, allocator *Allocator, candidate Candidate) Allocation {
	t.Helper()
	allocations, err := allocator.Allocate([]Candidate{candidate})
	if err != nil || len(allocations) != 1 {
		t.Fatal(err)
	}
	return allocations[0]
}

func mustMoney(value string) domain.Money { parsed, _ := domain.ParseMoney(value); return parsed }

func decimal(value int) string {
	if value == 0 {
		return "zero"
	}
	const digits = "0123456789"
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}

type staticPlanner struct{ plan execution.SimulatedPlan }

func (planner staticPlanner) Plan(context.Context, execution.ApprovedIntent) (execution.SimulatedPlan, error) {
	return planner.plan, nil
}

func plannedPlan(t *testing.T, intent execution.ApprovedIntent) execution.SimulatedPlan {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("plan-one")
	return execution.SimulatedPlan{ID: planID, Intent: intent, Namespace: "combined-book",
		DecisionLogicalTime: 1, Legs: []execution.PlannedLeg{plannedLeg(t)}}
}

func plannedLeg(t *testing.T) execution.PlannedLeg {
	t.Helper()
	orderID, _ := domain.NewVirtualOrderID("order-one")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	price, _ := domain.ParsePrice("100")
	return execution.PlannedLeg{OrderID: orderID, ClientOrderID: "client-one", Instrument: instrument,
		Side: domain.SideSell, Quantity: quantity, LimitPrice: price, ExpiresAt: 10}
}
