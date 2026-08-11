package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/recorder"
	"axiom/internal/replay"
)

type evaluationDatasetMember struct {
	input recordedDatasetInput
	role  string
}

func (materializer *ownerConsoleMaterializer) openEvaluationDataset(ctx context.Context,
	request ownerConsoleOfflineRequest, primary recordedDatasetInput,
) (replay.Source, backtest.DatasetDescriptor, string, error) {
	members, err := materializer.loadEvaluationDatasetMembers(ctx, request)
	if err != nil || len(members) == 0 || members[0].input.id != primary.id {
		if err == nil {
			err = fmt.Errorf("evaluation_dataset_members_unavailable")
		}
		return nil, backtest.DatasetDescriptor{}, "", err
	}
	readers, descriptors, role, err := materializer.openEvaluationDatasetMembers(members)
	if err != nil {
		return nil, backtest.DatasetDescriptor{}, "", err
	}
	source, descriptor, err := materializer.evaluationSourceForRole(ctx, request, role, readers, descriptors)
	if err != nil {
		return nil, backtest.DatasetDescriptor{}, "", err
	}
	source, err = windowEvaluationSource(source, request)
	return source, descriptor, role, err
}

func (materializer *ownerConsoleMaterializer) loadEvaluationDatasetMembers(ctx context.Context,
	request ownerConsoleOfflineRequest) ([]evaluationDatasetMember, error) {
	strategy := evaluationStrategyID(request.StrategyVersion)
	rows, err := materializer.pool.Query(ctx, `SELECT member.evidence_role,manifest.id,
  manifest.recorder_dataset_id,manifest.dataset_hash,manifest.manifest_revision,
  manifest.manifest_path,manifest.source_commit,manifest.dataset_kind
FROM evaluation_campaign_dataset_members member
JOIN dataset_manifests manifest ON manifest.id=member.dataset_id
WHERE member.campaign_id=$1 AND member.strategy_id=$2
  AND manifest.state IN ('ready','qualified') AND manifest.dataset_kind='public_market'
ORDER BY member.member_ordinal`, request.EvaluationCampaignID, strategy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]evaluationDatasetMember, 0, 2)
	for rows.Next() {
		var member evaluationDatasetMember
		if err = rows.Scan(&member.role, &member.input.id, &member.input.recorderID,
			&member.input.hash, &member.input.revision, &member.input.path,
			&member.input.sourceCommit, &member.input.kind); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func (materializer *ownerConsoleMaterializer) openEvaluationDatasetMembers(
	members []evaluationDatasetMember) ([]*backtest.DatasetReader, []backtest.DatasetDescriptor, string, error) {
	role := members[0].role
	readers := make([]*backtest.DatasetReader, 0, len(members))
	descriptors := make([]backtest.DatasetDescriptor, 0, len(members))
	for _, member := range members {
		if member.role != role {
			return nil, nil, "", fmt.Errorf("evaluation_dataset_role_conflict")
		}
		reader, descriptor, openErr := materializer.openDataset(member.input)
		if openErr != nil {
			return nil, nil, "", openErr
		}
		readers, descriptors = append(readers, reader), append(descriptors, descriptor)
	}
	return readers, descriptors, role, nil
}

func (materializer *ownerConsoleMaterializer) evaluationSourceForRole(ctx context.Context,
	request ownerConsoleOfflineRequest, role string, readers []*backtest.DatasetReader,
	descriptors []backtest.DatasetDescriptor) (replay.Source, backtest.DatasetDescriptor, error) {
	var source replay.Source
	descriptor := descriptors[0]
	var err error
	switch role {
	case "historical_candles":
		if len(readers) != 1 {
			return nil, backtest.DatasetDescriptor{}, fmt.Errorf("evaluation_historical_dataset_ambiguous")
		}
		candleSource, sourceErr := backtest.NewEvaluationCandleSource(readers[0])
		if sourceErr != nil {
			return nil, backtest.DatasetDescriptor{}, sourceErr
		}
		descriptor.RecordCount = candleSource.RecordCount()
		source = candleSource
	case "public_market":
		if len(readers) == 1 {
			source, err = backtest.NewDatasetSource(readers[0])
		} else {
			source, err = backtest.NewEvaluationMergedSource(readers...)
			if err != nil {
				return nil, backtest.DatasetDescriptor{}, err
			}
			descriptor, err = materializer.aggregateEvaluationDescriptor(ctx, request, descriptors)
		}
	default:
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("evaluation_dataset_role_invalid")
	}
	if err != nil {
		return nil, backtest.DatasetDescriptor{}, err
	}
	return source, descriptor, nil
}

func windowEvaluationSource(source replay.Source, request ownerConsoleOfflineRequest) (replay.Source, error) {
	if request.FirstOrdinal != nil {
		first, firstErr := strconv.ParseUint(*request.FirstOrdinal, 10, 64)
		last, lastErr := strconv.ParseUint(*request.LastOrdinal, 10, 64)
		if firstErr != nil || lastErr != nil {
			return nil, fmt.Errorf("evaluation_dataset_window_invalid")
		}
		windowed, err := backtest.NewEvaluationWindowSource(source, first, last)
		if err != nil {
			return nil, err
		}
		source = windowed
	}
	return source, nil
}

func (materializer *ownerConsoleMaterializer) aggregateEvaluationDescriptor(ctx context.Context,
	request ownerConsoleOfflineRequest, members []backtest.DatasetDescriptor,
) (backtest.DatasetDescriptor, error) {
	var registeredHash string
	if err := materializer.pool.QueryRow(ctx, `SELECT encode(manifest_hash,'hex')
FROM evaluation_campaign_datasets WHERE campaign_id=$1 AND strategy_id=$2`,
		request.EvaluationCampaignID, evaluationStrategyID(request.StrategyVersion)).Scan(&registeredHash); err != nil {
		return backtest.DatasetDescriptor{}, err
	}
	values := make([]string, 0)
	segmentHashes := make([]string, 0)
	var records, gaps, lowDensity uint64
	confidence := backtest.ConfidenceA
	complete := true
	for _, member := range members {
		values = append(values, member.ManifestHash)
		values = append(values, member.SegmentHashes...)
		segmentHashes = append(segmentHashes, member.SegmentHashes...)
		records += member.RecordCount
		gaps += member.GapCount
		lowDensity += member.LowDensitySegments
		complete = complete && member.Complete
		if member.Confidence > confidence {
			confidence = member.Confidence
		}
	}
	digest := sha256.Sum256([]byte(joinEvaluationHashes(values)))
	if hex.EncodeToString(digest[:]) != registeredHash {
		return backtest.DatasetDescriptor{}, fmt.Errorf("evaluation_dataset_set_hash_conflict")
	}
	return backtest.DatasetDescriptor{DatasetID: "evaluation-set:" + request.EvaluationCampaignID + ":" +
		evaluationStrategyID(request.StrategyVersion), ManifestHash: registeredHash, Revision: 1,
		SourceCommit: members[0].SourceCommit, SchemaVersion: "axiom.evaluation-dataset-set.v1",
		ParserVersion: "recorded-public-mixed.v1", NormalizationVersion: "recorded-public-mixed.v1",
		SegmentHashes: segmentHashes, RecordCount: records, GapCount: gaps, LowDensitySegments: lowDensity,
		Complete: complete, Confidence: confidence}, nil
}

func joinEvaluationHashes(values []string) string {
	result := "evaluation-dataset-set-v1"
	for _, value := range values {
		result += ":" + value
	}
	return result
}

func (materializer *ownerConsoleMaterializer) evaluationMetadata(ctx context.Context,
	campaignID string) (map[string]domain.InstrumentMetadata, map[string]string, error) {
	rows, err := materializer.pool.Query(ctx, `SELECT selected.exchange_id,selected.instrument_id,
  metadata.id,metadata.version,metadata.effective_at,metadata.price_tick::text,
  metadata.quantity_step::text,metadata.minimum_quantity::text,metadata.minimum_notional::text,
  instrument.base_asset,instrument.quote_asset
FROM evaluation_campaign_metadata selected
JOIN instrument_metadata_versions metadata ON metadata.id=selected.metadata_id
JOIN instruments instrument ON instrument.id=selected.instrument_id
WHERE selected.campaign_id=$1 ORDER BY selected.exchange_id,selected.instrument_id`, campaignID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	metadata := make(map[string]domain.InstrumentMetadata, 6)
	ids := make(map[string]string, 6)
	for rows.Next() {
		var exchange, symbol, id, priceTick, quantityStep, minimumQuantity, minimumNotional, base, quote string
		var version uint64
		var effectiveAt time.Time
		if err = rows.Scan(&exchange, &symbol, &id, &version, &effectiveAt, &priceTick,
			&quantityStep, &minimumQuantity, &minimumNotional, &base, &quote); err != nil {
			return nil, nil, err
		}
		baseAsset, baseErr := domain.ParseAssetSymbol(base)
		quoteAsset, quoteErr := domain.ParseAssetSymbol(quote)
		instrument, instrumentErr := domain.NewSpotInstrument(baseAsset, quoteAsset)
		price, priceErr := domain.ParsePrice(priceTick)
		step, stepErr := domain.ParseQuantity(quantityStep)
		minimum, minimumErr := domain.ParseQuantity(minimumQuantity)
		notional, notionalErr := domain.ParseNotional(minimumNotional)
		value := domain.InstrumentMetadata{Instrument: instrument, Version: version,
			EffectiveAt: effectiveAt.UTC(), PriceTick: price, QuantityStep: step,
			MinimumQuantity: minimum, MinimumNotional: notional}
		if baseErr != nil || quoteErr != nil || instrumentErr != nil || priceErr != nil || stepErr != nil ||
			minimumErr != nil || notionalErr != nil || value.Validate() != nil || symbol != instrument.Symbol() {
			return nil, nil, fmt.Errorf("evaluation_metadata_invalid")
		}
		key := exchange + ":" + symbol
		metadata[key], ids[key] = value, id
	}
	if rows.Err() != nil || len(metadata) != 6 {
		return nil, nil, fmt.Errorf("evaluation_metadata_incomplete")
	}
	return metadata, ids, nil
}

func evaluationStrategyID(version string) string {
	switch version {
	case "trend-following@1.0.0":
		return "trend-following"
	case "mean-reversion@1.0.0":
		return "mean-reversion"
	case "triangular-arbitrage@1.0.0":
		return "triangular-arbitrage"
	case "cross-exchange-arbitrage@1.0.0":
		return "cross-exchange-arbitrage"
	case "inventory-rebalancing@1.0.0":
		return "inventory-rebalancing"
	default:
		return ""
	}
}

func (materializer *ownerConsoleMaterializer) findDatasetManifest(input recordedDatasetInput) (string, string, error) {
	if filepath.Base(input.path) != input.path || input.path == "" {
		return "", "", fmt.Errorf("owner_console_job_dataset_identity_invalid")
	}
	roots := []string{materializer.root, filepath.Join(materializer.root, "binance"),
		filepath.Join(materializer.root, "bybit"), filepath.Join(materializer.root, "evaluation-history")}
	for _, root := range roots {
		path := filepath.Join(root, input.path)
		manifest, err := recorder.ReadManifest(path)
		if err == nil && manifest.Hash == input.hash && manifest.DatasetID == input.recorderID &&
			int64(manifest.Revision) == input.revision {
			return root, path, nil
		}
	}
	return "", "", fmt.Errorf("owner_console_job_dataset_identity_invalid")
}
