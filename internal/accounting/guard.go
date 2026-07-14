package accounting

import "axiom/internal/domain"

// JournalFailure is sanitized pause/incident evidence. It intentionally omits
// financial quantities and arbitrary caller payloads.
type JournalFailure struct {
	TransactionID domain.JournalTransactionID
	RunID         domain.RunID
	PortfolioID   domain.PortfolioID
	Code          string
}

// FailureHandler atomically pauses the affected scope and records an incident
// before a rejected posting can be treated as handled.
type FailureHandler interface {
	PauseAndRecord(JournalFailure) error
}

// GuardedJournal couples rejected postings to the fail-closed incident boundary.
type GuardedJournal struct {
	journal Journal
	handler FailureHandler
}

// NewGuardedJournal requires both a journal implementation and failure handler.
func NewGuardedJournal(journal Journal, handler FailureHandler) (*GuardedJournal, error) {
	if journal == nil || handler == nil {
		return nil, accountingError("journal_guard_invalid")
	}
	return &GuardedJournal{journal: journal, handler: handler}, nil
}

// Append delegates valid work and records sanitized fail-closed evidence for
// every rejected posting. A handler failure never converts the append to success.
func (journal *GuardedJournal) Append(transaction Transaction) error {
	err := journal.journal.Append(transaction)
	if err == nil {
		return nil
	}
	code := "journal_rejected"
	if accountingErr, ok := err.(Error); ok {
		code = accountingErr.Code
	}
	failure := JournalFailure{
		TransactionID: transaction.ID, RunID: transaction.RunID,
		PortfolioID: transaction.PortfolioID, Code: code,
	}
	if handlerErr := journal.handler.PauseAndRecord(failure); handlerErr != nil {
		return accountingError("journal_failure_handler_failed")
	}
	return err
}

// Transactions returns the guarded journal's immutable history.
func (journal *GuardedJournal) Transactions() []Transaction {
	return journal.journal.Transactions()
}

// Rebuild delegates deterministic projection rebuild.
func (journal *GuardedJournal) Rebuild() ([]Projection, string, error) {
	return journal.journal.Rebuild()
}

var _ Journal = (*GuardedJournal)(nil)
