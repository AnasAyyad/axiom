package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

func TestSandboxEngineAttestationIsExactAndExchangeScoped(t *testing.T) {
	t.Setenv(
		"AXIOM_SANDBOX_ACCOUNT_IDENTITY_HASH",
		strings.Repeat("a", 64),
	)
	t.Setenv(
		"AXIOM_SANDBOX_KEY_FINGERPRINT",
		strings.Repeat("b", 32),
	)
	t.Setenv("AXIOM_SANDBOX_OWNER_ATTESTED", "true")
	t.Setenv("AXIOM_SANDBOX_CREDENTIAL_GENERATION", "1")
	now := time.Unix(1_700_000_000, 0).UTC()
	_, binance, err := loadSandboxEngineAttestation(
		sandbox.ExchangeBinance,
		now,
	)
	if err != nil ||
		binance.AccountID != "binance-testnet-aaaaaaaaaaaaaaaa" ||
		binance.Environment != sandbox.EnvironmentBinanceSpotTestnet {
		t.Fatalf("binance account=%#v error=%v", binance, err)
	}
	_, bybit, err := loadSandboxEngineAttestation(
		sandbox.ExchangeBybit,
		now,
	)
	if err != nil ||
		bybit.AccountID != "bybit-demo-aaaaaaaaaaaaaaaa" ||
		bybit.Environment != sandbox.EnvironmentBybitDemo {
		t.Fatalf("bybit account=%#v error=%v", bybit, err)
	}
	t.Setenv("AXIOM_SANDBOX_OWNER_ATTESTED", "false")
	if _, _, err = loadSandboxEngineAttestation(
		sandbox.ExchangeBinance,
		now,
	); err == nil {
		t.Fatal("unattested account accepted")
	}
}

func TestSandboxEngineRequiresBothSubmissionSwitchLayers(t *testing.T) {
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	work := &sandboxEngineRoleWork{
		product:  product,
		exchange: sandbox.ExchangeBinance,
	}
	if work.sandboxSubmissionEnabled() {
		t.Fatal("default-off graph enabled submission")
	}
	work.product.Sandbox.IntegrationsEnabled = true
	work.product.Sandbox.SubmissionEnabled = true
	work.product.Sandbox.Exchanges[0].IntegrationEnabled = true
	if work.sandboxSubmissionEnabled() {
		t.Fatal("missing exchange submission switch was ignored")
	}
	work.product.Sandbox.Exchanges[0].SubmissionEnabled = true
	if !work.sandboxSubmissionEnabled() {
		t.Fatal("complete reviewed switch set was rejected")
	}
	work.exchange = sandbox.ExchangeBybit
	if work.sandboxSubmissionEnabled() {
		t.Fatal("Binance enablement leaked into Bybit")
	}
}

func TestSandboxEngineUnknownRecoveryIsBlockedWhenIneligible(t *testing.T) {
	loop := sandboxEngineLoop{}
	if err := loop.recover(context.Background(), false); err != nil {
		t.Fatalf("unknown recovery was not safely blocked: %v", err)
	}
}

type sandboxPrivateReconnectFixture struct {
	attempts    int
	succeedAt   int
	failureKind exchangecontracts.ErrorKind
}

func (source *sandboxPrivateReconnectFixture) Receive(
	context.Context,
) (sandbox.PrivateEvent, error) {
	return sandbox.PrivateEvent{}, errors.New("disconnected")
}

func (source *sandboxPrivateReconnectFixture) Reconnect(
	context.Context,
) error {
	source.attempts++
	if source.attempts < source.succeedAt {
		kind := source.failureKind
		if kind == "" {
			kind = exchangecontracts.ErrorTransient
		}
		return exchangecontracts.NewDetailedError(
			kind,
			exchangecontracts.OperationStream,
			0,
			0,
			"fixture_disconnect",
		)
	}
	return nil
}

func (*sandboxPrivateReconnectFixture) Close() error {
	return nil
}

