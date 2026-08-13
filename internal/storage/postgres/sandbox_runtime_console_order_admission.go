package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

type sandboxQualificationOrderAdmission struct {
	sessionID, armID, accountID, configurationID string
	exchange                                     sandbox.Exchange
	epoch                                        uint64
	arm                                          sandbox.Arm
	switches                                     [4]bool
	assetApproved                                bool
}

type sandboxQualificationOrderAdmissionFacts struct {
	revision, epoch, armRevision                       int64
	environment, sessionState, createdBy, strategyHash string
	armActor, armSession, reasonHash, authorizationID  string
	createdAt, expiresAt                               time.Time
	revokedAt                                          *time.Time
}

const sandboxQualificationOrderAdmissionSQL = `
SELECT session.id,session.state,session.revision,session.configuration_id,
       session.strategy_set_hash,session.created_by,
       membership.account_id,membership.account_epoch,account.exchange,
       account.environment,arm.id,arm.authorization_id,arm.actor_user_id,
       arm.actor_session_id,arm.reason_hash,arm.created_at,arm.expires_at,
       arm.revoked_at,arm.revision
FROM sandbox_runtime_sandbox_sessions session
JOIN sandbox_runtime_sandbox_session_accounts membership
  ON membership.session_id=session.id
JOIN sandbox_runtime_exchange_accounts account
  ON account.id=membership.account_id
 AND account.current_epoch=membership.account_epoch
JOIN sandbox_runtime_sandbox_arms arm ON arm.sandbox_session_id=session.id
WHERE session.id=$1 AND membership.account_id=$2 AND arm.id=$3`

const sandboxQualificationConfigurationSQL = `
SELECT canonical_payload FROM configuration_versions WHERE id=$1`

// CreateSandboxTestOrder persists a plan through the existing typed SandboxRuntime
// approval and dispatcher path. The API process performs no network I/O.
func (store *OwnerConsoleStore) CreateSandboxTestOrder(
	ctx context.Context,
	principal authentication.Principal,
	key string,
	body generated.SandboxTestOrderRequest,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginSandboxQualificationCommand(
		ctx, principal, key, "sandbox.order_create", "sandbox_session",
		body.SessionId, body.Reason, expected,
		map[string]any{"body": body},
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	instrument, quantity, price, style, err := sandboxQualificationOrderValues(body)
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_invalid")
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	now := store.clock.Now().UTC
	admission, err := store.sandboxQualificationOrderAdmission(
		ctx, principal, body, uint64(*expected), now, instrument,
	)
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_precondition")
		return generated.CommandAccepted{}, err
	}
	orderID, planID, err := store.submitSandboxQualificationOrderPlan(
		ctx, pending, admission, instrument, quantity, price, style, now,
	)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	return store.completeSandboxQualificationCommand(
		ctx, pending, principal, "sandbox.order_create", orderID,
		map[string]any{
			"order_id": orderID, "plan_id": planID,
			"state": "APPROVED", "real_trading_enabled": false,
		},
	)
}

func (store *OwnerConsoleStore) submitSandboxQualificationOrderPlan(
	ctx context.Context,
	pending sandboxQualificationPendingCommand,
	admission sandboxQualificationOrderAdmission,
	instrument domain.Instrument,
	quantity domain.Quantity,
	price domain.Price,
	style sandbox.OrderStyle,
	now time.Time,
) (string, string, error) {
	eligibility, safety, _, err := store.sandbox_runtime.CanaryAdmission(
		ctx, sandbox.SessionID(admission.sessionID), admission.armID,
		sandbox.AccountID(admission.accountID), admission.exchange,
		instrument.Symbol(), now, admission.switches,
	)
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_admission_rejected")
		return "", "", console.ErrPrecondition
	}
	planID := sandboxQualificationStableIdentifier("plan", pending.accepted.Id)
	orderID := sandboxQualificationStableIdentifier("order", pending.accepted.Id)
	plan, err := buildSandboxQualificationCanaryPlan(
		pending, admission, instrument, quantity, price, style,
		eligibility, safety, planID, orderID, now,
	)
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_pipeline_rejected")
		return "", "", console.ErrPrecondition
	}
	if err = store.sandbox_runtime.ApprovePlan(
		ctx, plan, sandboxQualificationSubmissionLimits, sandbox.NoKillPoint{},
	); err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_order_dispatch_rejected")
		return "", "", sandboxQualificationConsoleError(err)
	}
	return orderID, planID, nil
}

