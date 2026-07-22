package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CoherentViewRepository persists and restores immutable B2 view identities.
type CoherentViewRepository struct{ pool *pgxpool.Pool }

// NewCoherentViewRepository constructs a PostgreSQL coherent-view repository.
func NewCoherentViewRepository(pool *pgxpool.Pool) (*CoherentViewRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("coherent_view_repository_invalid")
	}
	return &CoherentViewRepository{pool: pool}, nil
}

// Commit atomically inserts one header and its complete canonical membership.
func (repository *CoherentViewRepository) Commit(
	ctx context.Context,
	view runtimecore.CoherentView,
	createdAt time.Time,
) error {
	if repository == nil || repository.pool == nil || createdAt.IsZero() || createdAt.Location() != time.UTC ||
		view.Identity() == "" || len(view.Members()) < 2 {
		return fmt.Errorf("coherent_view_commit_invalid")
	}
	policy, trigger, members := view.Policy(), view.Trigger(), view.Members()
	if trigger.MonotonicNanos > math.MaxInt64 || trigger.IngestOrdinal > math.MaxInt64 {
		return fmt.Errorf("coherent_view_commit_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("coherent_view_commit_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `INSERT INTO cross_market_view_headers
(id,version_vector_hash,policy_version,maximum_book_age_nanos,maximum_inter_book_skew_nanos,
 maximum_clock_uncertainty_nanos,trigger_monotonic_nanos,trigger_ingest_ordinal,trigger_utc,trigger_utc_unix_nanos,
 member_count,created_at)
VALUES ($1,$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, view.Identity(), policy.Version,
		policy.MaximumBookAge.Nanoseconds(), policy.MaximumInterBookSkew.Nanoseconds(),
		policy.MaximumClockUncertainty.Nanoseconds(), int64(trigger.MonotonicNanos),
		int64(trigger.IngestOrdinal), trigger.UTC, trigger.UTC.UnixNano(), len(members), createdAt); err != nil {
		return fmt.Errorf("coherent_view_header_rejected")
	}
	if err = insertCoherentViewMembers(ctx, tx, view.Identity(), members); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("coherent_view_commit_rejected")
	}
	return nil
}

func insertCoherentViewMembers(
	ctx context.Context,
	tx pgx.Tx,
	identity string,
	members []runtimecore.ViewReference,
) error {
	for ordinal, member := range members {
		if member.BookVersion > math.MaxInt64 || member.ConnectionGeneration > math.MaxInt64 ||
			member.ReceiveMonotonicNanos > math.MaxInt64 || member.IngestOrdinal > math.MaxInt64 {
			return fmt.Errorf("coherent_view_member_invalid")
		}
		var instrumentID string
		if err := tx.QueryRow(ctx, `SELECT id FROM instruments
WHERE base_asset=$1 AND quote_asset=$2 AND product='spot'`,
			member.Key.Instrument.Base, member.Key.Instrument.Quote).Scan(&instrumentID); err != nil {
			return fmt.Errorf("coherent_view_instrument_missing")
		}
		intervalStart := member.ReceiveUTC.Add(member.ClockOffset - member.ClockUncertainty)
		intervalEnd := member.ReceiveUTC.Add(member.ClockOffset + member.ClockUncertainty)
		if _, err := tx.Exec(ctx, `INSERT INTO cross_market_view_members
(cross_market_view_id,member_ordinal,exchange_id,instrument_id,book_version,connection_generation,
 receive_monotonic_nanos,receive_utc,receive_utc_unix_nanos,ingest_ordinal,clock_offset_nanos,clock_uncertainty_nanos,
 clock_interval_start,clock_interval_end,state_hash,collector_instance,collector_region)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, identity, ordinal,
			member.Key.Exchange, instrumentID, int64(member.BookVersion), int64(member.ConnectionGeneration),
			int64(member.ReceiveMonotonicNanos), member.ReceiveUTC, member.ReceiveUTC.UnixNano(), int64(member.IngestOrdinal),
			member.ClockOffset.Nanoseconds(), member.ClockUncertainty.Nanoseconds(), intervalStart, intervalEnd,
			member.StateHash, member.CollectorInstance, member.CollectorRegion); err != nil {
			return fmt.Errorf("coherent_view_member_rejected")
		}
	}
	return nil
}

