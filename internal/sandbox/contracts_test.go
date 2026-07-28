package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestCapabilityAndAccountContractsRejectUnsafeEnvironmentOrPermission(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	descriptor := CapabilityDescriptor{
		Exchange: ExchangeBinance, Environment: EnvironmentBinanceSpotTestnet,
		SpotOnly: true, ReadAccount: true, WriteSpotOrders: true, RESTOrderEntry: true,
		PrivateEvents: true, HMACSHA256: true,
		OrderStyles:    []OrderStyle{OrderStyleLimitGTC, OrderStyleLimitIOC, OrderStylePostOnly},
		ObservedAt:     now,
		CapabilityHash: testHash("capability"),
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	descriptor.Environment = EnvironmentBybitDemo
	if descriptor.Validate() == nil {
		t.Fatal("cross-environment Binance capability accepted")
	}
	descriptor.Environment = EnvironmentBinanceSpotTestnet
	descriptor.ProhibitedPermissions = []string{"wallet"}
	if descriptor.Validate() == nil {
		t.Fatal("prohibited account permission accepted")
	}

	identity := AccountIdentity{AccountID: "binance-account", Exchange: ExchangeBinance,
		Environment: EnvironmentBinanceSpotTestnet, AccountIdentityHash: testHash("account"),
		KeyFingerprint: testFingerprint("key"), CredentialGeneration: 1, OwnerAttested: true,
		ValidatedAt: now}
	if err := identity.Validate(); err != nil {
		t.Fatal(err)
	}
	identity.OwnerAttested = false
	if identity.Validate() == nil {
		t.Fatal("unattested Binance Testnet identity accepted")
	}
}

func TestSubmissionContractIsExactSpotLimitAndCapped(t *testing.T) {
	maximum, _ := domain.ParseNotional("10")
	submission := validSubmission(t)
	if err := submission.Validate(maximum); err != nil {
		t.Fatal(err)
	}
	tooLarge, _ := domain.ParseNotional("10.00000001")
	submission.Notional = tooLarge
	if submission.Validate(maximum) == nil {
		t.Fatal("over-cap order accepted")
	}
	submission = validSubmission(t)
	submission.Style = "MARKET"
	if submission.Validate(maximum) == nil {
		t.Fatal("market order accepted")
	}
}

func TestReservationMatchesExactSubmissionAssetAndAmount(t *testing.T) {
	submission := validSubmission(t)
	reservation := DurableReservation{
		ID: "reservation-1", AccountID: submission.AccountID,
		AccountEpoch: submission.AccountEpoch, OrderID: submission.OrderID.String(),
		Asset: "USDT", Quantity: "5",
	}
	if err := reservation.ValidateFor(submission); err != nil {
		t.Fatal(err)
	}
	reservation.Asset = "BTC"
	if reservation.ValidateFor(submission) == nil {
		t.Fatal("buy reservation used base asset")
	}
	submission.Side = domain.SideSell
	reservation.Asset, reservation.Quantity = "BTC", submission.Quantity.String()
	if err := reservation.ValidateFor(submission); err != nil {
		t.Fatal(err)
	}
	reservation.Quantity = "5"
	if reservation.ValidateFor(submission) == nil {
		t.Fatal("sell reservation used quote notional")
	}
}

func TestArmExpiresWithoutBlockingCancellationContract(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	arm := Arm{ID: "arm-1", SessionID: "session-1", AccountIDs: []AccountID{"account-1"},
		AuthorizationHash: testHash("authorization"), ActorUserID: "owner",
		ActorSessionID: "browser-session", ReasonHash: testHash("reason"),
		CreatedAt: now, ExpiresAt: now.Add(ArmLifetime), Revision: 1}
	if !arm.Active(now.Add(ArmLifetime - time.Nanosecond)) {
		t.Fatal("valid arm inactive before expiry")
	}
	if arm.Active(now.Add(ArmLifetime)) {
		t.Fatal("arm active at expiry")
	}
	submission := validSubmission(t)
	submission.Action = IntentCancel
	maximum, _ := domain.ParseNotional("10")
	if err := submission.Validate(maximum); err != nil {
		t.Fatalf("cancel contract rejected while arm state is independent: %v", err)
	}
}

func FuzzPrivateEventContract(f *testing.F) {
	f.Add([]byte(`{"identity":"event","account_id":"account","account_epoch":1}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		var event PrivateEvent
		if json.Unmarshal(encoded, &event) != nil || event.Validate() != nil {
			return
		}
		if event.Kind != PrivateOrderEvent && event.Kind != PrivateFillEvent &&
			event.Kind != PrivateBalanceEvent {
			t.Fatalf("unknown private event kind accepted: %q", event.Kind)
		}
	})
}

func validSubmission(t *testing.T) Submission {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("sandbox-plan")
	orderID, _ := domain.NewVirtualOrderID("sandbox-order")
	strategyID, _ := domain.NewStrategyID("trend")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("0.0001")
	price, _ := domain.ParsePrice("50000")
	notional, _ := domain.ParseNotional("5")
	return Submission{
		PlanID: planID, OrderID: orderID, AccountID: "binance-account", AccountEpoch: 1,
		ClientOrderID: "ax-0123456789abcdef", StrategyID: strategyID,
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
		Notional: notional, Style: OrderStyleLimitGTC, Action: IntentEntry,
		RequestHash: testHash("request"), PolicyHash: testHash("policy"),
		ApprovedAt: time.Unix(1_800_000_000, 0).UTC(),
	}
}

func testHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func testFingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}