func TestSandboxPrivateStreamReconnectRetriesUntilHealthyOrCanceled(
	t *testing.T,
) {
	source := &sandboxPrivateReconnectFixture{succeedAt: 3}
	if err := reconnectSandboxPrivateSourceAfter(
		context.Background(), source, time.Millisecond, time.Second,
	); err != nil || source.attempts != 3 {
		t.Fatalf("healthy reconnect attempts=%d", source.attempts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := &sandboxPrivateReconnectFixture{succeedAt: 1}
	if err := reconnectSandboxPrivateSourceAfter(
		ctx, canceled, time.Millisecond, time.Second,
	); !errors.Is(err, context.Canceled) || canceled.attempts != 0 {
		t.Fatalf("canceled reconnect attempts=%d", canceled.attempts)
	}
}

func TestSandboxPrivateStreamReconnectDeadlineAndTerminalClass(t *testing.T) {
	expiring := &sandboxPrivateReconnectFixture{succeedAt: 1_000}
	err := reconnectSandboxPrivateSourceAfter(
		context.Background(), expiring, time.Millisecond, 5*time.Millisecond,
	)
	kind, cause := sandbox.ClassifyRecoveryFailure(err)
	if kind != exchangecontracts.ErrorTransient ||
		cause != "recovery_deadline_exceeded" || expiring.attempts == 0 {
		t.Fatalf("deadline kind=%s cause=%s attempts=%d error=%v",
			kind, cause, expiring.attempts, err)
	}

	rateLimited := &sandboxPrivateReconnectFixture{
		succeedAt: 1_000, failureKind: exchangecontracts.ErrorRateLimit,
	}
	err = reconnectSandboxPrivateSourceAfter(
		context.Background(), rateLimited, time.Millisecond, time.Second,
	)
	if exchangecontracts.KindOf(err) != exchangecontracts.ErrorRateLimit ||
		rateLimited.attempts != 1 {
		t.Fatalf("rate limit attempts=%d error=%v", rateLimited.attempts, err)
	}
}

type sandboxEngineHealthLoopFixture struct {
	reconcileCalls int
	evaluateCalls  int
	targets        []bool
	reconcileErr   error
}

func (fixture *sandboxEngineHealthLoopFixture) refreshEligibility(
	context.Context,
	bool,
) (bool, error) {
	return true, nil
}

func (fixture *sandboxEngineHealthLoopFixture) reconcile(
	context.Context,
	bool,
) error {
	fixture.reconcileCalls++
	return fixture.reconcileErr
}

func (fixture *sandboxEngineHealthLoopFixture) evaluateStrategies(
	context.Context,
) error {
	fixture.evaluateCalls++
	return nil
}

func (fixture *sandboxEngineHealthLoopFixture) transitionReadiness(
	_ context.Context,
	_ bool,
	target bool,
) (bool, error) {
	fixture.targets = append(fixture.targets, target)
	return target, nil
}

func TestSandboxPrivateStreamRecoveryReconcilesImmediatelyAndStaysPaused(
	t *testing.T,
) {
	now := time.Date(2026, 8, 9, 2, 7, 42, 0, time.UTC)
	health := newSandboxEngineHealthWithClock(func() time.Time { return now })
	loop := &sandboxEngineHealthLoopFixture{}
	err := health.observePrivate(
		context.Background(), loop, sandboxPrivateStreamSignal{
			healthy:     false,
			failureKind: exchangecontracts.ErrorTransient,
			causeCode:   "private_stream_receive_failed",
		},
	)
	if err != nil || health.dispatchAllowed || !health.recovery.Active() ||
		health.ready || len(loop.targets) != 1 || loop.targets[0] {
		t.Fatalf("degraded health=%+v targets=%v error=%v", health, loop.targets, err)
	}

	now = now.Add(time.Second)
	err = health.observePrivate(
		context.Background(), loop, sandboxPrivateStreamSignal{
			healthy: true, reconcileNow: true,
		},
	)
	if err != nil || loop.reconcileCalls != 1 || health.dispatchAllowed ||
		!health.recovery.Active() || health.ready {
		t.Fatalf("first clean health=%+v calls=%d error=%v", health, loop.reconcileCalls, err)
	}

	now = now.Add(30 * time.Second)
	if err = health.reconcile(context.Background(), loop); err != nil ||
		loop.reconcileCalls != 2 || !health.dispatchAllowed ||
		health.recovery.State() != sandbox.RecoveryRecovered || !health.ready ||
		loop.evaluateCalls != 1 {
		t.Fatalf("recovered health=%+v calls=%d error=%v", health, loop.reconcileCalls, err)
	}
}
