package sandbox

import (
	"encoding/hex"
	"strings"
	"time"

	"axiom/internal/domain"
)

// Arm and reauthorization lifetimes are fixed sandbox runtime security policy.
const (
	ArmLifetime             = 15 * time.Minute
	ReauthorizationLifetime = 2 * time.Minute
)

// Validate checks one capability descriptor without accepting unknown product
// or order-entry surfaces.
func (descriptor CapabilityDescriptor) Validate() error {
	wantEnvironment, ok := environmentFor(descriptor.Exchange)
	if !ok || descriptor.Environment != wantEnvironment || !descriptor.SpotOnly ||
		!descriptor.ReadAccount || !descriptor.WriteSpotOrders || !descriptor.RESTOrderEntry ||
		!descriptor.PrivateEvents || descriptor.ObservedAt.IsZero() ||
		descriptor.ObservedAt.Location() != time.UTC || !hash256(descriptor.CapabilityHash) ||
		len(descriptor.ProhibitedPermissions) != 0 ||
		!equalOrderStyles(descriptor.OrderStyles,
			[]OrderStyle{OrderStyleLimitGTC, OrderStyleLimitIOC, OrderStylePostOnly}) {
		return contractError("capability_invalid")
	}
	if !descriptor.HMACSHA256 {
		return contractError("hmac_sha256_required")
	}
	return nil
}

// Validate checks one redacted account identity.
func (identity AccountIdentity) Validate() error {
	wantEnvironment, ok := environmentFor(identity.Exchange)
	if !ok || identity.AccountID == "" || identity.Environment != wantEnvironment ||
		!hash256(identity.AccountIdentityHash) || !fingerprint(identity.KeyFingerprint) ||
		identity.CredentialGeneration == 0 || identity.ValidatedAt.IsZero() ||
		identity.ValidatedAt.Location() != time.UTC ||
		(identity.Exchange == ExchangeBinance && !identity.OwnerAttested) {
		return contractError("account_identity_invalid")
	}
	return nil
}

// Validate checks one authoritative account snapshot.
func (snapshot AccountSnapshot) Validate() error {
	if snapshot.AccountID == "" || snapshot.Epoch == 0 || len(snapshot.Balances) == 0 ||
		!hash256(snapshot.OrdersHash) || !hash256(snapshot.FillsHash) ||
		!hash256(snapshot.SnapshotHash) || snapshot.ObservedAt.IsZero() ||
		snapshot.ObservedAt.Location() != time.UTC {
		return contractError("account_snapshot_invalid")
	}
	previous := domain.AssetSymbol("")
	for _, balance := range snapshot.Balances {
		if balance.Asset <= previous {
			return contractError("account_snapshot_invalid")
		}
		if _, err := domain.ParseAssetSymbol(string(balance.Asset)); err != nil {
			return contractError("account_snapshot_invalid")
		}
		previous = balance.Asset
	}
	return nil
}

// Validate checks one exact capped, spot-only durable submission.
func (submission Submission) Validate(maximum domain.Notional) error {
	zero, _ := domain.ParseNotional("0")
	if submission.PlanID.Value() == "" || submission.OrderID.Value() == "" ||
		submission.AccountID == "" || submission.AccountEpoch == 0 ||
		!validClientOrderID(submission.ClientOrderID) ||
		!validSandboxStrategy(submission.StrategyID.Value()) ||
		(submission.Side != domain.SideBuy && submission.Side != domain.SideSell) ||
		submission.Notional.Compare(zero) <= 0 || submission.Notional.Compare(maximum) > 0 ||
		!validOrderStyle(submission.Style) || !validIntentAction(submission.Action) ||
		!hash256(submission.RequestHash) || !hash256(submission.PolicyHash) ||
		submission.ApprovedAt.IsZero() || submission.ApprovedAt.Location() != time.UTC {
		return contractError("submission_invalid")
	}
	instrument, err := domain.NewSpotInstrument(submission.Instrument.Base, submission.Instrument.Quote)
	if err != nil || instrument != submission.Instrument {
		return contractError("submission_invalid")
	}
	return nil
}

// ValidateFor proves that one durable reservation covers the exact asset and
// amount consumed by its immutable spot submission.
func (reservation DurableReservation) ValidateFor(submission Submission) error {
	if reservation.ID == "" || reservation.AccountID != submission.AccountID ||
		reservation.AccountEpoch != submission.AccountEpoch ||
		reservation.OrderID != submission.OrderID.String() ||
		(reservation.State != "" && reservation.State != ReservationWaiting &&
			reservation.State != ReservationActive) ||
		reservation.ReleasedAt != nil ||
		reservation.ReleaseReason != "" {
		return contractError("reservation_invalid")
	}
	value, err := domain.ParseBalance(reservation.Quantity)
	if err != nil {
		return contractError("reservation_invalid")
	}
	switch submission.Side {
	case domain.SideBuy:
		want, parseErr := domain.ParseBalance(submission.Notional.String())
		if parseErr != nil || reservation.Asset != string(submission.Instrument.Quote) ||
			value.Compare(want) != 0 {
			return contractError("reservation_invalid")
		}
	case domain.SideSell:
		want, parseErr := domain.ParseBalance(submission.Quantity.String())
		if parseErr != nil || reservation.Asset != string(submission.Instrument.Base) ||
			value.Compare(want) != 0 {
			return contractError("reservation_invalid")
		}
	default:
		return contractError("reservation_invalid")
	}
	return nil
}

