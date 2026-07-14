package accounting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"

	"axiom/internal/domain"
)

// Direction is one side of an exact journal line.
type Direction string

// Supported journal directions.
const (
	Debit  Direction = "debit"
	Credit Direction = "credit"
)

// AccountClass keeps economically distinct facts separate.
type AccountClass string

// Required V1A account classes.
const (
	ExternalEquity         AccountClass = "external_equity"
	AvailableAsset         AccountClass = "available_asset"
	ReservedAsset          AccountClass = "reserved_asset"
	StrategyInventory      AccountClass = "strategy_inventory"
	ExchangeInventory      AccountClass = "exchange_inventory"
	TradeCostProceeds      AccountClass = "trade_cost_proceeds"
	FeeExpense             AccountClass = "fee_expense"
	SpreadAttribution      AccountClass = "spread_attribution"
	SlippageAttribution    AccountClass = "slippage_attribution"
	LatencyAttribution     AccountClass = "latency_attribution"
	RealizedPnL            AccountClass = "realized_pnl"
	UnrealizedPnL          AccountClass = "unrealized_pnl"
	InventoryValuation     AccountClass = "inventory_valuation"
	RebalancingExpense     AccountClass = "rebalancing_expense"
	RecoveryLoss           AccountClass = "recovery_loss"
	RoundingDust           AccountClass = "rounding_dust"
	ReconciliationSuspense AccountClass = "reconciliation_suspense"
)

// AccountKey is one typed ledger account/asset/owner dimension.
type AccountKey struct {
	Class AccountClass       `json:"class"`
	Asset domain.AssetSymbol `json:"asset"`
	Owner string             `json:"owner"`
}

// Line is one positive exact commodity posting.
type Line struct {
	Account   AccountKey     `json:"account"`
	Direction Direction      `json:"direction"`
	Quantity  domain.Balance `json:"quantity"`
	Lot       string         `json:"lot,omitempty"`
	Rounding  string         `json:"rounding,omitempty"`
}

// Transaction is one immutable journal fact with causation evidence.
type Transaction struct {
	ID                domain.JournalTransactionID  `json:"id"`
	Type              string                       `json:"type"`
	RunID             domain.RunID                 `json:"run_id"`
	PortfolioID       domain.PortfolioID           `json:"portfolio_id"`
	ConfigurationHash string                       `json:"configuration_hash"`
	CausationID       domain.EventID               `json:"causation_id"`
	RecordedAt        domain.EventTime             `json:"recorded_at"`
	IngestOrdinal     uint64                       `json:"ingest_ordinal"`
	ReversalOf        *domain.JournalTransactionID `json:"reversal_of,omitempty"`
	Lines             []Line                       `json:"lines"`
}

// Projection records exact debit and credit totals without collapsing assets.
type Projection struct {
	Account AccountKey     `json:"account"`
	Debits  domain.Balance `json:"debits"`
	Credits domain.Balance `json:"credits"`
}

// Journal appends validated transactions and rebuilds projections from truth.
type Journal interface {
	Append(Transaction) error
	Transactions() []Transaction
	Rebuild() ([]Projection, string, error)
}

// MemoryJournal is the deterministic A4 journal conformance model.
type MemoryJournal struct {
	mutex        sync.Mutex
	transactions []Transaction
	identities   map[string]struct{}
	byID         map[string]Transaction
	reversals    map[string]struct{}
}

// NewMemoryJournal constructs an empty append-only journal.
func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{
		identities: make(map[string]struct{}), byID: make(map[string]Transaction), reversals: make(map[string]struct{}),
	}
}

// Append validates per-asset balance and appends a defensive immutable copy.
func (journal *MemoryJournal) Append(transaction Transaction) error {
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	if err := ValidateTransaction(transaction); err != nil {
		return err
	}
	if _, exists := journal.identities[transaction.ID.String()]; exists {
		return accountingError("duplicate_transaction")
	}
	if transaction.ReversalOf != nil {
		targetID := transaction.ReversalOf.String()
		target, exists := journal.byID[targetID]
		if !exists || targetID == transaction.ID.String() {
			return accountingError("reversal_target_invalid")
		}
		if _, exists = journal.reversals[targetID]; exists {
			return accountingError("duplicate_reversal")
		}
		if !exactReversal(target.Lines, transaction.Lines) {
			return accountingError("reversal_posting_mismatch")
		}
	}
	transaction = cloneTransaction(transaction)
	journal.transactions = append(journal.transactions, transaction)
	journal.identities[transaction.ID.String()] = struct{}{}
	journal.byID[transaction.ID.String()] = transaction
	if transaction.ReversalOf != nil {
		journal.reversals[transaction.ReversalOf.String()] = struct{}{}
	}
	return nil
}

