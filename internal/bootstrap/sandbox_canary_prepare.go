package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func prepareSandboxCanary(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *postgresstore.V1CDispatcherStore,
	runtimeConfig config.Runtime,
	product config.Configuration,
	configurationID string,
	exchange sandbox.Exchange,
	inputFile string,
) (string, error) {
	request, err := readSandboxCanaryRequest(inputFile)
	if err != nil {
		return "", err
	}
	defer clearSandboxCanaryRequest(&request)
	return prepareSandboxCanaryRequest(
		ctx, pool, store, runtimeConfig, product,
		configurationID, exchange, request,
	)
}

func prepareSandboxCanaryRequest(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *postgresstore.V1CDispatcherStore,
	runtimeConfig config.Runtime,
	product config.Configuration,
	configurationID string,
	exchange sandbox.Exchange,
	request sandboxCanaryRequest,
) (string, error) {
	policy, err := loadSandboxCanaryPolicy(product, exchange, request)
	if err != nil {
		return "", err
	}
	preparation, err := beginSandboxCanary(
		ctx, pool, store, runtimeConfig, product, request,
		configurationID, exchange, policy.instrument,
	)
	if err != nil {
		return "", err
	}
	armed, succeeded := false, false
	defer func() {
		if !succeeded {
			stopFailedSandboxCanary(store, preparation.session, armed)
		}
	}()
	planID, armed, err := runArmedSandboxCanary(
		ctx,
		store,
		preparation.session,
		preparation.principal,
		preparation.consumed,
		configurationID,
		exchange,
		policy,
		preparation.identifiers,
	)
	if err != nil {
		return "", err
	}
	succeeded = true
	return planID, nil
}

type sandboxCanaryPreparation struct {
	principal   authentication.Principal
	consumed    authentication.ConsumedAuthorization
	identifiers sandboxCanaryIdentifiers
	session     sandbox.CanarySession
}

func beginSandboxCanary(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *postgresstore.V1CDispatcherStore,
	runtimeConfig config.Runtime,
	product config.Configuration,
	request sandboxCanaryRequest,
	configurationID string,
	exchange sandbox.Exchange,
	instrument domain.Instrument,
) (sandboxCanaryPreparation, error) {
	principal, consumed, err := authorizeSandboxCanary(
		ctx, pool, runtimeConfig, product, request,
	)
	if err != nil {
		return sandboxCanaryPreparation{}, err
	}
	identifiers, err := newSandboxCanaryIdentifiers(exchange)
	if err != nil {
		return sandboxCanaryPreparation{}, err
	}
	session, err := openSandboxCanarySession(
		ctx, store, configurationID, exchange, instrument,
		principal.UserID, identifiers,
	)
	if err != nil {
		return sandboxCanaryPreparation{}, err
	}
	return sandboxCanaryPreparation{
		principal: principal, consumed: consumed,
		identifiers: identifiers, session: session,
	}, nil
}

type sandboxCanaryPolicy struct {
	switches   [4]bool
	instrument domain.Instrument
	quantity   domain.Quantity
	price      domain.Price
	style      sandbox.OrderStyle
}

func loadSandboxCanaryPolicy(
	product config.Configuration,
	exchange sandbox.Exchange,
	request sandboxCanaryRequest,
) (sandboxCanaryPolicy, error) {
	switches, enabled := canarySwitches(product, exchange)
	instrument, assetApproved := canaryInstrument(
		product,
		request.Instrument,
	)
	quantity, quantityErr := domain.ParseQuantity(request.Quantity)
	price, priceErr := domain.ParsePrice(request.LimitPrice)
	style := sandbox.OrderStyle(request.Style)
	styleAllowed := style == sandbox.OrderStyleLimitGTC ||
		style == sandbox.OrderStyleLimitIOC ||
		style == sandbox.OrderStylePostOnly
	if !enabled || !assetApproved || quantityErr != nil ||
		priceErr != nil || !styleAllowed {
		return sandboxCanaryPolicy{},
			fmt.Errorf("sandbox_canary_policy_rejected")
	}
	return sandboxCanaryPolicy{
		switches: switches, instrument: instrument,
		quantity: quantity, price: price, style: style,
	}, nil
}

