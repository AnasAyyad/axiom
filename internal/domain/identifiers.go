package domain

import (
	"regexp"
	"strings"
)

const maximumIdentifierLength = 80

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type idKind interface{ prefix() string }

// ID is the opaque canonical representation shared by compile-time-distinct IDs.
type ID[K idKind] struct{ value string }

// String returns the canonical kind-prefixed identifier.
func (id ID[K]) String() string {
	var kind K
	return kind.prefix() + ":" + id.value
}

// Value returns the validated identifier without its kind prefix.
func (id ID[K]) Value() string { return id.value }

// MarshalText emits the canonical kind-prefixed identifier.
func (id ID[K]) MarshalText() ([]byte, error) {
	if !validIdentifier(id.value) {
		return nil, domainError(CodeInvalidIdentifier, "marshal_id")
	}
	return []byte(id.String()), nil
}

// UnmarshalText parses the exact kind prefix and identifier value.
func (id *ID[K]) UnmarshalText(text []byte) error {
	parsed, err := parseID[K](string(text))
	if err == nil {
		*id = parsed
	}
	return err
}

func newID[K idKind](value string) (ID[K], error) {
	if !validIdentifier(value) {
		return ID[K]{}, domainError(CodeInvalidIdentifier, "id")
	}
	return ID[K]{value: value}, nil
}

func parseID[K idKind](text string) (ID[K], error) {
	var kind K
	prefix, value, found := strings.Cut(text, ":")
	if !found || prefix != kind.prefix() {
		return ID[K]{}, domainError(CodeInvalidIdentifier, "id")
	}
	return newID[K](value)
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= maximumIdentifierLength && identifierPattern.MatchString(value)
}

type userIDKind struct{}
type sessionIDKind struct{}
type configurationSnapshotIDKind struct{}
type assetRegistryIDKind struct{}
type instrumentMetadataIDKind struct{}
type marketSegmentIDKind struct{}
type datasetIDKind struct{}
type runIDKind struct{}
type strategyIDKind struct{}
type portfolioIDKind struct{}
type virtualAccountIDKind struct{}
type reservationIDKind struct{}
type opportunityIDKind struct{}
type decisionIDKind struct{}
type executionPlanIDKind struct{}
type virtualOrderIDKind struct{}
type executionAttemptIDKind struct{}
type virtualFillIDKind struct{}
type journalTransactionIDKind struct{}
type modelVersionIDKind struct{}
type jobIDKind struct{}
type commandIDKind struct{}
type leaseIDKind struct{}
type inboxMessageIDKind struct{}
type outboxMessageIDKind struct{}
type incidentIDKind struct{}
type auditEventIDKind struct{}

func (userIDKind) prefix() string                  { return "user" }
func (sessionIDKind) prefix() string               { return "session" }
func (configurationSnapshotIDKind) prefix() string { return "config_snapshot" }
func (assetRegistryIDKind) prefix() string         { return "asset_registry" }
func (instrumentMetadataIDKind) prefix() string    { return "instrument_metadata" }
func (marketSegmentIDKind) prefix() string         { return "market_segment" }
func (datasetIDKind) prefix() string               { return "dataset" }
func (runIDKind) prefix() string                   { return "run" }
func (strategyIDKind) prefix() string              { return "strategy" }
func (portfolioIDKind) prefix() string             { return "portfolio" }
func (virtualAccountIDKind) prefix() string        { return "virtual_account" }
func (reservationIDKind) prefix() string           { return "reservation" }
func (opportunityIDKind) prefix() string           { return "opportunity" }
func (decisionIDKind) prefix() string              { return "decision" }
func (executionPlanIDKind) prefix() string         { return "execution_plan" }
func (virtualOrderIDKind) prefix() string          { return "virtual_order" }
func (executionAttemptIDKind) prefix() string      { return "execution_attempt" }
func (virtualFillIDKind) prefix() string           { return "virtual_fill" }
func (journalTransactionIDKind) prefix() string    { return "journal_transaction" }
func (modelVersionIDKind) prefix() string          { return "model_version" }
func (jobIDKind) prefix() string                   { return "job" }
func (commandIDKind) prefix() string               { return "command" }
func (leaseIDKind) prefix() string                 { return "lease" }
func (inboxMessageIDKind) prefix() string          { return "inbox_message" }
func (outboxMessageIDKind) prefix() string         { return "outbox_message" }
func (incidentIDKind) prefix() string              { return "incident" }
func (auditEventIDKind) prefix() string            { return "audit_event" }

