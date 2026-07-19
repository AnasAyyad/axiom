package simulation

import (
	"fmt"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
)

// JournalContext fixes ownership and causation for virtual fill postings.
type JournalContext struct {
	RunID             domain.RunID
	PortfolioID       domain.PortfolioID
	Owner             string
	ConfigurationHash string
}

// FillJournal posts every simulated fill and fee as exact balanced commodities.
type FillJournal struct {
	journal accounting.Journal
	context JournalContext
}

// NewFillJournal constructs a virtual journal adapter with no implicit owner.
func NewFillJournal(journal accounting.Journal, context JournalContext) (*FillJournal, error) {
	if journal == nil || context.RunID.Value() == "" || context.PortfolioID.Value() == "" ||
		context.Owner == "" || context.ConfigurationHash == "" {
		return nil, simulationError("journal_configuration_invalid")
	}
	return &FillJournal{journal: journal, context: context}, nil
}

// PostFill appends one balanced immutable transaction for base, quote, and fee assets.
func (journal *FillJournal) PostFill(order execution.OrderIdentity, fill execution.FillFact) error {
	transaction, err := journal.Transaction(order, fill)
	if err != nil {
		return err
	}
	return journal.journal.Append(transaction)
}

// Transaction returns the exact balanced transaction for a simulated fill so
// durable adapters can commit it in the same database transaction as the fill.
func (journal *FillJournal) Transaction(
	order execution.OrderIdentity,
	fill execution.FillFact,
) (accounting.Transaction, error) {
	return journal.fillTransaction(order, fill)
}

// PostAdjustment records dust or recovery loss without hiding the counter-account.
func (journal *FillJournal) PostAdjustment(
	typeName string,
	class accounting.AccountClass,
	asset domain.AssetSymbol,
	quantity domain.Balance,
	eventID domain.EventID,
	ordinal uint64,
) error {
	if typeName == "" || (class != accounting.RoundingDust && class != accounting.RecoveryLoss) {
		return simulationError("journal_adjustment_invalid")
	}
	transactionID, err := domain.NewJournalTransactionID(fmt.Sprintf("%s-%d", typeName, ordinal))
	if err != nil {
		return simulationError("journal_adjustment_invalid")
	}
	return journal.journal.Append(accounting.Transaction{ID: transactionID, Type: typeName,
		RunID: journal.context.RunID, PortfolioID: journal.context.PortfolioID,
		ConfigurationHash: journal.context.ConfigurationHash, CausationID: eventID,
		RecordedAt: mustEventTime(ordinal), IngestOrdinal: ordinal, Lines: balancedLines(class,
			accounting.AvailableAsset, asset, quantity, journal.context.Owner)})
}

func (journal *FillJournal) fillTransaction(
	order execution.OrderIdentity,
	fill execution.FillFact,
) (accounting.Transaction, error) {
	notional, err := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
	if err != nil {
		return accounting.Transaction{}, simulationError("fill_journal_notional_invalid")
	}
	quote, err := domain.ParseBalance(notional.String())
	base, baseErr := domain.ParseBalance(fill.Quantity.String())
	fee, feeErr := domain.ParseBalance(fill.Fee.String())
	rebate, rebateErr := domain.ParseBalance(fill.Rebate.String())
	if err != nil || baseErr != nil || feeErr != nil || rebateErr != nil {
		return accounting.Transaction{}, simulationError("fill_journal_quantity_invalid")
	}
	lines := fillLines(order, base, quote, journal.context.Owner)
	zero, _ := domain.ParseBalance("0")
	if fee.Compare(zero) > 0 {
		lines = append(lines, balancedLines(accounting.FeeExpense, accounting.AvailableAsset,
			fill.FeeAsset, fee, journal.context.Owner)...)
	}
	if rebate.Compare(zero) > 0 {
		lines = append(lines, balancedLines(accounting.AvailableAsset, accounting.FeeExpense,
			fill.FeeAsset, rebate, journal.context.Owner)...)
	}
	transactionID, eventID, err := fillIDs(fill)
	if err != nil {
		return accounting.Transaction{}, err
	}
	return accounting.Transaction{ID: transactionID, Type: "simulated_fill", RunID: journal.context.RunID,
		PortfolioID: journal.context.PortfolioID, ConfigurationHash: journal.context.ConfigurationHash,
		CausationID: eventID, RecordedAt: mustEventTime(fill.Ordinal), IngestOrdinal: fill.Ordinal, Lines: lines}, nil
}

func fillLines(order execution.OrderIdentity, base, quote domain.Balance, owner string) []accounting.Line {
	baseDebit, baseCredit := accounting.StrategyInventory, accounting.AvailableAsset
	quoteDebit, quoteCredit := accounting.TradeCostProceeds, accounting.AvailableAsset
	if order.Side == domain.SideSell {
		baseDebit, baseCredit = accounting.AvailableAsset, accounting.StrategyInventory
		quoteDebit, quoteCredit = accounting.AvailableAsset, accounting.TradeCostProceeds
	}
	lines := balancedLines(baseDebit, baseCredit, order.Instrument.Base, base, owner)
	return append(lines, balancedLines(quoteDebit, quoteCredit, order.Instrument.Quote, quote, owner)...)
}

func balancedLines(
	debit, credit accounting.AccountClass,
	asset domain.AssetSymbol,
	quantity domain.Balance,
	owner string,
) []accounting.Line {
	return []accounting.Line{
		{Account: accounting.AccountKey{Class: debit, Asset: asset, Owner: owner}, Direction: accounting.Debit, Quantity: quantity},
		{Account: accounting.AccountKey{Class: credit, Asset: asset, Owner: owner}, Direction: accounting.Credit, Quantity: quantity},
	}
}

func fillIDs(fill execution.FillFact) (domain.JournalTransactionID, domain.EventID, error) {
	transactionID, err := domain.NewJournalTransactionID("fill-" + fill.ID.Value())
	if err != nil {
		return domain.JournalTransactionID{}, domain.EventID{}, simulationError("fill_journal_identity_invalid")
	}
	eventID, err := domain.NewEventID("fill-event-" + fill.ID.Value())
	if err != nil {
		return domain.JournalTransactionID{}, domain.EventID{}, simulationError("fill_journal_identity_invalid")
	}
	return transactionID, eventID, nil
}

func mustEventTime(ordinal uint64) domain.EventTime {
	return domain.EventTime{UTC: eventTime(ordinal), Sequence: ordinal}
}
