package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/runs"

	"github.com/jackc/pgx/v5"
)

const resolveOwnerOfflineInputsSQL = `
SELECT configuration.id,dataset.id,generation.id
FROM strategy_versions strategy
JOIN experiment_registrations experiment ON experiment.strategy_version_id=strategy.id
JOIN configuration_versions configuration ON configuration.id=experiment.configuration_id
JOIN dataset_manifests dataset ON dataset.id=experiment.dataset_id
JOIN research_generations generation ON generation.experiment_id=experiment.id
WHERE strategy.id=$1
  AND experiment.status IN ('registered','running','completed','locked')
  AND dataset.state='qualified' AND dataset.dataset_kind='decision_inputs'
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(convert_from(configuration.canonical_payload,'UTF8')::jsonb->'instruments') item
    WHERE item->>'base'=$2 AND item->>'quote'=$3 AND item->>'product'='spot'
  )
  AND (
    (cardinality($4::text[])=1 AND (
      EXISTS (SELECT 1 FROM dataset_exchange_coverage coverage
        WHERE coverage.dataset_id=dataset.id AND coverage.exchange_id=($4::text[])[1] AND coverage.complete)
      OR EXISTS (SELECT 1 FROM dataset_segments member JOIN market_data_segments segment ON segment.id=member.segment_id
        WHERE member.dataset_id=dataset.id AND segment.exchange_id=($4::text[])[1] AND segment.state='ready')
    ))
    OR (cardinality($4::text[])>1 AND (
      SELECT count(DISTINCT coverage.exchange_id) FROM dataset_exchange_coverage coverage
      WHERE coverage.dataset_id=dataset.id AND coverage.exchange_id=ANY($4::text[]) AND coverage.complete
    )=cardinality($4::text[]))
  )
ORDER BY generation.registered_at DESC,generation.id DESC LIMIT 1`

// CreateRun resolves a semantic owner selection to the latest matching
// immutable inputs. No client-provided configuration, portfolio, dataset,
// model, or research-generation identifier reaches the durable command path.
func (store *OwnerConsoleStore) CreateRun(
	ctx context.Context,
	principal authentication.Principal,
	key string,
	request generated.RunCreateRequest,
) (generated.RunResource, error) {
	selection, err := ownerRunSelection(request)
	if err != nil {
		return generated.RunResource{}, err
	}
	registry, err := runs.DefaultRegistry()
	if err != nil {
		return generated.RunResource{}, console.ErrUnavailable
	}
	choices, blocker := registry.Catalogue(selection)
	if blocker != nil || len(choices) != 1 || choices[0].StrategyVersion != request.StrategyVersion {
		return generated.RunResource{}, console.ErrPrecondition
	}

	switch request.Mode {
	case generated.RunCreateRequestModeBacktest, generated.RunCreateRequestModeReplay:
		return store.createOwnerOfflineRun(ctx, principal, key, request)
	case generated.RunCreateRequestModeShadow:
		return store.createOwnerShadowRun(ctx, principal, key, request)
	case generated.RunCreateRequestModeSandbox:
		return store.createOwnerSandboxRun(ctx, principal, key, request)
	default:
		return generated.RunResource{}, console.NewWorkflowBlocker("RUN_MODE_UNAVAILABLE",
			"That run mode is not installed yet.",
			"This request cannot be mapped to a durable worker without bypassing the product pipeline.",
			"No run was created and no sandbox command was issued.",
			"Choose backtest, replay, public shadow, or exchange sandbox.", string(request.Mode), "installed run mode", "durable worker")
	}
}

