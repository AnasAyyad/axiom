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

// TrendExchange is the only virtual exchange owner in the initial portfolio.
const TrendExchange = "binance"

// TrendStrategy is the isolated initial research strategy owner.
const TrendStrategy = "trend"

// MultiStrategyResearchMeanReversionStrategy is the isolated mean reversion research strategy owner.
const MultiStrategyResearchMeanReversionStrategy = "mean_reversion"

// TriangularStrategyOwner is the isolated single-exchange cycle owner.
const TriangularStrategyOwner = "triangular"

// TrendNumeraire is the only functional reporting asset in initial trend.
const TrendNumeraire = "USDT"

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

// Portfolio owns exact projections and delegates exclusive funds to the durable storage ledger.
type Portfolio struct {
	mutex     sync.Mutex
	ownership Ownership
	numeraire domain.AssetSymbol
	ledger    *accounting.ReservationLedger
	balances  map[domain.AssetSymbol]accounting.BalanceKey
	positions map[domain.Instrument]Position
	revision  uint64
}

// AccountBalance is one free, authoritative asset amount copied from an
// immutable account snapshot. It deliberately excludes externally reserved
// funds: callers must reject or account for those before creating an
// allocation projection.
type AccountBalance struct {
	Asset     domain.AssetSymbol
	Available domain.Balance
}

// NewAccountBalancePortfolio constructs an isolated decision-time ownership
// projection from already-validated free account balances. It creates no
// accounting journal entries and must not be used as a substitute for durable
// fill accounting or reconciliation; those remain authoritative elsewhere.
func NewAccountBalancePortfolio(
	ownership Ownership,
	numeraire domain.AssetSymbol,
	balances []AccountBalance,
) (*Portfolio, error) {
	if ownership.PortfolioID.Value() == "" || ownership.AccountID.Value() == "" ||
		ownership.Strategy == "" || ownership.Exchange == "" || len(balances) == 0 {
		return nil, portfolioError("account_balance_portfolio_invalid")
	}
	if _, err := domain.ParseAssetSymbol(string(numeraire)); err != nil {
		return nil, portfolioError("account_balance_portfolio_invalid")
	}
	portfolio := newOwnedPortfolio(ownership, numeraire)
	hasNumeraire := false
	for _, balance := range balances {
		if _, err := domain.ParseAssetSymbol(string(balance.Asset)); err != nil ||
			portfolio.balances[balance.Asset].Asset != "" {
			return nil, portfolioError("account_balance_portfolio_invalid")
		}
		key := accounting.BalanceKey{Account: ownership.AccountID, Asset: balance.Asset}
		if err := portfolio.ledger.OpenBalance(key, balance.Available); err != nil {
			return nil, portfolioError("account_balance_portfolio_invalid")
		}
		portfolio.balances[balance.Asset] = key
		hasNumeraire = hasNumeraire || balance.Asset == numeraire
	}
	if !hasNumeraire {
		return nil, portfolioError("account_balance_portfolio_invalid")
	}
	return portfolio, nil
}

// InitializeDefaultTrend creates the locked 500 USDT baseline with zero BTC/ETH.
func InitializeDefaultTrend(
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
		TrendStrategy, journal, recordedAt)
}

// InitializeMeanReversion creates an isolated mean reversion portfolio whose inventory,
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
		MultiStrategyResearchMeanReversionStrategy, journal, recordedAt)
}

// InitializeTriangular creates one isolated triangular portfolio at an explicit
// production-public venue. Independent triangular experiments begin with
// settlement capital only; BTC and ETH remain zero until a simulated cycle.
func InitializeTriangular(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	startingCapital domain.Balance,
	exchange string,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	return initializeStrategyAtExchange(runID, portfolioID, accountID, configurationHash, startingCapital,
		TriangularStrategyOwner, exchange, journal, recordedAt)
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
	return initializeStrategyAtExchange(runID, portfolioID, accountID, configurationHash, startingCapital,
		strategy, TrendExchange, journal, recordedAt)
}

func initializeStrategyAtExchange(
	runID domain.RunID,
	portfolioID domain.PortfolioID,
	accountID domain.VirtualAccountID,
	configurationHash string,
	startingCapital domain.Balance,
	strategy string,
	exchange string,
	journal accounting.Journal,
	recordedAt domain.EventTime,
) (*Portfolio, error) {
	zero, _ := domain.ParseBalance("0")
	if runID.Value() == "" || portfolioID.Value() == "" || accountID.Value() == "" ||
		configurationHash == "" || startingCapital.Compare(zero) <= 0 || !supportedStrategy(strategy) ||
		!supportedExchange(exchange) || journal == nil || recordedAt.Validate() != nil {
		return nil, portfolioError("initialization_invalid")
	}
	portfolio := newPortfolioAtExchange(portfolioID, accountID, strategy, exchange)
	if err := portfolio.openInitialBalances(startingCapital); err != nil {
		return nil, err
	}
	if err := journal.Append(initializationTransaction(runID, portfolioID, configurationHash, startingCapital, strategy, recordedAt)); err != nil {
		return nil, portfolioError("initialization_journal_failed")
	}
	return portfolio, nil
}

func newPortfolioAtExchange(portfolioID domain.PortfolioID, accountID domain.VirtualAccountID,
	strategy, exchange string) *Portfolio {
	return newOwnedPortfolio(Ownership{PortfolioID: portfolioID, AccountID: accountID,
		Strategy: strategy, Exchange: exchange}, TrendNumeraire)
}

func newOwnedPortfolio(ownership Ownership, numeraire domain.AssetSymbol) *Portfolio {
	return &Portfolio{ownership: ownership, numeraire: numeraire,
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
	prefix := "trend-following"
	switch strategy {
	case MultiStrategyResearchMeanReversionStrategy:
		prefix = "mean-reversion"
	case TriangularStrategyOwner:
		prefix = "triangular-arbitrage"
	}
	id, _ := domain.NewJournalTransactionID(prefix + "-initialization")
	cause, _ := domain.NewEventID(prefix + "-initialization")
	asset, _ := domain.ParseAssetSymbol(TrendNumeraire)
	return accounting.Transaction{ID: id, Type: "portfolio_initialization", RunID: runID,
		PortfolioID: portfolioID, ConfigurationHash: configurationHash, CausationID: cause,
		RecordedAt: recordedAt, IngestOrdinal: recordedAt.Sequence, Lines: []accounting.Line{
			{Account: accounting.AccountKey{Class: accounting.AvailableAsset, Asset: asset, Owner: strategy}, Direction: accounting.Debit, Quantity: startingCapital},
			{Account: accounting.AccountKey{Class: accounting.ExternalEquity, Asset: asset, Owner: strategy}, Direction: accounting.Credit, Quantity: startingCapital},
		}}
}

func supportedStrategy(strategy string) bool {
	return strategy == TrendStrategy || strategy == MultiStrategyResearchMeanReversionStrategy || strategy == TriangularStrategyOwner
}

func supportedExchange(exchange string) bool { return exchange == "binance" || exchange == "bybit" }

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
