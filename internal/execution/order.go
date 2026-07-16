package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/domain"
)

// OrderIdentity is immutable for the lifetime of one logical virtual order.
type OrderIdentity struct {
	ID            domain.VirtualOrderID  `json:"id"`
	PlanID        domain.ExecutionPlanID `json:"plan_id"`
	ClientOrderID string                 `json:"client_order_id"`
	Instrument    domain.Instrument      `json:"instrument"`
	Side          domain.Side            `json:"side"`
	Quantity      domain.Quantity        `json:"quantity"`
}

// FeeFact preserves cumulative fees separately by charged asset.
type FeeFact struct {
	Asset  domain.AssetSymbol `json:"asset"`
	Total  domain.Fee         `json:"total"`
	Rebate domain.Fee         `json:"rebate"`
}

// FillFact is one immutable deduplicated simulated fill.
type FillFact struct {
	ID       domain.VirtualFillID `json:"id"`
	Quantity domain.Quantity      `json:"quantity"`
	Price    domain.Price         `json:"price"`
	Fee      domain.Fee           `json:"fee"`
	Rebate   domain.Fee           `json:"rebate"`
	FeeAsset domain.AssetSymbol   `json:"fee_asset"`
	Ordinal  uint64               `json:"ordinal"`
}

// OrderEvent is one canonical reducer input from simulation or reconciliation.
type OrderEvent struct {
	ID                 string                `json:"id"`
	OrderID            domain.VirtualOrderID `json:"order_id"`
	ClientOrderID      string                `json:"client_order_id"`
	State              OrderState            `json:"state"`
	ExchangeStatus     string                `json:"exchange_status"`
	CumulativeQuantity domain.Quantity       `json:"cumulative_quantity"`
	Fees               []FeeFact             `json:"fees"`
	Fills              []FillFact            `json:"fills"`
	OccurredAt         time.Time             `json:"occurred_at"`
	Ordinal            uint64                `json:"ordinal"`
}

// Order is one immutable snapshot of the reduced aggregate.
type Order struct {
	Identity           OrderIdentity   `json:"identity"`
	State              OrderState      `json:"state"`
	ExchangeStatus     string          `json:"exchange_status"`
	CumulativeQuantity domain.Quantity `json:"cumulative_quantity"`
	Fees               []FeeFact       `json:"fees"`
	Fills              []FillFact      `json:"fills"`
	Revision           uint64          `json:"revision"`
}

// Reduction classifies one applied, duplicate, stale, or incident-producing fact.
type Reduction struct {
	Order     Order
	Applied   bool
	Duplicate bool
	Stale     bool
	Incident  string
}

// OrderReducer validates and idempotently applies canonical events.
type OrderReducer struct {
	order       Order
	events      map[string]string
	fills       map[string]FillFact
	lastOrdinal uint64
}

// NewOrderReducer constructs a CREATED aggregate with exact zero facts.
func NewOrderReducer(identity OrderIdentity) (*OrderReducer, error) {
	zero, _ := domain.ParseQuantity("0")
	if err := validateIdentity(identity); err != nil {
		return nil, err
	}
	return &OrderReducer{order: Order{Identity: identity, State: OrderCreated,
		CumulativeQuantity: zero, Revision: 1}, events: make(map[string]string), fills: make(map[string]FillFact)}, nil
}

// Snapshot returns a defensive immutable aggregate copy.
func (reducer *OrderReducer) Snapshot() Order { return cloneOrder(reducer.order) }

// Reduce applies one event or returns an incident-classified invariant error.
func (reducer *OrderReducer) Reduce(event OrderEvent) (Reduction, error) {
	hash, err := eventHash(event)
	if err != nil || !validEvent(event) {
		return reducer.incident("event_invalid")
	}
	if prior, exists := reducer.events[event.ID]; exists {
		if prior == hash {
			return Reduction{Order: reducer.Snapshot(), Duplicate: true}, nil
		}
		return reducer.incident("event_identity_conflict")
	}
	if event.OrderID != reducer.order.Identity.ID || event.ClientOrderID != reducer.order.Identity.ClientOrderID {
		return reducer.incident("immutable_identity_conflict")
	}
	if event.CumulativeQuantity.Compare(reducer.order.CumulativeQuantity) < 0 {
		return reducer.incident("cumulative_fill_decreased")
	}
	if event.CumulativeQuantity.Compare(reducer.order.Identity.Quantity) > 0 {
		return reducer.incident("cumulative_fill_exceeded")
	}
	if err = reducer.validateFacts(event); err != nil {
		return reducer.incident(errorCode(err))
	}
	factsChanged := reducer.factsChanged(event)
	if !factsChanged && (event.State == reducer.order.State || event.Ordinal <= reducer.lastOrdinal) {
		reducer.events[event.ID] = hash
		return Reduction{Order: reducer.Snapshot(), Stale: true}, nil
	}
	if !transitionAllowed(reducer.order.State, event.State) {
		if factsChanged {
			return reducer.incident("state_regression")
		}
		reducer.events[event.ID] = hash
		return Reduction{Order: reducer.Snapshot(), Stale: true}, nil
	}
	reducer.apply(event, hash)
	return Reduction{Order: reducer.Snapshot(), Applied: true}, nil
}

func (reducer *OrderReducer) incident(code string) (Reduction, error) {
	return Reduction{Order: reducer.Snapshot(), Incident: code}, executionError(code)
}

func validateIdentity(identity OrderIdentity) error {
	zero, _ := domain.ParseQuantity("0")
	if identity.ID.Value() == "" || identity.PlanID.Value() == "" || identity.ClientOrderID == "" ||
		identity.Quantity.Compare(zero) <= 0 || (identity.Side != domain.SideBuy && identity.Side != domain.SideSell) {
		return executionError("order_identity_invalid")
	}
	validated, err := domain.NewSpotInstrument(identity.Instrument.Base, identity.Instrument.Quote)
	if err != nil || validated != identity.Instrument {
		return executionError("order_identity_invalid")
	}
	return nil
}

func validEvent(event OrderEvent) bool {
	return event.ID != "" && event.OrderID.Value() != "" && event.ClientOrderID != "" && validState(event.State) &&
		!event.OccurredAt.IsZero() && event.OccurredAt.Location() == time.UTC && event.Ordinal > 0
}

func eventHash(event OrderEvent) (string, error) {
	event.Fees = append([]FeeFact(nil), event.Fees...)
	event.Fills = append([]FillFact(nil), event.Fills...)
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", executionError("event_encode_failed")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func cloneOrder(order Order) Order {
	order.Fees = append([]FeeFact(nil), order.Fees...)
	order.Fills = append([]FillFact(nil), order.Fills...)
	return order
}
