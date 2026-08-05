package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
)

// DataCatalogue presents immutable, protected server-side dataset evidence in
// owner language. It does not return a storage path or accept a client upload.
func (store *A11ConsoleStore) DataCatalogue(ctx context.Context) (generated.DataCataloguePage, error) {
	rows, err := store.pool.Query(ctx, `
SELECT manifest.dataset_hash::text,manifest.dataset_kind,manifest.state,
  coalesce(manifest.quality_tier,''),manifest.coverage_start,manifest.coverage_end,
  (SELECT count(*)::integer FROM dataset_segments segment WHERE segment.dataset_id=manifest.id),
  (SELECT count(*)::integer FROM dataset_gaps gap WHERE gap.dataset_id=manifest.id),
  coalesce(
    nullif((SELECT array_agg(coverage.exchange_id ORDER BY coverage.exchange_id)
      FROM dataset_exchange_coverage coverage WHERE coverage.dataset_id=manifest.id), '{}'),
    (SELECT array_agg(DISTINCT segment.exchange_id ORDER BY segment.exchange_id)
      FROM dataset_segments member JOIN market_data_segments segment ON segment.id=member.segment_id
      WHERE member.dataset_id=manifest.id)
  )
FROM dataset_manifests manifest
WHERE manifest.state IN ('building','ready','qualified','rejected')
  AND EXISTS (
    SELECT 1 FROM dataset_exchange_coverage coverage WHERE coverage.dataset_id=manifest.id
    UNION ALL
    SELECT 1 FROM dataset_segments member JOIN market_data_segments segment ON segment.id=member.segment_id
      WHERE member.dataset_id=manifest.id
  )
ORDER BY manifest.coverage_end DESC,manifest.dataset_hash DESC LIMIT 100`)
	if err != nil {
		return generated.DataCataloguePage{}, err
	}
	defer rows.Close()
	page := generated.DataCataloguePage{Items: make([]generated.DataCatalogueItem, 0)}
	for rows.Next() {
		item, scanErr := scanOwnerDataCatalogue(rows)
		if scanErr != nil {
			return generated.DataCataloguePage{}, scanErr
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

type ownerDataCatalogueScanner interface{ Scan(...any) error }

func scanOwnerDataCatalogue(scanner ownerDataCatalogueScanner) (generated.DataCatalogueItem, error) {
	var hash, kind, state, quality string
	var start, end time.Time
	var segments, gaps int
	var exchanges []string
	if err := scanner.Scan(&hash, &kind, &state, &quality, &start, &end, &segments, &gaps, &exchanges); err != nil {
		return generated.DataCatalogueItem{}, err
	}
	if len(hash) != 64 || len(exchanges) == 0 || segments < 0 || gaps < 0 {
		return generated.DataCatalogueItem{}, fmt.Errorf("owner_data_catalogue_invalid")
	}
	item := generated.DataCatalogueItem{ManifestHash: hash, State: generated.DataCatalogueItemState(state),
		CoverageStart: start.UTC(), CoverageEnd: end.UTC(), SegmentCount: segments, KnownGapCount: gaps,
		Exchanges: make([]generated.DataCatalogueItemExchanges, 0, len(exchanges))}
	for _, exchange := range exchanges {
		if exchange != "binance" && exchange != "bybit" {
			return generated.DataCatalogueItem{}, fmt.Errorf("owner_data_catalogue_invalid")
		}
		item.Exchanges = append(item.Exchanges, generated.DataCatalogueItemExchanges(exchange))
	}
	switch kind {
	case "decision_inputs":
		item.Name = "Approved historical decision data ending " + end.UTC().Format("2006-01-02")
		item.Source = generated.DataCatalogueItemSource("approved_historical_data")
		item.SupportedModes = []generated.DataCatalogueItemSupportedModes{"backtest", "replay"}
	case "public_market":
		item.Name = "Recorded public market data ending " + end.UTC().Format("2006-01-02")
		item.Source = generated.DataCatalogueItemSource("recorded_public_data")
		item.SupportedModes = []generated.DataCatalogueItemSupportedModes{"shadow"}
	default:
		return generated.DataCatalogueItem{}, fmt.Errorf("owner_data_catalogue_invalid")
	}
	tier := generated.DataCatalogueItemQualityTier("unclassified")
	if quality == "A" {
		tier = generated.DataCatalogueItemQualityTier("tier_a")
	}
	item.QualityTier = &tier
	return item, nil
}

var _ console.DataCatalogueReadService = (*A11ConsoleStore)(nil)
