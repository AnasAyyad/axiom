package postgres

import (
	"context"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
)

const multiExchangeConsoleExchangesSQL = `SELECT exchange.id,exchange.name,
  coalesce(segment.ended_at,'epoch'::timestamptz),
  coalesce(segment.last_ordinal,0),
  ARRAY(SELECT DISTINCT capability.capability FROM exchange_capabilities capability
    WHERE capability.exchange_id=exchange.id AND capability.supported
    ORDER BY capability.capability),
  coalesce((SELECT count(DISTINCT metadata.instrument_id)::integer
    FROM instrument_metadata_versions metadata WHERE metadata.exchange_id=exchange.id),0),
  coalesce((SELECT count(*)::integer FROM data_quality_events quality
    JOIN dataset_manifests manifest ON manifest.id=quality.dataset_id
    JOIN dataset_exchange_coverage coverage ON coverage.dataset_id=manifest.id
    WHERE coverage.exchange_id=exchange.id AND quality.event_type='sequence_gap'),0),
  coalesce((SELECT count(*)::integer FROM public_connection_events connection
    WHERE connection.exchange_id=exchange.id AND connection.state='CONNECTING'
      AND connection.connection_generation>1),0)
FROM exchanges exchange
LEFT JOIN LATERAL (
  SELECT ended_at,last_ordinal FROM market_data_segments
  WHERE exchange_id=exchange.id AND state='ready'
  ORDER BY ended_at DESC,id DESC LIMIT 1
) segment ON true
WHERE exchange.environment='production_public' AND ($1='' OR exchange.id>$1)
ORDER BY exchange.id LIMIT $2`

const multiExchangeConsoleStrategiesSQL = `SELECT version.id,definition.family,definition.name,
  version.version::text,coalesce(version.supported_modes,ARRAY[]::text[]),
  coalesce(maturity.maturity,'EXPERIMENTAL'),
  coalesce((SELECT role FROM (
    SELECT 'champion'::text role,report.created_at FROM research_promotion_champion_challenger_reports report
      WHERE report.champion_strategy_version_id=version.id
    UNION ALL
    SELECT 'challenger'::text role,report.created_at FROM research_promotion_champion_challenger_reports report
      WHERE report.challenger_strategy_version_id=version.id
  ) roles ORDER BY created_at DESC LIMIT 1),'unassigned'),
  coalesce(suite.confidence_label,'insufficient'),
  coalesce(suite.viability_disposition,'undetermined'),
  coalesce(suite.disclaimer_policy,'no_production_profitability_claim'),
  version.created_at
FROM strategy_versions version
JOIN strategy_definitions definition ON definition.id=version.strategy_id
LEFT JOIN strategy_maturity_states maturity ON maturity.strategy_version_id=version.id
LEFT JOIN LATERAL (
  SELECT confidence_label,viability_disposition,disclaimer_policy
  FROM research_promotion_validation_suites WHERE strategy_version_id=version.id
  ORDER BY created_at DESC,id DESC LIMIT 1
) suite ON true
WHERE cardinality(coalesce(version.supported_modes,ARRAY[]::text[]))>0
  AND ($1='' OR version.id>$1)
ORDER BY version.id LIMIT $2`

// Exchanges returns generic production-public exchange health without changing
// the retained Binance-specific owner console alias.
func (store *OwnerConsoleStore) Exchanges(
	ctx context.Context,
	cursor string,
	limit int,
) (generated.ExchangePage, error) {
	position, err := decodeMultiExchangeConsoleStringCursor(store.cursor, "multi_exchange_console-exchanges", cursor)
	if err != nil {
		return generated.ExchangePage{}, err
	}
	rows, err := store.pool.Query(ctx, multiExchangeConsoleExchangesSQL, position, limit+1)
	if err != nil {
		return generated.ExchangePage{}, err
	}
	defer rows.Close()
	now := store.clock.Now().UTC
	items := make([]generated.ExchangeSummary, 0, limit+1)
	for rows.Next() {
		var id, name string
		var observed time.Time
		var revision int64
		var capabilities []string
		var instruments, gaps, reconnects int
		if err = rows.Scan(&id, &name, &observed, &revision, &capabilities, &instruments, &gaps, &reconnects); err != nil {
			return generated.ExchangePage{}, err
		}
		items = append(items, multiExchangeConsoleExchangeSummary(
			id, name, observed, now, revision, capabilities, instruments, gaps, reconnects,
		))
	}
	if err = rows.Err(); err != nil {
		return generated.ExchangePage{}, err
	}
	snapshot, err := multiExchangeConsoleSnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.ExchangePage{}, err
	}
	page := generated.ExchangePage{Items: items, Revision: snapshot, SnapshotRevision: snapshot}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		next := encodeMultiExchangeConsoleStringCursor(store.cursor, "multi_exchange_console-exchanges", items[len(items)-1].Id)
		page.NextCursor = &next
	}
	return page, nil
}

