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

// V1BMeanReversionStrategy is the isolated B3 research strategy owner.
const V1BMeanReversionStrategy = "mean_reversion"

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

// InitializeV1ATrend creates the locked V1A baseline of 500 USDT and zero BTC/ETH.
func InitializeV1ATrend(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	capital, _ := domain.ParseBalance("500.00")
	return InitializeTrend(runID, portfolioID, accountID, configurationHash, capital, journal, recordedAt)
}

// InitializeTrend creates an exact configured virtual USDT balance and zero
// BTC/ETH inventory with immutable journal proof.
func InitializeTrend(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	startingCapital domain.Balance,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	return initializeStrategy(runID, portfolioID, accountID, configurationHash, startingCapital,
		V1AStrategy, journal, recordedAt)
}

// InitializeMeanReversion creates an isolated B3 portfolio whose inventory,
// reservations, journal lines, and fills cannot be attributed to Trend.
func InitializeMeanReversion(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	startingCapital domain.Balance,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	return initializeStrategy(runID, portfolioID, accountID, configurationHash, startingCapital,
		V1BMeanReversionStrategy, journal, recordedAt)
}

func initializeStrategy(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	startingCapital domain.Balance,
	strategy string,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	zero, _ := domain.ParseBalance("0")
	if runID.Value() == "" || portfolioID.Value() == "" || accountID.Value() == "" ||
		configurationHash == "" || startingCapital.Compare(zero) <= 0 || !supportedStrategy(strategy) ||
		journal == nil || recordedAt.Validate() != nil {
		return nil, portfolioError("initialization_invalid")
	}
	portfolio := newPortfolio(portfolioID, accountID, strategy)
	if err := portfolio.openInitialBalances(startingCapital); err != nil {
		return nil, err
	}
	if err := journal.Append(initializationTransaction(runID, portfolioID, configurationHash, startingCapital, strategy, recordedAt)); err != nil {
		return nil, portfolioError("initialization_journal_failed")
	}
	return portfolio, nil
}

func newPortfolio(portfolioID domain.PortfolioID, accountID domain.VirtualAccountID, strategy string) *Portfolio {
	return &Portfolio{ownership: Ownership{PortfolioID: portfolioID, AccountID: accountID,
		Strategy: strategy, Exchange: V1AExchange}, numeraire: V1ANumeraire,
		ledger: accounting.NewReservationLedger(), balances: make(map[domain.AssetSymbol]accounting.BalanceKey),
		positions: make(map[domain.Instrument]Position), revision: 1}
}

func (portfolio *Portfolio) openInitialBalances(startingCapital domain.Balance) error {
	for _, item := range []struct{ asset, quantity string }{{"USDT", startingCapital.String()}, {"BTC", "0"}, {"ETH", "0"}} {
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
	startingCapital domain.Balance,
	strategy string,
	recordedAt domain.EventTime,
) accounting.Transaction {
	prefix := "v1a-trend"
	if strategy == V1BMeanReversionStrategy {
		prefix = "v1b-mean-reversion"
	}
	id, _ := domain.NewJournalTransactionID(prefix + "-initialization")
	cause, _ := domain.NewEventID(prefix + "-initialization")
	asset, _ := domain.ParseAssetSymbol(V1ANumeraire)
	return accounting.Transaction{ID: id, Type: "portfolio_initialization", RunID: runID,
		PortfolioID: portfolioID, ConfigurationHash: configurationHash, CausationID: cause,
		RecordedAt: recordedAt, IngestOrdinal: recordedAt.Sequence, Lines: []accounting.Line{
			{Account: accounting.AccountKey{Class: accounting.AvailableAsset, Asset: asset, Owner: strategy}, Direction: accounting.Debit, Quantity: startingCapital},
			{Account: accounting.AccountKey{Class: accounting.ExternalEquity, Asset: asset, Owner: strategy}, Direction: accounting.Credit, Quantity: startingCapital},
		}}
}

func supportedStrategy(strategy string) bool {
	return strategy == V1AStrategy || strategy == V1BMeanReversionStrategy
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
