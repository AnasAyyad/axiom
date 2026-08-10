package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/replay"
)

// StrategyDecisionJournalEntry is one immutable completed evaluation. PlanID
// is empty only when the decision did not reach durable plan approval; it is
// never patched later. Consumers must not infer an accepted plan from an
// empty reference.
type StrategyDecisionJournalEntry struct {
	Evidence   StrategyDecisionEvidence
	PlanID     string
	OccurredAt time.Time
}

// ValidFor proves the journal row is structurally bound to the exact active
// strategy work. It intentionally does not treat an empty journal as proof
// that no historical strategy order exists; the durable source performs that
// additional compatibility check before returning a projection input.
func (entry StrategyDecisionJournalEntry) ValidFor(
	work StrategySessionWork,
	now time.Time,
) error {
	identity, identityErr := strategyDecisionIdentityFromJSON(entry.Evidence.CanonicalDecision)
	if work.ValidAt(now) != nil || entry.OccurredAt.IsZero() ||
		entry.OccurredAt.Location() != time.UTC || entry.OccurredAt.After(now) ||
		identityErr != nil || identity.ID != entry.Evidence.DecisionID ||
		identity.Ordinal != entry.Evidence.EventOrdinal ||
		entry.Evidence.SessionID != work.SessionID ||
		entry.Evidence.AccountID != work.Account.ID ||
		entry.Evidence.AccountEpoch != work.Account.Epoch ||
		entry.Evidence.StrategyRevision != work.StrategyRevision ||
		entry.Evidence.Strategy != work.Strategy || entry.Evidence.Instrument != work.Instrument ||
		entry.Evidence.EventOrdinal == 0 || entry.Evidence.EventLogicalTime == 0 ||
		!json.Valid(entry.Evidence.CanonicalInput) || !json.Valid(entry.Evidence.CanonicalDecision) ||
		!hash256(entry.Evidence.InputHash) || !hash256(entry.Evidence.DecisionHash) ||
		entry.Evidence.InputHash != strategyDecisionHash(entry.Evidence.CanonicalInput) ||
		entry.Evidence.DecisionHash != strategyDecisionHash(entry.Evidence.CanonicalDecision) {
		return contractError("strategy_decision_journal_invalid")
	}
	return nil
}

// StrategyDecisionJournalSource returns all ordered immutable evaluations for
// one exact account epoch. It carries no adapter authority and cannot submit
// an order; it exists solely to make a later position projection reproducible.
type StrategyDecisionJournalSource interface {
	StrategyDecisionJournal(context.Context, StrategySessionWork, time.Time) ([]StrategyDecisionJournalEntry, error)
}

// StrategyDecisionRecorder appends one no-plan strategy decision under the
// scheduler's exact engine lease. It carries no order-submission authority.
// Accepted plan decisions are recorded by the durable plan transaction so
// their plan reference can never be patched later.
type StrategyDecisionRecorder interface {
	RecordSandboxStrategyDecision(
		context.Context,
		string,
		uint64,
		StrategySessionWork,
		StrategyDecisionEvidence,
		time.Time,
	) error
}

// StrategyDecisionEvidence preserves the exact canonical input and complete
// strategy decision from one automatic evaluation. A decision may be a
// rejection, a no-action position advance, or an accepted order candidate.
// The generic allocator candidate does not contain protective stops, holding
// time, or cooldown state, so this immutable record is required before a
// later fill reducer may reconstruct a strategy-owned position.
//
// It contains public decision inputs and strategy output only; it must never
// contain an account snapshot, credentials, signed request, or exchange
// private payload.
type StrategyDecisionEvidence struct {
	SessionID         SessionID
	AccountID         AccountID
	AccountEpoch      uint64
	StrategyRevision  uint64
	Strategy          string
	Instrument        string
	DecisionID        string
	EventOrdinal      uint64
	EventLogicalTime  uint64
	CanonicalInput    json.RawMessage
	CanonicalDecision json.RawMessage
	InputHash         string
	DecisionHash      string
}

func newStrategyDecisionEvidence(
	admission StrategySessionAdmission,
	material StrategyPipelineMaterial,
) (StrategyDecisionEvidence, error) {
	if admission.Valid() != nil || material.Event.Ordinal == 0 ||
		material.Event.LogicalTime == 0 || !json.Valid(material.Event.Canonical) ||
		!json.Valid(material.DecisionEvidence) ||
		material.Approved.DecisionID.String() == "" {
		return StrategyDecisionEvidence{}, contractError("strategy_decision_evidence_invalid")
	}
	result, err := NewStrategyDecisionEvidence(admission, material.Event,
		material.DecisionEvidence)
	if err != nil || result.DecisionID != material.Approved.DecisionID.String() {
		return StrategyDecisionEvidence{}, contractError("strategy_decision_evidence_invalid")
	}
	return result, nil
}