func openSandboxCanarySession(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	configurationID string,
	exchange sandbox.Exchange,
	instrument domain.Instrument,
	userID string,
	identifiers sandboxCanaryIdentifiers,
) (sandbox.CanarySession, error) {
	return createFreshCanarySession(
		ctx,
		store,
		sandbox.CanarySessionCommand{
			ID:       sandbox.SessionID(identifiers.sessionID),
			Exchange: exchange, Instrument: instrument.Symbol(),
			ConfigurationID: configurationID,
			StrategySetHash: canaryHash(
				configurationID,
				sandbox.StrategySandboxCanary,
			),
			CreatedBy: userID,
		},
	)
}

func stopFailedSandboxCanary(
	store *postgresstore.V1CDispatcherStore,
	session sandbox.CanarySession,
	riskLock bool,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = store.StopCanarySession(
		ctx,
		session.ID,
		session.AccountID,
		riskLock,
		time.Now().UTC(),
	)
}

func runArmedSandboxCanary(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	session sandbox.CanarySession,
	principal authentication.Principal,
	consumed authentication.ConsumedAuthorization,
	configurationID string,
	exchange sandbox.Exchange,
	policy sandboxCanaryPolicy,
	identifiers sandboxCanaryIdentifiers,
) (string, bool, error) {
	arm, err := createCanaryArm(
		ctx,
		store,
		session,
		principal,
		consumed,
		identifiers.armID,
	)
	if err != nil {
		return "", false, err
	}
	plan, cycle, err := buildAndApproveSandboxCanary(
		ctx,
		store,
		session,
		arm,
		configurationID,
		exchange,
		policy,
		identifiers,
	)
	if err != nil {
		return "", true, err
	}
	if err = exerciseSandboxCanary(
		ctx,
		store,
		session,
		exchange,
		plan,
		cycle,
	); err != nil {
		return "", true, err
	}
	return plan.ID, true, nil
}

func buildAndApproveSandboxCanary(
	ctx context.Context,
	store *postgresstore.V1CDispatcherStore,
	session sandbox.CanarySession,
	arm sandbox.Arm,
	configurationID string,
	exchange sandbox.Exchange,
	policy sandboxCanaryPolicy,
	identifiers sandboxCanaryIdentifiers,
) (sandbox.ApprovedSandboxPlan, uint64, error) {
	eligibility, safety, cycle, approvedAt, err := waitCanaryAdmission(
		ctx,
		store,
		session,
		arm.ID,
		policy.instrument.Symbol(),
		policy.switches,
	)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, 0, err
	}
	plan, err := buildSandboxCanaryPlan(
		session, arm, configurationID, exchange,
		policy, identifiers, eligibility, safety, approvedAt,
	)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, 0, err
	}
	if err = store.ApprovePlan(
		ctx,
		plan,
		sandbox.SubmissionLimits{
			MaximumOrderNotional: "10", MaximumDailyNotional: "50",
			MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
		},
		sandbox.NoKillPoint{},
	); err != nil {
		return sandbox.ApprovedSandboxPlan{}, 0, err
	}
	return plan, cycle, nil
}

func buildSandboxCanaryPlan(
	session sandbox.CanarySession,
	arm sandbox.Arm,
	configurationID string,
	exchange sandbox.Exchange,
	policy sandboxCanaryPolicy,
	identifiers sandboxCanaryIdentifiers,
	eligibility sandbox.EligibilitySnapshot,
	safety sandbox.EntrySafetySnapshot,
	approvedAt time.Time,
) (sandbox.ApprovedSandboxPlan, error) {
	intent := sandbox.CanaryIntent{
		ID: identifiers.intentID, Exchange: exchange,
		AccountID: session.AccountID, AccountEpoch: session.AccountEpoch,
		Instrument: policy.instrument, Side: domain.SideBuy,
		Quantity: policy.quantity, LimitPrice: policy.price, Style: policy.style,
		RequestedAt: approvedAt,
	}
	return sandbox.BuildCanaryPlan(
		intent,
		sandbox.CanaryPlanIdentifiers{
			PlanID: identifiers.planID, OrderID: identifiers.orderID,
			ReservationID: identifiers.reservationID,
			ClientOrderID: identifiers.clientOrderID,
		},
		sandbox.CanaryApprovalContext{
			SessionID: session.ID, Arm: arm,
			ConfigurationID: configurationID, AssetApproved: true,
			GlobalIntegrationEnabled:   policy.switches[0],
			GlobalSubmissionEnabled:    policy.switches[1],
			ExchangeIntegrationEnabled: policy.switches[2],
			ExchangeSubmissionEnabled:  policy.switches[3],
			Eligibility:                eligibility, EntrySafety: safety,
			ApprovedAt: approvedAt,
		},
	)
}
