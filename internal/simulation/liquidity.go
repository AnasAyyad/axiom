package simulation

import (
	"sort"
	"sync"

	"axiom/internal/domain"
)

// LiquidityKey identifies one displayed level inside one counterfactual world.
type LiquidityKey struct {
	Namespace     string
	Exchange      string
	Instrument    domain.Instrument
	MarketVersion uint64
	Side          domain.Side
	Price         string
}

// LiquidityLedger prevents combined-portfolio fills from reusing depth.
type LiquidityLedger struct {
	mutex    sync.Mutex
	consumed map[LiquidityKey]domain.Quantity
}

// NewLiquidityLedger constructs an empty run-local consumption view.
func NewLiquidityLedger() *LiquidityLedger {
	return &LiquidityLedger{consumed: make(map[LiquidityKey]domain.Quantity)}
}

// Consume atomically claims up to requested from one displayed quantity.
func (ledger *LiquidityLedger) Consume(
	key LiquidityKey,
	displayed domain.Quantity,
	requested domain.Quantity,
) (domain.Quantity, error) {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	if !validLiquidityKey(key) || displayed.String() == "0" || requested.String() == "0" {
		return domain.Quantity{}, simulationError("liquidity_request_invalid")
	}
	used, exists := ledger.consumed[key]
	if !exists {
		used, _ = domain.ParseQuantity("0")
	}
	remaining, err := displayed.Subtract(used)
	if err != nil {
		return domain.Quantity{}, simulationError("liquidity_projection_invalid")
	}
	claimed := requested
	if claimed.Compare(remaining) > 0 {
		claimed = remaining
	}
	if claimed.String() == "0" {
		return claimed, nil
	}
	updated, err := used.Add(claimed)
	if err != nil {
		return domain.Quantity{}, simulationError("liquidity_projection_invalid")
	}
	ledger.consumed[key] = updated
	return claimed, nil
}

// Consumed returns one exact run-local claim snapshot.
func (ledger *LiquidityLedger) Consumed(key LiquidityKey) domain.Quantity {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	quantity, exists := ledger.consumed[key]
	if !exists {
		quantity, _ = domain.ParseQuantity("0")
	}
	return quantity
}

func validLiquidityKey(key LiquidityKey) bool {
	return key.Namespace != "" && key.Exchange != "" && key.MarketVersion > 0 && key.Price != "" &&
		(key.Side == domain.SideBuy || key.Side == domain.SideSell) && key.Instrument.Product == domain.ProductSpot
}

// ScheduledLiquidityWork is one stable combined-portfolio claim operation.
type ScheduledLiquidityWork struct {
	Key     string
	Execute func(*LiquidityLedger) error
}

// LiquidityScheduler commits concurrent candidates in one stable order.
type LiquidityScheduler struct{ ledger *LiquidityLedger }

// NewLiquidityScheduler constructs a scheduler over one shared ledger.
func NewLiquidityScheduler(ledger *LiquidityLedger) (*LiquidityScheduler, error) {
	if ledger == nil {
		return nil, simulationError("liquidity_ledger_missing")
	}
	return &LiquidityScheduler{ledger: ledger}, nil
}

// Run sorts by stable key, then serially commits all liquidity effects.
func (scheduler *LiquidityScheduler) Run(work []ScheduledLiquidityWork) error {
	work = append([]ScheduledLiquidityWork(nil), work...)
	sort.Slice(work, func(left, right int) bool { return work[left].Key < work[right].Key })
	for index, item := range work {
		if item.Key == "" || item.Execute == nil || (index > 0 && work[index-1].Key == item.Key) {
			return simulationError("liquidity_work_invalid")
		}
		if err := item.Execute(scheduler.ledger); err != nil {
			return err
		}
	}
	return nil
}
