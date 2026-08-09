package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

func buildOperationalEvidenceReportContent(
	ctx context.Context, tx pgx.Tx, reportID, reportType string, generatedAt time.Time,
) ([]byte, error) {
	counts, err := operationalEvidenceReportCounts(ctx, tx, reportType)
	if err != nil {
		return nil, err
	}
	var mode, confidence, valuation, models, maturity, source string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT mode,confidence_tier,valuation_basis,
model_provenance::text,maturity,source_identity,source_revision FROM owner_console_reports
WHERE id=$1`, reportID).Scan(&mode, &confidence, &valuation, &models, &maturity,
		&source, &revision)
	if err != nil {
		return nil, err
	}
	content := map[string]any{
		"schema_version": "axiom.report.owner_console.operational_evidence", "report_id": reportID,
		"report_type": reportType, "mode": mode, "confidence_tier": confidence,
		"valuation_basis": valuation, "model_provenance": json.RawMessage(models),
		"maturity": maturity, "source_identity": source, "source_revision": revision,
		"generated_at": generatedAt.UTC().Format(time.RFC3339Nano), "summary_counts": counts,
		"strategy_viability":   "separate from platform readiness",
		"platform_readiness":   "requires accepted cumulative qualification evidence",
		"disclaimer":           "Historical, replay, shadow, Testnet, and Demo results do not prove profitability.",
		"real_trading_enabled": false,
	}
	return json.Marshal(content)
}

func operationalEvidenceReportCounts(ctx context.Context, tx pgx.Tx, reportType string) (map[string]int64, error) {
	queries := operationalEvidenceCountQueries(reportType)
	result := make(map[string]int64, len(queries))
	for name, query := range queries {
		var count int64
		if err := tx.QueryRow(ctx, query).Scan(&count); err != nil {
			return nil, err
		}
		result[name] = count
	}
	return result, nil
}

func operationalEvidenceCountQueries(reportType string) map[string]string {
	switch reportType {
	case "strategy_results":
		return map[string]string{"strategies": `SELECT count(*) FROM strategy_definitions`,
			"research_reports": `SELECT count(*) FROM research_reports`}
	case "decisions_orders":
		return map[string]string{"decisions": `SELECT count(*) FROM decisions`, "orders": `SELECT count(*) FROM orders`, "fills": `SELECT count(*) FROM fills`}
	case "portfolios":
		return map[string]string{"portfolios": `SELECT count(*) FROM portfolios`, "journal_transactions": `SELECT count(*) FROM journal_transactions`}
	case "inventory_pnl":
		return map[string]string{"accounts": `SELECT count(*) FROM virtual_accounts`, "ledger_entries": `SELECT count(*) FROM ledger_entries`}
	case "risk":
		return map[string]string{"risk_evaluations": `SELECT count(*) FROM risk_evaluations`, "open_incidents": `SELECT count(*) FROM incidents WHERE state<>'resolved'`}
	case "exchange_data_health":
		return map[string]string{"exchanges": `SELECT count(*) FROM exchanges`, "ready_segments": `SELECT count(*) FROM market_data_segments WHERE state='ready'`}
	case "lab_runs":
		return map[string]string{"lab_jobs": `SELECT count(*) FROM jobs WHERE job_type IN ('backtest','replay')`, "shadow_sessions": `SELECT count(*) FROM shadow_sessions`}
	case "sandbox_qualifications":
		return map[string]string{"qualification_runs": `SELECT count(*) FROM owner_console_qualification_runs`, "sandbox_submission_plans": `SELECT count(*) FROM sandbox_runtime_submission_plans`}
	default:
		return map[string]string{"open_incidents": `SELECT count(*) FROM incidents WHERE state<>'resolved'`,
			"failed_qualifications": `SELECT count(*) FROM owner_console_qualification_runs WHERE state='FAILED'`,
			"active_alerts":         `SELECT count(*) FROM alerts WHERE state<>'resolved'`}
	}
}