// Stable compile-time-distinct identifier types for every V1A aggregate.
type (
	UserID                  = ID[userIDKind]
	SessionID               = ID[sessionIDKind]
	ConfigurationSnapshotID = ID[configurationSnapshotIDKind]
	AssetRegistryID         = ID[assetRegistryIDKind]
	InstrumentMetadataID    = ID[instrumentMetadataIDKind]
	MarketSegmentID         = ID[marketSegmentIDKind]
	DatasetID               = ID[datasetIDKind]
	RunID                   = ID[runIDKind]
	StrategyID              = ID[strategyIDKind]
	PortfolioID             = ID[portfolioIDKind]
	VirtualAccountID        = ID[virtualAccountIDKind]
	ReservationID           = ID[reservationIDKind]
	OpportunityID           = ID[opportunityIDKind]
	DecisionID              = ID[decisionIDKind]
	ExecutionPlanID         = ID[executionPlanIDKind]
	VirtualOrderID          = ID[virtualOrderIDKind]
	ExecutionAttemptID      = ID[executionAttemptIDKind]
	VirtualFillID           = ID[virtualFillIDKind]
	JournalTransactionID    = ID[journalTransactionIDKind]
	ModelVersionID          = ID[modelVersionIDKind]
	JobID                   = ID[jobIDKind]
	CommandID               = ID[commandIDKind]
	LeaseID                 = ID[leaseIDKind]
	InboxMessageID          = ID[inboxMessageIDKind]
	OutboxMessageID         = ID[outboxMessageIDKind]
	IncidentID              = ID[incidentIDKind]
	AuditEventID            = ID[auditEventIDKind]
)

// NewUserID constructs a user identifier.
func NewUserID(value string) (UserID, error) { return newID[userIDKind](value) }

// NewSessionID constructs a session identifier.
func NewSessionID(value string) (SessionID, error) { return newID[sessionIDKind](value) }

// NewConfigurationSnapshotID constructs a configuration snapshot identifier.
func NewConfigurationSnapshotID(value string) (ConfigurationSnapshotID, error) {
	return newID[configurationSnapshotIDKind](value)
}

// NewAssetRegistryID constructs an asset-registry identifier.
func NewAssetRegistryID(value string) (AssetRegistryID, error) {
	return newID[assetRegistryIDKind](value)
}

// NewInstrumentMetadataID constructs an instrument-metadata identifier.
func NewInstrumentMetadataID(value string) (InstrumentMetadataID, error) {
	return newID[instrumentMetadataIDKind](value)
}

// NewMarketSegmentID constructs a market-segment identifier.
func NewMarketSegmentID(value string) (MarketSegmentID, error) {
	return newID[marketSegmentIDKind](value)
}

// NewDatasetID constructs a dataset identifier.
func NewDatasetID(value string) (DatasetID, error) { return newID[datasetIDKind](value) }

// NewRunID constructs a run identifier.
func NewRunID(value string) (RunID, error) { return newID[runIDKind](value) }

// NewStrategyID constructs a strategy identifier.
func NewStrategyID(value string) (StrategyID, error) { return newID[strategyIDKind](value) }

// NewPortfolioID constructs a portfolio identifier.
func NewPortfolioID(value string) (PortfolioID, error) { return newID[portfolioIDKind](value) }

// NewVirtualAccountID constructs a virtual-account identifier.
func NewVirtualAccountID(value string) (VirtualAccountID, error) {
	return newID[virtualAccountIDKind](value)
}

// NewReservationID constructs a reservation identifier.
func NewReservationID(value string) (ReservationID, error) { return newID[reservationIDKind](value) }

// NewOpportunityID constructs an opportunity identifier.
func NewOpportunityID(value string) (OpportunityID, error) { return newID[opportunityIDKind](value) }

// NewDecisionID constructs a decision identifier.
func NewDecisionID(value string) (DecisionID, error) { return newID[decisionIDKind](value) }

// NewExecutionPlanID constructs an execution-plan identifier.
func NewExecutionPlanID(value string) (ExecutionPlanID, error) {
	return newID[executionPlanIDKind](value)
}

// NewVirtualOrderID constructs a virtual-order identifier.
func NewVirtualOrderID(value string) (VirtualOrderID, error) { return newID[virtualOrderIDKind](value) }

// NewExecutionAttemptID constructs an execution-attempt identifier.
func NewExecutionAttemptID(value string) (ExecutionAttemptID, error) {
	return newID[executionAttemptIDKind](value)
}

// NewVirtualFillID constructs a virtual-fill identifier.
func NewVirtualFillID(value string) (VirtualFillID, error) { return newID[virtualFillIDKind](value) }

// NewJournalTransactionID constructs a journal-transaction identifier.
func NewJournalTransactionID(value string) (JournalTransactionID, error) {
	return newID[journalTransactionIDKind](value)
}

// NewModelVersionID constructs a model-version identifier.
func NewModelVersionID(value string) (ModelVersionID, error) { return newID[modelVersionIDKind](value) }

// NewJobID constructs a job identifier.
func NewJobID(value string) (JobID, error) { return newID[jobIDKind](value) }

// NewCommandID constructs a command identifier.
func NewCommandID(value string) (CommandID, error) { return newID[commandIDKind](value) }

// NewLeaseID constructs a lease identifier.
func NewLeaseID(value string) (LeaseID, error) { return newID[leaseIDKind](value) }

// NewInboxMessageID constructs an inbox-message identifier.
func NewInboxMessageID(value string) (InboxMessageID, error) { return newID[inboxMessageIDKind](value) }

// NewOutboxMessageID constructs an outbox-message identifier.
func NewOutboxMessageID(value string) (OutboxMessageID, error) {
	return newID[outboxMessageIDKind](value)
}

// NewIncidentID constructs an incident identifier.
func NewIncidentID(value string) (IncidentID, error) { return newID[incidentIDKind](value) }

// NewAuditEventID constructs an audit-event identifier.
func NewAuditEventID(value string) (AuditEventID, error) { return newID[auditEventIDKind](value) }
