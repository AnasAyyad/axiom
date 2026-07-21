package reconciliation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
)

// State contains canonical hashes for every required virtual projection.
type State struct {
	Orders       string
	Fills        string
	Reservations string
	Balances     string
	Positions    string
	Ownership    string
	Journal      string
	Projections  string
	Duplicates   []string
	Differences  []Discrepancy
}

// Classification is one durable discrepancy class.
type Classification string

// Supported discrepancy classifications.
const (
	MissingFact      Classification = "missing_fact"
	DuplicateFact    Classification = "duplicate_fact"
	InconsistentFact Classification = "inconsistent_fact"
	UnknownFact      Classification = "unknown_fact"
)

// Discrepancy is one immutable authoritative/simulator difference.
type Discrepancy struct {
	Category       string
	Classification Classification
	ExpectedHash   string
	ActualHash     string
	Asset          domain.AssetSymbol
	Quantity       domain.Balance
	Critical       bool
}

// Case is durable reconciliation and quarantine evidence.
type Case struct {
	ID            string
	Scope         string
	State         string
	Discrepancies []Discrepancy
	IncidentID    string
	OpenedAt      time.Time
}

// CaseStore persists reconciliation history without overwriting source facts.
type CaseStore interface{ Create(Case) error }

// IncidentSink creates one critical incident.
type IncidentSink interface {
	Create(string, string, time.Time) (string, error)
}

// Quarantine blocks new entries for an affected scope.
type Quarantine interface{ Block(string, string) error }

// Context fixes exact suspense journal ownership.
type Context struct {
	RunID             domain.RunID
	PortfolioID       domain.PortfolioID
	ConfigurationHash string
	Owner             string
}

// Reconciler compares current states and never mutates either history.
type Reconciler struct {
	cases      CaseStore
	incidents  IncidentSink
	quarantine Quarantine
	journal    accounting.Journal
	context    Context
}

// NewReconciler constructs the fail-closed virtual reconciliation boundary.
func NewReconciler(
	cases CaseStore,
	incidents IncidentSink,
	quarantine Quarantine,
	journal accounting.Journal,
	context Context,
) (*Reconciler, error) {
	if cases == nil || incidents == nil || quarantine == nil || journal == nil || context.RunID.Value() == "" ||
		context.PortfolioID.Value() == "" || context.ConfigurationHash == "" || context.Owner == "" {
		return nil, reconciliationError("reconciler_configuration_invalid")
	}
	return &Reconciler{cases: cases, incidents: incidents, quarantine: quarantine,
		journal: journal, context: context}, nil
}

// Reconcile persists every mismatch and quarantines any critical uncertainty.
func (reconciler *Reconciler) Reconcile(scope string, expected, actual State, at time.Time) (Case, error) {
	if scope == "" || at.IsZero() || at.Location() != time.UTC || !validState(expected) || !validState(actual) {
		return Case{}, reconciliationError("reconciliation_input_invalid")
	}
	discrepancies := compareStates(expected, actual)
	if len(discrepancies) == 0 {
		return Case{}, nil
	}
	identifier := caseID(scope, expected, actual)
	result := Case{ID: identifier, Scope: scope, State: "open", Discrepancies: discrepancies, OpenedAt: at}
	if critical(discrepancies) {
		incident, err := reconciler.incidents.Create(scope, "critical_reconciliation_mismatch", at)
		if err != nil || reconciler.quarantine.Block(scope, "critical_reconciliation_mismatch") != nil {
			return Case{}, reconciliationError("reconciliation_quarantine_failed")
		}
		result.State, result.IncidentID = "quarantined", incident
		if err = reconciler.postSuspense(identifier, discrepancies, at); err != nil {
			return Case{}, err
		}
	}
	if err := reconciler.cases.Create(result); err != nil {
		return Case{}, reconciliationError("reconciliation_case_failed")
	}
	return cloneCase(result), nil
}

func compareStates(expected, actual State) []Discrepancy {
	left := []struct{ category, hash string }{{"orders", expected.Orders}, {"fills", expected.Fills},
		{"reservations", expected.Reservations}, {"balances", expected.Balances}, {"positions", expected.Positions},
		{"ownership", expected.Ownership}, {"journal", expected.Journal}, {"projections", expected.Projections}}
	right := []string{actual.Orders, actual.Fills, actual.Reservations, actual.Balances,
		actual.Positions, actual.Ownership, actual.Journal, actual.Projections}
	result := make([]Discrepancy, 0)
	for index, item := range left {
		if item.hash != right[index] {
			classification := InconsistentFact
			if right[index] == emptyHash() {
				classification = MissingFact
			}
			result = append(result, Discrepancy{Category: item.category, Classification: classification,
				ExpectedHash: item.hash, ActualHash: right[index], Critical: true})
		}
	}
	for _, duplicate := range actual.Duplicates {
		result = append(result, Discrepancy{Category: duplicate, Classification: DuplicateFact, Critical: true})
	}
	result = append(result, actual.Differences...)
	return result
}

func (reconciler *Reconciler) postSuspense(caseIdentity string, items []Discrepancy, at time.Time) error {
	for index, item := range items {
		if item.Asset == "" || item.Quantity.String() == "0" {
			continue
		}
		transactionID, _ := domain.NewJournalTransactionID(fmt.Sprintf("reconcile-%s-%d", caseIdentity[:12], index+1))
		cause, _ := domain.NewEventID(fmt.Sprintf("reconcile-%s-%d", caseIdentity[:12], index+1))
		transaction := accounting.Transaction{ID: transactionID, Type: "reconciliation_suspense",
			RunID: reconciler.context.RunID, PortfolioID: reconciler.context.PortfolioID,
			ConfigurationHash: reconciler.context.ConfigurationHash, CausationID: cause,
			RecordedAt: domain.EventTime{UTC: at, Sequence: uint64(index + 1)}, IngestOrdinal: uint64(index + 1),
			Lines: suspenseLines(item, reconciler.context.Owner)}
		if err := reconciler.journal.Append(transaction); err != nil {
			return reconciliationError("reconciliation_suspense_failed")
		}
	}
	return nil
}

func suspenseLines(item Discrepancy, owner string) []accounting.Line {
	return []accounting.Line{
		{Account: accounting.AccountKey{Class: accounting.ReconciliationSuspense, Asset: item.Asset, Owner: owner},
			Direction: accounting.Debit, Quantity: item.Quantity},
		{Account: accounting.AccountKey{Class: accounting.AvailableAsset, Asset: item.Asset, Owner: owner},
			Direction: accounting.Credit, Quantity: item.Quantity},
	}
}

func validState(state State) bool {
	for _, hash := range []string{state.Orders, state.Fills, state.Reservations, state.Balances,
		state.Positions, state.Ownership, state.Journal, state.Projections} {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size {
			return false
		}
	}
	for _, item := range state.Differences {
		if item.Category == "" || item.Classification == "" || !item.Critical || item.Asset == "" ||
			item.Quantity.String() == "0" {
			return false
		}
	}
	return true
}

func caseID(scope string, expected, actual State) string {
	encoded, _ := json.Marshal([]any{scope, expected, actual})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func emptyHash() string {
	digest := sha256.Sum256(nil)
	return hex.EncodeToString(digest[:])
}

func critical(items []Discrepancy) bool {
	for _, item := range items {
		if item.Critical {
			return true
		}
	}
	return false
}

func cloneCase(value Case) Case {
	value.Discrepancies = append([]Discrepancy(nil), value.Discrepancies...)
	return value
}
