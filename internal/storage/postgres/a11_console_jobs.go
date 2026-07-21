package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/research"

	"github.com/jackc/pgx/v5"
)

// Job returns one durable backtest or replay lifecycle record.
func (store *A11ConsoleStore) Job(ctx context.Context, id, eventOrdinal string) (generated.JobResource, error) {
	var item generated.JobResource
	var kind, state, runID string
	var revision int64
	var failure *string
	var requestPayload, result []byte
	err := store.pool.QueryRow(ctx, `SELECT id,job_type,state,created_at,updated_at,progress_revision,failure_code,
	  coalesce(result_payload::text,''),coalesce(run_id,''),request_payload FROM jobs
	  WHERE id=$1 AND job_type IN ('backtest','replay') AND request_payload IS NOT NULL`, id).
		Scan(&item.Id, &kind, &state, &item.CreatedAt, &item.UpdatedAt, &revision, &failure, &result, &runID, &requestPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.JobResource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.JobResource{}, err
	}
	item.Kind = generated.JobResourceKind(kind)
	item.State = generated.JobResourceState(state)
	item.Revision = strconv.FormatInt(revision, 10)
	item.FailureCode = failure
	request, requestErr := decodeA11OfflineRequest(kind, requestPayload)
	if requestErr != nil {
		return generated.JobResource{}, requestErr
	}
	item.RegisteredReport, err = store.registeredReport(ctx, request.ResearchGenerationID)
	if err != nil {
		return generated.JobResource{}, err
	}
	if err = store.populateJobMode(ctx, &item, kind, runID, eventOrdinal); err != nil {
		return generated.JobResource{}, err
	}
	if len(result) > 0 {
		var projection generated.JobResult
		if err = json.Unmarshal(result, &projection); err != nil {
			return generated.JobResource{}, err
		}
		item.Result = &projection
	}
	return item, nil
}

func (store *A11ConsoleStore) populateJobMode(ctx context.Context, item *generated.JobResource, kind, runID, eventOrdinal string) error {
	if kind == "backtest" {
		if eventOrdinal != "" {
			return console.ErrInvalidRequest
		}
		item.ModeLabel = generated.BACKTEST
		return nil
	}
	item.ModeLabel = generated.REPLAY
	inspection, err := store.replayInspection(ctx, runID, eventOrdinal)
	if err != nil {
		return err
	}
	item.ReplayInspection = inspection
	if inspection != nil {
		cursor := generated.Revision(inspection.Ordinal)
		item.CursorOrdinal = &cursor
	}
	return nil
}