// NewStrategyDecisionEvidence binds one complete, pure strategy decision to
// its canonical public input. It deliberately accepts no plan, allocation, or
// exchange facts, which lets callers preserve a rejected or no-action
// evaluation before there is anything that may be submitted.
func NewStrategyDecisionEvidence(
	admission StrategySessionAdmission,
	event replay.Event,
	decision json.RawMessage,
) (StrategyDecisionEvidence, error) {
	if admission.Valid() != nil || event.Ordinal == 0 || event.LogicalTime == 0 ||
		!json.Valid(event.Canonical) || !json.Valid(decision) {
		return StrategyDecisionEvidence{}, contractError("strategy_decision_evidence_invalid")
	}
	identity, err := strategyDecisionIdentityFromJSON(decision)
	if err != nil || identity.Ordinal != event.Ordinal {
		return StrategyDecisionEvidence{}, contractError("strategy_decision_evidence_invalid")
	}
	result := StrategyDecisionEvidence{SessionID: admission.Work.SessionID,
		AccountID: admission.Work.Account.ID, AccountEpoch: admission.Work.Account.Epoch,
		StrategyRevision: admission.Work.StrategyRevision,
		Strategy:         admission.Work.Strategy, Instrument: admission.Work.Instrument,
		DecisionID: identity.ID, EventOrdinal: event.Ordinal, EventLogicalTime: event.LogicalTime,
		CanonicalInput:    append(json.RawMessage(nil), event.Canonical...),
		CanonicalDecision: append(json.RawMessage(nil), decision...)}
	result.InputHash = strategyDecisionHash(result.CanonicalInput)
	result.DecisionHash = strategyDecisionHash(result.CanonicalDecision)
	if result.ValidFor(admission, admission.ApprovedAt) != nil {
		return StrategyDecisionEvidence{}, contractError("strategy_decision_evidence_invalid")
	}
	return result, nil
}

// ValidFor proves the record binds one exact strategy session work snapshot
// to the same instant as plan admission. Strategy-specific parsing happens in
// the position projector; this boundary first protects immutability and
// provenance for every accepted one-leg strategy decision.
func (evidence StrategyDecisionEvidence) ValidFor(
	admission StrategySessionAdmission,
	approvedAt time.Time,
) error {
	identity, identityErr := strategyDecisionIdentityFromJSON(evidence.CanonicalDecision)
	if admission.Valid() != nil || identityErr != nil || identity.ID != evidence.DecisionID ||
		identity.Ordinal != evidence.EventOrdinal ||
		evidence.SessionID != admission.Work.SessionID ||
		evidence.AccountID != admission.Work.Account.ID ||
		evidence.AccountEpoch != admission.Work.Account.Epoch ||
		evidence.StrategyRevision != admission.Work.StrategyRevision ||
		evidence.Strategy != admission.Work.Strategy || evidence.Instrument != admission.Work.Instrument ||
		evidence.DecisionID == "" || evidence.EventOrdinal == 0 || evidence.EventLogicalTime == 0 ||
		!json.Valid(evidence.CanonicalInput) || !json.Valid(evidence.CanonicalDecision) ||
		!hash256(evidence.InputHash) || !hash256(evidence.DecisionHash) ||
		evidence.InputHash != strategyDecisionHash(evidence.CanonicalInput) ||
		evidence.DecisionHash != strategyDecisionHash(evidence.CanonicalDecision) ||
		!admission.ApprovedAt.Equal(approvedAt) {
		return contractError("strategy_decision_evidence_invalid")
	}
	return nil
}

// ValidForPlan verifies the subset of provenance a generic durable dispatcher
// can prove without reloading strategy-session admission facts. The builder
// already performs the stricter ValidFor check before it creates the plan.
func (evidence StrategyDecisionEvidence) ValidForPlan(plan ApprovedSandboxPlan) error {
	identity, identityErr := strategyDecisionIdentityFromJSON(evidence.CanonicalDecision)
	if identityErr != nil || identity.ID != evidence.DecisionID ||
		identity.Ordinal != evidence.EventOrdinal || !identity.OrderCapable() ||
		evidence.SessionID != plan.SessionID || evidence.AccountID == "" ||
		evidence.AccountEpoch == 0 || evidence.StrategyRevision == 0 || evidence.Strategy == "" || evidence.Instrument == "" ||
		evidence.DecisionID == "" || evidence.EventOrdinal == 0 || evidence.EventLogicalTime == 0 ||
		!json.Valid(evidence.CanonicalInput) || !json.Valid(evidence.CanonicalDecision) ||
		!hash256(evidence.InputHash) || !hash256(evidence.DecisionHash) ||
		evidence.InputHash != strategyDecisionHash(evidence.CanonicalInput) ||
		evidence.DecisionHash != strategyDecisionHash(evidence.CanonicalDecision) {
		return contractError("strategy_decision_evidence_invalid")
	}
	return nil
}

type strategyDecisionIdentity struct {
	ID        string          `json:"id"`
	Ordinal   uint64          `json:"ordinal"`
	Action    string          `json:"action"`
	Candidate json.RawMessage `json:"candidate"`
}

func strategyDecisionIdentityFromJSON(payload []byte) (strategyDecisionIdentity, error) {
	var identity strategyDecisionIdentity
	if !json.Valid(payload) || json.Unmarshal(payload, &identity) != nil ||
		identity.ID == "" || identity.Ordinal == 0 ||
		(identity.Action != "none" && identity.Action != "entry" && identity.Action != "exit") {
		return strategyDecisionIdentity{}, contractError("strategy_decision_evidence_invalid")
	}
	if len(identity.Candidate) > 0 && !json.Valid(identity.Candidate) {
		return strategyDecisionIdentity{}, contractError("strategy_decision_evidence_invalid")
	}
	return identity, nil
}

// OrderCapable reports whether the decision may proceed to an execution plan.
func (identity strategyDecisionIdentity) OrderCapable() bool {
	return (identity.Action == "entry" || identity.Action == "exit") && len(identity.Candidate) > 0 &&
		string(identity.Candidate) != "null"
}

func strategyDecisionHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
