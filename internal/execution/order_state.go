package execution

// OrderState is one canonical durable virtual-order lifecycle state.
type OrderState string

// Canonical V1 order states.
const (
	OrderCreated          OrderState = "CREATED"
	OrderValidating       OrderState = "VALIDATING"
	OrderReserved         OrderState = "RESERVED"
	OrderApproved         OrderState = "APPROVED"
	OrderSubmitting       OrderState = "SUBMITTING"
	OrderAcknowledged     OrderState = "ACKNOWLEDGED"
	OrderPartiallyFilled  OrderState = "PARTIALLY_FILLED"
	OrderFilled           OrderState = "FILLED"
	OrderCancelPending    OrderState = "CANCEL_PENDING"
	OrderCanceled         OrderState = "CANCELED"
	OrderRejected         OrderState = "REJECTED"
	OrderExpired          OrderState = "EXPIRED"
	OrderUnknown          OrderState = "UNKNOWN"
	OrderRecoveryRequired OrderState = "RECOVERY_REQUIRED"
	OrderRecovered        OrderState = "RECOVERED"
)

func validState(state OrderState) bool {
	switch state {
	case OrderCreated, OrderValidating, OrderReserved, OrderApproved, OrderSubmitting,
		OrderAcknowledged, OrderPartiallyFilled, OrderFilled, OrderCancelPending,
		OrderCanceled, OrderRejected, OrderExpired, OrderUnknown, OrderRecoveryRequired, OrderRecovered:
		return true
	default:
		return false
	}
}

func transitionAllowed(from, to OrderState) bool {
	if from == to {
		return true
	}
	allowed := allowedTransitions[from]
	for _, candidate := range allowed {
		if candidate == to {
			return true
		}
	}
	return false
}

var allowedTransitions = map[OrderState][]OrderState{
	OrderCreated:          {OrderValidating, OrderRejected},
	OrderValidating:       {OrderReserved, OrderRejected},
	OrderReserved:         {OrderApproved, OrderRejected, OrderExpired},
	OrderApproved:         {OrderSubmitting, OrderCancelPending, OrderExpired},
	OrderSubmitting:       {OrderAcknowledged, OrderPartiallyFilled, OrderFilled, OrderRejected, OrderExpired, OrderUnknown},
	OrderAcknowledged:     {OrderPartiallyFilled, OrderFilled, OrderCancelPending, OrderCanceled, OrderExpired, OrderUnknown, OrderRecoveryRequired},
	OrderPartiallyFilled:  {OrderFilled, OrderCancelPending, OrderCanceled, OrderExpired, OrderUnknown, OrderRecoveryRequired},
	OrderCancelPending:    {OrderPartiallyFilled, OrderFilled, OrderCanceled, OrderUnknown, OrderRecoveryRequired},
	OrderCanceled:         {OrderPartiallyFilled, OrderFilled, OrderRecoveryRequired},
	OrderExpired:          {OrderPartiallyFilled, OrderFilled, OrderRecoveryRequired},
	OrderRejected:         {OrderRecoveryRequired},
	OrderUnknown:          {OrderAcknowledged, OrderPartiallyFilled, OrderFilled, OrderCanceled, OrderRejected, OrderExpired, OrderRecoveryRequired},
	OrderRecoveryRequired: {OrderRecovered},
}
