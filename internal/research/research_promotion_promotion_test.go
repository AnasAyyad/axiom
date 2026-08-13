package research

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/domain"
)

type promotionStoreStub struct {
	command PromotionCommand
	result  PromotionResult
	err     error
}

func (store *promotionStoreStub) ApplyPromotion(
	_ context.Context,
	command PromotionCommand,
) (PromotionResult, error) {
	store.command = command
	return store.result, store.err
}

func TestResearchPromotionPromotionRequiresRecentAuthenticationAndExplicitRequest(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	clock, _ := domain.NewReplayClock(now)
	store := &promotionStoreStub{result: PromotionResult{CommandID: "stored-command",
		Outcome: "applied", Maturity: MaturityBacktestValidated, Revision: 2}}
	service, err := NewPromotionService(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	principal := authentication.Principal{UserID: "owner-research_promotion", SessionID: "session-research_promotion", ReauthenticatedAt: now}
	request := PromotionRequest{StrategyVersionID: "trend-v1", EvidenceID: "suite-research_promotion",
		EvidenceHash: strings.Repeat("a", 64), Target: MaturityBacktestValidated,
		ExpectedRevision: 1, IdempotencyKey: "promotion-research_promotion-1", Reason: "registered gates passed"}
	result, err := service.Promote(context.Background(), principal, request)
	if err != nil || result.Maturity != MaturityBacktestValidated ||
		store.command.ActorUserID != principal.UserID ||
		store.command.SessionID != principal.SessionID ||
		!validEvidenceHash(store.command.PayloadHash) {
		t.Fatalf("promotion = %#v %#v %v", result, store.command, err)
	}

	principal.ReauthenticatedAt = now.Add(-authentication.RecentReauthentication - time.Second)
	if _, err = service.Promote(context.Background(), principal, request); err == nil {
		t.Fatal("stale-authentication promotion accepted")
	}
	request.Target = MaturitySandboxIntegrationValidated
	principal.ReauthenticatedAt = now
	if _, err = service.Promote(context.Background(), principal, request); err == nil {
		t.Fatal("multi-strategy research sandbox maturity accepted")
	}
	request.Target = Maturity("UNKNOWN")
	if _, err = service.Promote(context.Background(), principal, request); err == nil {
		t.Fatal("unknown maturity accepted")
	}
}

func TestResearchPromotionPromotionReturnsDurablePolicyRejection(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	clock, _ := domain.NewReplayClock(now)
	store := &promotionStoreStub{result: PromotionResult{CommandID: "stored-command",
		Outcome: "rejected", Maturity: MaturityExperimental, Revision: 1,
		FailureCode: "promotion_evidence_ineligible"}}
	service, _ := NewPromotionService(store, clock)
	principal := authentication.Principal{UserID: "owner-research_promotion", SessionID: "session-research_promotion",
		ReauthenticatedAt: now}
	request := PromotionRequest{StrategyVersionID: "trend-v1", EvidenceID: "suite-research_promotion",
		EvidenceHash: strings.Repeat("a", 64), Target: MaturityBacktestValidated,
		ExpectedRevision: 1, IdempotencyKey: "promotion-research_promotion-1", Reason: "review"}
	result, err := service.Promote(context.Background(), principal, request)
	var researchFailure Error
	if !errors.As(err, &researchFailure) ||
		researchFailure.Code != "promotion_evidence_ineligible" ||
		result.Outcome != "rejected" {
		t.Fatalf("durable rejection = %#v %v", result, err)
	}
}

func TestResearchPromotionMaturityTransitionsAreSequentialAndRejectable(t *testing.T) {
	valid := [][2]Maturity{
		{MaturityExperimental, MaturityBacktestValidated},
		{MaturityBacktestValidated, MaturityReplayValidated},
		{MaturityReplayValidated, MaturityShadowValidated},
		{MaturityExperimental, MaturityRejected},
		{MaturityShadowValidated, MaturityRejected},
	}
	for _, transition := range valid {
		if !ValidMaturityTransition(transition[0], transition[1]) {
			t.Fatalf("valid transition rejected: %s -> %s", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]Maturity{
		{MaturityExperimental, MaturityReplayValidated},
		{MaturityBacktestValidated, MaturityShadowValidated},
		{MaturityShadowValidated, MaturitySandboxIntegrationValidated},
		{MaturityRejected, MaturityBacktestValidated},
	} {
		if ValidMaturityTransition(transition[0], transition[1]) {
			t.Fatalf("invalid transition accepted: %s -> %s", transition[0], transition[1])
		}
	}
}