// ValidateSubmissionTopology enforces the closed sandbox strategy and venue
// shape. Triangular plans are exact three-market cycles on one account;
// cross-exchange plans are paired opposite legs on Binance and Bybit. No
// other strategy is permitted to smuggle a multi-leg plan into the dispatcher.
func ValidateSubmissionTopology(
	submissions []Submission,
	exchangeByAccount map[AccountID]Exchange,
) error {
	if len(submissions) < 1 || len(submissions) > 3 {
		return contractError("submission_topology_invalid")
	}
	strategy := submissions[0].StrategyID.Value()
	if !validSandboxStrategy(strategy) {
		return contractError("submission_topology_invalid")
	}
	for _, submission := range submissions {
		exchange, exists := exchangeByAccount[submission.AccountID]
		if submission.StrategyID.Value() != strategy || !exists ||
			(exchange != ExchangeBinance && exchange != ExchangeBybit) {
			return contractError("submission_topology_invalid")
		}
	}
	switch strategy {
	case StrategyTrend, StrategyMeanReversion, StrategySandboxCanary:
		if len(submissions) != 1 {
			return contractError("submission_topology_invalid")
		}
		return nil
	case StrategyTriangular:
		return validateTriangularSubmissionTopology(submissions, exchangeByAccount)
	case StrategyCrossExchangeArbitrage:
		return validateCrossExchangeSubmissionTopology(submissions, exchangeByAccount)
	default:
		return contractError("submission_topology_invalid")
	}
}

func validateTriangularSubmissionTopology(
	submissions []Submission,
	exchangeByAccount map[AccountID]Exchange,
) error {
	if len(submissions) != 3 {
		return contractError("submission_topology_invalid")
	}
	account := submissions[0].AccountID
	epoch := submissions[0].AccountEpoch
	venue := exchangeByAccount[account]
	instruments := make(map[string]struct{}, len(submissions))
	inputs := make([]domain.AssetSymbol, len(submissions))
	outputs := make([]domain.AssetSymbol, len(submissions))
	for index, submission := range submissions {
		if submission.AccountID != account || submission.AccountEpoch != epoch ||
			exchangeByAccount[submission.AccountID] != venue {
			return contractError("submission_topology_invalid")
		}
		symbol := submission.Instrument.Symbol()
		if _, duplicate := instruments[symbol]; duplicate {
			return contractError("submission_topology_invalid")
		}
		instruments[symbol] = struct{}{}
		switch submission.Side {
		case domain.SideBuy:
			inputs[index], outputs[index] = submission.Instrument.Quote, submission.Instrument.Base
		case domain.SideSell:
			inputs[index], outputs[index] = submission.Instrument.Base, submission.Instrument.Quote
		default:
			return contractError("submission_topology_invalid")
		}
	}
	for index := range submissions {
		if outputs[index] != inputs[(index+1)%len(submissions)] {
			return contractError("submission_topology_invalid")
		}
	}
	return nil
}

func validateCrossExchangeSubmissionTopology(
	submissions []Submission,
	exchangeByAccount map[AccountID]Exchange,
) error {
	if len(submissions) != 2 || submissions[0].AccountID == submissions[1].AccountID ||
		submissions[0].AccountEpoch == 0 || submissions[1].AccountEpoch == 0 ||
		exchangeByAccount[submissions[0].AccountID] == exchangeByAccount[submissions[1].AccountID] ||
		submissions[0].Instrument != submissions[1].Instrument ||
		submissions[0].Side == submissions[1].Side ||
		submissions[0].Quantity.Compare(submissions[1].Quantity) != 0 {
		return contractError("submission_topology_invalid")
	}
	venues := map[Exchange]bool{
		exchangeByAccount[submissions[0].AccountID]: true,
		exchangeByAccount[submissions[1].AccountID]: true,
	}
	if !venues[ExchangeBinance] || !venues[ExchangeBybit] {
		return contractError("submission_topology_invalid")
	}
	return nil
}