// Load restores and revalidates one exact coherent-view identity.
func (repository *CoherentViewRepository) Load(
	ctx context.Context,
	identity string,
) (runtimecore.CoherentView, error) {
	if repository == nil || repository.pool == nil || len(identity) != 64 {
		return runtimecore.CoherentView{}, fmt.Errorf("coherent_view_load_invalid")
	}
	policy, trigger, memberCount, err := repository.loadCoherentViewHeader(ctx, identity)
	if err != nil {
		return runtimecore.CoherentView{}, err
	}
	members, err := repository.loadCoherentViewMembers(ctx, identity, memberCount)
	if err != nil {
		return runtimecore.CoherentView{}, err
	}
	view, err := runtimecore.RestoreCoherentView(identity, policy, trigger, members)
	if err != nil {
		return runtimecore.CoherentView{}, fmt.Errorf("coherent_view_load_rejected")
	}
	return view, nil
}

func (repository *CoherentViewRepository) loadCoherentViewHeader(
	ctx context.Context,
	identity string,
) (runtimecore.CoherentPolicy, runtimecore.AsOfTrigger, int, error) {
	var policy runtimecore.CoherentPolicy
	var trigger runtimecore.AsOfTrigger
	var maximumAge, maximumSkew, maximumUncertainty int64
	var triggerMonotonic, triggerOrdinal, triggerUTCUnixNanos int64
	var memberCount int
	err := repository.pool.QueryRow(ctx, `SELECT policy_version,maximum_book_age_nanos,
maximum_inter_book_skew_nanos,maximum_clock_uncertainty_nanos,trigger_monotonic_nanos,
trigger_ingest_ordinal,trigger_utc,trigger_utc_unix_nanos,member_count FROM cross_market_view_headers WHERE id=$1`, identity).Scan(
		&policy.Version, &maximumAge, &maximumSkew, &maximumUncertainty, &triggerMonotonic,
		&triggerOrdinal, &trigger.UTC, &triggerUTCUnixNanos, &memberCount)
	if err != nil {
		return policy, trigger, 0, fmt.Errorf("coherent_view_load_missing")
	}
	policy.MaximumBookAge, policy.MaximumInterBookSkew = time.Duration(maximumAge), time.Duration(maximumSkew)
	policy.MaximumClockUncertainty = time.Duration(maximumUncertainty)
	trigger.MonotonicNanos, trigger.IngestOrdinal = uint64(triggerMonotonic), uint64(triggerOrdinal)
	trigger.UTC = time.Unix(0, triggerUTCUnixNanos).UTC()
	return policy, trigger, memberCount, nil
}

func (repository *CoherentViewRepository) loadCoherentViewMembers(
	ctx context.Context,
	identity string,
	memberCount int,
) ([]runtimecore.ViewReference, error) {
	rows, err := repository.pool.Query(ctx, `SELECT member.exchange_id,instrument.base_asset,instrument.quote_asset,
member.book_version,member.connection_generation,member.receive_monotonic_nanos,member.receive_utc_unix_nanos,
member.ingest_ordinal,member.clock_offset_nanos,member.clock_uncertainty_nanos,member.state_hash,
member.collector_instance,member.collector_region
FROM cross_market_view_members member JOIN instruments instrument ON instrument.id=member.instrument_id
WHERE member.cross_market_view_id=$1 ORDER BY member.member_ordinal`, identity)
	if err != nil {
		return nil, fmt.Errorf("coherent_view_load_unavailable")
	}
	defer rows.Close()
	members := make([]runtimecore.ViewReference, 0, memberCount)
	for rows.Next() {
		var member runtimecore.ViewReference
		var base, quote string
		var bookVersion, generation, receiveMonotonic, receiveUTCUnixNanos, ingestOrdinal, offset, uncertainty int64
		if err = rows.Scan(&member.Key.Exchange, &base, &quote, &bookVersion,
			&generation, &receiveMonotonic, &receiveUTCUnixNanos,
			&ingestOrdinal, &offset, &uncertainty, &member.StateHash,
			&member.CollectorInstance, &member.CollectorRegion); err != nil {
			return nil, fmt.Errorf("coherent_view_load_rejected")
		}
		member.Key.Instrument, err = domain.NewSpotInstrument(domain.AssetSymbol(base), domain.AssetSymbol(quote))
		if err != nil {
			return nil, fmt.Errorf("coherent_view_load_rejected")
		}
		member.ClockOffset, member.ClockUncertainty = time.Duration(offset), time.Duration(uncertainty)
		member.ReceiveUTC = time.Unix(0, receiveUTCUnixNanos).UTC()
		member.BookVersion, member.ConnectionGeneration = uint64(bookVersion), uint64(generation)
		member.ReceiveMonotonicNanos, member.IngestOrdinal = uint64(receiveMonotonic), uint64(ingestOrdinal)
		members = append(members, member)
	}
	if rows.Err() != nil || len(members) != memberCount {
		return nil, fmt.Errorf("coherent_view_load_incomplete")
	}
	return members, nil
}
