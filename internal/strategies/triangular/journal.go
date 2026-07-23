package triangular

import (
	"fmt"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/strategies/arbitrage"
)

// JournalContext fixes durable ownership and causation for one B4 cycle.
type JournalContext struct {
	RunID             domain.RunID
	PortfolioID       domain.PortfolioID
	Owner             string
	ConfigurationHash string
	RecordedAt        domain.EventTime
	FirstOrdinal      uint64
}

// CycleJournal emits economically separated, exact, balanced B4 facts.
type CycleJournal struct {
	journal accounting.Journal
	context JournalContext
}

// NewCycleJournal constructs the B4 journal boundary.
func NewCycleJournal(journal accounting.Journal, context JournalContext) (*CycleJournal, error) {
	if journal == nil || context.RunID.Value() == "" || context.PortfolioID.Value() == "" ||
		context.Owner == "" || context.ConfigurationHash == "" || context.RecordedAt.Validate() != nil ||
		context.FirstOrdinal == 0 {
		return nil, strategyError("journal_configuration_invalid")
	}
	return &CycleJournal{journal: journal, context: context}, nil
}

// Transactions builds the complete validated posting set before durable append.
func (journal *CycleJournal) Transactions(
	candidate Candidate,
	result SimulationResult,
) ([]accounting.Transaction, error) {
	if candidate.ID == "" || result.CandidateID != candidate.ID || candidate.ConfigurationHash != journal.context.ConfigurationHash {
		return nil, strategyError("journal_result_invalid")
	}
	postings, err := cyclePostings(candidate, result)
	if err != nil {
		return nil, err
	}
	return journal.buildTransactions(candidate, postings)
}

func cyclePostings(candidate Candidate, result SimulationResult) ([]cyclePosting, error) {
	usdt, _ := domain.ParseAssetSymbol("USDT")
	postings := make([]cyclePosting, 0, 8)
	if result.Outcome == OutcomeFullSuccess || result.Outcome == OutcomeNegativeAfterLatency {
		trade, err := tradeEconomics(candidate)
		if err != nil {
			return nil, err
		}
		postings = appendPositive(postings, "trade_economics", accounting.TradeCostProceeds,
			accounting.RealizedPnL, usdt, trade)
	}
	fees, spread, err := actualCosts(result)
	if err != nil {
		return nil, err
	}
	postings = appendPositive(postings, "fees", accounting.FeeExpense,
		accounting.RealizedPnL, usdt, fees)
	postings = appendPositive(postings, "spread_depth", accounting.SpreadAttribution,
		accounting.RealizedPnL, usdt, spread)
	postings, err = appendDust(postings, result)
	if err != nil {
		return nil, err
	}
	if result.Outcome == OutcomeFullSuccess || result.Outcome == OutcomeNegativeAfterLatency {
		latency, latencyErr := positiveDifference(candidate.Final, result.FinalUSDT)
		if latencyErr != nil {
			return nil, latencyErr
		}
		postings = appendPositive(postings, "latency", accounting.LatencyAttribution,
			accounting.RealizedPnL, usdt, latency)
	}
	if result.Recovery.Recovered {
		recovery := pnlBalance(result.Recovery.Loss)
		postings = appendPositive(postings, "recovery_unwind", accounting.RecoveryLoss,
			accounting.RealizedPnL, usdt, recovery)
	}
	if result.Recovery.Quarantined {
		quantity, quantityErr := domain.ParseBalance(result.Recovery.Input.String())
		if quantityErr != nil {
			return nil, strategyError("journal_result_invalid")
		}
		postings = appendPositive(postings, "stranded_inventory", accounting.ReconciliationSuspense,
			accounting.StrategyInventory, result.Recovery.Asset, quantity)
	}
	return postings, nil
}

func (journal *CycleJournal) buildTransactions(
	candidate Candidate,
	postings []cyclePosting,
) ([]accounting.Transaction, error) {
	transactions := make([]accounting.Transaction, 0, len(postings))
	for index, posting := range postings {
		transaction, transactionErr := journal.transaction(
			candidate, posting, journal.context.FirstOrdinal+uint64(index),
		)
		if transactionErr != nil {
			return nil, transactionErr
		}
		if transactionErr = accounting.ValidateTransaction(transaction); transactionErr != nil {
			return nil, strategyError("journal_transaction_invalid")
		}
		transactions = append(transactions, transaction)
	}
	return transactions, nil
}

// Post validates the complete posting set, then appends each immutable fact.
func (journal *CycleJournal) Post(candidate Candidate, result SimulationResult) error {
	transactions, err := journal.Transactions(candidate, result)
	if err != nil {
		return err
	}
	for _, transaction := range transactions {
		if err = journal.journal.Append(transaction); err != nil {
			return strategyError("journal_append_failed")
		}
	}
	return nil
}

// PostReconciliation records an explicit operator/system adjustment without
// hiding it in strategy P&L.
func (journal *CycleJournal) PostReconciliation(
	candidate Candidate,
	asset domain.AssetSymbol,
	quantity domain.Balance,
	ordinal uint64,
) error {
	posting := cyclePosting{
		name: "reconciliation_adjustment", debit: accounting.ReconciliationSuspense,
		credit: accounting.StrategyInventory, asset: asset, quantity: quantity,
	}
	transaction, err := journal.transaction(candidate, posting, ordinal)
	if err != nil {
		return err
	}
	if err = journal.journal.Append(transaction); err != nil {
		return strategyError("journal_append_failed")
	}
	return nil
}

