package sandbox

import (
	"sort"
	"time"
)

// StrategySessionState is the durable lifecycle of an automatic, armed
// sandbox strategy session. It never denotes a production execution mode.
type StrategySessionState string

const (
	StrategySessionPrepared StrategySessionState = "prepared"
	StrategySessionRunning  StrategySessionState = "running"
	StrategySessionBlocked  StrategySessionState = "blocked"
	StrategySessionStopped  StrategySessionState = "stopped"
)

// StrategySessionAccount binds an immutable current account epoch to one
// strategy worker. Credentials remain owned by the matching exchange engine.
type StrategySessionAccount struct {
	ID       AccountID
	Epoch    uint64
	Exchange Exchange
}

// StrategySessionCommand requests only a prepared parent context. Account
// selection, engine health, lease, and reconciliation checks remain storage
// responsibilities; this command cannot carry executable order values.
type StrategySessionCommand struct {
	ID              SessionID
	Strategy        string
	Exchanges       []Exchange
	Instrument      string
	ConfigurationID string
	StrategySetHash string
	CreatedBy       string
	CreatedAt       time.Time
}

// Validate checks the closed topology requested before storage resolves exact
// current account epochs. Inventory rebalancing remains advisory-only.
func (command StrategySessionCommand) Validate() error {
	if command.ID == "" || command.ConfigurationID == "" || command.CreatedBy == "" ||
		command.CreatedAt.IsZero() || command.CreatedAt.Location() != time.UTC ||
		(command.Instrument != "BTCUSDT" && command.Instrument != "ETHUSDT") ||
		len(command.StrategySetHash) != 64 {
		return contractError("strategy_session_command_invalid")
	}
	exchanges := append([]Exchange(nil), command.Exchanges...)
	sort.Slice(exchanges, func(left, right int) bool { return exchanges[left] < exchanges[right] })
	for index, exchange := range exchanges {
		if (exchange != ExchangeBinance && exchange != ExchangeBybit) ||
			(index > 0 && exchanges[index-1] == exchange) {
			return contractError("strategy_session_command_invalid")
		}
	}
	if (command.Strategy == StrategyCrossExchangeArbitrage && len(exchanges) == 2) ||
		((command.Strategy == StrategyTrend || command.Strategy == StrategyMeanReversion || command.Strategy == StrategyTriangular) && len(exchanges) == 1) {
		return nil
	}
	return contractError("strategy_session_command_invalid")
}

// StrategySession is the strategy-owned control state. Admission still
// requires central risk, reservation, dispatcher, and reconciliation checks.
type StrategySession struct {
	ID        SessionID
	Strategy  string
	Accounts  []StrategySessionAccount
	State     StrategySessionState
	CreatedAt time.Time
}

// ValidateStart permits only automatic spot strategies after the owner has
// armed every exact account. Inventory rebalancing is intentionally absent:
// it is advisory and cannot submit transfers or exchange orders.
func (session StrategySession) ValidateStart(arm Arm, now time.Time) error {
	if !validStrategySession(session) || !arm.Active(now) || arm.SessionID != session.ID {
		return contractError("strategy_session_start_rejected")
	}
	armed := make(map[AccountID]struct{}, len(arm.AccountIDs))
	for _, account := range arm.AccountIDs {
		armed[account] = struct{}{}
	}
	for _, account := range session.Accounts {
		if _, exists := armed[account.ID]; !exists {
			return contractError("strategy_session_start_rejected")
		}
	}
	return nil
}

// Start changes only a prepared session to running after arm validation.
func (session StrategySession) Start(arm Arm, now time.Time) (StrategySession, error) {
	if session.State != StrategySessionPrepared || session.ValidateStart(arm, now) != nil {
		return StrategySession{}, contractError("strategy_session_start_rejected")
	}
	session.State = StrategySessionRunning
	return session, nil
}

// Stop is always available for a valid session and prevents new strategy entries.
func (session StrategySession) Stop() (StrategySession, error) {
	if !validStrategySession(session) || (session.State != StrategySessionPrepared && session.State != StrategySessionRunning && session.State != StrategySessionBlocked) {
		return StrategySession{}, contractError("strategy_session_stop_rejected")
	}
	session.State = StrategySessionStopped
	return session, nil
}

func validStrategySession(session StrategySession) bool {
	if session.ID == "" || session.CreatedAt.IsZero() || session.CreatedAt.Location() != time.UTC ||
		(session.State != StrategySessionPrepared && session.State != StrategySessionRunning && session.State != StrategySessionBlocked && session.State != StrategySessionStopped) {
		return false
	}
	accounts := append([]StrategySessionAccount(nil), session.Accounts...)
	sort.Slice(accounts, func(left, right int) bool { return accounts[left].ID < accounts[right].ID })
	for index, account := range accounts {
		if account.ID == "" || account.Epoch == 0 || (account.Exchange != ExchangeBinance && account.Exchange != ExchangeBybit) ||
			(index > 0 && accounts[index-1].ID == account.ID) {
			return false
		}
	}
	switch session.Strategy {
	case StrategyTrend, StrategyMeanReversion, StrategyTriangular:
		return len(accounts) == 1
	case StrategyCrossExchangeArbitrage:
		return len(accounts) == 2 && accounts[0].Exchange != accounts[1].Exchange
	default:
		return false
	}
}
