package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

// RunOutputs returns the immutable evidence already written by the offline
// reducer. It does not reconstruct an event from a mutable projection.
func (store *A11ConsoleStore) RunOutputs(ctx context.Context, id, requested string) (generated.RunOutputPage, error) {
	if _, err := store.Run(ctx, id); err != nil {
		return generated.RunOutputPage{}, err
	}
	kind, exposed, ok := ownerRunOutputKind(requested)
	if !ok {
		return generated.RunOutputPage{}, console.ErrInvalidRequest
	}
	rows, err := store.pool.Query(ctx, `SELECT ordinal,output_hash::text,convert_from(canonical_payload,'UTF8')
	  FROM run_canonical_outputs WHERE run_id=$1 AND output_kind=$2 ORDER BY ordinal ASC LIMIT 500`, id, kind)
	if err != nil {
		return generated.RunOutputPage{}, err
	}
	defer rows.Close()
	page := generated.RunOutputPage{Items: make([]generated.RunOutput, 0)}
	for rows.Next() {
		var ordinal int64
		var hash, payload string
		if err = rows.Scan(&ordinal, &hash, &payload); err != nil {
			return generated.RunOutputPage{}, err
		}
		if ordinal < 0 || !ownerRunCanonicalPayloadValid(payload) {
			return generated.RunOutputPage{}, fmt.Errorf("run_output_projection_invalid")
		}
		page.Items = append(page.Items, generated.RunOutput{Ordinal: strconv.FormatInt(ordinal, 10),
			Kind: exposed, ContentHash: hash, CanonicalPayload: payload})
	}
	return page, rows.Err()
}

func ownerRunOutputKind(value string) (stored string, exposed generated.RunOutputKind, ok bool) {
	switch value {
	case "event":
		return "event", generated.Event, true
	case "decision":
		return "decision", generated.Decision, true
	case "order":
		return "order", generated.Order, true
	case "projection":
		return "projection", generated.Execution, true
	default:
		return "", "", false
	}
}

func ownerRunCanonicalPayloadValid(payload string) bool {
	return len(payload) >= 2 && len(payload) <= 1_048_576 && json.Valid([]byte(payload))
}

// RunPortfolio returns the newest reducer-owned balance projection. Shadow
// sessions and queued offline jobs honestly report that a portfolio snapshot is
// not recorded yet instead of using a current global balance as a substitute.
func (store *A11ConsoleStore) RunPortfolio(ctx context.Context, id string) (generated.RunPortfolioProjection, error) {
	if _, err := store.Run(ctx, id); err != nil {
		return generated.RunPortfolioProjection{}, err
	}
	var ordinal int64
	var hash, payload string
	err := store.pool.QueryRow(ctx, `SELECT ordinal,output_hash::text,convert_from(canonical_payload,'UTF8')
	  FROM run_canonical_outputs WHERE run_id=$1 AND output_kind='balance' ORDER BY ordinal DESC LIMIT 1`, id).
		Scan(&ordinal, &hash, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		reason := "No reducer-owned portfolio snapshot has been recorded for this run yet."
		return generated.RunPortfolioProjection{State: generated.RunPortfolioProjectionStateNotRecorded, WaitingReason: &reason}, nil
	}
	if err != nil {
		return generated.RunPortfolioProjection{}, err
	}
	if ordinal < 0 || !ownerRunCanonicalPayloadValid(payload) {
		return generated.RunPortfolioProjection{}, fmt.Errorf("run_portfolio_projection_invalid")
	}
	value := strconv.FormatInt(ordinal, 10)
	return generated.RunPortfolioProjection{State: generated.RunPortfolioProjectionStateRecorded,
		Ordinal: &value, ContentHash: &hash, CanonicalPayload: &payload}, nil
}

// RunRisk makes the current evidence boundary visible. Existing durable output
// records do not yet contain a run-scoped risk evaluation, so reporting the
// global risk state here would be misleading.
func (store *A11ConsoleStore) RunRisk(ctx context.Context, id string) (generated.RunRiskProjection, error) {
	if _, err := store.Run(ctx, id); err != nil {
		return generated.RunRiskProjection{}, err
	}
	return generated.RunRiskProjection{State: generated.NotRecorded,
		Summary: "This run has no separately recorded run-scoped risk projection yet; global risk state is intentionally not substituted."}, nil
}

// RunEvidence exposes only reproducibility hashes and identifiers that are
// safe to display in advanced details. It never returns a filesystem path,
// dataset ID, configuration ID, credential, or signed request material.
func (store *A11ConsoleStore) RunEvidence(ctx context.Context, id string) (generated.RunEvidence, error) {
	if _, err := store.Run(ctx, id); err != nil {
		return generated.RunEvidence{}, err
	}
	var manifest, source, configuration, dataset, namespace, confidence string
	err := store.pool.QueryRow(ctx, `SELECT manifest_hash::text,source_commit,configuration_hash::text,
	  dataset_manifest_hash::text,model_namespace_id,confidence_tier
	  FROM run_manifests WHERE run_id=$1`, id).
		Scan(&manifest, &source, &configuration, &dataset, &namespace, &confidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.RunEvidence{State: generated.RunEvidenceStateNotRecorded}, nil
	}
	if err != nil {
		return generated.RunEvidence{}, err
	}
	tier := generated.RunEvidenceConfidenceTier(confidence)
	if !tier.Valid() {
		return generated.RunEvidence{}, fmt.Errorf("run_evidence_projection_invalid")
	}
	return generated.RunEvidence{State: generated.RunEvidenceStateRecorded, ManifestHash: &manifest,
		SourceCommit: &source, ConfigurationHash: &configuration, DatasetManifestHash: &dataset,
		ModelNamespace: &namespace, ConfidenceTier: &tier}, nil
}

var _ console.RunReadService = (*A11ConsoleStore)(nil)
