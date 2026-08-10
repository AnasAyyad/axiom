package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
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

type sandboxPrivateReconnectFixture struct {
	attempts  int
	succeedAt int
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
		return errors.New("still disconnected")
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
	if !reconnectSandboxPrivateSourceAfter(
		context.Background(), source, time.Millisecond,
	) || source.attempts != 3 {
		t.Fatalf("healthy reconnect attempts=%d", source.attempts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := &sandboxPrivateReconnectFixture{succeedAt: 1}
	if reconnectSandboxPrivateSourceAfter(
		ctx, canceled, time.Millisecond,
	) || canceled.attempts != 0 {
		t.Fatalf("canceled reconnect attempts=%d", canceled.attempts)
	}
}
