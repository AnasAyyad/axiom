package portfolio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"

	"axiom/internal/accounting"
	"axiom/internal/domain"
)

// V1AExchange is the only virtual exchange owner in the initial portfolio.
const V1AExchange = "binance"

// V1AStrategy is the isolated initial research strategy owner.
const V1AStrategy = "trend"

// V1ANumeraire is the only functional reporting asset in V1A.
const V1ANumeraire = "USDT"

// Ownership fixes every virtual unit to one strategy/account/exchange/portfolio.
type Ownership struct {
	PortfolioID domain.PortfolioID
	AccountID   domain.VirtualAccountID
	Strategy    string
	Exchange    string
}

// Position is one exact spot-only owned inventory projection.
type Position struct {
	Instrument          domain.Instrument
	Quantity            domain.Balance
	Cost                domain.Money
	WeightedAverageCost domain.Price
	RealizedPnL         domain.PnL
	UnrealizedPnL       domain.PnL
	Revision            uint64
}

// Snapshot is one canonical virtual portfolio state.
type Snapshot struct {
	Ownership Ownership
	Numeraire domain.AssetSymbol
	Balances  map[domain.AssetSymbol]accounting.BalanceSnapshot
	Positions []Position
	Revision  uint64
}

// Portfolio owns exact projections and delegates exclusive funds to the A4 ledger.
type Portfolio struct {
	mutex     sync.Mutex
	ownership Ownership
	numeraire domain.AssetSymbol
	ledger    *accounting.ReservationLedger
	balances  map[domain.AssetSymbol]accounting.BalanceKey
	positions map[domain.Instrument]Position
	revision  uint64
}

// InitializeV1ATrend creates exactly 500 USDT and zero BTC/ETH with journal proof.
func InitializeV1ATrend(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	if runID.Value() == "" || portfolioID.Value() == "" || accountID.Value() == "" ||
		configurationHash == "" || journal == nil || recordedAt.Validate() != nil {
		return nil, portfolioError("initialization_invalid")
	}
	portfolio := newV1APortfolio(portfolioID, accountID)
	if err := portfolio.openInitialBalances(); err != nil {
		return nil, err
	}
	if err := journal.Append(initializationTransaction(runID, portfolioID, configurationHash, recordedAt)); err != nil {
		return nil, portfolioError("initialization_journal_failed")
	}
	return portfolio, nil
}

func newV1APortfolio(portfolioID domain.PortfolioID, accountID domain.VirtualAccountID) *Portfolio {
	return &Portfolio{ownership: Ownership{PortfolioID: portfolioID, AccountID: accountID,
		Strategy: V1AStrategy, Exchange: V1AExchange}, numeraire: V1ANumeraire,
		ledger: accounting.NewReservationLedger(), balances: make(map[domain.AssetSymbol]accounting.BalanceKey),
		positions: make(map[domain.Instrument]Position), revision: 1}
}

func (portfolio *Portfolio) openInitialBalances() error {
	for _, item := range []struct{ asset, quantity string }{{"USDT", "500.00"}, {"BTC", "0"}, {"ETH", "0"}} {
		asset, _ := domain.ParseAssetSymbol(item.asset)
		quantity, _ := domain.ParseBalance(item.quantity)
		key := accounting.BalanceKey{Account: portfolio.ownership.AccountID, Asset: asset}
		if err := portfolio.ledger.OpenBalance(key, quantity); err != nil {
			return portfolioError("initialization_balance_failed")
		}
		portfolio.balances[asset] = key
	}
	return nil
}

func initializationTransaction(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	configurationHash string,
	recordedAt domain.EventTime,
) accounting.Transaction {
	id, _ := domain.NewJournalTransactionID("v1a-trend-initialization")
	cause, _ := domain.NewEventID("v1a-trend-initialization")
	asset, _ := domain.ParseAssetSymbol(V1ANumeraire)
	quantity, _ := domain.ParseBalance("500.00")
	return accounting.Transaction{ID: id, Type: "portfolio_initialization", RunID: runID,
		PortfolioID: portfolioID, ConfigurationHash: configurationHash, CausationID: cause,
		RecordedAt: recordedAt, IngestOrdinal: recordedAt.Sequence, Lines: []accounting.Line{
			{Account: accounting.AccountKey{Class: accounting.AvailableAsset, Asset: asset, Owner: V1AStrategy}, Direction: accounting.Debit, Quantity: quantity},
			{Account: accounting.AccountKey{Class: accounting.ExternalEquity, Asset: asset, Owner: V1AStrategy}, Direction: accounting.Credit, Quantity: quantity},
		}}
}

// Snapshot returns exact ownership, balances, and sorted positions.
func (portfolio *Portfolio) Snapshot() Snapshot {
	portfolio.mutex.Lock()
	defer portfolio.mutex.Unlock()
	balances := make(map[domain.AssetSymbol]accounting.BalanceSnapshot, len(portfolio.balances))
	for asset, key := range portfolio.balances {
		balances[asset], _ = portfolio.ledger.Balance(key)
	}
	positions := make([]Position, 0, len(portfolio.positions))
	for _, position := range portfolio.positions {
		positions = append(positions, position)
	}
	sort.Slice(positions, func(left, right int) bool {
		leftKey := string(positions[left].Instrument.Base) + "/" + string(positions[left].Instrument.Quote)
		rightKey := string(positions[right].Instrument.Base) + "/" + string(positions[right].Instrument.Quote)
		return leftKey < rightKey
	})
	return Snapshot{Ownership: portfolio.ownership, Numeraire: portfolio.numeraire,
		Balances: balances, Positions: positions, Revision: portfolio.revision}
}

// CanonicalHash returns the exact restart-comparison hash.
func (snapshot Snapshot) CanonicalHash() string {
	encoded, _ := json.Marshal(snapshot)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Ledger returns the owned exclusive reservation foundation.
func (portfolio *Portfolio) Ledger() *accounting.ReservationLedger { return portfolio.ledger }

// BalanceKey returns the explicit owned balance dimension.
func (portfolio *Portfolio) BalanceKey(asset domain.AssetSymbol) (accounting.BalanceKey, bool) {
	portfolio.mutex.Lock()
	defer portfolio.mutex.Unlock()
	key, exists := portfolio.balances[asset]
	return key, exists
}