// Validate checks one normalized private event and its canonical reducer fact.
func (event PrivateEvent) Validate() error {
	if event.Identity == "" || len(event.Identity) > 128 || event.AccountID == "" ||
		event.AccountEpoch == 0 || !validPrivateEventKind(event.Kind) ||
		event.OccurredAt.IsZero() || event.ReceivedAt.IsZero() ||
		event.OccurredAt.Location() != time.UTC || event.ReceivedAt.Location() != time.UTC ||
		event.ReceivedAt.Before(event.OccurredAt) || !hash256(event.NativeOrderHash) {
		return contractError("private_event_invalid")
	}
	switch event.Kind {
	case PrivateOrderEvent:
		if event.OrderEvent == nil || event.OrderID.Value() == "" ||
			event.ClientOrderID == "" || event.NativeFillHash != "" || event.BalanceHash != "" {
			return contractError("private_event_invalid")
		}
	case PrivateFillEvent:
		if event.OrderEvent == nil || event.OrderID.Value() == "" ||
			event.ClientOrderID == "" || !hash256(event.NativeFillHash) ||
			event.BalanceHash != "" {
			return contractError("private_event_invalid")
		}
	case PrivateBalanceEvent:
		if event.OrderEvent != nil || event.OrderID.Value() != "" ||
			event.ClientOrderID != "" || event.NativeFillHash != "" ||
			!hash256(event.BalanceHash) {
			return contractError("private_event_invalid")
		}
	}
	return nil
}

// Validate checks one exchange-authoritative reconciliation result.
func (result ReconciliationResult) Validate() error {
	if result.ID == "" || result.AccountID == "" || result.AccountEpoch == 0 ||
		(result.State != "clean" && result.State != "quarantined") ||
		!hash256(result.EvidenceHash) || result.ReconciledAt.IsZero() ||
		result.ReconciledAt.Location() != time.UTC {
		return contractError("reconciliation_invalid")
	}
	critical := false
	for _, difference := range result.Differences {
		if difference.Category == "" || difference.Classification == "" ||
			!hash256(difference.ExpectedHash) || !hash256(difference.ActualHash) {
			return contractError("reconciliation_invalid")
		}
		critical = critical || difference.Critical
	}
	if (result.State == "clean" && (len(result.Differences) != 0 || critical)) ||
		(result.State == "quarantined" && !critical) {
		return contractError("reconciliation_invalid")
	}
	return nil
}

// Active reports whether the arm currently permits new submission.
func (arm Arm) Active(now time.Time) bool {
	return arm.Validate() == nil && arm.RevokedAt == nil &&
		!now.Before(arm.CreatedAt) && now.Before(arm.ExpiresAt)
}

// Validate checks one 15-minute, one-revision manual arm.
func (arm Arm) Validate() error {
	if arm.ID == "" || arm.SessionID == "" || len(arm.AccountIDs) == 0 ||
		!hash256(arm.AuthorizationHash) || arm.ActorUserID == "" ||
		arm.ActorSessionID == "" || !hash256(arm.ReasonHash) ||
		arm.CreatedAt.IsZero() || arm.ExpiresAt.Sub(arm.CreatedAt) != ArmLifetime ||
		arm.CreatedAt.Location() != time.UTC || arm.ExpiresAt.Location() != time.UTC ||
		arm.Revision == 0 {
		return contractError("arm_invalid")
	}
	seen := make(map[AccountID]struct{}, len(arm.AccountIDs))
	for _, accountID := range arm.AccountIDs {
		if accountID == "" {
			return contractError("arm_invalid")
		}
		if _, exists := seen[accountID]; exists {
			return contractError("arm_invalid")
		}
		seen[accountID] = struct{}{}
	}
	if arm.RevokedAt != nil && (arm.RevokedAt.Location() != time.UTC ||
		arm.RevokedAt.Before(arm.CreatedAt)) {
		return contractError("arm_invalid")
	}
	return nil
}

func environmentFor(exchange Exchange) (Environment, bool) {
	switch exchange {
	case ExchangeBinance:
		return EnvironmentBinanceSpotTestnet, true
	case ExchangeBybit:
		return EnvironmentBybitDemo, true
	default:
		return "", false
	}
}

func validOrderStyle(style OrderStyle) bool {
	return style == OrderStyleLimitGTC || style == OrderStyleLimitIOC || style == OrderStylePostOnly
}

func validIntentAction(action IntentAction) bool {
	return action == IntentEntry || action == IntentExit || action == IntentCancel || action == IntentRecovery
}

func validSandboxStrategy(strategy string) bool {
	switch strategy {
	case StrategyTrend, StrategyMeanReversion, StrategyTriangular,
		StrategyCrossExchangeArbitrage, StrategySandboxCanary:
		return true
	default:
		return false
	}
}

func validPrivateEventKind(kind PrivateEventKind) bool {
	return kind == PrivateOrderEvent || kind == PrivateFillEvent || kind == PrivateBalanceEvent
}

func equalOrderStyles(left, right []OrderStyle) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hash256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

func fingerprint(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16 && strings.ToLower(value) == value
}

func validClientOrderID(value string) bool {
	if len(value) < 8 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '-' {
			return false
		}
	}
	return true
}

// Error is one bounded sandbox contract violation.
type Error struct{ Code string }

// Error returns the bounded sandbox contract code.
func (failure *Error) Error() string { return "sandbox:" + failure.Code }

func contractError(code string) error { return &Error{Code: code} }
