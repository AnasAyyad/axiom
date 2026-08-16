package bybit

import (
	"context"
	"errors"
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

// ErrDemoStartupIdentity reports a fail-closed Demo identity or adapter
// bootstrap failure.
var ErrDemoStartupIdentity = errors.New("bybit_demo_startup_identity_rejected")

const demoHistoryPageLimit = 100

// SandboxAdapter is the complete adapter-neutral Bybit Demo authenticated
// boundary. All authenticated traffic remains fixed to Demo.
type SandboxAdapter struct {
	client       *SandboxClient
	identity     sandbox.AccountIdentity
	epoch        uint64
	lookup       sandbox.SubmissionLookup
	expectations sandbox.SnapshotExpectationReader
	marketData   exchangecontracts.MarketDataSource
	strategyData sandbox.StrategyMarketData
	rules        map[string]DemoInstrumentRules
	rateBudget   *demoRateBudget
}

var (
	_ sandbox.AccountReader = (*SandboxAdapter)(nil)
	_ sandbox.OrderBroker   = (*SandboxAdapter)(nil)
	_ sandbox.Reconciler    = (*SandboxAdapter)(nil)
)

// NewSandboxAdapter loads all approved Demo Spot instrument rules before it
// exposes the order-capable neutral boundary.
func NewSandboxAdapter(
	ctx context.Context,
	client *SandboxClient,
	identity sandbox.AccountIdentity,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	expectations sandbox.SnapshotExpectationReader,
) (*SandboxAdapter, error) {
	if client == nil || identity.Validate() != nil ||
		identity.Exchange != sandbox.ExchangeBybit ||
		identity.Environment != sandbox.EnvironmentBybitDemo ||
		epoch == 0 || lookup == nil || expectations == nil {
		return nil, ErrDemoStartupIdentity
	}
	rules := make(map[string]DemoInstrumentRules, 3)
	for _, instrument := range approvedInstruments() {
		loaded, err := client.loadDemoRules(ctx, instrument)
		if err != nil {
			return nil, err
		}
		rules[instrument.Symbol()] = loaded
	}
	marketData, err := NewPublicClient(publicEndpointSet, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	// The Demo engine has no direct external network. Reuse the fixed,
	// credential-free public proxy transport already owned by the Demo client.
	marketData.httpClient = client.publicDoer
	adapter, err := newSandboxAdapterForTestWithMarketData(
		client,
		identity,
		epoch,
		lookup,
		expectations,
		rules,
		marketData,
	)
	if err != nil {
		return nil, err
	}
	adapter.strategyData = marketData
	return adapter, nil
}

// StrategyMarketData exposes only credential-free Bybit public data to a
// future Demo strategy worker.
func (adapter *SandboxAdapter) StrategyMarketData() (sandbox.StrategyMarketData, error) {
	if adapter == nil || adapter.strategyData == nil {
		return nil, ErrDemoRequest
	}
	return adapter.strategyData, nil
}

// StrategyInstrumentRules returns a defensive, credential-free copy of the
// exact Demo filters loaded during adapter startup. It exposes no client,
// account identity, endpoint override, signer, or order capability.
func (adapter *SandboxAdapter) StrategyInstrumentRules() ([]DemoInstrumentRules, error) {
	if adapter == nil || len(adapter.rules) != len(approvedInstruments()) {
		return nil, ErrDemoRequest
	}
	result := make([]DemoInstrumentRules, 0, len(adapter.rules))
	for _, rule := range adapter.rules {
		if rule.validate() != nil {
			return nil, ErrDemoRequest
		}
		result = append(result, rule)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Instrument.Symbol() < result[right].Instrument.Symbol()
	})
	return result, nil
}

func newSandboxAdapterForTest(
	client *SandboxClient,
	identity sandbox.AccountIdentity,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	expectations sandbox.SnapshotExpectationReader,
	rules map[string]DemoInstrumentRules,
) (*SandboxAdapter, error) {
	return newSandboxAdapterForTestWithMarketData(
		client, identity, epoch, lookup, expectations, rules, nil,
	)
}

func newSandboxAdapterForTestWithMarketData(
	client *SandboxClient,
	identity sandbox.AccountIdentity,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	expectations sandbox.SnapshotExpectationReader,
	rules map[string]DemoInstrumentRules,
	marketData exchangecontracts.MarketDataSource,
) (*SandboxAdapter, error) {
	if client == nil || identity.Validate() != nil || epoch == 0 ||
		lookup == nil || expectations == nil || len(rules) != 3 {
		return nil, ErrDemoStartupIdentity
	}
	for _, instrument := range approvedInstruments() {
		rule, exists := rules[instrument.Symbol()]
		if !exists || rule.Instrument != instrument || rule.validate() != nil {
			return nil, ErrDemoStartupIdentity
		}
	}
	budget, err := newDemoRateBudget(120, 20, 10*time.Second)
	if err != nil {
		return nil, ErrDemoStartupIdentity
	}
	return &SandboxAdapter{
		client: client, identity: identity, epoch: epoch, lookup: lookup,
		expectations: expectations, marketData: marketData, rules: rules,
		rateBudget: budget,
	}, nil
}

// Capabilities returns the closed Demo Spot capability descriptor.
func (adapter *SandboxAdapter) Capabilities(
	context.Context,
) (sandbox.CapabilityDescriptor, error) {
	descriptor := sandbox.CapabilityDescriptor{
		Exchange:    sandbox.ExchangeBybit,
		Environment: sandbox.EnvironmentBybitDemo,
		SpotOnly:    true, ReadAccount: true, WriteSpotOrders: true,
		RESTOrderEntry: true, PrivateEvents: true, HMACSHA256: true,
		OrderStyles: []sandbox.OrderStyle{
			sandbox.OrderStyleLimitGTC,
			sandbox.OrderStyleLimitIOC,
			sandbox.OrderStylePostOnly,
		},
		ObservedAt: adapter.client.now().UTC(),
	}
	descriptor.CapabilityHash = canonicalDemoHash([]string{
		"bybit", "demo", "spot", "rest_order_entry", "hmac_sha256",
		"LIMIT_GTC", "LIMIT_IOC", "POST_ONLY",
		"order.spot", "execution.spot", "wallet",
	})
	if descriptor.Validate() != nil {
		return sandbox.CapabilityDescriptor{}, ErrDemoRequest
	}
	return descriptor, nil
}

// Identity returns the startup-validated Demo account identity.
func (adapter *SandboxAdapter) Identity(
	context.Context,
) (sandbox.AccountIdentity, error) {
	return adapter.identity, nil
}

// Submit creates one validated Demo Spot order through REST.
func (adapter *SandboxAdapter) Submit(
	ctx context.Context,
	submission sandbox.Submission,
) (sandbox.PrivateEvent, error) {
	maximum, _ := domain.ParseNotional("10")
	if submission.Validate(maximum) != nil ||
		submission.AccountID != adapter.identity.AccountID ||
		submission.AccountEpoch != adapter.epoch {
		return sandbox.PrivateEvent{}, ErrDemoRequest
	}
	rejection, err := adapter.checkDemoSubmission(ctx, submission)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	if rejection != "" {
		return rejectedDemoEvent(
			submission, adapter.client.now().UTC(), rejection,
		), nil
	}
	body, err := adapter.client.create(ctx, submission)
	if errors.Is(err, ErrDemoRejected) {
		return rejectedDemoEvent(
			submission, adapter.client.now().UTC(), "create_rejected",
		), nil
	}
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	event, err := normalizeDemoAcknowledgement(
		body, submission, execution.OrderAcknowledged,
		adapter.client.now().UTC(),
	)
	if err != nil {
		return sandbox.PrivateEvent{}, ErrDemoAmbiguous
	}
	return event, nil
}