func (store *OwnerConsoleStore) createOwnerOfflineRun(ctx context.Context, principal authentication.Principal,
	key string, request generated.RunCreateRequest,
) (generated.RunResource, error) {
	offline, installed := ownerOfflineStrategy(request.StrategyId, request.StrategyVersion)
	if !installed {
		return generated.RunResource{}, console.NewWorkflowBlocker("STRATEGY_RUNTIME_UNAVAILABLE",
			"That strategy is not runnable in this mode yet.",
			"Its semantic catalogue record exists, but its shared durable offline runtime has not been installed.",
			"No run was created and no order-capable path was reached.",
			"Choose an installed recorded-data strategy from the catalogue, or wait for its runtime to be installed.",
			"offline runtime not installed", "shared offline runtime installed", "strategy runtime")
	}
	exchanges := make([]string, 0, len(request.Exchanges))
	for _, selected := range request.Exchanges {
		exchanges = append(exchanges, string(selected))
	}
	configurationID, datasetID, generationID, err := store.resolveOwnerOfflineInputs(
		ctx, offline.storageID, exchanges, request.Instrument)
	if err != nil {
		return generated.RunResource{}, err
	}
	seed, err := ownerRunSeed()
	if err != nil {
		return generated.RunResource{}, err
	}
	if request.Mode == generated.RunCreateRequestModeBacktest {
		job, createErr := store.CreateJob(ctx, principal, "backtest", key, generated.OfflineJobRequest{
			ConfigurationId: configurationID, DatasetId: datasetID, ResearchGenerationId: generationID,
			RootSeedHash: seed, StrategyVersion: generated.OfflineJobRequestStrategyVersion(offline.version)})
		if createErr != nil {
			return generated.RunResource{}, createErr
		}
		return store.Run(ctx, job.Id)
	}
	job, err := store.CreateJob(ctx, principal, "replay", key, generated.ReplayJobRequest{
		ConfigurationId: configurationID, DatasetId: datasetID, ResearchGenerationId: generationID,
		RootSeedHash: seed, StrategyVersion: generated.ReplayJobRequestStrategyVersion(offline.version)})
	if err != nil {
		return generated.RunResource{}, err
	}
	return store.Run(ctx, job.Id)
}

func (store *OwnerConsoleStore) createOwnerShadowRun(ctx context.Context, principal authentication.Principal,
	key string, request generated.RunCreateRequest,
) (generated.RunResource, error) {
	shadow, installed := ownerShadowStrategy(request.StrategyId, request.StrategyVersion)
	if !installed {
		return generated.RunResource{}, console.NewWorkflowBlocker("STRATEGY_RUNTIME_UNAVAILABLE",
			"That strategy does not have a public-shadow worker yet.",
			"Public shadow is not substituted with a recorded-data worker or a different strategy evaluator.",
			"No shadow session was created.", "Choose an installed public-shadow strategy or wait for this worker.",
			"public-shadow runtime not installed", "public-shadow runtime installed", "strategy runtime")
	}
	paired := shadow.storageID == "cross-exchange-arbitrage-1-0-0"
	if (!paired && len(request.Exchanges) != 1) || (paired && !ownerPairedShadowExchanges(request.Exchanges)) {
		return generated.RunResource{}, console.NewWorkflowBlocker("EXCHANGE_SELECTION_UNSUPPORTED",
			"Choose the exchange set shown for this shadow strategy.",
			"Single-venue strategies require one venue; Cross-Exchange Arbitrage requires the exact Binance and Bybit pair.",
			"No shadow session was created.", "Use the server-advertised run choice without changing its venues.",
			"exchange set mismatch", "advertised exchange set", "public-shadow runtime")
	}
	exchange := "binance"
	if !paired {
		exchange = string(request.Exchanges[0])
	}
	portfolioID, configurationID, err := store.resolveOwnerShadowInputs(
		ctx, shadow.storageID, exchange, request.Instrument)
	if err != nil {
		return generated.RunResource{}, err
	}
	session, err := store.createSelectedShadow(ctx, principal, key, portfolioID, configurationID,
		shadow.storageID, request, exchange, request.Instrument)
	if err != nil {
		return generated.RunResource{}, err
	}
	return store.Run(ctx, session.Id)
}

func (store *OwnerConsoleStore) createOwnerSandboxRun(ctx context.Context, principal authentication.Principal,
	key string, request generated.RunCreateRequest,
) (generated.RunResource, error) {
	instrument, validInstrument := ownerSandboxRunInstrument(request.Instrument)
	strategy := generated.SandboxStrategySessionCreateRequestStrategyId(request.StrategyId)
	if !validInstrument || !strategy.Valid() {
		return generated.RunResource{}, console.ErrInvalidRequest
	}
	exchanges := make([]generated.SandboxExchange, 0, len(request.Exchanges))
	for _, exchange := range request.Exchanges {
		value := generated.SandboxExchange(exchange)
		if !value.Valid() {
			return generated.RunResource{}, console.ErrInvalidRequest
		}
		exchanges = append(exchanges, value)
	}
	accepted, err := store.CreateSandboxStrategySession(ctx, principal, key,
		generated.SandboxStrategySessionCreateRequest{StrategyId: strategy, Exchanges: exchanges,
			Instrument: instrument, Preset: generated.SandboxStrategySessionCreateRequestPresetLatestQualifiedInputs,
			Reason: "Owner prepared a reviewed exchange sandbox run"})
	if err != nil {
		return generated.RunResource{}, err
	}
	return store.Run(ctx, accepted.TargetId)
}

type ownerOfflineRuntime struct {
	storageID string
	version   string
}

