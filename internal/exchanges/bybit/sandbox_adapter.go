package bybit

import (
	"context"
	"errors"
	"time"

	"axiom/internal/domain"
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
	return newSandboxAdapterForTest(
		client,
		identity,
		epoch,
		lookup,
		expectations,
		rules,
	)
}

func newSandboxAdapterForTest(
	client *SandboxClient,
	identity sandbox.AccountIdentity,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	expectations sandbox.SnapshotExpectationReader,
	rules map[string]DemoInstrumentRules,
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
		expectations: expectations, rules: rules, rateBudget: budget,
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

func (adapter *SandboxAdapter) checkDemoSubmission(
	ctx context.Context,
	submission sandbox.Submission,
) (string, error) {
	if err := adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		10,
		demoRequestEntry,
	); err != nil {
		return "rate_budget", nil
	}
	rules, exists := adapter.rules[submission.Instrument.Symbol()]
	if !exists {
		return "instrument_filter", nil
	}
	owned, err := adapter.availableBalance(ctx, submission.Instrument.Base)
	if err != nil {
		// A failed preflight account read occurs before create construction and
		// cannot be an ambiguous submission. Reduce it as a deterministic local
		// rejection so recovery never queries a provider order that cannot
		// exist.
		return "account_unavailable", nil
	}
	if rules.validateSubmission(submission, owned) != nil {
		return "instrument_filter", nil
	}
	return "", nil
}

// Query resolves one deterministic client order ID from authoritative Demo
// order and execution history.
func (adapter *SandboxAdapter) Query(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) ([]sandbox.PrivateEvent, error) {
	submission, found, err := adapter.resolveSubmission(
		ctx,
		account,
		epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return nil, err
	}
	if err = adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		4,
		demoRequestReconcile,
	); err != nil {
		return nil, err
	}
	body, err := adapter.client.query(ctx, submission)
	if errors.Is(err, ErrDemoOrderNotFound) {
		return adapter.normalizeDemoHistoryQuery(ctx, submission)
	}
	if err != nil {
		return nil, err
	}
	return adapter.normalizeDemoQuery(ctx, submission, body)
}

func (adapter *SandboxAdapter) normalizeDemoQuery(
	ctx context.Context,
	submission sandbox.Submission,
	body []byte,
) ([]sandbox.PrivateEvent, error) {
	result, err := decodeDemoResult[orderListResult](body)
	if err != nil || !bindDemoOrderCategories(&result) ||
		len(result.List) > 1 {
		return nil, ErrDemoPayload
	}
	if len(result.List) == 0 {
		return adapter.normalizeDemoHistoryQuery(ctx, submission)
	}
	return adapter.normalizeDemoQueryOrder(
		ctx, submission, result.List[0], body,
	)
}

func (adapter *SandboxAdapter) normalizeDemoHistoryQuery(
	ctx context.Context,
	submission sandbox.Submission,
) ([]sandbox.PrivateEvent, error) {
	order, body, err := adapter.exactOrderHistory(ctx, submission)
	if err != nil {
		return nil, err
	}
	return adapter.normalizeDemoQueryOrder(ctx, submission, order, body)
}

func (adapter *SandboxAdapter) normalizeDemoQueryOrder(
	ctx context.Context,
	submission sandbox.Submission,
	order demoOrderPayload,
	body []byte,
) ([]sandbox.PrivateEvent, error) {
	executions, err := adapter.completeExecutionHistory(
		ctx,
		submission.Instrument.Symbol(),
		order.OrderID,
		submission.ClientOrderID,
	)
	if err != nil {
		return nil, err
	}
	event, err := normalizeDemoOrder(
		order,
		executions,
		submission,
		adapter.client.now().UTC(),
		body,
	)
	if err != nil {
		return nil, err
	}
	return []sandbox.PrivateEvent{event}, nil
}

// Cancel requests cancellation without depending on entry enablement.
func (adapter *SandboxAdapter) Cancel(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) (sandbox.PrivateEvent, error) {
	submission, found, err := adapter.resolveSubmission(
		ctx,
		account,
		epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return sandbox.PrivateEvent{}, err
	}
	if err = adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		2,
		demoRequestCancel,
	); err != nil {
		return sandbox.PrivateEvent{}, err
	}
	body, err := adapter.client.cancel(ctx, submission)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	return normalizeDemoAcknowledgement(
		body,
		submission,
		execution.OrderCancelPending,
		adapter.client.now().UTC(),
	)
}

func rejectedDemoEvent(
	submission sandbox.Submission,
	receivedAt time.Time,
	reason string,
) sandbox.PrivateEvent {
	zero, _ := domain.ParseQuantity("0")
	nativeHash := canonicalDemoHash([]string{
		"bybit", submission.ClientOrderID, submission.RequestHash, reason,
	})
	orderEvent := execution.OrderEvent{
		ID:      "bybit-rejected-" + submission.ClientOrderID + "-" + nativeHash[:12],
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: execution.OrderRejected, ExchangeStatus: "REJECTED",
		CumulativeQuantity: zero, OccurredAt: receivedAt,
		Ordinal: uint64(receivedAt.UnixMilli()),
	}
	return sandbox.PrivateEvent{
		Identity:  "bybit-rejected-" + submission.ClientOrderID + "-" + nativeHash[:12],
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateOrderEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: nativeHash, OrderEvent: &orderEvent,
		OccurredAt: receivedAt, ReceivedAt: receivedAt,
	}
}
