package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/execution"

	"github.com/jackc/pgx/v5"
)

const v1cConsoleOrdersSQL = `
SELECT outbox.order_id,outbox.account_id,account.exchange,
       account.environment,outbox.order_state,outbox.intent_action,
       outbox.instrument,outbox.side,outbox.quantity::text,
       outbox.limit_price::text,outbox.reserved_notional::text,
       outbox.order_style,outbox.attempt,outbox.approved_at,
       outbox.updated_at,
       CASE WHEN outbox.order_state='UNKNOWN' THEN outbox.updated_at END
FROM v1c_submission_outbox outbox
JOIN v1c_exchange_accounts account ON account.id=outbox.account_id
WHERE ($1='' OR account.exchange=$1)
  AND ($2='' OR outbox.order_state=$2)
  AND ($3::timestamptz IS NULL OR (outbox.updated_at,outbox.order_id)<($3,$4))
ORDER BY outbox.updated_at DESC,outbox.order_id DESC
LIMIT $5`

// SandboxOrders returns current redacted order aggregates with exact fills.
func (store *A11ConsoleStore) SandboxOrders(
	ctx context.Context,
	cursor string,
	limit int,
	exchangeFilter, stateFilter string,
) (generated.SandboxOrderPage, error) {
	if limit < 1 || limit > 200 {
		return generated.SandboxOrderPage{}, console.ErrInvalidRequest
	}
	cursorTime, cursorID, _, err := decodeA11TimeCursor(
		store.cursor, v1cConsoleOrderScope, cursor,
	)
	if err != nil {
		return generated.SandboxOrderPage{}, err
	}
	rows, err := store.pool.Query(ctx, v1cConsoleOrdersSQL,
		exchangeFilter, stateFilter, nullableTime(cursorTime),
		cursorID, limit+1)
	if err != nil {
		return generated.SandboxOrderPage{}, err
	}
	defer rows.Close()
	items, err := store.readV1CConsoleOrders(ctx, rows, limit+1)
	if err != nil {
		return generated.SandboxOrderPage{}, err
	}
	return store.c6OrderPage(items, limit), nil
}