type cyclePosting struct {
	name          string
	debit, credit accounting.AccountClass
	asset         domain.AssetSymbol
	quantity      domain.Balance
}

func (journal *CycleJournal) transaction(
	candidate Candidate,
	posting cyclePosting,
	ordinal uint64,
) (accounting.Transaction, error) {
	suffix := candidate.ID
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	transactionID, err := domain.NewJournalTransactionID(
		fmt.Sprintf("b4-%s-%s-%d", suffix, posting.name, ordinal),
	)
	if err != nil {
		return accounting.Transaction{}, strategyError("journal_identity_invalid")
	}
	eventID, err := domain.NewEventID(fmt.Sprintf("b4-event-%s-%d", suffix, ordinal))
	if err != nil {
		return accounting.Transaction{}, strategyError("journal_identity_invalid")
	}
	return accounting.Transaction{
		ID: transactionID, Type: "b4_" + posting.name,
		RunID: journal.context.RunID, PortfolioID: journal.context.PortfolioID,
		ConfigurationHash: journal.context.ConfigurationHash, CausationID: eventID,
		RecordedAt: journal.context.RecordedAt, IngestOrdinal: ordinal,
		Lines: []accounting.Line{
			{
				Account: accounting.AccountKey{
					Class: posting.debit, Asset: posting.asset, Owner: journal.context.Owner,
				},
				Direction: accounting.Debit, Quantity: posting.quantity,
			},
			{
				Account: accounting.AccountKey{
					Class: posting.credit, Asset: posting.asset, Owner: journal.context.Owner,
				},
				Direction: accounting.Credit, Quantity: posting.quantity,
			},
		},
	}, nil
}

func actualCosts(result SimulationResult) (domain.Balance, domain.Balance, error) {
	fees, _ := domain.ParseBalance("0")
	spread, _ := domain.ParseBalance("0")
	legs := append([]arbitrage.Result(nil), result.Legs...)
	if result.Recovery.Leg != nil {
		legs = append(legs, *result.Recovery.Leg)
	}
	for _, leg := range legs {
		fee, feeErr := domain.ParseBalance(leg.FeeQuoteEquivalent.String())
		spreadCost, spreadErr := domain.ParseBalance(leg.SpreadCost.String())
		if feeErr != nil || spreadErr != nil {
			return domain.Balance{}, domain.Balance{}, strategyError("journal_result_invalid")
		}
		var err error
		fees, err = fees.Add(fee)
		if err != nil {
			return domain.Balance{}, domain.Balance{}, strategyError("journal_result_invalid")
		}
		spread, err = spread.Add(spreadCost)
		if err != nil {
			return domain.Balance{}, domain.Balance{}, strategyError("journal_result_invalid")
		}
	}
	return fees, spread, nil
}

func tradeEconomics(candidate Candidate) (domain.Balance, error) {
	value, err := domain.ParseBalance(candidate.ExpectedNet.String())
	if err != nil {
		return domain.Balance{}, strategyError("journal_result_invalid")
	}
	for _, leg := range candidate.Legs {
		fee, feeErr := domain.ParseBalance(leg.FeeQuoteEquivalent.String())
		spread, spreadErr := domain.ParseBalance(leg.SpreadCost.String())
		if feeErr != nil || spreadErr != nil {
			return domain.Balance{}, strategyError("journal_result_invalid")
		}
		value, err = value.Add(fee)
		if err == nil {
			value, err = value.Add(spread)
		}
		if err != nil {
			return domain.Balance{}, strategyError("journal_result_invalid")
		}
	}
	return value, nil
}

func appendDust(postings []cyclePosting, result SimulationResult) ([]cyclePosting, error) {
	legs := result.Legs
	if result.Recovery.Leg != nil {
		legs = append(append([]arbitrage.Result(nil), legs...), *result.Recovery.Leg)
	}
	for index, leg := range legs {
		dust, err := domain.ParseBalance(leg.SourceDust.String())
		if err != nil {
			return nil, strategyError("journal_result_invalid")
		}
		postings = appendPositive(postings, "rounding_dust_"+uintString(uint64(index+1)),
			accounting.RoundingDust, accounting.StrategyInventory, leg.Source, dust)
	}
	return postings, nil
}

func positiveDifference(left, right domain.Quantity) (domain.Balance, error) {
	if left.Compare(right) <= 0 {
		return domain.ParseBalance("0")
	}
	difference, err := left.Subtract(right)
	if err != nil {
		return domain.Balance{}, strategyError("journal_result_invalid")
	}
	return domain.ParseBalance(difference.String())
}

func pnlBalance(value domain.PnL) domain.Balance {
	text := value.String()
	if len(text) > 0 && text[0] == '-' {
		text = text[1:]
	}
	quantity, err := domain.ParseBalance(text)
	if err != nil {
		quantity, _ = domain.ParseBalance("0")
	}
	return quantity
}

func appendPositive(
	postings []cyclePosting,
	name string,
	debit, credit accounting.AccountClass,
	asset domain.AssetSymbol,
	quantity domain.Balance,
) []cyclePosting {
	zero, _ := domain.ParseBalance("0")
	if quantity.Compare(zero) > 0 {
		return append(postings, cyclePosting{
			name: name, debit: debit, credit: credit, asset: asset, quantity: quantity,
		})
	}
	return postings
}