func buildSandboxQualificationCanaryPlan(
	pending sandboxQualificationPendingCommand,
	admission sandboxQualificationOrderAdmission,
	instrument domain.Instrument,
	quantity domain.Quantity,
	price domain.Price,
	style sandbox.OrderStyle,
	eligibility sandbox.EligibilitySnapshot,
	safety sandbox.EntrySafetySnapshot,
	planID, orderID string,
	now time.Time,
) (sandbox.ApprovedSandboxPlan, error) {
	return sandbox.BuildCanaryPlan(
		sandbox.CanaryIntent{
			ID:       sandboxQualificationStableIdentifier("intent", pending.accepted.Id),
			Exchange: admission.exchange, AccountID: sandbox.AccountID(admission.accountID),
			AccountEpoch: admission.epoch, Instrument: instrument,
			Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
			Style: style, RequestedAt: now,
		},
		sandbox.CanaryPlanIdentifiers{
			PlanID: planID, OrderID: orderID,
			ReservationID: sandboxQualificationStableIdentifier("reservation", pending.accepted.Id),
			ClientOrderID: sandboxQualificationStableIdentifier("sandbox-qualification", pending.accepted.Id),
		},
		sandbox.CanaryApprovalContext{
			SessionID: sandbox.SessionID(admission.sessionID),
			Arm:       admission.arm, ConfigurationID: admission.configurationID,
			AssetApproved:              admission.assetApproved,
			GlobalIntegrationEnabled:   admission.switches[0],
			GlobalSubmissionEnabled:    admission.switches[1],
			ExchangeIntegrationEnabled: admission.switches[2],
			ExchangeSubmissionEnabled:  admission.switches[3],
			Eligibility:                eligibility, EntrySafety: safety, ApprovedAt: now,
		},
	)
}

func (store *OwnerConsoleStore) sandboxQualificationOrderAdmission(
	ctx context.Context,
	principal authentication.Principal,
	body generated.SandboxTestOrderRequest,
	expectedRevision uint64,
	now time.Time,
	instrument domain.Instrument,
) (sandboxQualificationOrderAdmission, error) {
	result, facts, err := store.loadSandboxQualificationOrderAdmission(ctx, body)
	if err != nil {
		return sandboxQualificationOrderAdmission{}, err
	}
	if facts.sessionState != "ARMED" || uint64(facts.revision) != expectedRevision ||
		facts.createdBy != principal.UserID || facts.armActor != principal.UserID ||
		facts.armSession != principal.SessionID || facts.revokedAt != nil ||
		!now.Before(facts.expiresAt.UTC()) ||
		string(result.exchange) != string(body.Exchange) ||
		!sandboxQualificationEnvironmentAllowed(result.exchange, facts.environment) ||
		facts.strategyHash != stableSandboxRuntimeHash(
			result.configurationID, sandbox.StrategySandboxCanary,
		) {
		return sandboxQualificationOrderAdmission{}, console.ErrPrecondition
	}
	product, err := store.sandboxQualificationConfiguration(ctx, result.configurationID)
	if err != nil {
		return sandboxQualificationOrderAdmission{}, console.ErrPrecondition
	}
	result.switches, result.assetApproved = sandboxQualificationPolicyFacts(
		product, result.exchange, instrument,
	)
	if !result.switches[0] || !result.switches[1] ||
		!result.switches[2] || !result.switches[3] ||
		!result.assetApproved {
		return sandboxQualificationOrderAdmission{}, console.ErrPrecondition
	}
	result.epoch = uint64(facts.epoch)
	result.arm = sandbox.Arm{
		ID: result.armID, SessionID: sandbox.SessionID(result.sessionID),
		AccountIDs:        []sandbox.AccountID{sandbox.AccountID(result.accountID)},
		AuthorizationHash: stableSandboxRuntimeHash(facts.authorizationID),
		ActorUserID:       facts.armActor, ActorSessionID: facts.armSession,
		ReasonHash: facts.reasonHash, CreatedAt: facts.createdAt.UTC(),
		ExpiresAt: facts.expiresAt.UTC(), RevokedAt: utcPointer(facts.revokedAt),
		Revision: uint64(facts.armRevision),
	}
	return result, nil
}

