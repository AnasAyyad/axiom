package bootstrap

import (
	"context"
	"fmt"

	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

func (work *sandboxEngineRoleWork) validateSandboxIdentity(
	ctx context.Context,
	store *postgresstore.SandboxRuntimeDispatcherStore,
	configurationID string,
	attestation sandboxEngineAttestation,
) (any, sandbox.AccountIdentity, error) {
	switch work.exchange {
	case sandbox.ExchangeBinance:
		client, err := binance.NewSandboxClient(store, configurationID)
		if err != nil {
			return nil, sandbox.AccountIdentity{}, err
		}
		identity, err := client.ValidateStartup(
			ctx,
			binance.BinanceTestnetAttestation{
				AccountIdentityHash: attestation.accountHash,
				KeyFingerprint:      attestation.keyFingerprint,
				TestnetOnly:         attestation.ownerAttested,
			},
		)
		return client, identity, err
	case sandbox.ExchangeBybit:
		client, err := bybit.NewSandboxClient(store, configurationID)
		if err != nil {
			return nil, sandbox.AccountIdentity{}, err
		}
		identity, err := client.ValidateStartup(
			ctx,
			bybit.BybitDemoAttestation{
				AccountIdentityHash: attestation.accountHash,
				KeyFingerprint:      attestation.keyFingerprint,
				DemoOnly:            attestation.ownerAttested,
			},
		)
		return client, identity, err
	default:
		return nil, sandbox.AccountIdentity{},
			fmt.Errorf("sandbox_engine_exchange_invalid")
	}
}

func (work *sandboxEngineRoleWork) buildSandboxAdapter(
	ctx context.Context,
	client any,
	identity sandbox.AccountIdentity,
	epoch uint64,
	store *postgresstore.SandboxRuntimeDispatcherStore,
) (sandboxEngineAdapter, error) {
	switch typed := client.(type) {
	case *binance.SandboxClient:
		return binance.NewSandboxAdapter(
			ctx,
			typed,
			identity,
			epoch,
			store,
			store,
		)
	case *bybit.SandboxClient:
		return bybit.NewSandboxAdapter(
			ctx,
			typed,
			identity,
			epoch,
			store,
			store,
		)
	default:
		return nil, fmt.Errorf("sandbox_engine_client_invalid")
	}
}

func (work *sandboxEngineRoleWork) openPrivateSource(
	ctx context.Context,
	client any,
	adapter sandboxEngineAdapter,
	store *postgresstore.SandboxRuntimeDispatcherStore,
) (sandbox.PrivateEventSource, error) {
	switch typed := client.(type) {
	case *binance.SandboxClient:
		boundary, ok := adapter.(*binance.SandboxAdapter)
		if !ok {
			return nil, fmt.Errorf("sandbox_engine_adapter_invalid")
		}
		return binance.NewPrivateEventSource(
			ctx,
			typed,
			boundary,
			store,
		)
	case *bybit.SandboxClient:
		boundary, ok := adapter.(*bybit.SandboxAdapter)
		if !ok {
			return nil, fmt.Errorf("sandbox_engine_adapter_invalid")
		}
		return bybit.NewPrivateEventSource(
			ctx,
			typed,
			boundary,
			store,
		)
	default:
		return nil, fmt.Errorf("sandbox_engine_client_invalid")
	}
}