func multiExchangeConsoleExchangeSummary(id, name string, observed, now time.Time, revision int64,
	capabilities []string, instruments, gaps, reconnects int) generated.ExchangeSummary {
	hasEvidence := !observed.Equal(time.Unix(0, 0).UTC())
	websocket, book, recorder := "stale", "stale", "unavailable"
	confidence := generated.QualityEvidenceConfidence("unknown")
	if hasEvidence {
		websocket, book, recorder = "healthy", "healthy", "healthy"
		confidence = generated.QualityEvidenceConfidence("high")
		if now.Sub(observed) > 5*time.Minute {
			websocket, book, recorder = "stale", "stale", "degraded"
			confidence = generated.QualityEvidenceConfidence("low")
		}
	} else {
		observed = now
	}
	var age *generated.Revision
	if hasEvidence {
		value := generated.Revision(strconv.FormatInt(max(now.Sub(observed).Milliseconds(), 0), 10))
		age = &value
	}
	quality := multiExchangeConsoleQuality("market_data_segments", observed, confidence, multiExchangeConsoleFreshness(now, observed))
	quality.ProvenanceComplete = hasEvidence
	return generated.ExchangeSummary{
		Id: id, Name: name, Environment: generated.ExchangeSummaryEnvironment("production_public"),
		PublicOnly: true, WebsocketState: generated.ExchangeSummaryWebsocketState(websocket),
		BookState:     generated.ExchangeSummaryBookState(book),
		RecorderState: generated.ExchangeSummaryRecorderState(recorder),
		Capabilities:  capabilities, Instruments: instruments, LastMessageAgeMs: age,
		SequenceGaps: &gaps, Reconnects: &reconnects,
		Quality:  quality,
		Revision: strconv.FormatInt(revision, 10),
	}
}

// Strategies returns one generic version, mode, and evidence projection.
func (store *OwnerConsoleStore) Strategies(
	ctx context.Context,
	cursor string,
	limit int,
) (generated.StrategyPage, error) {
	position, err := decodeMultiExchangeConsoleStringCursor(store.cursor, "multi_exchange_console-strategies", cursor)
	if err != nil {
		return generated.StrategyPage{}, err
	}
	rows, err := store.pool.Query(ctx, multiExchangeConsoleStrategiesSQL, position, limit+1)
	if err != nil {
		return generated.StrategyPage{}, err
	}
	defer rows.Close()
	items := make([]generated.StrategySummary, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanMultiExchangeConsoleStrategy(rows)
		if scanErr != nil {
			return generated.StrategyPage{}, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return generated.StrategyPage{}, err
	}
	snapshot, err := multiExchangeConsoleSnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.StrategyPage{}, err
	}
	page := generated.StrategyPage{Items: items, Revision: snapshot, SnapshotRevision: snapshot}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		next := encodeMultiExchangeConsoleStringCursor(store.cursor, "multi_exchange_console-strategies", items[len(items)-1].Id)
		page.NextCursor = &next
	}
	return page, nil
}

type multiExchangeConsoleRowScanner interface{ Scan(...any) error }

func scanMultiExchangeConsoleStrategy(row multiExchangeConsoleRowScanner) (generated.StrategySummary, error) {
	var item generated.StrategySummary
	var modes []string
	var maturity, role, confidence, viability string
	if err := row.Scan(&item.Id, &item.Family, &item.Name, &item.Version, &modes,
		&maturity, &role, &confidence, &viability, &item.Disclaimer, &item.CreatedAt); err != nil {
		return generated.StrategySummary{}, err
	}
	item.Maturity = generated.StrategySummaryMaturity(normalizeMultiExchangeConsoleMaturity(maturity))
	item.EvidenceRole = generated.StrategySummaryEvidenceRole(role)
	item.Confidence = generated.StrategySummaryConfidence(normalizeMultiExchangeConsoleConfidence(confidence))
	item.Viability = generated.StrategySummaryViability(normalizeMultiExchangeConsoleViability(viability))
	item.Revision = strconv.FormatInt(item.CreatedAt.UnixNano(), 10)
	for _, mode := range modes {
		if mode == "backtest" || mode == "replay" || mode == "shadow" {
			item.SupportedModes = append(item.SupportedModes, generated.StrategySummarySupportedModes(mode))
		}
	}
	return item, nil
}

func normalizeMultiExchangeConsoleMaturity(value string) string {
	switch value {
	case "EXPERIMENTAL", "BACKTEST_VALIDATED", "REPLAY_VALIDATED", "SHADOW_VALIDATED", "REJECTED":
		return value
	default:
		return "EXPERIMENTAL"
	}
}

func normalizeMultiExchangeConsoleConfidence(value string) string {
	switch value {
	case "formal_tier_a", "local_tier_b", "insufficient", "rejected":
		return value
	default:
		return "insufficient"
	}
}

func normalizeMultiExchangeConsoleViability(value string) string {
	switch value {
	case "viable_for_more_research", "rejected":
		return value
	default:
		return "undetermined"
	}
}

var _ console.ReadService = (*OwnerConsoleStore)(nil)
