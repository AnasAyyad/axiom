package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

// SandboxStrategySizingFactStore provides only immutable account and decision
// projections. It intentionally has no exchange adapter, allocator, or
// submission method.
type SandboxStrategySizingFactStore interface {
	sandbox.AccountSnapshotHistoryReader
	sandbox.StrategyDecisionJournalSource
	sandbox.StrategyRiskFactsSource
	SandboxStrategyAssetEligibilitySource
}

// SandboxStrategyAssetEligibilitySource resolves the current approved
// screening version shared by the base and quote assets of a one-leg session.
// The legacy strategy input contracts carry one version field, so a divergent
// asset-screening version is deliberately unavailable rather than collapsed
// into an arbitrary one.
type SandboxStrategyAssetEligibilitySource interface {
	SandboxStrategyAssetEligibility(
		context.Context,
		sandbox.StrategySessionWork,
		time.Time,
	) (uint64, error)
}

// SandboxStrategySizingFactsReader derives complete per-evaluation facts from
// a bound sandbox runtime configuration, the exact scheduler lease, and immutable storage
// projections. It does not read an account from an exchange or infer balances.
type SandboxStrategySizingFactsReader struct {
	store SandboxStrategySizingFactStore
}

// NewSandboxStrategySizingFactsReader constructs the non-secret sizing source.
func NewSandboxStrategySizingFactsReader(
	store SandboxStrategySizingFactStore,
) (*SandboxStrategySizingFactsReader, error) {
	if store == nil {
		return nil, fmt.Errorf("sandbox_strategy_sizing_facts_source_invalid")
	}
	return &SandboxStrategySizingFactsReader{store: store}, nil
}