func ownerShadowStrategy(id, version string) (ownerOfflineRuntime, bool) {
	switch {
	case id == "trend-following" && version == "trend-following@1.0.0":
		return ownerOfflineRuntime{storageID: "trend-following-1-0-0", version: "trend-following@1.0.0"}, true
	case id == "mean-reversion" && version == "mean-reversion@1.0.0":
		return ownerOfflineRuntime{storageID: "mean-reversion-1-0-0", version: "mean-reversion@1.0.0"}, true
	case id == "triangular-arbitrage" && version == "triangular-arbitrage@1.0.0":
		return ownerOfflineRuntime{storageID: "triangular-arbitrage-1-0-0", version: "triangular-arbitrage@1.0.0"}, true
	case id == "cross-exchange-arbitrage" && version == "cross-exchange-arbitrage@1.0.0":
		return ownerOfflineRuntime{storageID: "cross-exchange-arbitrage-1-0-0", version: "cross-exchange-arbitrage@1.0.0"}, true
	default:
		return ownerOfflineRuntime{}, false
	}
}

func ownerPairedShadowExchanges(values []generated.RunCreateRequestExchanges) bool {
	if len(values) != 2 {
		return false
	}
	seen := make(map[string]bool, 2)
	for _, value := range values {
		seen[string(value)] = true
	}
	return len(seen) == 2 && seen["binance"] && seen["bybit"]
}

func ownerOfflineStrategy(id, version string) (ownerOfflineRuntime, bool) {
	switch {
	case id == "trend-following" && version == "trend-following@1.0.0":
		return ownerOfflineRuntime{storageID: "trend-following-1-0-0", version: "trend-following@1.0.0"}, true
	case id == "mean-reversion" && version == "mean-reversion@1.0.0":
		return ownerOfflineRuntime{storageID: "mean-reversion-1-0-0", version: "mean-reversion@1.0.0"}, true
	case id == "triangular-arbitrage" && version == "triangular-arbitrage@1.0.0":
		return ownerOfflineRuntime{storageID: "triangular-arbitrage-1-0-0", version: "triangular-arbitrage@1.0.0"}, true
	case id == "cross-exchange-arbitrage" && version == "cross-exchange-arbitrage@1.0.0":
		return ownerOfflineRuntime{storageID: "cross-exchange-arbitrage-1-0-0", version: "cross-exchange-arbitrage@1.0.0"}, true
	case id == "inventory-rebalancing" && version == "inventory-rebalancing@1.0.0":
		return ownerOfflineRuntime{storageID: "inventory-rebalancing-1-0-0", version: "inventory-rebalancing@1.0.0"}, true
	default:
		return ownerOfflineRuntime{}, false
	}
}

func ownerRunSelection(request generated.RunCreateRequest) (runs.Selection, error) {
	exchanges := make([]runs.Exchange, 0, len(request.Exchanges))
	for _, exchange := range request.Exchanges {
		exchanges = append(exchanges, runs.Exchange(exchange))
	}
	if request.Preset != generated.LatestQualifiedInputs {
		return runs.Selection{}, console.ErrInvalidRequest
	}
	return runs.Selection{StrategyID: request.StrategyId, Mode: runs.Mode(request.Mode),
		Exchanges: exchanges, Instrument: request.Instrument}, nil
}

func (store *OwnerConsoleStore) resolveOwnerOfflineInputs(
	ctx context.Context, strategyID string, exchanges []string, instrument string,
) (string, string, string, error) {
	base, quote, ok := ownerRunInstrument(instrument)
	if !ok || len(exchanges) == 0 || len(exchanges) > 2 {
		return "", "", "", console.ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(exchanges))
	for _, exchange := range exchanges {
		if (exchange != "binance" && exchange != "bybit") || exchange == "" {
			return "", "", "", console.ErrInvalidRequest
		}
		if _, duplicate := seen[exchange]; duplicate {
			return "", "", "", console.ErrInvalidRequest
		}
		seen[exchange] = struct{}{}
	}
	var configurationID, datasetID, generationID string
	err := store.pool.QueryRow(ctx, resolveOwnerOfflineInputsSQL, strategyID, base, quote, exchanges).
		Scan(&configurationID, &datasetID, &generationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", console.NewWorkflowBlocker("QUALIFIED_INPUTS_UNAVAILABLE",
			"No qualified recorded inputs match this selection.",
			"A durable backtest or replay needs one qualified decision-input dataset and its matching immutable configuration and research registration.",
			"No run was created.", "Register and qualify matching protected data, then try again.",
			"qualified inputs unavailable", "qualified matching inputs", "dataset", "configuration", "research registration")
	}
	if err != nil {
		return "", "", "", err
	}
	return configurationID, datasetID, generationID, nil
}

