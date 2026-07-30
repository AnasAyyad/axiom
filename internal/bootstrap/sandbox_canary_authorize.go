package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"axiom/internal/authentication"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
	"axiom/internal/security"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

type sandboxCanaryIdentifiers struct {
	intentID, sessionID, armID, planID    string
	orderID, reservationID, clientOrderID string
}

func newSandboxCanaryIdentifiers(
	exchange sandbox.Exchange,
) (sandboxCanaryIdentifiers, error) {
	suffix, err := randomCanaryIdentifier("v1c")
	if err != nil {
		return sandboxCanaryIdentifiers{}, err
	}
	shortExchange := "b"
	if exchange == sandbox.ExchangeBybit {
		shortExchange = "y"
	}
	base := "canary-" + shortExchange + "-" + strings.TrimPrefix(suffix, "v1c-")
	return sandboxCanaryIdentifiers{
		intentID: "intent-" + base, sessionID: "session-" + base,
		armID: "arm-" + base, planID: "plan-" + base,
		orderID: "order-" + base, reservationID: "reservation-" + base,
		clientOrderID: "ax-" + base,
	}, nil
}

func clearSandboxCanaryRequest(request *sandboxCanaryRequest) {
	request.Email = ""
	request.Password = ""
	request.TOTP = ""
	request.Reason = ""
	request.Instrument = ""
	request.Side = ""
	request.Quantity = ""
	request.LimitPrice = ""
	request.Style = ""
}

func authorizeSandboxCanary(
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	product config.Configuration,
	request sandboxCanaryRequest,
) (authentication.Principal, authentication.ConsumedAuthorization, error) {
	loginService, authorizationService, err :=
		newSandboxCanaryAuthorizationServices(pool, runtimeConfig, product)
	if err != nil {
		return authentication.Principal{},
			authentication.ConsumedAuthorization{}, err
	}
	principal, err := loginSandboxCanary(ctx, loginService, request)
	if err != nil {
		return authentication.Principal{},
			authentication.ConsumedAuthorization{}, err
	}
	consumed, err := consumeSandboxCanaryAuthorization(
		ctx, authorizationService, principal, request,
	)
	return principal, consumed, err
}

func newSandboxCanaryAuthorizationServices(
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	product config.Configuration,
) (
	*authentication.Service,
	*authentication.SandboxAuthorizationService,
	error,
) {
	users, authorizations, err := newSandboxCanaryAuthenticationStores(pool)
	if err != nil {
		return nil, nil, err
	}
	csrfKey, err := security.ReadSecretFile(
		runtimeConfig.Authentication.CSRFKeyFile,
	)
	if err != nil {
		return nil, nil,
			fmt.Errorf("sandbox_canary_authentication_unavailable")
	}
	clock := &domain.SystemClock{}
	loginService, err := authentication.NewService(
		users, clock, []byte(csrfKey),
	)
	csrfKey = ""
	if err != nil {
		return nil, nil,
			fmt.Errorf("sandbox_canary_authentication_unavailable")
	}
	seedPath, err := totpSeedPath(product)
	if err != nil {
		return nil, nil, err
	}
	authorizationService, err :=
		authentication.NewSandboxAuthorizationService(
			users, authorizations, clock, seedPath,
		)
	if err != nil {
		return nil, nil,
			fmt.Errorf("sandbox_canary_reauthentication_failed")
	}
	return loginService, authorizationService, nil
}

func newSandboxCanaryAuthenticationStores(
	pool *pgxpool.Pool,
) (
	*postgresstore.A11AuthenticationStore,
	*postgresstore.V1CAuthenticationStore,
	error,
) {
	users, err := postgresstore.NewA11AuthenticationStore(pool)
	if err != nil {
		return nil, nil, err
	}
	authorizations, err := postgresstore.NewV1CAuthenticationStore(pool)
	if err != nil {
		return nil, nil, err
	}
	return users, authorizations, nil
}

func loginSandboxCanary(
	ctx context.Context,
	service *authentication.Service,
	request sandboxCanaryRequest,
) (authentication.Principal, error) {
	correlationID, err := randomCanaryIdentifier("login")
	if err != nil {
		return authentication.Principal{}, err
	}
	login, err := service.Login(
		ctx, request.Email, request.Password,
		"sandbox-canary-local", correlationID,
	)
	if err != nil {
		return authentication.Principal{},
			fmt.Errorf("sandbox_canary_reauthentication_failed")
	}
	return login.Principal, nil
}

func consumeSandboxCanaryAuthorization(
	ctx context.Context,
	service *authentication.SandboxAuthorizationService,
	principal authentication.Principal,
	request sandboxCanaryRequest,
) (authentication.ConsumedAuthorization, error) {
	grant, err := service.Reauthenticate(
		ctx, principal, request.Password, request.TOTP,
		authentication.PurposeSandboxArm,
		"sandbox-canary-local", request.Reason,
	)
	if err != nil {
		return authentication.ConsumedAuthorization{},
			fmt.Errorf("sandbox_canary_reauthentication_failed")
	}
	consumed, err := service.Consume(
		ctx, principal, grant.Token,
		authentication.PurposeSandboxArm,
	)
	grant.Token = ""
	if err != nil {
		return authentication.ConsumedAuthorization{},
			fmt.Errorf("sandbox_canary_authorization_failed")
	}
	return consumed, nil
}
