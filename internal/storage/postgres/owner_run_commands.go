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

// CreateRun resolves a semantic owner selection to the latest matching
// immutable inputs. No client-provided configuration, portfolio, dataset,
// model, or research-generation identifier reaches the durable command path.
func (store *A11ConsoleStore) CreateRun(
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

	// The existing durable materializer is the production Trend implementation.
	// Do not route another strategy's input into it merely because its semantic
	// metadata is visible in the catalogue.
	if request.StrategyId != "trend-following" || request.StrategyVersion != "trend-following@1.0.0" {
		return generated.RunResource{}, console.ErrPrecondition
	}
	if len(request.Exchanges) != 1 {
		return generated.RunResource{}, console.ErrPrecondition
	}
	exchange := string(request.Exchanges[0])
	switch request.Mode {
	case generated.RunCreateRequestModeBacktest, generated.RunCreateRequestModeReplay:
		configurationID, datasetID, generationID, resolveErr := store.resolveOwnerOfflineInputs(ctx, exchange, request.Instrument)
		if resolveErr != nil {
			return generated.RunResource{}, resolveErr
		}
		seed, seedErr := ownerRunSeed()
		if seedErr != nil {
			return generated.RunResource{}, seedErr
		}
		if request.Mode == generated.RunCreateRequestModeBacktest {
			job, createErr := store.CreateJob(ctx, principal, "backtest", key, generated.OfflineJobRequest{
				ConfigurationId: configurationID, DatasetId: datasetID, ResearchGenerationId: generationID,
				RootSeedHash: seed, StrategyVersion: generated.OfflineJobRequestStrategyVersionTrendV1a1,
			})
			if createErr != nil {
				return generated.RunResource{}, createErr
			}
			return store.Run(ctx, job.Id)
		}
		job, createErr := store.CreateJob(ctx, principal, "replay", key, generated.ReplayJobRequest{
			ConfigurationId: configurationID, DatasetId: datasetID, ResearchGenerationId: generationID,
			RootSeedHash: seed, StrategyVersion: generated.ReplayJobRequestStrategyVersionTrendV1a1,
		})
		if createErr != nil {
			return generated.RunResource{}, createErr
		}
		return store.Run(ctx, job.Id)
	case generated.RunCreateRequestModeShadow:
		if exchange != "binance" {
			return generated.RunResource{}, console.ErrPrecondition
		}
		portfolioID, configurationID, resolveErr := store.resolveOwnerShadowInputs(ctx, request.Instrument)
		if resolveErr != nil {
			return generated.RunResource{}, resolveErr
		}
		session, createErr := store.CreateShadow(ctx, principal, key, generated.ShadowSessionRequest{
			ConfigurationId: configurationID, PortfolioId: portfolioID,
			StrategyVersion: generated.ShadowSessionRequestStrategyVersionTrendV1a1,
		})
		if createErr != nil {
			return generated.RunResource{}, createErr
		}
		return store.Run(ctx, session.Id)
	default:
		return generated.RunResource{}, console.ErrPrecondition
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

func (store *A11ConsoleStore) resolveOwnerOfflineInputs(
	ctx context.Context,
	exchange, instrument string,
) (string, string, string, error) {
	base, quote, ok := ownerRunInstrument(instrument)
	if !ok {
		return "", "", "", console.ErrInvalidRequest
	}
	var configurationID, datasetID, generationID string
	err := store.pool.QueryRow(ctx, `
SELECT configuration.id,dataset.id,generation.id
FROM strategy_versions strategy
JOIN experiment_registrations experiment ON experiment.strategy_version_id=strategy.id
JOIN configuration_versions configuration ON configuration.id=experiment.configuration_id
JOIN dataset_manifests dataset ON dataset.id=experiment.dataset_id
JOIN research_generations generation ON generation.experiment_id=experiment.id
WHERE strategy.id='trend-v1a-1'
  AND experiment.status IN ('registered','running','completed','locked')
  AND dataset.state='qualified' AND dataset.dataset_kind='decision_inputs'
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(convert_from(configuration.canonical_payload,'UTF8')::jsonb->'instruments') item
    WHERE item->>'base'=$1 AND item->>'quote'=$2 AND item->>'product'='spot'
  )
  AND (
    EXISTS (SELECT 1 FROM dataset_exchange_coverage coverage
      WHERE coverage.dataset_id=dataset.id AND coverage.exchange_id=$3 AND coverage.complete)
    OR EXISTS (SELECT 1 FROM dataset_segments member JOIN market_data_segments segment ON segment.id=member.segment_id
      WHERE member.dataset_id=dataset.id AND segment.exchange_id=$3 AND segment.state='ready')
  )
ORDER BY generation.registered_at DESC,generation.id DESC LIMIT 1`, base, quote, exchange).
		Scan(&configurationID, &datasetID, &generationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", console.ErrPrecondition
	}
	if err != nil {
		return "", "", "", err
	}
	return configurationID, datasetID, generationID, nil
}

func (store *A11ConsoleStore) resolveOwnerShadowInputs(ctx context.Context, instrument string) (string, string, error) {
	base, quote, ok := ownerRunInstrument(instrument)
	if !ok {
		return "", "", console.ErrInvalidRequest
	}
	var portfolioID, configurationID string
	err := store.pool.QueryRow(ctx, `
SELECT portfolio.id,configuration.id
FROM strategy_versions strategy
JOIN experiment_registrations experiment ON experiment.strategy_version_id=strategy.id
JOIN configuration_versions configuration ON configuration.id=experiment.configuration_id
CROSS JOIN portfolios portfolio
WHERE strategy.id='trend-v1a-1'
  AND experiment.status IN ('registered','running','completed','locked')
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(convert_from(configuration.canonical_payload,'UTF8')::jsonb->'instruments') item
    WHERE item->>'base'=$1 AND item->>'quote'=$2 AND item->>'product'='spot'
  )
ORDER BY portfolio.id,configuration.id LIMIT 1`, base, quote).Scan(&portfolioID, &configurationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", console.ErrPrecondition
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
func (store *A11ConsoleStore) ControlRun(
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
	default:
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
}

var _ console.RunCommandService = (*A11ConsoleStore)(nil)
