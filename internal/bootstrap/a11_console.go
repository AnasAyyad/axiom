package bootstrap

import (
	"context"
	"errors"

	"axiom/internal/api/console"
	"axiom/internal/authentication"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/security"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

type a11ConsoleSetup struct {
	options    *console.Options
	dependency *a11Readiness
}

type a11Readiness struct {
	pool                *pgxpool.Pool
	authenticationReady bool
}

// Ping reports unready until authentication bootstrap and storage are usable.
func (dependency *a11Readiness) Ping(ctx context.Context) error {
	if dependency == nil || dependency.pool == nil || !dependency.authenticationReady {
		return errors.New("a11_authentication_not_ready")
	}
	return dependency.pool.Ping(ctx)
}

func setupA11Console(ctx context.Context, pool *pgxpool.Pool, runtimeConfig config.Runtime) a11ConsoleSetup {
	readiness := &a11Readiness{pool: pool}
	options := &console.Options{AllowedOrigins: append([]string(nil), runtimeConfig.Authentication.AllowedOrigins...),
		SecureCookies: runtimeConfig.Authentication.SecureCookies}
	store, authenticationService, clock, err := setupA11Authentication(
		ctx, pool, runtimeConfig,
	)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	cursorKey, err := security.ReadSecretFile(runtimeConfig.Authentication.SessionSigningKeyFile)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	consoleStore, err := postgresstore.NewA11ConsoleStore(pool, []byte(cursorKey), clock)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	v1cAuthenticationStore, err := postgresstore.NewV1CAuthenticationStore(pool)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	sandboxAuthorizations, err := authentication.NewSandboxAuthorizationService(
		store,
		v1cAuthenticationStore,
		clock,
		runtimeConfig.Authentication.TOTPSeedFile,
	)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	readiness.authenticationReady = true
	options.Authentication = authenticationService
	options.SandboxAuthorizations = sandboxAuthorizations
	options.Read = consoleStore
	options.Commands = consoleStore
	options.Streams = consoleStore
	options.SandboxRead = consoleStore
	options.SandboxCommands = consoleStore
	options.D1Read = consoleStore
	options.D1Commands = consoleStore
	return a11ConsoleSetup{options: options, dependency: readiness}
}

func setupA11Authentication(
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
) (*postgresstore.A11AuthenticationStore, *authentication.Service, *domain.SystemClock, error) {
	store, err := postgresstore.NewA11AuthenticationStore(pool)
	if err != nil {
		return nil, nil, nil, err
	}
	count, err := store.UserCount(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	csrfKey, err := security.ReadSecretFile(runtimeConfig.Authentication.CSRFKeyFile)
	if err != nil {
		return nil, nil, nil, err
	}
	clock := &domain.SystemClock{}
	service, err := authentication.NewService(store, clock, []byte(csrfKey))
	if err != nil {
		return nil, nil, nil, err
	}
	if count == 0 && bootstrapA11Owner(ctx, service, runtimeConfig) != nil {
		return nil, nil, nil, errors.New("a11_owner_bootstrap_failed")
	}
	return store, service, clock, nil
}

func bootstrapA11Owner(
	ctx context.Context,
	service *authentication.Service,
	runtimeConfig config.Runtime,
) error {
	email, emailErr := security.ReadSecretFile(runtimeConfig.Authentication.BootstrapOwnerEmailFile)
	hash, hashErr := security.ReadSecretFile(runtimeConfig.Authentication.BootstrapOwnerPasswordHashFile)
	if emailErr != nil || hashErr != nil {
		return errors.New("a11_owner_secret_unavailable")
	}
	created, err := service.Bootstrap(ctx, email, hash)
	if err != nil || !created {
		return errors.New("a11_owner_bootstrap_rejected")
	}
	return nil
}