func (store *A11ConsoleStore) readV1CConsoleOrders(
	ctx context.Context,
	rows pgx.Rows,
	capacity int,
) ([]generated.SandboxOrder, error) {
	items := make([]generated.SandboxOrder, 0, capacity)
	for rows.Next() {
		item, scanErr := store.scanV1CConsoleOrder(ctx, rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) c6OrderPage(
	items []generated.SandboxOrder,
	limit int,
) generated.SandboxOrderPage {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		value := encodeA11TimeCursor(
			store.cursor,
			v1cConsoleOrderScope,
			last.UpdatedAt,
			last.Id,
		)
		next = &value
	}
	revision := "0"
	if len(items) > 0 {
		revision = strconv.FormatInt(items[0].UpdatedAt.UnixNano(), 10)
	}
	return generated.SandboxOrderPage{
		Items: items, HasMore: hasMore, NextCursor: next, Revision: revision,
	}
}

type v1cConsoleRowScanner interface {
	Scan(...any) error
}

type v1cConsoleOrderRow struct {
	id, accountID, exchange, environment, state, action string
	instrument, side, quantity, price, notional, style  string
	attempt                                             int
	createdAt, updatedAt                                time.Time
	unknownSince                                        *time.Time
}

func (store *A11ConsoleStore) scanV1CConsoleOrder(
	ctx context.Context,
	row v1cConsoleRowScanner,
) (generated.SandboxOrder, error) {
	var value v1cConsoleOrderRow
	if err := row.Scan(
		&value.id, &value.accountID, &value.exchange, &value.environment,
		&value.state, &value.action, &value.instrument, &value.side,
		&value.quantity, &value.price, &value.notional, &value.style,
		&value.attempt, &value.createdAt, &value.updatedAt, &value.unknownSince,
	); err != nil {
		return generated.SandboxOrder{}, err
	}
	fills, err := store.v1cConsoleOrderFills(ctx, value.id)
	if err != nil {
		return generated.SandboxOrder{}, err
	}
	return generatedSandboxOrder(value, fills), nil
}

func generatedSandboxOrder(
	value v1cConsoleOrderRow,
	fills []generated.SandboxFill,
) generated.SandboxOrder {
	recovery := "not_required"
	if value.state == "UNKNOWN" || value.state == "RECOVERY_REQUIRED" {
		recovery = "required"
	}
	if value.state == "ACKNOWLEDGED" && value.attempt > 1 {
		recovery = "querying"
	}
	if value.state != "UNKNOWN" && value.state != "RECOVERY_REQUIRED" &&
		value.attempt > 1 {
		recovery = "reconciled"
	}
	return generated.SandboxOrder{
		Id:             value.id,
		AccountId:      value.accountID,
		Exchange:       generated.SandboxExchange(value.exchange),
		Environment:    generated.SandboxEnvironment(value.environment),
		State:          generated.SandboxOrderState(value.state),
		Action:         generated.SandboxOrderAction(value.action),
		Instrument:     generated.SandboxOrderInstrument(value.instrument),
		Side:           generated.SandboxOrderSide(value.side),
		Quantity:       generated.NonnegativeDecimal(value.quantity),
		LimitPrice:     generated.NonnegativeDecimal(value.price),
		Notional:       generated.NonnegativeDecimal(value.notional),
		Style:          generated.SandboxOrderStyle(value.style),
		Attempt:        value.attempt,
		RecoveryStatus: generated.SandboxOrderRecoveryStatus(recovery),
		UnknownSince:   utcPointer(value.unknownSince),
		CreatedAt:      value.createdAt.UTC(),
		UpdatedAt:      value.updatedAt.UTC(),
		Revision:       strconv.FormatInt(value.updatedAt.UnixNano(), 10),
		Fills:          fills,
		AuditUrl:       "/api/v1/audit-events?event_type=v1c_order",
	}
}

func (store *A11ConsoleStore) v1cConsoleOrderFills(
	ctx context.Context,
	orderID string,
) ([]generated.SandboxFill, error) {
	rows, err := store.pool.Query(ctx, `
SELECT native_fill_id_hash,canonical_fill,occurred_at
FROM v1c_exchange_fills WHERE order_id=$1
ORDER BY occurred_at,native_fill_id_hash`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]generated.SandboxFill, 0)
	for rows.Next() {
		var id string
		var raw []byte
		var occurredAt time.Time
		if err = rows.Scan(&id, &raw, &occurredAt); err != nil {
			return nil, err
		}
		var event execution.OrderEvent
		if json.Unmarshal(raw, &event) != nil || len(event.Fills) == 0 {
			return nil, fmt.Errorf("v1c_console_fill_invalid")
		}
		fill := event.Fills[len(event.Fills)-1]
		result = append(result, generated.SandboxFill{
			Id:          id,
			OrderId:     orderID,
			Quantity:    generated.NonnegativeDecimal(fill.Quantity.String()),
			Price:       generated.NonnegativeDecimal(fill.Price.String()),
			FeeQuantity: generated.NonnegativeDecimal(fill.Fee.String()),
			FeeAsset:    generated.SandboxFillFeeAsset(fill.FeeAsset),
			OccurredAt:  occurredAt.UTC(),
			AuditUrl:    "/api/v1/audit-events?event_type=v1c_fill",
		})
	}
	return result, rows.Err()
}

func (store *A11ConsoleStore) v1cConsoleRiskState(
	ctx context.Context,
	accounts []generated.SandboxAccount,
) (string, error) {
	for _, account := range accounts {
		if account.State == "LOCKED" || account.State == "QUARANTINED" {
			return "LOCKED", nil
		}
	}
	for _, account := range accounts {
		if account.State == "DEGRADED" || account.Stale {
			return "PAUSED", nil
		}
	}
	var state string
	err := store.pool.QueryRow(ctx, `
SELECT next_state FROM risk_state_events
ORDER BY entity_revision DESC LIMIT 1`).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "PAUSED", nil
	}
	if err != nil {
		return "", err
	}
	switch state {
	case "NORMAL", "CAUTIOUS", "PAUSED", "LOCKED":
		return state, nil
	default:
		return "PAUSED", nil
	}
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
