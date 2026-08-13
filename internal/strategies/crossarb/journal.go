package crossarb

import (
	"fmt"

	"axiom/internal/accounting"
	"axiom/internal/domain"
)

// JournalContext fixes durable ownership and causation for cross-exchange arbitrage facts.
type JournalContext struct {
	RunID             domain.RunID
	PortfolioID       domain.PortfolioID
	Owner             string
	ConfigurationHash string
	RecordedAt        domain.EventTime
	FirstOrdinal      uint64
}

// AttributionValue keeps a signed economic direction with non-negative exact
// posting magnitude.
type AttributionValue struct {
	Amount domain.Balance `json:"amount"`
	Gain   bool           `json:"gain"`
}

// PortfolioAttribution preserves execution, both base inventories,
// stablecoin valuation, explicit costs, restoration, and combined P&L.
type PortfolioAttribution struct {
	ExecutionPnL        AttributionValue `json:"execution_pnl"`
	BTCInventoryPnL     AttributionValue `json:"btc_inventory_pnl"`
	ETHInventoryPnL     AttributionValue `json:"eth_inventory_pnl"`
	StablecoinValuation AttributionValue `json:"stablecoin_valuation"`
	Fees                domain.Balance   `json:"fees"`
	Spread              domain.Balance   `json:"spread"`
	Slippage            domain.Balance   `json:"slippage"`
	Latency             domain.Balance   `json:"latency"`
	Recovery            domain.Balance   `json:"recovery"`
	Rebalancing         domain.Balance   `json:"rebalancing"`
	CombinedPnL         AttributionValue `json:"combined_pnl"`
	// ZeroCategories distinguishes an observed exact zero from an omitted
	// attribution. Journal lines remain positive-only while canonical evidence
	// still proves every required category.
	ZeroCategories []string `json:"zero_categories,omitempty"`
}

// CrossExchangeJournal emits exact independently balanced commodity facts.
type CrossExchangeJournal struct {
	journal accounting.Journal
	context JournalContext
}

// NewCrossExchangeJournal constructs the cross-exchange arbitrage accounting boundary.
func NewCrossExchangeJournal(
	journal accounting.Journal,
	context JournalContext,
) (*CrossExchangeJournal, error) {
	if journal == nil || context.RunID.Value() == "" || context.PortfolioID.Value() == "" ||
		context.Owner == "" || context.ConfigurationHash == "" ||
		context.RecordedAt.Validate() != nil || context.FirstOrdinal == 0 {
		return nil, strategyError("journal_configuration_invalid")
	}
	return &CrossExchangeJournal{journal: journal, context: context}, nil
}

// Transactions validates the complete categorized set before any append.
func (journal *CrossExchangeJournal) Transactions(
	candidate Candidate,
	result SimulationResult,
	attribution PortfolioAttribution,
) ([]accounting.Transaction, error) {
	if candidate.ID == "" || result.CandidateID != candidate.ID ||
		candidate.ConfigurationHash != journal.context.ConfigurationHash {
		return nil, strategyError("journal_result_invalid")
	}
	postings := attributionPostings(attribution)
	transactions := make([]accounting.Transaction, 0, len(postings))
	zeroCategories, err := validatedZeroCategories(attribution, postings)
	if err != nil {
		return nil, err
	}
	for index, posting := range postings {
		zero, _ := domain.ParseBalance("0")
		if posting.quantity.Compare(zero) == 0 {
			if !zeroCategories[posting.name] {
				return nil, strategyError("journal_attribution_incomplete")
			}
			continue
		}
		transaction, err := journal.transaction(candidate, posting,
			journal.context.FirstOrdinal+uint64(index))
		if err != nil || accounting.ValidateTransaction(transaction) != nil {
			return nil, strategyError("journal_transaction_invalid")
		}
		transactions = append(transactions, transaction)
	}
	if len(transactions) == 0 {
		return nil, strategyError("journal_attribution_incomplete")
	}
	return transactions, nil
}

func validatedZeroCategories(attribution PortfolioAttribution, postings []journalPosting) (map[string]bool, error) {
	result := make(map[string]bool, len(attribution.ZeroCategories))
	for _, category := range attribution.ZeroCategories {
		if category == "" || result[category] {
			return nil, strategyError("journal_attribution_incomplete")
		}
		result[category] = true
	}
	zero, _ := domain.ParseBalance("0")
	for _, posting := range postings {
		if result[posting.name] != (posting.quantity.Compare(zero) == 0) {
			return nil, strategyError("journal_attribution_incomplete")
		}
		delete(result, posting.name)
	}
	if len(result) != 0 {
		return nil, strategyError("journal_attribution_incomplete")
	}
	result = make(map[string]bool, len(attribution.ZeroCategories))
	for _, category := range attribution.ZeroCategories {
		result[category] = true
	}
	return result, nil
}

