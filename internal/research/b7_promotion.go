package research

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/domain"
)

// Maturity is a research-governance label, independent of release status or
// any production-profitability claim.
type Maturity string

// V1 research maturity labels.
const (
	MaturityExperimental                Maturity = "EXPERIMENTAL"
	MaturityBacktestValidated           Maturity = "BACKTEST_VALIDATED"
	MaturityReplayValidated             Maturity = "REPLAY_VALIDATED"
	MaturityShadowValidated             Maturity = "SHADOW_VALIDATED"
	MaturitySandboxIntegrationValidated Maturity = "SANDBOX_INTEGRATION_VALIDATED"
	MaturityRejected                    Maturity = "REJECTED"
)

// PromotionRequest is one explicit optimistic-lock maturity command.
type PromotionRequest struct {
	StrategyVersionID string   `json:"strategy_version_id"`
	EvidenceID        string   `json:"evidence_id"`
	EvidenceHash      string   `json:"evidence_hash"`
	Target            Maturity `json:"target"`
	ExpectedRevision  uint64   `json:"expected_revision"`
	IdempotencyKey    string   `json:"idempotency_key"`
	Reason            string   `json:"reason"`
}

// PromotionCommand is the authenticated persistence command.
type PromotionCommand struct {
	PromotionRequest
	CommandID   string    `json:"command_id"`
	PayloadHash string    `json:"payload_hash"`
	ActorUserID string    `json:"actor_user_id"`
	SessionID   string    `json:"session_id"`
	CommandTime time.Time `json:"command_time"`
}

// PromotionResult is the durable idempotent maturity command result.
type PromotionResult struct {
	CommandID   string
	Outcome     string
	Maturity    Maturity
	Revision    uint64
	FailureCode string
}

// PromotionStore applies one authenticated promotion atomically.
type PromotionStore interface {
	ApplyPromotion(context.Context, PromotionCommand) (PromotionResult, error)
}

// PromotionService gates explicit research maturity commands.
type PromotionService struct {
	store PromotionStore
	clock domain.Clock
}

// NewPromotionService constructs the B7 authenticated command boundary.
func NewPromotionService(store PromotionStore, clock domain.Clock) (*PromotionService, error) {
	if store == nil || clock == nil {
		return nil, researchError("promotion_service_dependency_missing")
	}
	return &PromotionService{store: store, clock: clock}, nil
}

// Promote requires an authenticated permission and recent reauthentication,
// then delegates atomic idempotency/concurrency enforcement to persistence.
func (service *PromotionService) Promote(
	ctx context.Context,
	principal authentication.Principal,
	request PromotionRequest,
) (PromotionResult, error) {
	now := service.clock.Now().UTC
	if err := validatePromotionPrincipal(principal, now); err != nil {
		return PromotionResult{}, err
	}
	if !validPromotionRequest(request) {
		return PromotionResult{}, researchError("promotion_request_invalid")
	}
	commandID, err := promotionIdentifier()
	if err != nil {
		return PromotionResult{}, researchError("promotion_identifier_failed")
	}
	payload, err := json.Marshal(struct {
		Actor string `json:"actor"`
		PromotionRequest
	}{Actor: principal.UserID, PromotionRequest: request})
	if err != nil {
		return PromotionResult{}, researchError("promotion_payload_failed")
	}
	digest := sha256.Sum256(payload)
	command := PromotionCommand{PromotionRequest: request, CommandID: commandID,
		PayloadHash: hex.EncodeToString(digest[:]), ActorUserID: principal.UserID,
		SessionID: principal.SessionID, CommandTime: now}
	result, err := service.store.ApplyPromotion(ctx, command)
	if err != nil {
		return PromotionResult{}, err
	}
	if result.FailureCode != "" {
		return result, researchError(result.FailureCode)
	}
	return result, nil
}

// ValidMaturityTransition enforces the sequential statistical promotion state
// machine. Sandbox integration remains unavailable in V1B.
func ValidMaturityTransition(prior, target Maturity) bool {
	if target == MaturityRejected {
		return prior != MaturityRejected
	}
	switch prior {
	case MaturityExperimental:
		return target == MaturityBacktestValidated
	case MaturityBacktestValidated:
		return target == MaturityReplayValidated
	case MaturityReplayValidated:
		return target == MaturityShadowValidated
	default:
		return false
	}
}

func validatePromotionPrincipal(
	principal authentication.Principal,
	now time.Time,
) error {
	if principal.UserID == "" || principal.SessionID == "" ||
		principal.ReauthenticatedAt.Location() != time.UTC ||
		principal.ReauthenticatedAt.After(now) ||
		now.Sub(principal.ReauthenticatedAt) > authentication.RecentReauthentication ||
		authentication.RequirePermission(principal, "research.promote") != nil {
		return researchError("promotion_unauthorized")
	}
	return nil
}

func validPromotionRequest(request PromotionRequest) bool {
	validTarget := request.Target == MaturityBacktestValidated ||
		request.Target == MaturityReplayValidated ||
		request.Target == MaturityShadowValidated ||
		request.Target == MaturityRejected
	return validTarget &&
		researchIdentifier.MatchString(request.StrategyVersionID) &&
		researchIdentifier.MatchString(request.EvidenceID) &&
		validEvidenceHash(request.EvidenceHash) &&
		request.ExpectedRevision > 0 &&
		researchIdentifier.MatchString(request.IdempotencyKey) &&
		request.Reason != ""
}

func promotionIdentifier() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "promotion-" + hex.EncodeToString(value), nil
}
