package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

// Instruments returns latest Binance Spot metadata in stable symbol order.
func (store *A11ConsoleStore) Instruments(ctx context.Context, cursor string, limit int) (generated.InstrumentPage, error) {
	symbol, id, err := decodeA11PairCursor(store.cursor, "instruments", cursor)
	if err != nil {
		return generated.InstrumentPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT i.id,i.base_asset||i.quote_asset,i.product,m.version,
    m.price_tick::text,m.quantity_step::text,m.minimum_quantity::text,m.minimum_notional::text
    FROM instruments i JOIN LATERAL (SELECT * FROM instrument_metadata_versions candidate
		JOIN exchanges exchange ON exchange.id=candidate.exchange_id
		WHERE candidate.instrument_id=i.id AND exchange.id='binance' AND exchange.environment='production_public'
		ORDER BY version DESC LIMIT 1) m ON true
    WHERE ($1='' OR (i.base_asset||i.quote_asset,i.id)>($1,$2)) ORDER BY i.base_asset||i.quote_asset,i.id LIMIT $3`, symbol, id, limit+1)
	if err != nil {
		return generated.InstrumentPage{}, err
	}
	defer rows.Close()
	items := make([]generated.Instrument, 0, limit+1)
	for rows.Next() {
		var item generated.Instrument
		var product string
		var revision int64
		if err = rows.Scan(&item.Id, &item.Symbol, &product, &revision, &item.PriceTick, &item.QuantityStep, &item.MinimumQuantity, &item.MinimumNotional); err != nil {
			return generated.InstrumentPage{}, err
		}
		item.Product, item.MetadataVersion = generated.InstrumentProduct(product), strconv.FormatInt(revision, 10)
		items = append(items, item)
	}
	return instrumentPage(store, items, limit), rows.Err()
}

func instrumentPage(store *A11ConsoleStore, items []generated.Instrument, limit int) generated.InstrumentPage {
	page := generated.InstrumentPage{Items: items, Revision: "0"}
	if len(items) > 0 {
		page.Revision = items[len(items)-1].MetadataVersion
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := store.cursor.Encode("instruments", last.Symbol+a11CursorSeparator+last.Id)
		page.NextCursor = &next
	}
	return page
}

// Portfolios returns virtual-only USDT ledger summaries.
func (store *A11ConsoleStore) Portfolios(ctx context.Context, cursor string, limit int) (generated.PortfolioPage, error) {
	position, err := store.cursor.Decode("portfolios", cursor)
	if err != nil {
		return generated.PortfolioPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT p.id,coalesce(sum(vb.available) FILTER(WHERE vb.asset_symbol=p.reporting_asset),0)::text,
    coalesce(sum(vb.reserved) FILTER(WHERE vb.asset_symbol=p.reporting_asset),0)::text,
    coalesce(max(vb.revision),0),coalesce(max(r.mode),'shadow')
    FROM portfolios p LEFT JOIN virtual_accounts va ON va.portfolio_id=p.id LEFT JOIN virtual_balances vb ON vb.account_id=va.id
    LEFT JOIN runs r ON r.id=va.run_id WHERE ($1='' OR p.id>$1) GROUP BY p.id ORDER BY p.id LIMIT $2`, position, limit+1)
	if err != nil {
		return generated.PortfolioPage{}, err
	}
	defer rows.Close()
	items := make([]generated.PortfolioSummary, 0, limit+1)
	for rows.Next() {
		var item generated.PortfolioSummary
		var mode string
		var revision int64
		if err = rows.Scan(&item.Id, &item.Available, &item.Reserved, &revision, &mode); err != nil {
			return generated.PortfolioPage{}, err
		}
		item.Equity = item.Available
		item.Label = generated.PortfolioSummaryLabel("VIRTUAL")
		item.Mode = generated.PortfolioSummaryMode(mode)
		item.Revision = strconv.FormatInt(revision, 10)
		items = append(items, item)
	}
	page := generated.PortfolioPage{Items: items, Revision: "0"}
	if len(items) > 0 {
		page.Revision = items[len(items)-1].Revision
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		next := store.cursor.Encode("portfolios", items[len(items)-1].Id)
		page.NextCursor = &next
	}
	return page, rows.Err()
}