func (store *OwnerConsoleStore) loadSandboxQualificationOrderAdmission(
	ctx context.Context,
	body generated.SandboxTestOrderRequest,
) (sandboxQualificationOrderAdmission, sandboxQualificationOrderAdmissionFacts, error) {
	var result sandboxQualificationOrderAdmission
	var facts sandboxQualificationOrderAdmissionFacts
	err := store.pool.QueryRow(
		ctx, sandboxQualificationOrderAdmissionSQL, body.SessionId, body.AccountId, body.ArmId,
	).Scan(
		&result.sessionID, &facts.sessionState, &facts.revision,
		&result.configurationID, &facts.strategyHash, &facts.createdBy,
		&result.accountID, &facts.epoch, &result.exchange, &facts.environment,
		&result.armID, &facts.authorizationID, &facts.armActor,
		&facts.armSession, &facts.reasonHash, &facts.createdAt,
		&facts.expiresAt, &facts.revokedAt, &facts.armRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, facts, console.ErrNotFound
	}
	return result, facts, err
}

func (store *OwnerConsoleStore) sandboxQualificationConfiguration(
	ctx context.Context,
	configurationID string,
) (config.Configuration, error) {
	var payload []byte
	if err := store.pool.QueryRow(ctx, sandboxQualificationConfigurationSQL,
		configurationID,
	).Scan(&payload); err != nil {
		return config.Configuration{}, err
	}
	var product config.Configuration
	if json.Unmarshal(payload, &product) != nil || config.Validate(product) != nil ||
		product.SchemaVersion != config.SchemaVersionSandboxRuntime {
		return config.Configuration{}, fmt.Errorf("sandbox_qualification_configuration_rejected")
	}
	return product, nil
}

func sandboxQualificationPolicyFacts(
	product config.Configuration,
	exchange sandbox.Exchange,
	instrument domain.Instrument,
) ([4]bool, bool) {
	switches := [4]bool{
		product.Sandbox.IntegrationsEnabled,
		product.Sandbox.SubmissionEnabled,
		false,
		false,
	}
	for _, candidate := range product.Sandbox.Exchanges {
		if candidate.ID == string(exchange) {
			switches[2] = candidate.IntegrationEnabled
			switches[3] = candidate.SubmissionEnabled
		}
	}
	baseApproved, quoteApproved := false, false
	for _, asset := range product.Assets {
		if asset.Symbol == instrument.Base && asset.Status == domain.AssetApproved {
			baseApproved = true
		}
		if asset.Symbol == instrument.Quote && asset.Status == domain.AssetApproved {
			quoteApproved = true
		}
	}
	return switches, baseApproved && quoteApproved
}

func sandboxQualificationEnvironmentAllowed(exchange sandbox.Exchange, environment string) bool {
	return (exchange == sandbox.ExchangeBinance &&
		environment == string(sandbox.EnvironmentBinanceSpotTestnet)) ||
		(exchange == sandbox.ExchangeBybit &&
			environment == string(sandbox.EnvironmentBybitDemo))
}

func sandboxQualificationOrderValues(
	body generated.SandboxTestOrderRequest,
) (domain.Instrument, domain.Quantity, domain.Price, sandbox.OrderStyle, error) {
	base := domain.AssetSymbol("BTC")
	if body.Instrument == generated.SandboxTestOrderRequestInstrumentETHUSDT {
		base = "ETH"
	}
	instrument, instrumentErr := domain.NewSpotInstrument(base, "USDT")
	quantity, quantityErr := domain.ParseQuantity(string(body.Quantity))
	price, priceErr := domain.ParsePrice(string(body.LimitPrice))
	style := sandbox.OrderStyle(body.Style)
	if instrumentErr != nil || quantityErr != nil || priceErr != nil ||
		(body.Instrument != generated.SandboxTestOrderRequestInstrumentBTCUSDT &&
			body.Instrument != generated.SandboxTestOrderRequestInstrumentETHUSDT) ||
		body.Side != generated.SandboxTestOrderRequestSideBuy ||
		(style != sandbox.OrderStyleLimitGTC &&
			style != sandbox.OrderStyleLimitIOC &&
			style != sandbox.OrderStylePostOnly) {
		return domain.Instrument{}, domain.Quantity{}, domain.Price{}, "",
			console.ErrInvalidRequest
	}
	return instrument, quantity, price, style, nil
}
