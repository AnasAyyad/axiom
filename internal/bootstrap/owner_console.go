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

type ownerConsoleSetup struct {
	options    *console.Options
	dependency *ownerConsoleReadiness
}

type ownerConsoleReadiness struct {
	pool                *pgxpool.Pool
	authenticationReady bool
}

// Ping reports unready until authentication bootstrap and storage are usable.
func (dependency *ownerConsoleReadiness) Ping(ctx context.Context) error {
	if dependency == nil || dependency.pool == nil || !dependency.authenticationReady {
		return errors.New("owner_console_authentication_not_ready")
	}
	return dependency.pool.Ping(ctx)
}

func setupOwnerConsole(ctx context.Context, pool *pgxpool.Pool, runtimeConfig config.Runtime) ownerConsoleSetup {
	readiness := &ownerConsoleReadiness{pool: pool}
	options := &console.Options{AllowedOrigins: append([]string(nil), runtimeConfig.Authentication.AllowedOrigins...),
		SecureCookies: runtimeConfig.Authentication.SecureCookies}
	store, authenticationService, clock, err := setupOwnerAuthentication(
		ctx, pool, runtimeConfig,
	)
	if err != nil {
		return ownerConsoleSetup{options: options, dependency: readiness}
	}
	cursorKey, err := security.ReadSecretFile(runtimeConfig.Authentication.SessionSigningKeyFile)
	if err != nil {
		return ownerConsoleSetup{options: options, dependency: readiness}
	}
	consoleStore, err := postgresstore.NewOwnerConsoleStore(pool, []byte(cursorKey), clock)
	if err != nil {
		return ownerConsoleSetup{options: options, dependency: readiness}
	}
	sandboxRuntimeAuthenticationStore, err := postgresstore.NewSandboxRuntimeAuthenticationStore(pool)
	if err != nil {
		return ownerConsoleSetup{options: options, dependency: readiness}
	}
	sandboxAuthorizations, err := authentication.NewSandboxAuthorizationService(
		store,
		sandboxRuntimeAuthenticationStore,
		clock,
		runtimeConfig.Authentication.TOTPSeedFile,
	)
	if err != nil {
		return ownerConsoleSetup{options: options, dependency: readiness}
	}
	readiness.authenticationReady = true
	options.Authentication = authenticationService
	options.SandboxAuthorizations = sandboxAuthorizations
	options.Read = consoleStore
	options.Commands = consoleStore
	options.Streams = consoleStore
	options.SandboxRead = consoleStore
	options.SandboxCommands = consoleStore
	options.OwnerControlRead = consoleStore
	options.OwnerControlCommands = consoleStore
	options.OperationalEvidenceRead = consoleStore
	options.Runs = consoleStore
	options.RunCommands = consoleStore
	options.DataCatalogue = consoleStore
	return ownerConsoleSetup{options: options, dependency: readiness}
}

func setupOwnerAuthentication(
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
) (*postgresstore.OwnerAuthenticationStore, *authentication.Service, *domain.SystemClock, error) {
	store, err := postgresstore.NewOwnerAuthenticationStore(pool)
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
	if count == 0 && bootstrapOwnerConsoleOwner(ctx, service, runtimeConfig) != nil {
		return nil, nil, nil, errors.New("owner_console_owner_bootstrap_failed")
	}
	return store, service, clock, nil
}

func bootstrapOwnerConsoleOwner(
	ctx context.Context,
	service *authentication.Service,
	runtimeConfig config.Runtime,
) error {
	email, emailErr := security.ReadSecretFile(runtimeConfig.Authentication.BootstrapOwnerEmailFile)
	hash, hashErr := security.ReadSecretFile(runtimeConfig.Authentication.BootstrapOwnerPasswordHashFile)
	if emailErr != nil || hashErr != nil {
		return errors.New("owner_console_owner_secret_unavailable")
	}
	created, err := service.Bootstrap(ctx, email, hash)
	if err != nil || !created {
		return errors.New("owner_console_owner_bootstrap_rejected")
	}
	return nil
}