// Post appends only after the full set validates.
func (journal *CrossExchangeJournal) Post(
	candidate Candidate,
	result SimulationResult,
	attribution PortfolioAttribution,
) error {
	transactions, err := journal.Transactions(candidate, result, attribution)
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

type journalPosting struct {
	name          string
	debit, credit accounting.AccountClass
	asset         domain.AssetSymbol
	quantity      domain.Balance
}

func attributionPostings(attribution PortfolioAttribution) []journalPosting {
	usdt, _ := domain.ParseAssetSymbol("USDT")
	btc, _ := domain.ParseAssetSymbol("BTC")
	eth, _ := domain.ParseAssetSymbol("ETH")
	postings := []journalPosting{
		signedPosting("execution_pnl", accounting.TradeCostProceeds, accounting.RealizedPnL,
			usdt, attribution.ExecutionPnL),
		signedPosting("btc_inventory_market_pnl", accounting.InventoryValuation, accounting.UnrealizedPnL,
			btc, attribution.BTCInventoryPnL),
		signedPosting("eth_inventory_market_pnl", accounting.InventoryValuation, accounting.UnrealizedPnL,
			eth, attribution.ETHInventoryPnL),
		signedPosting("stablecoin_valuation", accounting.InventoryValuation, accounting.UnrealizedPnL,
			usdt, attribution.StablecoinValuation),
		costPosting("fees", accounting.FeeExpense, usdt, attribution.Fees),
		costPosting("spread", accounting.SpreadAttribution, usdt, attribution.Spread),
		costPosting("slippage", accounting.SlippageAttribution, usdt, attribution.Slippage),
		costPosting("latency", accounting.LatencyAttribution, usdt, attribution.Latency),
		costPosting("recovery", accounting.RecoveryLoss, usdt, attribution.Recovery),
		costPosting("inventory_restoration", accounting.RebalancingExpense, usdt, attribution.Rebalancing),
		signedPosting("combined_pnl", accounting.RealizedPnL, accounting.ExternalEquity,
			usdt, attribution.CombinedPnL),
	}
	return postings
}

func signedPosting(
	name string,
	debit, credit accounting.AccountClass,
	asset domain.AssetSymbol,
	value AttributionValue,
) journalPosting {
	if value.Gain {
		debit, credit = credit, debit
	}
	return journalPosting{name: name, debit: debit, credit: credit, asset: asset, quantity: value.Amount}
}

func costPosting(
	name string,
	class accounting.AccountClass,
	asset domain.AssetSymbol,
	value domain.Balance,
) journalPosting {
	return journalPosting{
		name: name, debit: class, credit: accounting.RealizedPnL, asset: asset, quantity: value,
	}
}

func (journal *CrossExchangeJournal) transaction(
	candidate Candidate,
	posting journalPosting,
	ordinal uint64,
) (accounting.Transaction, error) {
	suffix := candidate.ID
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	transactionID, err := domain.NewJournalTransactionID(
		fmt.Sprintf("cross_exchange_arbitrage-%s-%s-%d", suffix, posting.name, ordinal),
	)
	if err != nil {
		return accounting.Transaction{}, err
	}
	eventID, err := domain.NewEventID(fmt.Sprintf("cross_exchange_arbitrage-event-%s-%d", suffix, ordinal))
	if err != nil {
		return accounting.Transaction{}, err
	}
	return accounting.Transaction{
		ID: transactionID, Type: "cross_exchange_arbitrage_" + posting.name,
		RunID: journal.context.RunID, PortfolioID: journal.context.PortfolioID,
		ConfigurationHash: journal.context.ConfigurationHash, CausationID: eventID,
		RecordedAt: journal.context.RecordedAt, IngestOrdinal: ordinal,
		Lines: []accounting.Line{
			{Account: accounting.AccountKey{Class: posting.debit, Asset: posting.asset,
				Owner: journal.context.Owner}, Direction: accounting.Debit, Quantity: posting.quantity},
			{Account: accounting.AccountKey{Class: posting.credit, Asset: posting.asset,
				Owner: journal.context.Owner}, Direction: accounting.Credit, Quantity: posting.quantity},
		},
	}, nil
}