// Portfolio returns exact stored balances and positions without browser calculation.
func (store *A11ConsoleStore) Portfolio(ctx context.Context, id string) (generated.PortfolioDetail, error) {
	var mode string
	var updated time.Time
	var revision int64
	err := store.pool.QueryRow(ctx, `SELECT coalesce(max(r.mode),'shadow'),coalesce(max(vb.updated_at),p.created_at),coalesce(max(vb.revision),1)
    FROM portfolios p LEFT JOIN virtual_accounts va ON va.portfolio_id=p.id LEFT JOIN virtual_balances vb ON vb.account_id=va.id
    LEFT JOIN runs r ON r.id=va.run_id WHERE p.id=$1 GROUP BY p.id,p.created_at`, id).Scan(&mode, &updated, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.PortfolioDetail{}, console.ErrNotFound
	}
	if err != nil {
		return generated.PortfolioDetail{}, err
	}
	balancesRows, err := store.pool.Query(ctx, `SELECT vb.asset_symbol,sum(vb.available)::text,sum(vb.reserved)::text FROM virtual_balances vb JOIN virtual_accounts va ON va.id=vb.account_id WHERE va.portfolio_id=$1 GROUP BY vb.asset_symbol ORDER BY vb.asset_symbol`, id)
	if err != nil {
		return generated.PortfolioDetail{}, err
	}
	defer balancesRows.Close()
	balances := []generated.Balance{}
	available, reserved := "0", "0"
	for balancesRows.Next() {
		var item generated.Balance
		if err = balancesRows.Scan(&item.Asset, &item.Available, &item.Reserved); err != nil {
			return generated.PortfolioDetail{}, err
		}
		balances = append(balances, item)
		if item.Asset == "USDT" {
			available, reserved = item.Available, item.Reserved
		}
	}
	positionsRows, err := store.pool.Query(ctx, `SELECT p.instrument_id,sum(p.quantity)::text,sum(p.weighted_average_cost)::text,sum(p.realized_pnl)::text,sum(p.unrealized_pnl)::text
    FROM positions p JOIN virtual_accounts va ON va.id=p.account_id WHERE va.portfolio_id=$1 GROUP BY p.instrument_id ORDER BY p.instrument_id`, id)
	if err != nil {
		return generated.PortfolioDetail{}, err
	}
	defer positionsRows.Close()
	positions := []generated.Position{}
	for positionsRows.Next() {
		var item generated.Position
		if err = positionsRows.Scan(&item.Instrument, &item.Quantity, &item.AverageCost, &item.RealizedPnl, &item.UnrealizedPnl); err != nil {
			return generated.PortfolioDetail{}, err
		}
		item.Fees = "0"
		positions = append(positions, item)
	}
	return generated.PortfolioDetail{Id: id, Label: generated.PortfolioDetailLabel("VIRTUAL"), Mode: generated.PortfolioDetailMode(mode), Available: available, Reserved: reserved, Equity: available,
		Balances: balances, Positions: positions, Revision: strconv.FormatInt(revision, 10), UpdatedAt: updated.UTC()}, nil
}

// Journal returns immutable ledger lines ordered by transaction time and line.
func (store *A11ConsoleStore) Journal(ctx context.Context, portfolioID, cursor string, limit int) (generated.JournalPage, error) {
	scope := "journal:" + portfolioID
	occurred, transactionID, line, err := decodeA11TimeCursor(store.cursor, scope, cursor)
	if err != nil {
		return generated.JournalPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT jt.id,le.line_number,le.asset_symbol,le.direction,le.quantity::text,jt.correlation_id,jt.recorded_at
    FROM journal_transactions jt JOIN ledger_entries le ON le.transaction_id=jt.id WHERE jt.portfolio_id=$1 AND
		($2::timestamptz IS NULL OR jt.recorded_at<$2 OR (jt.recorded_at=$2 AND jt.id<$3) OR
		 (jt.recorded_at=$2 AND jt.id=$3 AND le.line_number>$4))
    ORDER BY jt.recorded_at DESC,jt.id DESC,le.line_number LIMIT $5`, portfolioID, nullableA11Time(occurred), transactionID, line, limit+1)
	if err != nil {
		return generated.JournalPage{}, err
	}
	defer rows.Close()
	items := make([]generated.JournalEntry, 0, limit+1)
	for rows.Next() {
		var item generated.JournalEntry
		var line int
		if err = rows.Scan(&item.TransactionId, &line, &item.Asset, &item.Direction, &item.Quantity, &item.CorrelationId, &item.OccurredAt); err != nil {
			return generated.JournalPage{}, err
		}
		item.Id = item.TransactionId + ":" + strconv.Itoa(line)
		items = append(items, item)
	}
	page := generated.JournalPage{Items: items, Revision: "0", Virtual: true}
	if len(items) > 0 {
		page.Revision = strconv.FormatInt(items[0].OccurredAt.UnixNano(), 10)
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		lastLine, _ := strconv.Atoi(last.Id[strings.LastIndex(last.Id, ":")+1:])
		next := encodeA11TimeCursor(store.cursor, scope, last.OccurredAt, last.TransactionId, lastLine)
		page.NextCursor = &next
	}
	return page, rows.Err()
}

func nullableA11Time(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
