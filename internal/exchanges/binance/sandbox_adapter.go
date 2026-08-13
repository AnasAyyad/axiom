package binance

import (
	"context"
	"errors"
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

const (
	sandboxHistoryPageLimit   = 100
	sandboxSnapshotBaseWeight = 221
	sandboxHistoryPageWeight  = 20
)

// SandboxAdapter is the complete adapter-neutral Binance Spot Testnet
// authenticated boundary.
type SandboxAdapter struct {
	client       *SandboxClient
	identity     sandbox.AccountIdentity
	epoch        uint64
	lookup       sandbox.SubmissionLookup
	expectations sandbox.SnapshotExpectationReader
	marketData   exchangecontracts.MarketDataSource
	strategyData sandbox.StrategyMarketData
	rules        map[string]SandboxInstrumentRules
	rateBudget   *sandboxRateBudget
}

var (
	_ sandbox.AccountReader = (*SandboxAdapter)(nil)
	_ sandbox.OrderBroker   = (*SandboxAdapter)(nil)
	_ sandbox.Reconciler    = (*SandboxAdapter)(nil)
)

// NewSandboxAdapter loads all approved Testnet filters before exposing an
// order-capable neutral adapter.
func NewSandboxAdapter(
	ctx context.Context,
	client *SandboxClient,
	identity sandbox.AccountIdentity,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	expectations sandbox.SnapshotExpectationReader,
) (*SandboxAdapter, error) {
	if client == nil || identity.Validate() != nil ||
		identity.Exchange != sandbox.ExchangeBinance ||
		identity.Environment != sandbox.EnvironmentBinanceSpotTestnet ||
		epoch == 0 || lookup == nil || expectations == nil {
		return nil, ErrSandboxStartupIdentity
	}
	rules := make(map[string]SandboxInstrumentRules, 3)
	for _, instrument := range approvedSandboxInstruments() {
		loaded, err := client.loadSandboxRules(ctx, instrument)
		if err != nil {
			return nil, err
		}
		rules[instrument.Symbol()] = loaded
	}
	marketData, err := NewTestnetPublicClient(&domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	adapter, err := newSandboxAdapterForTestWithMarketData(
		client, identity, epoch, lookup, expectations, rules, marketData,
	)
	if err != nil {
		return nil, err
	}
	adapter.strategyData = marketData
	return adapter, nil
}

// StrategyMarketData exposes only the credential-free Testnet public source
// for a future automatic strategy worker.
func (adapter *SandboxAdapter) StrategyMarketData() (sandbox.StrategyMarketData, error) {
	if adapter == nil || adapter.strategyData == nil {
		return nil, ErrSandboxRequest
	}
	return adapter.strategyData, nil
}

// StrategyInstrumentRules returns a defensive, credential-free copy of the
// exact Testnet filters loaded during adapter startup. The returned values
// contain no client, signer, account identity, endpoint, or order capability.
func (adapter *SandboxAdapter) StrategyInstrumentRules() ([]SandboxInstrumentRules, error) {
	if adapter == nil || len(adapter.rules) != len(approvedSandboxInstruments()) {
		return nil, ErrSandboxRequest
	}
	result := make([]SandboxInstrumentRules, 0, len(adapter.rules))
	for _, rule := range adapter.rules {
		if rule.Instrument.Product != domain.ProductSpot || rule.SourceHash == "" ||
			rule.ObservedAt.IsZero() || rule.ObservedAt.Location() != time.UTC {
			return nil, ErrSandboxRequest
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
	rules map[string]SandboxInstrumentRules,
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
	rules map[string]SandboxInstrumentRules,
	marketData exchangecontracts.MarketDataSource,
) (*SandboxAdapter, error) {
	if client == nil || identity.Validate() != nil || epoch == 0 ||
		lookup == nil || expectations == nil || len(rules) != 3 {
		return nil, ErrSandboxStartupIdentity
	}
	for _, instrument := range approvedSandboxInstruments() {
		rule, exists := rules[instrument.Symbol()]
		if !exists || rule.Instrument != instrument ||
			rule.SourceHash == "" || rule.ObservedAt.IsZero() {
			return nil, ErrSandboxStartupIdentity
		}
	}
	rateBudget, err := newSandboxRateBudget(1200, 100, time.Minute)
	if err != nil {
		return nil, ErrSandboxStartupIdentity
	}
	return &SandboxAdapter{
		client: client, identity: identity, epoch: epoch, lookup: lookup,
		expectations: expectations, marketData: marketData, rules: rules,
		rateBudget: rateBudget,
	}, nil
}

// Capabilities returns the closed Testnet authenticated descriptor.
func (adapter *SandboxAdapter) Capabilities(
	context.Context,
) (sandbox.CapabilityDescriptor, error) {
	now := adapter.client.now().UTC()
	descriptor := sandbox.CapabilityDescriptor{
		Exchange:    sandbox.ExchangeBinance,
		Environment: sandbox.EnvironmentBinanceSpotTestnet,
		SpotOnly:    true, ReadAccount: true, WriteSpotOrders: true,
		RESTOrderEntry: true, PrivateEvents: true, HMACSHA256: true,
		OrderStyles: []sandbox.OrderStyle{
			sandbox.OrderStyleLimitGTC,
			sandbox.OrderStyleLimitIOC,
			sandbox.OrderStylePostOnly,
		},
		ObservedAt: now,
	}
	descriptor.CapabilityHash = canonicalHash([]string{
		"binance", "spot_testnet", "rest_order_entry", "hmac_sha256",
		"LIMIT_GTC", "LIMIT_IOC", "POST_ONLY", "private_events",
	})
	if descriptor.Validate() != nil {
		return sandbox.CapabilityDescriptor{}, ErrSandboxRequest
	}
	return descriptor, nil
}

// Identity returns the redacted, owner-attested startup identity.
func (adapter *SandboxAdapter) Identity(
	context.Context,
) (sandbox.AccountIdentity, error) {
	return adapter.identity, nil
}

// Snapshot loads balances plus complete approved-symbol order/fill history.
func (adapter *SandboxAdapter) Snapshot(
	ctx context.Context,
) (sandbox.AccountSnapshot, error) {
	if err := adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		sandboxSnapshotBaseWeight,
		sandboxRequestReconcile,
	); err != nil {
		return sandbox.AccountSnapshot{}, err
	}
	balances, allOrders, allFills, err := adapter.loadSandboxSnapshotFacts(ctx)
	if err != nil {
		return sandbox.AccountSnapshot{}, err
	}
	return adapter.buildSandboxSnapshot(balances, allOrders, allFills)
}

func (adapter *SandboxAdapter) loadSandboxSnapshotFacts(
	ctx context.Context,
) (
	[]sandbox.Balance,
	[]sandboxOrderPayload,
	[]sandboxFillPayload,
	error,
) {
	accountBody, err := adapter.client.account(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	balances, err := normalizeSandboxBalances(accountBody)
	if err != nil {
		return nil, nil, nil, err
	}
	openBody, err := adapter.client.openOrders(ctx, "")
	if err != nil {
		return nil, nil, nil, err
	}
	openOrders, err := decodeSandboxOrders(openBody)
	if err != nil {
		return nil, nil, nil, err
	}
	allOrders := append([]sandboxOrderPayload(nil), openOrders...)
	allFills := make([]sandboxFillPayload, 0)
	for _, instrument := range approvedSandboxInstruments() {
		history, historyErr := adapter.completeOrderHistory(ctx, instrument.Symbol())
		if historyErr != nil {
			return nil, nil, nil, historyErr
		}
		allOrders = append(allOrders, history...)
		fills, fillErr := adapter.completeFillHistory(ctx, instrument.Symbol())
		if fillErr != nil {
			return nil, nil, nil, fillErr
		}
		allFills = append(allFills, fills...)
	}
	return balances, allOrders, allFills, nil
}

func (adapter *SandboxAdapter) buildSandboxSnapshot(
	balances []sandbox.Balance,
	allOrders []sandboxOrderPayload,
	allFills []sandboxFillPayload,
) (sandbox.AccountSnapshot, error) {
	ordersHash := canonicalSandboxOrdersHash(allOrders)
	fillsHash := canonicalSandboxFillsHash(allFills)
	snapshot := sandbox.AccountSnapshot{
		AccountID: adapter.identity.AccountID, Epoch: adapter.epoch,
		Balances: balances, OrdersHash: ordersHash, FillsHash: fillsHash,
		ObservedAt: adapter.client.now().UTC(),
	}
	snapshot.SnapshotHash = canonicalHash(struct {
		AccountID  sandbox.AccountID `json:"account_id"`
		Epoch      uint64            `json:"epoch"`
		Balances   []sandbox.Balance `json:"balances"`
		OrdersHash string            `json:"orders_hash"`
		FillsHash  string            `json:"fills_hash"`
	}{
		AccountID: snapshot.AccountID, Epoch: snapshot.Epoch,
		Balances: snapshot.Balances, OrdersHash: ordersHash, FillsHash: fillsHash,
	})
	if snapshot.Validate() != nil {
		return sandbox.AccountSnapshot{}, ErrSandboxPayload
	}
	return snapshot, nil
}

// Submit validates current balances, exact current Testnet filters, and the
// safe test-create route before sending one real Testnet create.
func (adapter *SandboxAdapter) Submit(
	ctx context.Context,
	submission sandbox.Submission,
) (sandbox.PrivateEvent, error) {
	maximum, _ := domain.ParseNotional("10")
	if submission.Validate(maximum) != nil ||
		submission.AccountID != adapter.identity.AccountID ||
		submission.AccountEpoch != adapter.epoch {
		return sandbox.PrivateEvent{}, ErrSandboxRequest
	}
	rejection, err := adapter.checkSandboxSubmission(ctx, submission)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	if rejection != "" {
		return rejectedSandboxEvent(
			submission, adapter.client.now().UTC(), rejection,
		), nil
	}
	body, err := adapter.client.create(ctx, submission)
	if errors.Is(err, ErrSandboxRejected) {
		return rejectedSandboxEvent(
			submission, adapter.client.now().UTC(), "create_rejected",
		), nil
	}
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	event, err := normalizeSandboxAcknowledgement(
		body, submission, adapter.client.now().UTC(),
	)
	if err != nil {
		return sandbox.PrivateEvent{}, ErrSandboxAmbiguous
	}
	return event, nil
}

func (adapter *SandboxAdapter) checkSandboxSubmission(
	ctx context.Context,
	submission sandbox.Submission,
) (string, error) {
	if err := adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		25,
		sandboxRequestEntry,
	); err != nil {
		return "rate_budget", nil
	}
	rules, exists := adapter.rules[submission.Instrument.Symbol()]
	if !exists {
		return "instrument_filter", nil
	}
	owned, err := adapter.availableBalance(ctx, submission.Instrument.Base)
	if err != nil {
		// No create request has been built or sent at this boundary. Persist a
		// deterministic rejection instead of classifying the attempt as
		// ambiguous and entering unknown-order recovery for an order that
		// cannot exist at the exchange.
		return "account_unavailable", nil
	}
	reference, err := adapter.client.averagePrice(ctx, submission.Instrument)
	if err != nil {
		return "book_unavailable", nil
	}
	if rules.validateSubmission(
		submission,
		owned,
		reference,
		reference.ValidatedThrough,
	) != nil {
		return "instrument_filter", nil
	}
	if _, err = adapter.client.testCreate(ctx, submission); err != nil {
		return "test_create", nil
	}
	return "", nil
}