func (store *A11ConsoleStore) registeredReport(ctx context.Context, generationID string) (*generated.RegisteredResearchReport, error) {
	var item generated.RegisteredResearchReport
	var canonical []byte
	var confidence, viability string
	err := store.pool.QueryRow(ctx, `SELECT id,manifest_hash::text,canonical_manifest,confidence_label,
	  platform_correctness,strategy_evidence,viability_disposition,created_at
	  FROM research_reports WHERE research_generation_id=$1 AND confidence_label<>'insufficient'
	  ORDER BY created_at DESC,id DESC LIMIT 1`, generationID).Scan(&item.Id, &item.ManifestHash, &canonical,
		&confidence, &item.PlatformCorrectness, &item.StrategyEvidence, &viability, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	manifest, err := research.ValidateReportCanonical(canonical, item.ManifestHash, generationID, "")
	if err != nil || len(canonical) < 2 || len(canonical) > 1_048_576 ||
		manifest.ConfidenceLabel != confidence || manifest.PlatformCorrectness != item.PlatformCorrectness ||
		manifest.StrategyEvidence != item.StrategyEvidence || manifest.ViabilityDisposition != viability {
		return nil, fmt.Errorf("a11_registered_report_invalid")
	}
	item.ResearchGenerationId = generationID
	item.ConfidenceLabel = generated.RegisteredResearchReportConfidenceLabel(confidence)
	item.Viability = generated.RegisteredResearchReportViability(viability)
	item.Disclaimer = manifest.Disclaimer
	item.RunReferences = append([]string(nil), manifest.RunReferences...)
	item.CanonicalManifest = string(canonical)
	item.Benchmarks, err = a11ResearchSlices(manifest.Benchmarks)
	if err != nil {
		return nil, err
	}
	item.Stress, err = a11ResearchSlices(manifest.Stress)
	if err != nil {
		return nil, err
	}
	item.Capacity = make([]generated.ResearchCapacityPoint, len(manifest.Capacity))
	for index, point := range manifest.Capacity {
		item.Capacity[index] = generated.ResearchCapacityPoint{
			Notional: point.Notional, NetReturn: point.NetReturn, FillRate: point.FillRate,
		}
	}
	return &item, nil
}

func a11ResearchSlices(source []research.ResultSlice) ([]generated.ResearchResultSlice, error) {
	result := make([]generated.ResearchResultSlice, len(source))
	for index, slice := range source {
		if slice.Trades > math.MaxInt64 {
			return nil, fmt.Errorf("a11_registered_report_invalid")
		}
		result[index] = generated.ResearchResultSlice{Name: slice.Name, NetReturn: slice.NetReturn,
			MaxDrawdown: slice.MaxDrawdown, Trades: int64(slice.Trades)}
	}
	return result, nil
}

func (store *A11ConsoleStore) replayInspection(ctx context.Context, runID, requested string) (*generated.ReplayEventInspection, error) {
	if runID == "" {
		if requested != "" {
			return nil, console.ErrNotFound
		}
		return nil, nil
	}
	var count, newest int64
	if err := store.pool.QueryRow(ctx, `SELECT count(*)::bigint,coalesce(max(ordinal),0)::bigint
	  FROM run_canonical_outputs WHERE run_id=$1 AND output_kind='event'`, runID).Scan(&count, &newest); err != nil {
		return nil, err
	}
	if count == 0 {
		if requested != "" {
			return nil, console.ErrNotFound
		}
		return nil, nil
	}
	selected, err := a11ReplayOrdinal(newest, requested)
	if err != nil {
		return nil, err
	}
	return store.loadReplayInspection(ctx, runID, count, selected)
}

func a11ReplayOrdinal(newest int64, requested string) (int64, error) {
	if requested == "" {
		return newest, nil
	}
	parsed, err := strconv.ParseInt(requested, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != requested {
		return 0, console.ErrInvalidRequest
	}
	return parsed, nil
}

func (store *A11ConsoleStore) loadReplayInspection(ctx context.Context, runID string, count, selected int64) (*generated.ReplayEventInspection, error) {
	rows, err := store.pool.Query(ctx, `SELECT output_kind,output_hash::text,convert_from(canonical_payload,'UTF8')
	  FROM run_canonical_outputs WHERE run_id=$1 AND ordinal=$2
	  AND output_kind IN ('event','decision','order','projection','balance') ORDER BY output_kind`, runID, selected)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inspection := generated.ReplayEventInspection{EventCount: strconv.FormatInt(count, 10),
		Ordinal: strconv.FormatInt(selected, 10)}
	found := 0
	for rows.Next() {
		var kind, hash, payload string
		if err = rows.Scan(&kind, &hash, &payload); err != nil || !json.Valid([]byte(payload)) {
			return nil, fmt.Errorf("a11_replay_inspection_invalid")
		}
		switch kind {
		case "event":
			inspection.EventHash, inspection.CanonicalEvent = hash, payload
		case "decision":
			inspection.CanonicalDecision = payload
		case "order":
			inspection.CanonicalOrders = payload
		case "projection":
			inspection.CanonicalExecutionEvents = payload
		case "balance":
			inspection.CanonicalBalances = payload
		}
		found++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if found != 5 || !a11ReplayInspectionComplete(inspection) {
		return nil, console.ErrNotFound
	}
	return &inspection, nil
}

func a11ReplayInspectionComplete(inspection generated.ReplayEventInspection) bool {
	return inspection.EventHash != "" && inspection.CanonicalEvent != "" &&
		inspection.CanonicalDecision != "" && inspection.CanonicalOrders != "" &&
		inspection.CanonicalExecutionEvents != "" && inspection.CanonicalBalances != ""
}
