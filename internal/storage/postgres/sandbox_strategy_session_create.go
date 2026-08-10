package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/config"
	"axiom/internal/sandbox"
)

// CreateSandboxStrategySession prepares one server-resolved strategy session.
// It has no exchange client, cannot arm an account, and cannot create an
// order. The later arm, start, decision-time admission, planning, and
// dispatch boundaries remain independently mandatory.
func (store *OwnerConsoleStore) CreateSandboxStrategySession(
	ctx context.Context,
	principal authentication.Principal,
	key string,
	body generated.SandboxStrategySessionCreateRequest,
) (generated.CommandAccepted, error) {
	selection, err := resolveSandboxQualificationSandboxStrategySelection(body)
	if err != nil {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	configurationID, configurationHash, product, err := store.activeSandboxConfiguration(ctx)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	if product.SchemaVersion != config.SchemaVersionSandboxRuntime {
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	sessionID := "sandbox-strategy-" + ownerConsoleDedupe(principal.UserID, key)[:32]
	strategySetHash := ownerConsoleHash([]byte(configurationHash + "\x00" +
		selection.semanticID + "\x00" + selection.version + "\x00" +
		string(body.Preset)))
	pending, err := store.beginSandboxQualificationCommand(
		ctx, principal, key, "sandbox.strategy_session_prepare",
		"sandbox_strategy_session", sessionID, body.Reason, nil,
		sandboxStrategyCommandPayload(selection, body, configurationID, configurationHash, strategySetHash),
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	_, err = store.sandbox_runtime.CreateStrategySession(ctx, sandbox.StrategySessionCommand{
		ID: sandbox.SessionID(sessionID), Strategy: selection.strategy, Exchanges: selection.exchanges,
		Instrument: string(body.Instrument), ConfigurationID: configurationID,
		StrategySetHash: strategySetHash, CreatedBy: principal.UserID,
		CreatedAt: store.clock.Now().UTC,
	})
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_strategy_session_prepare_rejected")
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	return store.completeSandboxQualificationCommand(ctx, pending, principal,
		"sandbox.strategy_session_prepare", sessionID, map[string]any{"strategy_session_id": sessionID,
			"state": "prepared", "strategy_id": selection.semanticID, "strategy_version": selection.version})
}

type sandboxQualificationSandboxStrategySelection struct {
	semanticID string
	version    string
	strategy   string
	exchanges  []sandbox.Exchange
}

func sandboxStrategyCommandPayload(selection sandboxQualificationSandboxStrategySelection,
	body generated.SandboxStrategySessionCreateRequest, configurationID, configurationHash, strategySetHash string,
) map[string]any {
	return map[string]any{"strategy_id": selection.semanticID, "strategy_version": selection.version,
		"exchanges": selection.exchanges, "instrument": body.Instrument, "preset": body.Preset,
		"configuration_id": configurationID, "configuration_hash": configurationHash,
		"strategy_set_hash": strategySetHash}
}

func resolveSandboxQualificationSandboxStrategySelection(
	body generated.SandboxStrategySessionCreateRequest,
) (sandboxQualificationSandboxStrategySelection, error) {
	if !body.StrategyId.Valid() || !body.Instrument.Valid() || !body.Preset.Valid() ||
		len(body.Exchanges) == 0 || len(body.Exchanges) > 2 ||
		len(body.Reason) < 8 || len(body.Reason) > 500 ||
		strings.ContainsAny(body.Reason, "\r\n\x00") {
		return sandboxQualificationSandboxStrategySelection{}, fmt.Errorf("sandbox_strategy_selection_invalid")
	}
	selection := sandboxQualificationSandboxStrategySelection{semanticID: string(body.StrategyId)}
	switch body.StrategyId {
	case generated.SandboxStrategySessionCreateRequestStrategyIdTrendFollowing:
		selection.strategy, selection.version = sandbox.StrategyTrend, "trend-following@1.0.0"
	case generated.SandboxStrategySessionCreateRequestStrategyIdMeanReversion:
		selection.strategy, selection.version = sandbox.StrategyMeanReversion, "mean-reversion@1.0.0"
	case generated.SandboxStrategySessionCreateRequestStrategyIdTriangularArbitrage:
		selection.strategy, selection.version = sandbox.StrategyTriangular, "triangular-arbitrage@1.0.0"
	case generated.SandboxStrategySessionCreateRequestStrategyIdCrossExchangeArbitrage:
		selection.strategy, selection.version = sandbox.StrategyCrossExchangeArbitrage, "cross-exchange-arbitrage@1.0.0"
	default:
		return sandboxQualificationSandboxStrategySelection{}, fmt.Errorf("sandbox_strategy_unknown")
	}
	exchanges, err := resolveSandboxQualificationSandboxExchanges(body.Exchanges)
	if err != nil {
		return sandboxQualificationSandboxStrategySelection{}, err
	}
	selection.exchanges = exchanges
	if selection.strategy == sandbox.StrategyCrossExchangeArbitrage {
		if len(selection.exchanges) != 2 || selection.exchanges[0] != sandbox.ExchangeBinance ||
			selection.exchanges[1] != sandbox.ExchangeBybit {
			return sandboxQualificationSandboxStrategySelection{}, fmt.Errorf("sandbox_strategy_topology_invalid")
		}
	} else if len(selection.exchanges) != 1 {
		return sandboxQualificationSandboxStrategySelection{}, fmt.Errorf("sandbox_strategy_topology_invalid")
	}
	return selection, nil
}

func resolveSandboxQualificationSandboxExchanges(values []generated.SandboxExchange) ([]sandbox.Exchange, error) {
	exchanges := make([]sandbox.Exchange, 0, len(values))
	seen := make(map[generated.SandboxExchange]struct{}, len(values))
	for _, exchange := range values {
		if _, duplicate := seen[exchange]; duplicate {
			return nil, fmt.Errorf("sandbox_strategy_exchange_duplicate")
		}
		seen[exchange] = struct{}{}
		switch exchange {
		case generated.SandboxExchangeBinance:
			exchanges = append(exchanges, sandbox.ExchangeBinance)
		case generated.SandboxExchangeBybit:
			exchanges = append(exchanges, sandbox.ExchangeBybit)
		default:
			return nil, fmt.Errorf("sandbox_strategy_exchange_invalid")
		}
	}
	sort.Slice(exchanges, func(left, right int) bool { return exchanges[left] < exchanges[right] })
	return exchanges, nil
}

func (store *OwnerConsoleStore) activeSandboxConfiguration(
	ctx context.Context,
) (string, string, config.Configuration, error) {
	var id, hash string
	err := store.pool.QueryRow(ctx, `
SELECT configuration.id,configuration.configuration_hash::text
FROM configuration_activations activation
JOIN configuration_versions configuration ON configuration.id=activation.configuration_id
ORDER BY activation.revision DESC
LIMIT 1`).Scan(&id, &hash)
	if err != nil || id == "" || len(hash) != 64 {
		return "", "", config.Configuration{}, fmt.Errorf("sandbox_active_configuration_unavailable")
	}
	product, err := store.sandboxQualificationConfiguration(ctx, id)
	if err != nil {
		return "", "", config.Configuration{}, err
	}
	return id, hash, product, nil
}

var _ console.SandboxCommandService = (*OwnerConsoleStore)(nil)
