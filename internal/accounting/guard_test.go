package accounting

import (
	"errors"
	"testing"
)

type recordingFailureHandler struct {
	failures []JournalFailure
	err      error
}

func (handler *recordingFailureHandler) PauseAndRecord(failure JournalFailure) error {
	handler.failures = append(handler.failures, failure)
	return handler.err
}

func TestGuardedJournalRecordsSanitizedFailureAndNeverAppends(t *testing.T) {
	handler := &recordingFailureHandler{}
	journal, err := NewGuardedJournal(NewMemoryJournal(), handler)
	if err != nil {
		t.Fatal(err)
	}
	invalid := journalTransaction(t)
	invalid.Lines[1].Account.Asset = "BTC"
	if err = journal.Append(invalid); err == nil {
		t.Fatal("unbalanced posting accepted")
	}
	if len(handler.failures) != 1 || handler.failures[0].Code != "unbalanced_commodity" ||
		handler.failures[0].TransactionID.String() != invalid.ID.String() || len(journal.Transactions()) != 0 {
		t.Fatalf("guard evidence/history = %#v / %#v", handler.failures, journal.Transactions())
	}
}

func TestGuardedJournalHandlerFailureStillRejects(t *testing.T) {
	handler := &recordingFailureHandler{err: errors.New("incident store unavailable")}
	journal, _ := NewGuardedJournal(NewMemoryJournal(), handler)
	invalid := journalTransaction(t)
	invalid.Lines = invalid.Lines[:1]
	err := journal.Append(invalid)
	accountingErr, ok := err.(Error)
	if !ok || accountingErr.Code != "journal_failure_handler_failed" || len(journal.Transactions()) != 0 {
		t.Fatalf("handler failure did not fail closed: %v", err)
	}
}

func TestGuardedJournalDoesNotEmitFailureForValidPosting(t *testing.T) {
	handler := &recordingFailureHandler{}
	journal, _ := NewGuardedJournal(NewMemoryJournal(), handler)
	if err := journal.Append(journalTransaction(t)); err != nil {
		t.Fatal(err)
	}
	if len(handler.failures) != 0 || len(journal.Transactions()) != 1 {
		t.Fatalf("valid posting emitted failure: %#v", handler.failures)
	}
}
