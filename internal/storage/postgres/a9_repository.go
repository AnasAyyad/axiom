package postgres

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A9InitializationWrite is immutable database proof for the exact V1A Trend portfolio.
type A9InitializationWrite struct {
	Journal   generated.InsertJournalTransactionParams
	Entries   []generated.InsertLedgerEntryParams
	Ownership generated.InsertPortfolioOwnershipParams
	Snapshot  generated.InsertA9AccountSnapshotParams
}

// A9AllocationWrite atomically claims owned funds/inventory and displayed liquidity.
type A9AllocationWrite struct {
	Candidate         generated.InsertAllocationCandidateParams
	Scores            []generated.InsertAllocationScoreComponentParams
	Funds             generated.InsertReservationParams
	FundsQuantity     interface{}
	LiquidityDomainID string
	LiquidityRevision int64
	LiquidityQuantity interface{}
	Liquidity         generated.InsertLiquidityReservationParams
	Link              generated.LinkAllocationReservationsParams
	Journal           generated.InsertJournalTransactionParams
	Entries           []generated.InsertLedgerEntryParams
}

// A9AllocationClose atomically closes both exclusive claims and their projections.
type A9AllocationClose struct {
	Candidate         generated.CloseAllocationCandidateParams
	Funds             generated.CloseReservationParams
	FundsAccountID    string
	FundsAsset        string
	FundsQuantity     interface{}
	Liquidity         generated.CloseLiquidityReservationParams
	LiquidityDomainID string
	LiquidityQuantity interface{}
	Journal           generated.InsertJournalTransactionParams
	Entries           []generated.InsertLedgerEntryParams
}

// A9Repository owns transactional portfolio and allocation persistence boundaries.
type A9Repository struct{ pool *pgxpool.Pool }

// NewA9Repository constructs the portfolio persistence boundary.
func NewA9Repository(pool *pgxpool.Pool) (*A9Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("a9_repository_pool_missing")
	}
	return &A9Repository{pool: pool}, nil
}

