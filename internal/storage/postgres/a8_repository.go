package postgres

import (
	"context"
	"fmt"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A8AtomicWrite is one complete order/fill/journal/checkpoint/outbox boundary.
type A8AtomicWrite struct {
	OrderEvent  generated.InsertCanonicalOrderEventParams
	Order       generated.ReduceCanonicalOrderParams
	Fills       []generated.InsertCanonicalFillParams
	Journal     generated.InsertJournalTransactionParams
	Entries     []generated.InsertLedgerEntryParams
	Postings    []generated.InsertFillJournalPostingParams
	Balances    []generated.UpdateVirtualBalanceProjectionParams
	Positions   []generated.UpsertPositionProjectionParams
	Projections []generated.UpsertProjectionRevisionParams
	Checkpoint  generated.InsertA8CheckpointParams
	Outbox      generated.InsertOutboxParams
}

// A8Repository persists virtual execution facts through reviewed sqlc queries.
type A8Repository struct{ pool *pgxpool.Pool }

// NewA8Repository constructs a repository over one PostgreSQL pool.
func NewA8Repository(pool *pgxpool.Pool) (*A8Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("a8_repository_pool_missing")
	}
	return &A8Repository{pool: pool}, nil
}

// Commit atomically persists every durable fact or none of them.
func (repository *A8Repository) Commit(ctx context.Context, write A8AtomicWrite) error {
	if !validA8Write(write) {
		return fmt.Errorf("a8_atomic_write_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a8_atomic_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = executeA8Write(ctx, generated.New(tx), write); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("a8_atomic_commit_failed")
	}
	return nil
}

func executeA8Write(ctx context.Context, queries *generated.Queries, write A8AtomicWrite) error {
	if _, err := queries.LockCanonicalOrder(ctx, write.Order.ID); err != nil {
		return fmt.Errorf("a8_order_lock_failed")
	}
	if _, err := queries.InsertCanonicalOrderEvent(ctx, write.OrderEvent); err != nil {
		return fmt.Errorf("a8_order_event_failed")
	}
	for _, fill := range write.Fills {
		if _, err := queries.InsertCanonicalFill(ctx, fill); err != nil {
			return fmt.Errorf("a8_fill_failed")
		}
	}
	return executeA8Accounting(ctx, queries, write)
}

func executeA8Accounting(ctx context.Context, queries *generated.Queries, write A8AtomicWrite) error {
	if _, err := queries.InsertJournalTransaction(ctx, write.Journal); err != nil {
		return fmt.Errorf("a8_journal_failed")
	}
	for _, entry := range write.Entries {
		if _, err := queries.InsertLedgerEntry(ctx, entry); err != nil {
			return fmt.Errorf("a8_journal_entry_failed")
		}
	}
	for _, posting := range write.Postings {
		if _, err := queries.InsertFillJournalPosting(ctx, posting); err != nil {
			return fmt.Errorf("a8_fill_posting_failed")
		}
	}
	for _, balance := range write.Balances {
		if _, err := queries.UpdateVirtualBalanceProjection(ctx, balance); err != nil {
			return fmt.Errorf("a8_balance_projection_failed")
		}
	}
	for _, position := range write.Positions {
		if _, err := queries.UpsertPositionProjection(ctx, position); err != nil {
			return fmt.Errorf("a8_position_projection_failed")
		}
	}
	for _, projection := range write.Projections {
		if _, err := queries.UpsertProjectionRevision(ctx, projection); err != nil {
			return fmt.Errorf("a8_projection_revision_failed")
		}
	}
	if _, err := queries.ReduceCanonicalOrder(ctx, write.Order); err != nil {
		return fmt.Errorf("a8_order_reduce_failed")
	}
	if _, err := queries.InsertA8Checkpoint(ctx, write.Checkpoint); err != nil {
		return fmt.Errorf("a8_checkpoint_failed")
	}
	if _, err := queries.InsertOutbox(ctx, write.Outbox); err != nil {
		return fmt.Errorf("a8_outbox_failed")
	}
	return nil
}

func validA8Write(write A8AtomicWrite) bool {
	return write.Order.ID != "" && write.OrderEvent.ID != "" && write.OrderEvent.OrderID == write.Order.ID &&
		len(write.Fills) > 0 && write.Journal.ID != "" && len(write.Entries) >= 2 &&
		len(write.Postings) > 0 && len(write.Balances) > 0 && len(write.Positions) > 0 &&
		len(write.Projections) > 0 && write.Checkpoint.ID != "" && write.Outbox.ID != ""
}
