-- name: LockVirtualBalance :one
SELECT * FROM virtual_balances
WHERE account_id = $1 AND asset_symbol = $2
FOR UPDATE;

-- name: ReserveVirtualBalance :one
UPDATE virtual_balances
SET available = available - $3,
    reserved = reserved + $3,
    revision = revision + 1,
    updated_at = $4
WHERE account_id = $1 AND asset_symbol = $2 AND available >= $3 AND $3 > 0
RETURNING *;

-- name: InsertReservation :one
INSERT INTO reservations (
  id, account_id, asset_symbol, quantity, remaining_quantity, state,
  fencing_token, revision, created_at, updated_at
) VALUES ($1, $2, $3, $4, $4, 'active', $5, 1, $6, $6)
RETURNING *;

-- name: LockReservation :one
SELECT * FROM reservations
WHERE id = $1
FOR UPDATE;

-- name: CloseReservation :one
UPDATE reservations
SET state = $2,
    remaining_quantity = CASE WHEN $2 = 'quarantined' THEN remaining_quantity ELSE 0 END,
    revision = revision + 1,
    updated_at = $3
WHERE id = $1 AND state = 'active' AND revision = $4 AND fencing_token = $5
RETURNING *;

-- name: SettleReservationFill :one
UPDATE reservations
SET state = CASE WHEN sqlc.arg(final_fill)::boolean THEN 'consumed' ELSE 'active' END,
    remaining_quantity = CASE WHEN sqlc.arg(final_fill)::boolean THEN remaining_quantity * 0 ELSE remaining_quantity - sqlc.arg(debit_quantity) END,
    revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = 'active' AND revision = sqlc.arg(revision)
  AND fencing_token = sqlc.arg(fencing_token) AND sqlc.arg(debit_quantity) > remaining_quantity * 0
  AND remaining_quantity >= sqlc.arg(debit_quantity)
  AND (sqlc.arg(final_fill)::boolean OR remaining_quantity > sqlc.arg(debit_quantity))
RETURNING *;

-- name: ReleaseVirtualBalance :one
UPDATE virtual_balances
SET available = available + $3,
    reserved = reserved - $3,
    revision = revision + 1,
    updated_at = $4
WHERE account_id = $1 AND asset_symbol = $2 AND reserved >= $3 AND $3 > 0
RETURNING *;

-- name: SettleReservedVirtualBalance :one
UPDATE virtual_balances
SET available = available + sqlc.arg(release_quantity),
    reserved = reserved - sqlc.arg(reserved_reduction),
    revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE account_id = sqlc.arg(account_id) AND asset_symbol = sqlc.arg(asset_symbol)
  AND sqlc.arg(release_quantity) >= 0 AND sqlc.arg(reserved_reduction) > 0
  AND reserved >= sqlc.arg(reserved_reduction)
RETURNING *;

-- name: CreditVirtualBalance :one
UPDATE virtual_balances
SET available = available + sqlc.arg(quantity),
    revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE account_id = sqlc.arg(account_id) AND asset_symbol = sqlc.arg(asset_symbol)
  AND sqlc.arg(quantity) > 0
RETURNING *;

-- name: ConsumeVirtualBalance :one
UPDATE virtual_balances
SET reserved = reserved - $3,
    revision = revision + 1,
    updated_at = $4
WHERE account_id = $1 AND asset_symbol = $2 AND reserved >= $3 AND $3 > 0
RETURNING *;

-- name: InsertJournalTransaction :one
INSERT INTO journal_transactions (
  id, transaction_type, run_id, portfolio_id, order_id, fill_id,
  configuration_id, causation_id, correlation_id, reversal_of, recorded_at, ingest_ordinal
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: InsertLedgerEntry :one
INSERT INTO ledger_entries (
  transaction_id, line_number, account_class, account_owner, asset_symbol,
  direction, quantity, functional_value, lot_reference, rounding_metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: JournalAssetDifferences :many
SELECT asset_symbol,
  sum(CASE direction WHEN 'debit' THEN quantity ELSE -quantity END)::numeric AS difference
FROM ledger_entries
WHERE transaction_id = $1
GROUP BY asset_symbol
ORDER BY asset_symbol;

-- name: RebuildAccountProjection :many
SELECT account_owner, account_class, asset_symbol,
  sum(CASE WHEN direction = 'debit' THEN quantity ELSE 0 END)::numeric AS debits,
  sum(CASE WHEN direction = 'credit' THEN quantity ELSE 0 END)::numeric AS credits
FROM ledger_entries
GROUP BY account_owner, account_class, asset_symbol
ORDER BY account_owner, account_class, asset_symbol;