// InitializeV1ATrend proves exact balances before atomically posting initialization evidence.
func (repository *A9Repository) InitializeV1ATrend(ctx context.Context, write A9InitializationWrite) error {
	if write.Journal.ID == "" || len(write.Entries) != 2 || write.Ownership.AccountID == "" ||
		write.Ownership.InitializationTransactionID != write.Journal.ID || write.Snapshot.AccountID != write.Ownership.AccountID {
		return fmt.Errorf("a9_initialization_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a9_initialization_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = requireExactV1ABalances(ctx, tx, write.Ownership.AccountID); err != nil {
		return err
	}
	queries := generated.New(tx)
	if _, err = queries.InsertJournalTransaction(ctx, write.Journal); err != nil {
		return fmt.Errorf("a9_initialization_journal_failed")
	}
	for _, entry := range write.Entries {
		if _, err = queries.InsertLedgerEntry(ctx, entry); err != nil {
			return fmt.Errorf("a9_initialization_entry_failed")
		}
	}
	if _, err = queries.InsertPortfolioOwnership(ctx, write.Ownership); err != nil {
		return fmt.Errorf("a9_initialization_ownership_failed")
	}
	if _, err = queries.InsertA9AccountSnapshot(ctx, write.Snapshot); err != nil {
		return fmt.Errorf("a9_initialization_snapshot_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("a9_initialization_commit_failed")
	}
	return nil
}

func requireExactV1ABalances(ctx context.Context, tx pgx.Tx, accountID string) error {
	rows, err := tx.Query(ctx, `SELECT asset_symbol,available::text,reserved::text
FROM virtual_balances WHERE account_id=$1 ORDER BY asset_symbol FOR UPDATE`, accountID)
	if err != nil {
		return fmt.Errorf("a9_initialization_balance_unavailable")
	}
	defer rows.Close()
	want := map[string]string{"BTC": "0.000000000000000000/0.000000000000000000",
		"ETH":  "0.000000000000000000/0.000000000000000000",
		"USDT": "500.000000000000000000/0.000000000000000000"}
	seen := 0
	for rows.Next() {
		var asset, available, reserved string
		if rows.Scan(&asset, &available, &reserved) != nil || want[asset] != available+"/"+reserved {
			return fmt.Errorf("a9_initialization_balance_invalid")
		}
		seen++
	}
	if rows.Err() != nil || seen != len(want) {
		return fmt.Errorf("a9_initialization_balance_invalid")
	}
	return nil
}

// Reserve atomically persists ranking evidence and both exclusive ownership claims.
func (repository *A9Repository) Reserve(ctx context.Context, write A9AllocationWrite) error {
	if !validA9Allocation(write) {
		return fmt.Errorf("a9_allocation_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a9_allocation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = executeA9Reserve(ctx, generated.New(tx), write); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("a9_allocation_commit_failed")
	}
	return nil
}

func executeA9Reserve(ctx context.Context, queries *generated.Queries, write A9AllocationWrite) error {
	var err error
	if _, err = queries.LockVirtualBalance(ctx, generated.LockVirtualBalanceParams{
		AccountID: write.Funds.AccountID, AssetSymbol: write.Funds.AssetSymbol}); err != nil {
		return fmt.Errorf("a9_funds_lock_failed")
	}
	if _, err = queries.LockLiquidityDomain(ctx, write.LiquidityDomainID); err != nil {
		return fmt.Errorf("a9_liquidity_lock_failed")
	}
	if _, err = queries.ReserveVirtualBalance(ctx, generated.ReserveVirtualBalanceParams{
		AccountID: write.Funds.AccountID, AssetSymbol: write.Funds.AssetSymbol,
		Available: write.FundsQuantity, UpdatedAt: write.Funds.CreatedAt}); err != nil {
		return fmt.Errorf("a9_funds_reservation_failed")
	}
	if _, err = queries.ReserveLiquidityDomain(ctx, generated.ReserveLiquidityDomainParams{
		ID: write.LiquidityDomainID, AvailableQuantity: write.LiquidityQuantity,
		UpdatedAt: write.Liquidity.CreatedAt, Revision: write.LiquidityRevision}); err != nil {
		return fmt.Errorf("a9_liquidity_reservation_failed")
	}
	if err = insertA9Journal(ctx, queries, write.Journal, write.Entries); err != nil {
		return err
	}
	if _, err = queries.InsertAllocationCandidate(ctx, write.Candidate); err != nil {
		return fmt.Errorf("a9_candidate_failed")
	}
	for _, score := range write.Scores {
		if _, err = queries.InsertAllocationScoreComponent(ctx, score); err != nil {
			return fmt.Errorf("a9_score_failed")
		}
	}
	if _, err = queries.InsertReservation(ctx, write.Funds); err != nil {
		return fmt.Errorf("a9_funds_claim_failed")
	}
	if _, err = queries.InsertLiquidityReservation(ctx, write.Liquidity); err != nil {
		return fmt.Errorf("a9_liquidity_claim_failed")
	}
	if _, err = queries.LinkAllocationReservations(ctx, write.Link); err != nil {
		return fmt.Errorf("a9_allocation_link_failed")
	}
	return nil
}

func validA9Allocation(write A9AllocationWrite) bool {
	return write.Candidate.ID != "" && write.Candidate.State == "reserved" && len(write.Scores) > 0 &&
		write.Funds.ID != "" && write.Funds.AccountID == write.Candidate.AccountID && write.FundsQuantity != nil &&
		write.LiquidityDomainID != "" && write.LiquidityRevision > 0 && write.LiquidityQuantity != nil &&
		write.Liquidity.ID != "" && write.Liquidity.CandidateID == write.Candidate.ID &&
		write.Liquidity.DomainID == write.LiquidityDomainID && write.Link.CandidateID == write.Candidate.ID &&
		write.Link.ReservationID == write.Funds.ID && write.Link.LiquidityReservationID == write.Liquidity.ID &&
		validA9Journal(write.Journal, write.Entries)
}

// Close atomically consumes, releases, expires, or quarantines both allocation claims.
func (repository *A9Repository) Close(ctx context.Context, write A9AllocationClose) error {
	if !validA9Close(write) {
		return fmt.Errorf("a9_allocation_close_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a9_allocation_close_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.LockVirtualBalance(ctx, generated.LockVirtualBalanceParams{
		AccountID: write.FundsAccountID, AssetSymbol: write.FundsAsset}); err != nil {
		return fmt.Errorf("a9_allocation_close_balance_lock_failed")
	}
	if _, err = queries.LockReservation(ctx, write.Funds.ID); err != nil {
		return fmt.Errorf("a9_allocation_close_funds_lock_failed")
	}
	if _, err = queries.LockLiquidityDomain(ctx, write.LiquidityDomainID); err != nil {
		return fmt.Errorf("a9_allocation_close_domain_lock_failed")
	}
	if _, err = queries.LockLiquidityReservation(ctx, write.Liquidity.ID); err != nil {
		return fmt.Errorf("a9_allocation_close_liquidity_lock_failed")
	}
	if err = insertA9Journal(ctx, queries, write.Journal, write.Entries); err != nil {
		return err
	}
	if _, err = queries.CloseAllocationCandidate(ctx, write.Candidate); err != nil {
		return fmt.Errorf("a9_allocation_close_candidate_failed")
	}
	if _, err = queries.CloseReservation(ctx, write.Funds); err != nil {
		return fmt.Errorf("a9_allocation_close_funds_failed")
	}
	if err = closeFundsProjection(ctx, queries, write); err != nil {
		return err
	}
	if _, err = queries.CloseLiquidityReservation(ctx, write.Liquidity); err != nil {
		return fmt.Errorf("a9_allocation_close_liquidity_failed")
	}
	if write.Funds.State == "released" || write.Funds.State == "expired" {
		if _, err = queries.ReleaseLiquidityDomain(ctx, generated.ReleaseLiquidityDomainParams{
			ID: write.LiquidityDomainID, AvailableQuantity: write.LiquidityQuantity,
			UpdatedAt: write.Liquidity.UpdatedAt}); err != nil {
			return fmt.Errorf("a9_allocation_close_domain_failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("a9_allocation_close_commit_failed")
	}
	return nil
}

func closeFundsProjection(ctx context.Context, queries *generated.Queries, write A9AllocationClose) error {
	params := generated.ReleaseVirtualBalanceParams{AccountID: write.FundsAccountID,
		AssetSymbol: write.FundsAsset, Available: write.FundsQuantity, UpdatedAt: write.Funds.UpdatedAt}
	switch write.Funds.State {
	case "released", "expired":
		if _, err := queries.ReleaseVirtualBalance(ctx, params); err != nil {
			return fmt.Errorf("a9_allocation_close_balance_failed")
		}
	case "consumed":
		if _, err := queries.ConsumeVirtualBalance(ctx, generated.ConsumeVirtualBalanceParams{
			AccountID: write.FundsAccountID, AssetSymbol: write.FundsAsset,
			Reserved: write.FundsQuantity, UpdatedAt: write.Funds.UpdatedAt}); err != nil {
			return fmt.Errorf("a9_allocation_close_balance_failed")
		}
	case "quarantined":
		return nil
	}
	return nil
}

func validA9Close(write A9AllocationClose) bool {
	state := write.Funds.State
	candidateState := state
	if state == "consumed" {
		candidateState = "settled"
	}
	return write.Candidate.ID != "" && write.Candidate.Revision > 0 && write.Candidate.State == candidateState &&
		(state == "consumed" || state == "released" || state == "expired" || state == "quarantined") &&
		write.Funds.ID != "" && write.Funds.Revision > 0 && write.Funds.FencingToken > 0 &&
		write.FundsAccountID != "" && write.FundsAsset != "" && write.FundsQuantity != nil &&
		write.Liquidity.ID != "" && write.Liquidity.State == state && write.Liquidity.Revision > 0 &&
		write.Liquidity.FencingToken == write.Funds.FencingToken && write.LiquidityDomainID != "" &&
		write.LiquidityQuantity != nil && validA9Journal(write.Journal, write.Entries)
}

// A9RecoveryEvidenceStore durably enforces the existing ordered recovery sequence.
type A9RecoveryEvidenceStore struct {
	pool      *pgxpool.Pool
	context   context.Context
	attemptID string
	now       func() time.Time
}

// NewA9RecoveryEvidenceStore constructs a durable stage-evidence adapter.
func NewA9RecoveryEvidenceStore(
	ctx context.Context,
	pool *pgxpool.Pool,
	attemptID string,
	now func() time.Time,
) (*A9RecoveryEvidenceStore, error) {
	if ctx == nil || pool == nil || attemptID == "" || now == nil {
		return nil, fmt.Errorf("a9_recovery_store_invalid")
	}
	return &A9RecoveryEvidenceStore{pool: pool, context: ctx, attemptID: attemptID, now: now}, nil
}

// Append persists only the next expected startup-recovery stage.
func (store *A9RecoveryEvidenceStore) Append(stage runtimecore.RecoveryStage, evidenceHash string) error {
	if !validHash(evidenceHash) {
		return fmt.Errorf("a9_recovery_evidence_invalid")
	}
	tx, err := store.pool.BeginTx(store.context, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a9_recovery_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var state string
	if err = tx.QueryRow(store.context,
		"SELECT state FROM startup_recovery_attempts WHERE id=$1 FOR UPDATE", store.attemptID).Scan(&state); err != nil || state != "locked" {
		return fmt.Errorf("a9_recovery_attempt_unavailable")
	}
	var ordinal int32
	if err = tx.QueryRow(store.context,
		"SELECT count(*)::integer FROM startup_recovery_evidence WHERE attempt_id=$1", store.attemptID).Scan(&ordinal); err != nil {
		return fmt.Errorf("a9_recovery_progress_unavailable")
	}
	sequence := runtimecore.RecoverySequence()
	if int(ordinal) >= len(sequence) || sequence[ordinal] != stage {
		return fmt.Errorf("a9_recovery_stage_rejected")
	}
	queries := generated.New(tx)
	if _, err = queries.InsertStartupRecoveryEvidence(store.context, generated.InsertStartupRecoveryEvidenceParams{
		AttemptID: store.attemptID, Ordinal: ordinal, Stage: string(stage), EvidenceHash: evidenceHash,
		RecordedAt: a9Timestamp(store.now())}); err != nil {
		return fmt.Errorf("a9_recovery_evidence_failed")
	}
	if err = tx.Commit(store.context); err != nil {
		return fmt.Errorf("a9_recovery_commit_failed")
	}
	return nil
}

// Complete transitions a fully evidenced attempt to administratively ready and still paused.
func (store *A9RecoveryEvidenceStore) Complete() error {
	tx, err := store.pool.BeginTx(store.context, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a9_recovery_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var count int
	if err = tx.QueryRow(store.context,
		"SELECT count(*) FROM startup_recovery_evidence WHERE attempt_id=$1", store.attemptID).Scan(&count); err != nil ||
		count != len(runtimecore.RecoverySequence()) {
		return fmt.Errorf("a9_recovery_incomplete")
	}
	if _, err = generated.New(tx).CompleteStartupRecoveryAttempt(store.context,
		generated.CompleteStartupRecoveryAttemptParams{ID: store.attemptID, CompletedAt: a9Timestamp(store.now())}); err != nil {
		return fmt.Errorf("a9_recovery_completion_failed")
	}
	if err = tx.Commit(store.context); err != nil {
		return fmt.Errorf("a9_recovery_commit_failed")
	}
	return nil
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func a9Timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: !value.IsZero() && value.Location() == time.UTC}
}

var _ interface {
	Append(runtimecore.RecoveryStage, string) error
} = (*A9RecoveryEvidenceStore)(nil)
