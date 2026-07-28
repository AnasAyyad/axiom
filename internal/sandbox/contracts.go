package sandbox

import (
	"context"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
)

// Exchange is one closed V1C authenticated sandbox venue.
type Exchange string

// Environment is one closed virtual external-account environment.
type Environment string

// Closed V1C exchange and environment values.
const (
	ExchangeBinance Exchange = "binance"
	ExchangeBybit   Exchange = "bybit"

	EnvironmentBinanceSpotTestnet Environment = "spot_testnet"
	EnvironmentBybitDemo          Environment = "demo"
)

// AccountID is the non-secret stable local identity of one exchange account.
type AccountID string

// SessionID is one sandbox execution-session identity.
type SessionID string

// OrderStyle is one exact V1C spot limit style.
type OrderStyle string

// V1C supports only these exact limit-order styles.
const (
	OrderStyleLimitGTC OrderStyle = "LIMIT_GTC"
	OrderStyleLimitIOC OrderStyle = "LIMIT_IOC"
	OrderStylePostOnly OrderStyle = "POST_ONLY"
)

// IntentAction is the central-risk action attached to a durable order.
type IntentAction string

// Closed strategy identifiers accepted by the V1C durable dispatcher.
const (
	StrategyTrend                  = "trend"
	StrategyMeanReversion          = "mean-reversion"
	StrategyTriangular             = "triangular"
	StrategyCrossExchangeArbitrage = "cross-exchange-arbitrage"
	StrategySandboxCanary          = "sandbox-canary"
)

// Intent actions preserve cancellation and bounded recovery while locked.
const (
	IntentEntry    IntentAction = "ENTRY"
	IntentExit     IntentAction = "EXIT"
	IntentCancel   IntentAction = "CANCEL"
	IntentRecovery IntentAction = "RECOVERY"
)

// CapabilityDescriptor is the normalized authenticated-account capability
// proof obtained at startup.
type CapabilityDescriptor struct {
	Exchange              Exchange     `json:"exchange"`
	Environment           Environment  `json:"environment"`
	SpotOnly              bool         `json:"spot_only"`
	ReadAccount           bool         `json:"read_account"`
	WriteSpotOrders       bool         `json:"write_spot_orders"`
	RESTOrderEntry        bool         `json:"rest_order_entry"`
	PrivateEvents         bool         `json:"private_events"`
	HMACSHA256            bool         `json:"hmac_sha256"`
	OrderStyles           []OrderStyle `json:"order_styles"`
	ObservedAt            time.Time    `json:"observed_at"`
	CapabilityHash        string       `json:"capability_hash"`
	ProhibitedPermissions []string     `json:"prohibited_permissions"`
}

// AccountIdentity is a redacted startup account and key-generation proof.
type AccountIdentity struct {
	AccountID            AccountID   `json:"account_id"`
	Exchange             Exchange    `json:"exchange"`
	Environment          Environment `json:"environment"`
	AccountIdentityHash  string      `json:"account_identity_hash"`
	KeyFingerprint       string      `json:"key_fingerprint"`
	CredentialGeneration uint64      `json:"credential_generation"`
	OwnerAttested        bool        `json:"owner_attested"`
	ValidatedAt          time.Time   `json:"validated_at"`
}

// Balance is one authoritative exchange-account asset balance.
type Balance struct {
	Asset     domain.AssetSymbol `json:"asset"`
	Available domain.Balance     `json:"available"`
	Reserved  domain.Balance     `json:"reserved"`
}

// AccountSnapshot is one immutable authoritative exchange-account view.
type AccountSnapshot struct {
	AccountID    AccountID `json:"account_id"`
	Epoch        uint64    `json:"epoch"`
	Balances     []Balance `json:"balances"`
	OrdersHash   string    `json:"orders_hash"`
	FillsHash    string    `json:"fills_hash"`
	SnapshotHash string    `json:"snapshot_hash"`
	ObservedAt   time.Time `json:"observed_at"`
}

// Submission is the only adapter-neutral durable sandbox order request.
type Submission struct {
	PlanID        domain.ExecutionPlanID `json:"plan_id"`
	OrderID       domain.VirtualOrderID  `json:"order_id"`
	AccountID     AccountID              `json:"account_id"`
	AccountEpoch  uint64                 `json:"account_epoch"`
	ClientOrderID string                 `json:"client_order_id"`
	StrategyID    domain.StrategyID      `json:"strategy_id"`
	Instrument    domain.Instrument      `json:"instrument"`
	Side          domain.Side            `json:"side"`
	Quantity      domain.Quantity        `json:"quantity"`
	LimitPrice    domain.Price           `json:"limit_price"`
	Notional      domain.Notional        `json:"notional"`
	Style         OrderStyle             `json:"style"`
	Action        IntentAction           `json:"action"`
	RequestHash   string                 `json:"request_hash"`
	PolicyHash    string                 `json:"policy_hash"`
	ApprovedAt    time.Time              `json:"approved_at"`
}