func exactReversal(original, reversal []Line) bool {
	if len(original) != len(reversal) {
		return false
	}
	wanted := make(map[string]int, len(original))
	for _, line := range original {
		if line.Direction == Debit {
			line.Direction = Credit
		} else {
			line.Direction = Debit
		}
		encoded, _ := json.Marshal(line)
		wanted[string(encoded)]++
	}
	for _, line := range reversal {
		encoded, _ := json.Marshal(line)
		key := string(encoded)
		if wanted[key] == 0 {
			return false
		}
		wanted[key]--
	}
	return true
}

// Transactions returns defensive copies of immutable journal facts.
func (journal *MemoryJournal) Transactions() []Transaction {
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	return cloneTransactions(journal.transactions)
}

// Rebuild deterministically derives per-account debit/credit projections.
func (journal *MemoryJournal) Rebuild() ([]Projection, string, error) {
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	return rebuild(journal.transactions)
}

// ValidateTransaction proves exact independent balance for every commodity.
func ValidateTransaction(transaction Transaction) error {
	if transaction.ID.Value() == "" || transaction.Type == "" || transaction.RunID.Value() == "" ||
		transaction.PortfolioID.Value() == "" || transaction.ConfigurationHash == "" ||
		transaction.CausationID.Value() == "" || transaction.RecordedAt.Validate() != nil ||
		transaction.IngestOrdinal == 0 || len(transaction.Lines) < 2 {
		return accountingError("invalid_transaction")
	}
	totals := make(map[domain.AssetSymbol][2]domain.Balance)
	for _, line := range transaction.Lines {
		if !validLine(line) {
			return accountingError("invalid_line")
		}
		pair := totals[line.Account.Asset]
		index := 0
		if line.Direction == Credit {
			index = 1
		}
		var err error
		pair[index], err = pair[index].Add(line.Quantity)
		if err != nil {
			return accountingError("quantity_overflow")
		}
		totals[line.Account.Asset] = pair
	}
	for _, pair := range totals {
		if pair[0].Compare(pair[1]) != 0 {
			return accountingError("unbalanced_commodity")
		}
	}
	return nil
}

func validLine(line Line) bool {
	zero, _ := domain.ParseBalance("0")
	_, assetErr := domain.ParseAssetSymbol(string(line.Account.Asset))
	return validAccountClass(line.Account.Class) && assetErr == nil && line.Account.Owner != "" &&
		(line.Direction == Debit || line.Direction == Credit) && line.Quantity.Compare(zero) > 0
}

func validAccountClass(class AccountClass) bool {
	switch class {
	case ExternalEquity, AvailableAsset, ReservedAsset, StrategyInventory,
		ExchangeInventory, TradeCostProceeds, FeeExpense, SpreadAttribution,
		SlippageAttribution, LatencyAttribution, RealizedPnL, UnrealizedPnL,
		InventoryValuation, RebalancingExpense, RecoveryLoss, RoundingDust,
		ReconciliationSuspense:
		return true
	default:
		return false
	}
}

func rebuild(transactions []Transaction) ([]Projection, string, error) {
	zero, _ := domain.ParseBalance("0")
	projections := make(map[string]Projection)
	for _, transaction := range transactions {
		if err := ValidateTransaction(transaction); err != nil {
			return nil, "", err
		}
		for _, line := range transaction.Lines {
			key := accountIdentity(line.Account)
			projection, exists := projections[key]
			if !exists {
				projection = Projection{Account: line.Account, Debits: zero, Credits: zero}
			}
			var err error
			if line.Direction == Debit {
				projection.Debits, err = projection.Debits.Add(line.Quantity)
			} else {
				projection.Credits, err = projection.Credits.Add(line.Quantity)
			}
			if err != nil {
				return nil, "", accountingError("projection_overflow")
			}
			projections[key] = projection
		}
	}
	keys := make([]string, 0, len(projections))
	for key := range projections {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Projection, 0, len(keys))
	for _, key := range keys {
		result = append(result, projections[key])
	}
	encoded, _ := json.Marshal(result)
	digest := sha256.Sum256(encoded)
	return result, hex.EncodeToString(digest[:]), nil
}

func accountIdentity(account AccountKey) string {
	encoded, _ := json.Marshal(account)
	return string(encoded)
}

func cloneTransactions(source []Transaction) []Transaction {
	cloned := append([]Transaction(nil), source...)
	for index := range cloned {
		cloned[index] = cloneTransaction(cloned[index])
	}
	return cloned
}

func cloneTransaction(transaction Transaction) Transaction {
	transaction.Lines = append([]Line(nil), transaction.Lines...)
	if transaction.ReversalOf != nil {
		value := *transaction.ReversalOf
		transaction.ReversalOf = &value
	}
	return transaction
}

// Error is one stable accounting invariant failure without financial payload.
type Error struct{ Code string }

// Error returns the stable accounting failure code.
func (err Error) Error() string { return "accounting: " + err.Code }

func accountingError(code string) error { return Error{Code: code} }

var _ Journal = (*MemoryJournal)(nil)
