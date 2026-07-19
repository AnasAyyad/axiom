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
	store, err := postgresstore.NewA11AuthenticationStore(pool)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	count, err := store.UserCount(ctx)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	csrfKey, err := security.ReadSecretFile(runtimeConfig.Authentication.CSRFKeyFile)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	cursorKey, err := security.ReadSecretFile(runtimeConfig.Authentication.SessionSigningKeyFile)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	clock := &domain.SystemClock{}
	authenticationService, err := authentication.NewService(store, clock, []byte(csrfKey))
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	if count == 0 {
		email, emailErr := security.ReadSecretFile(runtimeConfig.Authentication.BootstrapOwnerEmailFile)
		hash, hashErr := security.ReadSecretFile(runtimeConfig.Authentication.BootstrapOwnerPasswordHashFile)
		if emailErr != nil || hashErr != nil {
			return a11ConsoleSetup{options: options, dependency: readiness}
		}
		created, bootstrapErr := authenticationService.Bootstrap(ctx, email, hash)
		if bootstrapErr != nil || !created {
			return a11ConsoleSetup{options: options, dependency: readiness}
		}
	}
	consoleStore, err := postgresstore.NewA11ConsoleStore(pool, []byte(cursorKey), clock)
	if err != nil {
		return a11ConsoleSetup{options: options, dependency: readiness}
	}
	readiness.authenticationReady = true
	options.Authentication = authenticationService
	options.Read = consoleStore
	options.Commands = consoleStore
	options.Streams = consoleStore
	return a11ConsoleSetup{options: options, dependency: readiness}
}
