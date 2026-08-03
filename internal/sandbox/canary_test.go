package sandbox

import (
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestBuildCanaryPlanUsesCompleteApprovalPipeline(t *testing.T) {
	intent, identifiers, approval := canaryFixture(t)
	plan, err := BuildCanaryPlan(intent, identifiers, approval)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Submissions) != 1 || len(plan.Reservations) != 1 ||
		plan.Submissions[0].StrategyID.Value() != StrategySandboxCanary ||
		plan.Submissions[0].Action != IntentEntry ||
		plan.Reservations[0].Asset != "USDT" ||
		plan.Reservations[0].Quantity != plan.Submissions[0].Notional.String() ||
		plan.Pipeline.IntentKind != ApprovalCanaryIntent ||
		plan.Pipeline.ValidateFor(plan) != nil {
		t.Fatalf("canary plan = %#v", plan)
	}
	maximum, _ := domain.ParseNotional("10")
	if plan.Submissions[0].Validate(maximum) != nil ||
		plan.Reservations[0].ValidateFor(plan.Submissions[0]) != nil {
		t.Fatal("canary plan failed dispatcher contracts")
	}
}

func TestBuildCanaryPlanFailsClosed(t *testing.T) {
	intent, identifiers, approval := canaryFixture(t)
	cases := []struct {
		name   string
		mutate func(*CanaryIntent, *CanaryApprovalContext)
	}{
		{"sell", func(value *CanaryIntent, _ *CanaryApprovalContext) {
			value.Side = domain.SideSell
		}},
		{"over cap", func(value *CanaryIntent, _ *CanaryApprovalContext) {
			value.LimitPrice, _ = domain.ParsePrice("10001")
		}},
		{"global disabled", func(_ *CanaryIntent, value *CanaryApprovalContext) {
			value.GlobalSubmissionEnabled = false
			value.EntrySafety.GlobalSubmissionEnabled = false
		}},
		{"stale eligibility", func(_ *CanaryIntent, value *CanaryApprovalContext) {
			value.Eligibility.ObservedAt = value.ApprovedAt.Add(-time.Second)
		}},
		{"unapproved asset", func(_ *CanaryIntent, value *CanaryApprovalContext) {
			value.AssetApproved = false
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate, context := intent, approval
			test.mutate(&candidate, &context)
			if _, err := BuildCanaryPlan(candidate, identifiers, context); err == nil {
				t.Fatal("unsafe canary accepted")
			}
		})
	}
}

func canaryFixture(
	t *testing.T,
) (CanaryIntent, CanaryPlanIdentifiers, CanaryApprovalContext) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0).UTC()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("0.001")
	price, _ := domain.ParsePrice("10000")
	account := AccountID("binance-testnet-canary")
	session := SessionID("canary-session")
	approval := canaryApprovalFixture(now, account, session)
	return CanaryIntent{
			ID: "canary-intent", Exchange: ExchangeBinance,
			AccountID: account, AccountEpoch: 1, Instrument: instrument,
			Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
			Style: OrderStyleLimitGTC, RequestedAt: now,
		},
		CanaryPlanIdentifiers{
			PlanID: "canary-plan", OrderID: "canary-order",
			ReservationID: "canary-reservation",
			ClientOrderID: "ax-canary-order",
		},
		approval
}

func canaryApprovalFixture(
	now time.Time,
	account AccountID,
	session SessionID,
) CanaryApprovalContext {
	arm := Arm{
		ID: "canary-arm", SessionID: session,
		AccountIDs:        []AccountID{account},
		AuthorizationHash: hashCanaryValues("authorization"),
		ActorUserID:       "owner", ActorSessionID: "owner-session",
		ReasonHash: hashCanaryValues("reason"),
		CreatedAt:  now, ExpiresAt: now.Add(ArmLifetime), Revision: 1,
	}
	eligibility := EligibilitySnapshot{
		ObservedAt: now, Exchange: "binance", Instrument: "BTCUSDT",
		BookHealthy: true, BookFresh: true, BookEligible: true,
		ClockEligible: true, Eligible: true,
	}
	safety := EntrySafetySnapshot{
		AccountID: account, AccountEpoch: 1, Exchange: ExchangeBinance,
		ObservedAt: now, State: EngineArmed, ArmActive: true,
		GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
		ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
		PublicEligible: true, PrivateStreamHealthy: true,
		AccountStateFresh: true, ReconciliationClean: true,
		LeaseHeld: true, EvidenceHealthy: true,
		OpenCapacityAvailable: true, DailyCapacityAvailable: true,
	}
	return CanaryApprovalContext{
		SessionID: session, Arm: arm,
		ConfigurationID: "canary-config", AssetApproved: true,
		GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
		ExchangeIntegrationEnabled: true,
		ExchangeSubmissionEnabled:  true,
		Eligibility:                eligibility, EntrySafety: safety, ApprovedAt: now,
	}
}