// PrivateEventKind is one normalized durable private-account event.
type PrivateEventKind string

// Closed normalized private-event kinds.
const (
	PrivateOrderEvent   PrivateEventKind = "order"
	PrivateFillEvent    PrivateEventKind = "fill"
	PrivateBalanceEvent PrivateEventKind = "balance"
)

// PrivateEvent is the redacted normalized input to the canonical reducer.
type PrivateEvent struct {
	Identity        string                `json:"identity"`
	AccountID       AccountID             `json:"account_id"`
	AccountEpoch    uint64                `json:"account_epoch"`
	Kind            PrivateEventKind      `json:"kind"`
	OrderID         domain.VirtualOrderID `json:"order_id"`
	ClientOrderID   string                `json:"client_order_id"`
	NativeOrderHash string                `json:"native_order_hash"`
	NativeFillHash  string                `json:"native_fill_hash,omitempty"`
	OrderEvent      *execution.OrderEvent `json:"order_event,omitempty"`
	BalanceHash     string                `json:"balance_hash,omitempty"`
	OccurredAt      time.Time             `json:"occurred_at"`
	ReceivedAt      time.Time             `json:"received_at"`
}

// Arm is one bounded manual submission authorization state.
type Arm struct {
	ID                string      `json:"id"`
	SessionID         SessionID   `json:"session_id"`
	AccountIDs        []AccountID `json:"account_ids"`
	AuthorizationHash string      `json:"authorization_hash"`
	ActorUserID       string      `json:"actor_user_id"`
	ActorSessionID    string      `json:"actor_session_id"`
	ReasonHash        string      `json:"reason_hash"`
	CreatedAt         time.Time   `json:"created_at"`
	ExpiresAt         time.Time   `json:"expires_at"`
	RevokedAt         *time.Time  `json:"revoked_at,omitempty"`
	Revision          uint64      `json:"revision"`
}

// AuditEvent is one high-risk, hash-linked, redacted control fact.
type AuditEvent struct {
	ID             string    `json:"id"`
	ActorUserID    string    `json:"actor_user_id"`
	ActorSessionID string    `json:"actor_session_id"`
	Purpose        string    `json:"purpose"`
	SourceHash     string    `json:"source_hash"`
	ReasonHash     string    `json:"reason_hash"`
	Revision       uint64    `json:"revision"`
	BeforeHash     string    `json:"before_hash"`
	AfterHash      string    `json:"after_hash"`
	Result         string    `json:"result"`
	PreviousHash   string    `json:"previous_hash"`
	EventHash      string    `json:"event_hash"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// Difference is one classified exchange-authoritative reconciliation fact.
type Difference struct {
	Category       string             `json:"category"`
	Classification string             `json:"classification"`
	ExpectedHash   string             `json:"expected_hash"`
	ActualHash     string             `json:"actual_hash"`
	Asset          domain.AssetSymbol `json:"asset,omitempty"`
	Quantity       domain.Balance     `json:"quantity,omitempty"`
	Critical       bool               `json:"critical"`
}

// ReconciliationResult is one immutable account-epoch reconciliation outcome.
type ReconciliationResult struct {
	ID           string       `json:"id"`
	AccountID    AccountID    `json:"account_id"`
	AccountEpoch uint64       `json:"account_epoch"`
	State        string       `json:"state"`
	Differences  []Difference `json:"differences"`
	EvidenceHash string       `json:"evidence_hash"`
	ReconciledAt time.Time    `json:"reconciled_at"`
}

// AccountReader owns authenticated account snapshots for one exact account.
type AccountReader interface {
	Capabilities(context.Context) (CapabilityDescriptor, error)
	Identity(context.Context) (AccountIdentity, error)
	Snapshot(context.Context) (AccountSnapshot, error)
}

// OrderBroker owns only testnet/demo submission, cancellation, and query.
type OrderBroker interface {
	Submit(context.Context, Submission) (PrivateEvent, error)
	Cancel(context.Context, AccountID, uint64, string) (PrivateEvent, error)
	Query(context.Context, AccountID, uint64, string) ([]PrivateEvent, error)
}

// PrivateEventSource receives normalized account facts without exposing DTOs.
type PrivateEventSource interface {
	Receive(context.Context) (PrivateEvent, error)
	Close() error
}

// Reconciler loads authoritative account facts after uncertainty or restart.
type Reconciler interface {
	Reconcile(context.Context, AccountID, uint64) (ReconciliationResult, error)
}

// EligibilitySnapshot is the single public book-and-clock admission boundary.
type EligibilitySnapshot = exchangecontracts.CollectorHealthSnapshot
