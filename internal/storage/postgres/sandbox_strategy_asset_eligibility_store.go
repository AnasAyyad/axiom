package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

// strategyAssetEligibilitySQL reads only the latest effective approval for
// each exact spot leg. A one-leg strategy's current candidate contract has a
// single eligibility version, so both assets must be approved at the same
// version. Returning an error on a divergent version prevents a later planner
// from treating one asset's screening evidence as the other's.
const strategyAssetEligibilitySQL = `
WITH requested_assets(symbol) AS (
  VALUES ($1::text),($2::text)
), current_screening AS (
  SELECT requested.symbol,screening.status,screening.version
  FROM requested_assets requested
  LEFT JOIN LATERAL (
    SELECT status,version
    FROM asset_screening_versions
    WHERE asset_symbol=requested.symbol
      AND effective_at<=$3
    ORDER BY version DESC
    LIMIT 1
  ) screening ON true
)
SELECT count(*),count(*) FILTER (WHERE status='approved'),
       min(version),max(version)
FROM current_screening`

// SandboxStrategyAssetEligibility returns the common current approval version
// for BTC/USDT or ETH/USDT. This is a read-only policy projection; it never
// trusts an engine startup cycle, configuration default, or browser value as
// proof that either asset is currently executable.
func (store *SandboxRuntimeDispatcherStore) SandboxStrategyAssetEligibility(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (uint64, error) {
	if store == nil || ctx == nil || work.ValidAt(now) != nil ||
		now.IsZero() || now.Location() != time.UTC {
		return 0, fmt.Errorf("sandbox_strategy_asset_eligibility_invalid")
	}
	base, quote, ok := sandboxStrategyAssets(work.Instrument)
	if !ok {
		return 0, fmt.Errorf("sandbox_strategy_asset_eligibility_invalid")
	}
	var count, approved int64
	var minimum, maximum *int64
	err := store.pool.QueryRow(ctx, strategyAssetEligibilitySQL, base, quote, now).Scan(
		&count, &approved, &minimum, &maximum,
	)
	if err != nil || count != 2 || approved != 2 || minimum == nil || maximum == nil ||
		*minimum <= 0 || *minimum != *maximum {
		return 0, fmt.Errorf("sandbox_strategy_asset_eligibility_unavailable")
	}
	return uint64(*minimum), nil
}

func sandboxStrategyAssets(instrument string) (string, string, bool) {
	switch instrument {
	case "BTCUSDT":
		return "BTC", "USDT", true
	case "ETHUSDT":
		return "ETH", "USDT", true
	default:
		return "", "", false
	}
}