func (store *OwnerConsoleStore) resolveOwnerShadowInputs(ctx context.Context, strategyID, exchange, instrument string) (string, string, error) {
	base, quote, ok := ownerRunInstrument(instrument)
	if !ok || (exchange != "binance" && exchange != "bybit") || strategyID == "" {
		return "", "", console.ErrInvalidRequest
	}
	var portfolioID, configurationID string
	err := store.pool.QueryRow(ctx, `
SELECT portfolio.id,configuration.id
FROM strategy_versions strategy
JOIN experiment_registrations experiment ON experiment.strategy_version_id=strategy.id
JOIN configuration_versions configuration ON configuration.id=experiment.configuration_id
CROSS JOIN portfolios portfolio
WHERE strategy.id=$1
	  AND experiment.status IN ('registered','running','completed','locked')
	  AND EXISTS (
	    SELECT 1
	    FROM jsonb_array_elements(convert_from(configuration.canonical_payload,'UTF8')::jsonb->'instruments') item
	    WHERE item->>'base'=$3 AND item->>'quote'=$4 AND item->>'product'='spot'
	  )
  AND (
    EXISTS (
      SELECT 1
      FROM jsonb_array_elements(coalesce(convert_from(configuration.canonical_payload,'UTF8')::jsonb->'exchanges','[]'::jsonb)) venue
      WHERE venue->>'id'=$2
        AND EXISTS (
          SELECT 1 FROM jsonb_array_elements(venue->'instruments') item
          WHERE item->>'base'=$3 AND item->>'quote'=$4 AND item->>'product'='spot'
        )
    )
    OR ($2='binance' AND jsonb_array_length(coalesce(convert_from(configuration.canonical_payload,'UTF8')::jsonb->'exchanges','[]'::jsonb))=0)
  )
ORDER BY portfolio.id,configuration.id LIMIT 1`, strategyID, exchange, base, quote).
		Scan(&portfolioID, &configurationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", console.NewWorkflowBlocker("SHADOW_INPUTS_UNAVAILABLE",
			"No approved shadow configuration is available for this instrument.",
			"A public-data shadow session needs an immutable matching configuration and virtual portfolio before its existing readiness checks can run.",
			"No shadow session was created.", "Register a matching configuration and virtual portfolio, then try again.",
			"shadow inputs unavailable", "approved configuration and portfolio", "configuration", "portfolio")
	}
	if err != nil {
		return "", "", err
	}
	return portfolioID, configurationID, nil
}

func ownerRunInstrument(value string) (string, string, bool) {
	base, quote, found := strings.Cut(value, "/")
	return base, quote, found && base != "" && quote != "" && !strings.Contains(quote, "/")
}

func ownerSandboxRunInstrument(value string) (generated.SandboxStrategySessionCreateRequestInstrument, bool) {
	switch value {
	case "BTC/USDT":
		return generated.SandboxStrategySessionCreateRequestInstrumentBTCUSDT, true
	case "ETH/USDT":
		return generated.SandboxStrategySessionCreateRequestInstrumentETHUSDT, true
	default:
		return "", false
	}
}

func ownerRunSeed() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}

// ControlRun routes a semantic owner command only to an existing durable
// lifecycle. A command is never silently remapped to another run type.
func (store *OwnerConsoleStore) ControlRun(
	ctx context.Context,
	principal authentication.Principal,
	id, action, key string,
	body generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	var kind string
	err := store.pool.QueryRow(ctx, `
	SELECT kind FROM (
	  SELECT job_type AS kind FROM jobs WHERE id=$1 AND job_type IN ('backtest','replay')
	  UNION ALL
	  SELECT 'shadow' AS kind FROM shadow_sessions WHERE id=$1
	  UNION ALL
	  SELECT 'sandbox' AS kind FROM sandbox_strategy_sessions WHERE id=$1
	) source LIMIT 1`, id).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.CommandAccepted{}, console.ErrNotFound
	}
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	switch kind {
	case "replay":
		if action != "pause" && action != "resume" && action != "step" {
			return generated.CommandAccepted{}, console.ErrPrecondition
		}
		return store.ControlJob(ctx, principal, id, action, key, body)
	case "shadow":
		if action != "stop" {
			return generated.CommandAccepted{}, console.ErrPrecondition
		}
		return store.StopShadow(ctx, principal, id, key, body)
	case "sandbox":
		if action != "stop" {
			return generated.CommandAccepted{}, console.ErrPrecondition
		}
		return store.StopSandboxStrategySession(ctx, principal, id, key, body)
	default:
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
}

var _ console.RunCommandService = (*OwnerConsoleStore)(nil)