// SandboxStrategySizingFacts returns a single immutable, capped fact set. Its
// ordinal comes only from the committed session journal; the supplied lease is
// copied exactly into the strategy input fencing token.
func (reader *SandboxStrategySizingFactsReader) SandboxStrategySizingFacts(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	product config.Configuration,
	admission sandbox.StrategySessionAdmission,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (SandboxStrategySizingFacts, error) {
	if !reader.validSizingFactsRequest(ctx, work, product, admission, lease, now) {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_sizing_facts_source_invalid")
	}
	snapshot, err := reader.sizingFactsSnapshot(ctx, work, now)
	if err != nil {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_sizing_facts_unavailable")
	}
	entries, err := reader.store.StrategyDecisionJournal(ctx, work, now)
	if err != nil {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_sizing_facts_unavailable")
	}
	ordinal, logicalTime, priorTriggerHash, err := nextSandboxStrategyEvaluationSequence(entries, work, now)
	if err != nil {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_sizing_facts_invalid")
	}
	riskFacts, err := reader.store.StrategyRiskFacts(ctx, work, snapshot, now)
	if err != nil || riskFacts.ValidFor(work, snapshot, now) != nil {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_risk_facts_unavailable")
	}
	assetEligibility, err := reader.store.SandboxStrategyAssetEligibility(ctx, work, now)
	if err != nil || assetEligibility == 0 {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_asset_eligibility_unavailable")
	}
	maximum, err := domain.ParseMoney(product.Sandbox.MaximumOrderNotional.Value)
	if err != nil {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_sizing_facts_invalid")
	}
	fee, err := publicShadowFeeRate(product.Models.Fee)
	zero, zeroErr := domain.ParsePrice("0")
	if err != nil || zeroErr != nil || product.Models.Latency != "fixed-zero-v1" {
		return SandboxStrategySizingFacts{}, fmt.Errorf("sandbox_strategy_sizing_facts_invalid")
	}
	return buildSandboxStrategySizingFacts(work, product, lease, snapshot, riskFacts,
		assetEligibility, maximum, fee, zero, ordinal, logicalTime, priorTriggerHash), nil
}

func (reader *SandboxStrategySizingFactsReader) validSizingFactsRequest(ctx context.Context,
	work sandbox.StrategySessionWork, product config.Configuration,
	admission sandbox.StrategySessionAdmission, lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) bool {
	return reader != nil && reader.store != nil && ctx != nil && work.ValidAt(now) == nil &&
		lease.ValidFor(work) == nil && config.Validate(product) == nil &&
		product.SchemaVersion == config.SchemaVersionSandboxRuntime && admission.Valid() == nil &&
		admission.Work == work && admission.ApprovedAt.Equal(now)
}

func (reader *SandboxStrategySizingFactsReader) sizingFactsSnapshot(ctx context.Context,
	work sandbox.StrategySessionWork, now time.Time,
) (sandbox.AccountSnapshot, error) {
	snapshot, found, err := reader.store.LatestAccountSnapshot(ctx, work.Account.ID, work.Account.Epoch)
	if err != nil || !found || snapshot.Validate() != nil || snapshot.AccountID != work.Account.ID ||
		snapshot.Epoch != work.Account.Epoch || snapshot.ObservedAt.After(now) ||
		now.Sub(snapshot.ObservedAt) > 250*time.Millisecond {
		return sandbox.AccountSnapshot{}, fmt.Errorf("sandbox_strategy_sizing_facts_unavailable")
	}
	return snapshot, nil
}

func buildSandboxStrategySizingFacts(work sandbox.StrategySessionWork, product config.Configuration,
	lease sandbox.StrategySessionExecutionLease, snapshot sandbox.AccountSnapshot,
	riskFacts sandbox.StrategyRiskFacts, assetEligibility uint64, maximum domain.Money,
	fee domain.Rate, zero domain.Price, ordinal, logicalTime uint64, priorTriggerHash string,
) SandboxStrategySizingFacts {
	return SandboxStrategySizingFacts{AccountSnapshot: snapshot, CentralRiskFacts: riskFacts,
		PortfolioRevision: work.SessionRevision,
		PositionRevision:  work.StrategyRevision, AssetEligibility: assetEligibility,
		RiskPolicyID: riskFacts.PolicyID, RiskPolicyVersion: riskFacts.PolicyVersion,
		RiskPolicyHash: riskFacts.PolicyHash, MinimumReserve: riskFacts.MinimumReserve,
		MaximumReserved: riskFacts.MaximumReserved, MaximumOrderNotional: maximum,
		EntryFeeRate: fee, ExitFeeRate: fee, GapAllowance: zero, LatencyDeterioration: zero,
		SlippageAllowance: zero, LiquidityDomain: "strategy-session-" + string(work.SessionID),
		FencingToken: lease.Fence, EvaluationOrdinal: ordinal, EvaluationLogicalTime: logicalTime,
		PriorEvaluationTriggerHash: priorTriggerHash,
		ConfigurationHash:          work.ConfigurationHash, ConfigurationVersion: product.SchemaVersion,
		FeeModelID: product.Models.Fee, LatencyModelID: product.Models.Latency, FillModelID: "fill-v1",
		SlippageModelID: "slippage-v1", GapModelID: "gap-v1", CorrelationModelID: "strategy-set-v1"}
}

func nextSandboxStrategyEvaluationSequence(
	entries []sandbox.StrategyDecisionJournalEntry,
	work sandbox.StrategySessionWork,
	now time.Time,
) (uint64, uint64, string, error) {
	var ordinal, logicalTime uint64
	var priorTriggerHash string
	for _, entry := range entries {
		if entry.ValidFor(work, now) != nil || entry.Evidence.EventOrdinal <= ordinal ||
			entry.Evidence.EventLogicalTime <= logicalTime {
			return 0, 0, "", fmt.Errorf("sandbox_strategy_sequence_invalid")
		}
		triggerHash, err := sandboxStrategyEvaluationTriggerFromCanonicalInput(
			work, entry.Evidence.CanonicalInput,
		)
		if err != nil {
			return 0, 0, "", fmt.Errorf("sandbox_strategy_sequence_invalid")
		}
		ordinal, logicalTime = entry.Evidence.EventOrdinal, entry.Evidence.EventLogicalTime
		priorTriggerHash = triggerHash
	}
	if ordinal == ^uint64(0) || logicalTime == ^uint64(0) {
		return 0, 0, "", fmt.Errorf("sandbox_strategy_sequence_invalid")
	}
	return ordinal + 1, logicalTime + 1, priorTriggerHash, nil
}

// SandboxStrategyAdmissionAdapter binds a preconfigured engine switch set to
// the existing store admission source. It exposes only the evaluated admission
// result to the strategy executor.
type SandboxStrategyAdmissionAdapter struct {
	source   sandbox.StrategySessionAdmissionSource
	switches [4]bool
}

// NewSandboxStrategyAdmissionAdapter creates a narrow admission reader.
func NewSandboxStrategyAdmissionAdapter(
	source sandbox.StrategySessionAdmissionSource,
	switches [4]bool,
) (*SandboxStrategyAdmissionAdapter, error) {
	if source == nil {
		return nil, fmt.Errorf("sandbox_strategy_admission_adapter_invalid")
	}
	return &SandboxStrategyAdmissionAdapter{source: source, switches: switches}, nil
}

// SandboxStrategySessionAdmission rechecks the configured gate set at the
// exact scheduler time. Disabled gates cause the underlying source to deny.
func (adapter *SandboxStrategyAdmissionAdapter) SandboxStrategySessionAdmission(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (sandbox.StrategySessionAdmission, error) {
	if adapter == nil || adapter.source == nil || ctx == nil || work.ValidAt(now) != nil {
		return sandbox.StrategySessionAdmission{}, fmt.Errorf("sandbox_strategy_admission_adapter_invalid")
	}
	return adapter.source.StrategySessionAdmission(ctx, work, now, adapter.switches)
}

var _ SandboxStrategySizingFactsSource = (*SandboxStrategySizingFactsReader)(nil)
var _ SandboxStrategyAdmissionReader = (*SandboxStrategyAdmissionAdapter)(nil)
