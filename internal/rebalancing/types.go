package rebalancing

import (
	"time"

	"axiom/internal/domain"
)

// EdgeKind identifies one reviewed advisory graph fact.
type EdgeKind string

// inventory rebalancing graph edges are either executable-depth trade facts or advisory transfer facts.
const (
	TradeEdge    EdgeKind = "trade"
	TransferEdge EdgeKind = "transfer"
)

// RecommendationMethod identifies why one advisory route was selected.
type RecommendationMethod string

// inventory rebalancing prefers an eligible natural reverse plan over a reviewed transfer route.
const (
	NaturalReverseMethod RecommendationMethod = "natural_reverse_arbitrage"
	GraphRouteMethod     RecommendationMethod = "reviewed_graph_route"
)

// Node is one canonical asset owned at one exchange.
type Node struct {
	Exchange string
	Asset    domain.AssetSymbol
}

// Approval is the immutable human review attached to a route fact.
type Approval struct {
	Approved   bool
	Actor      string
	Reference  string
	ApprovedAt time.Time
}

// Provenance is the immutable source, observation, expiry, confidence, and
// approval evidence attached to one route fact.
type Provenance struct {
	Source     string
	Observer   string
	ObservedAt time.Time
	ExpiresAt  time.Time
	Confidence domain.Percent
	Approval   Approval
	Hash       string
}

// CostBreakdown preserves every exact fixed-point route cost independently.
type CostBreakdown struct {
	Fee             domain.Money
	Spread          domain.Money
	Depth           domain.Money
	Delay           domain.Money
	NetworkFee      domain.Money
	Compatibility   domain.Money
	VolatilityRisk  domain.Money
	OperationalRisk domain.Money
}

// Edge is one immutable reviewed trade or advisory transfer fact.
type Edge struct {
	ID                  string
	Version             uint64
	Kind                EdgeKind
	From                Node
	To                  Node
	Instrument          string
	Network             string
	SourceChain         string
	DestinationChain    string
	MinimumQuantity     domain.Balance
	Available           bool
	WithdrawalAvailable bool
	DepositAvailable    bool
	Compatible          bool
	Ambiguous           bool
	Costs               CostBreakdown
	MinimumDuration     time.Duration
	MaximumDuration     time.Duration
	RiskScore           domain.Percent
	Warnings            []string
	ManualChecklist     []string
	Provenance          Provenance
}

// NaturalReversalPlan binds a cross-exchange arbitrage inventory imbalance to two reviewed trade
// facts that restore venue distribution without a transfer.
type NaturalReversalPlan struct {
	ID                               string
	CrossExchangeArbitrageDecisionID string
	Source                           Node
	Destination                      Node
	SellFactID                       string
	BuyFactID                        string
}

// Request is one immutable advisory route evaluation.
type Request struct {
	ID                string
	Source            Node
	Destination       Node
	Quantity          domain.Balance
	DecisionTime      time.Time
	Configuration     Configuration
	ConfigurationHash string
	FactSetHash       string
	NaturalReversals  []NaturalReversalPlan
}

// Step is one ordered immutable fact used by a recommendation.
type Step struct {
	Index uint32
	Role  string
	Fact  Edge
}

// Recommendation is the complete inventory rebalancing read-only route evidence.
type Recommendation struct {
	ID                string
	RequestID         string
	Method            RecommendationMethod
	Source            Node
	Destination       Node
	Quantity          domain.Balance
	Steps             []Step
	Costs             CostBreakdown
	TotalCost         domain.Money
	MinimumDuration   time.Duration
	MaximumDuration   time.Duration
	RiskScore         domain.Percent
	Warnings          []string
	ManualChecklist   []string
	ConfigurationHash string
	FactSetHash       string
	CanonicalHash     string
	RecordedAt        time.Time
	AdvisoryOnly      bool
}

// Diagnostics reports bounded deterministic route-search evidence.
type Diagnostics struct {
	ReviewedFacts  uint32
	EligibleFacts  uint32
	RejectedFacts  uint32
	CandidatePaths uint32
}
