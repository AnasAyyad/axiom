// Code generated from api/openapi.yaml by scripts/generate-openapi-types.mjs.
// DO NOT EDIT.

export interface components {
  schemas: {
    "AuditEvent": {
      "actor": string;
      "causation_id": string;
      "correlation_id": string;
      "event_type": string;
      "id": string;
      "recorded_at": components["schemas"]["Timestamp"];
      "redacted": boolean;
      "safe_detail"?: string;
    };
    "AuditEventPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["AuditEvent"]>;
    };
    "Balance": {
      "asset": string;
      "available": components["schemas"]["NonnegativeDecimal"];
      "reserved": components["schemas"]["NonnegativeDecimal"];
    };
    "BinanceHealth": {
      "book_state": "healthy" | "rebuilding" | "gapped" | "stale";
      "capabilities"?: Array<string>;
      "clock_drift_ms"?: string;
      "environment": "production_public";
      "gaps"?: number;
      "observed_at": components["schemas"]["Timestamp"];
      "public_only": true;
      "rebuilds"?: number;
      "recorder_state": "healthy" | "degraded" | "paused" | "unavailable";
      "revision": components["schemas"]["Revision"];
      "websocket_state": "healthy" | "reconnecting" | "degraded" | "stale";
    };
    "BuildInformation": {
      "built_at": string;
      "commit": string;
      "dirty": boolean;
      "go_version": string;
      "version": string;
    };
    "ChampionChallengerPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["ChampionChallengerReport"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "ChampionChallengerReport": {
      "challenger_strategy_version": string;
      "challenger_suite_id"?: string;
      "champion_strategy_version": string;
      "champion_suite_id"?: string;
      "confidence": string;
      "created_at": components["schemas"]["Timestamp"];
      "disclaimer": string;
      "disposition": string;
      "id": string;
      "manifest_hash": string;
      "revision": components["schemas"]["Revision"];
      "viability": string;
    };
    "CommandAccepted": {
      "correlation_id": string;
      "created_at": components["schemas"]["Timestamp"];
      "id": string;
      "revision": components["schemas"]["Revision"];
      "state": "pending" | "applied" | "rejected" | "failed";
      "target_id": string;
    };
    "Decimal": string;
    "DetailedHealthResponse": {
      "components": Array<components["schemas"]["HealthComponent"]>;
      "lifecycle_state": "STARTING" | "READY_PAUSED" | "RUNNING" | "DEGRADED" | "STOPPING";
      "real_trading_enabled": false;
      "role": string;
      "status": "ready" | "not_ready";
    };
    "Error": {
      "code": string;
      "correlation_id": string;
      "field_details"?: Record<string, string>;
      "message": string;
    };
    "EvidenceTimelineEvent": {
      "correlation_id": string;
      "event_type": string;
      "index": number;
      "label": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "revision": components["schemas"]["Revision"];
    };
    "ExchangePage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["ExchangeSummary"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "ExchangeSummary": {
      "book_state": "healthy" | "rebuilding" | "gapped" | "stale";
      "capabilities": Array<string>;
      "environment": "production_public";
      "id": string;
      "instruments": number;
      "last_message_age_ms"?: components["schemas"]["Revision"];
      "name": string;
      "public_only": true;
      "quality": components["schemas"]["QualityEvidence"];
      "reconnects"?: number;
      "recorder_state": "healthy" | "degraded" | "paused" | "unavailable";
      "revision": components["schemas"]["Revision"];
      "sequence_gaps"?: number;
      "websocket_state": "healthy" | "reconnecting" | "degraded" | "stale";
    };
    "HealthComponent": {
      "name": "postgres" | "authentication" | "outbox" | "public_binance" | "disk" | "recovery";
      "reason_code"?: "required_dependency_unavailable" | "bootstrap_required" | "stale" | "locked";
      "status": "ready" | "not_ready";
    };
    "HealthResponse": {
      "phase": "A1" | "A11";
      "reason_code"?: string;
      "role": string;
      "status": "live" | "ready" | "not_ready";
    };
    "IncidentDetail": components["schemas"]["IncidentSummary"] & {
      "replay_window": {
      "dataset_id": string;
      "first_ordinal": components["schemas"]["Revision"];
      "last_ordinal": components["schemas"]["Revision"];
    };
      "timeline": Array<components["schemas"]["TimelineEvent"]>;
    };
    "IncidentPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["IncidentSummary"]>;
    };
    "IncidentSummary": {
      "id": string;
      "opened_at": components["schemas"]["Timestamp"];
      "reason_code": string;
      "revision": components["schemas"]["Revision"];
      "severity": "warning" | "error" | "critical";
      "state": "open" | "acknowledged" | "resolved";
    };
    "Instrument": {
      "id": string;
      "metadata_version": components["schemas"]["Revision"];
      "minimum_notional": components["schemas"]["NonnegativeDecimal"];
      "minimum_quantity": components["schemas"]["NonnegativeDecimal"];
      "price_tick": components["schemas"]["NonnegativeDecimal"];
      "product": "spot";
      "quantity_step": components["schemas"]["NonnegativeDecimal"];
      "symbol": string;
    };
    "InstrumentPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["Instrument"]>;
    };
    "InventoryImpact": {
      "after": components["schemas"]["NonnegativeDecimal"];
      "asset": string;
      "band_state": string;
      "before": components["schemas"]["NonnegativeDecimal"];
      "exchange": string;
      "natural_reverse_preferred": boolean;
    };
    "InventoryPage": components["schemas"]["Page"] & {
      "combined_balance": false;
      "isolation_notice": string;
      "items": Array<components["schemas"]["InventoryPosition"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "InventoryPosition": {
      "after": components["schemas"]["NonnegativeDecimal"];
      "asset": string;
      "available": components["schemas"]["NonnegativeDecimal"];
      "before": components["schemas"]["NonnegativeDecimal"];
      "cost_basis"?: components["schemas"]["NonnegativeDecimal"];
      "exchange": string;
      "experiment_id": string;
      "id": string;
      "inventory_pnl"?: components["schemas"]["Decimal"];
      "market_value"?: components["schemas"]["NonnegativeDecimal"];
      "maximum_band"?: components["schemas"]["NonnegativeDecimal"];
      "minimum_band"?: components["schemas"]["NonnegativeDecimal"];
      "portfolio_id": string;
      "quality": components["schemas"]["QualityEvidence"];
      "reserved": components["schemas"]["NonnegativeDecimal"];
      "revision": components["schemas"]["Revision"];
      "status": string;
      "strategy_version": string;
      "target"?: components["schemas"]["NonnegativeDecimal"];
      "updated_at": components["schemas"]["Timestamp"];
      "virtual": true;
    };
    "JobResource": {
      "created_at": components["schemas"]["Timestamp"];
      "cursor_ordinal"?: components["schemas"]["Revision"];
      "failure_code"?: string;
      "id": string;
      "kind": "backtest" | "replay";
      "mode_label": "BACKTEST" | "REPLAY";
      "progress"?: components["schemas"]["NonnegativeDecimal"];
      "registered_report"?: components["schemas"]["RegisteredResearchReport"];
      "replay_inspection"?: components["schemas"]["ReplayEventInspection"];
      "result"?: components["schemas"]["JobResult"];
      "revision": components["schemas"]["Revision"];
      "state": "QUEUED" | "RUNNING" | "PAUSE_REQUESTED" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "SUCCEEDED" | "FAILED";
      "updated_at"?: components["schemas"]["Timestamp"];
    };
    "JobResult": {
      "confidence_label": "local_tier_b" | "formal_tier_a" | "insufficient" | "rejected";
      "disclaimer": string;
      "metrics"?: Record<string, components["schemas"]["Decimal"]>;
      "platform_correctness": string;
      "report_hash": string;
      "report_id": string;
      "reproducibility": string;
      "research_coverage": "single_run_incomplete" | "registered_suite_complete";
      "result_hash": string;
      "strategy_evidence": string;
      "viability": "undetermined" | "viable_for_more_research" | "rejected";
    };
    "JournalEntry": {
      "asset": string;
      "correlation_id": string;
      "direction": "debit" | "credit";
      "id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "transaction_id": string;
    };
    "JournalPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["JournalEntry"]>;
      "virtual": true;
    };
    "LoginRequest": {
      "email": string;
      "password": string;
    };
    "LoginResponse": {
      "csrf_token": string;
      "expires_at": components["schemas"]["Timestamp"];
      "user": components["schemas"]["SessionUser"];
    };
    "ManualChecklistStep": {
      "index": number;
      "instruction": string;
      "manual_only": true;
    };
    "NonnegativeDecimal": string;
    "OfflineJobRequest": {
      "configuration_id": string;
      "dataset_id": string;
      "research_generation_id": string;
      "root_seed_hash": string;
      "strategy_version": "trend.v1a.1";
    };
    "OpportunityDetail": {
      "cost_attribution": Record<string, components["schemas"]["Decimal"]>;
      "inventory": Array<components["schemas"]["InventoryImpact"]>;
      "legs": Array<components["schemas"]["OpportunityLeg"]>;
      "raw_evidence_available": boolean;
      "recovery": components["schemas"]["RecoveryAnalysis"];
      "summary": components["schemas"]["OpportunitySummary"];
      "timeline": Array<components["schemas"]["EvidenceTimelineEvent"]>;
    };
    "OpportunityLeg": {
      "arrival_offset_nanos"?: components["schemas"]["Revision"];
      "depth_cost": components["schemas"]["NonnegativeDecimal"];
      "exchange": string;
      "fee_asset": string;
      "fee_quantity": components["schemas"]["NonnegativeDecimal"];
      "fee_quote_equivalent": components["schemas"]["NonnegativeDecimal"];
      "gross_output": components["schemas"]["NonnegativeDecimal"];
      "index": number;
      "input_quantity": components["schemas"]["NonnegativeDecimal"];
      "instrument": string;
      "net_output": components["schemas"]["NonnegativeDecimal"];
      "revision": components["schemas"]["Revision"];
      "side": "buy" | "sell";
      "source_asset"?: string;
      "state": string;
      "target_asset"?: string;
      "trade_quantity": components["schemas"]["NonnegativeDecimal"];
      "vwap": components["schemas"]["NonnegativeDecimal"];
    };
    "OpportunityPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["OpportunitySummary"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "OpportunitySummary": {
      "buy_exchange"?: string;
      "cycle_path"?: Array<string>;
      "exchange"?: string;
      "expected_profit": components["schemas"]["Decimal"];
      "gross_metric": components["schemas"]["Decimal"];
      "id": string;
      "instrument"?: string;
      "kind": "triangular" | "cross_exchange";
      "label": string;
      "lifetime_nanos"?: components["schemas"]["Revision"];
      "maximum_size": components["schemas"]["NonnegativeDecimal"];
      "net_metric": components["schemas"]["Decimal"];
      "opportunity_age_nanos"?: components["schemas"]["Revision"];
      "quality": components["schemas"]["QualityEvidence"];
      "recorded_at": components["schemas"]["Timestamp"];
      "revision": components["schemas"]["Revision"];
      "sell_exchange"?: string;
      "simulation_only": true;
      "status": "detected" | "qualified" | "simulated" | "recovery_required" | "quarantined" | "rejected" | "expired";
      "strategy_version": string;
      "tested_size": components["schemas"]["NonnegativeDecimal"];
      "worst_case_profit": components["schemas"]["Decimal"];
    };
    "Page": {
      "has_more": boolean;
      "next_cursor"?: string;
      "revision": components["schemas"]["Revision"];
    };
    "PortfolioDetail": components["schemas"]["PortfolioSummary"] & {
      "balances": Array<components["schemas"]["Balance"]>;
      "positions": Array<components["schemas"]["Position"]>;
      "updated_at": components["schemas"]["Timestamp"];
    };
    "PortfolioPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["PortfolioSummary"]>;
    };
    "PortfolioSummary": {
      "available": components["schemas"]["NonnegativeDecimal"];
      "equity": components["schemas"]["NonnegativeDecimal"];
      "id": string;
      "label": "VIRTUAL";
      "mode": "backtest" | "replay" | "paper" | "shadow";
      "reserved": components["schemas"]["NonnegativeDecimal"];
      "revision": components["schemas"]["Revision"];
    };
    "Position": {
      "average_cost": components["schemas"]["NonnegativeDecimal"];
      "fees": components["schemas"]["NonnegativeDecimal"];
      "instrument": string;
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "realized_pnl": components["schemas"]["Decimal"];
      "unrealized_pnl": components["schemas"]["Decimal"];
    };
    "QualityEvidence": {
      "confidence": "high" | "medium" | "low" | "insufficient" | "unknown";
      "expires_at"?: components["schemas"]["Timestamp"];
      "freshness": "live" | "fresh" | "stale" | "historical" | "expired";
      "observed_at": components["schemas"]["Timestamp"];
      "observer"?: string;
      "provenance_complete": boolean;
      "source": string;
      "tier": "formal_tier_a" | "local_tier_b" | "integration_only" | "unknown";
      "warnings"?: Array<string>;
    };
    "RebalancingDetail": {
      "checklist": Array<components["schemas"]["ManualChecklistStep"]>;
      "execution_available": false;
      "route": Array<components["schemas"]["RebalancingRouteStep"]>;
      "summary": components["schemas"]["RebalancingSummary"];
    };
    "RebalancingPage": components["schemas"]["Page"] & {
      "execution_available": false;
      "items": Array<components["schemas"]["RebalancingSummary"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "RebalancingRouteStep": {
      "approved": boolean;
      "confidence": components["schemas"]["NonnegativeDecimal"];
      "expected_cost": components["schemas"]["NonnegativeDecimal"];
      "fact_id": string;
      "fact_version": components["schemas"]["Revision"];
      "from_asset": string;
      "from_exchange": string;
      "index": number;
      "maximum_duration_nanos": components["schemas"]["Revision"];
      "minimum_duration_nanos": components["schemas"]["Revision"];
      "network"?: string;
      "provenance_hash": string;
      "role": "trade" | "transfer";
      "to_asset": string;
      "to_exchange": string;
      "warnings": Array<string>;
    };
    "RebalancingSummary": {
      "advisory_only": true;
      "destination_asset": string;
      "destination_exchange": string;
      "id": string;
      "maximum_duration_nanos": components["schemas"]["Revision"];
      "method": "natural_reverse_arbitrage" | "reviewed_graph_route";
      "minimum_duration_nanos": components["schemas"]["Revision"];
      "quality": components["schemas"]["QualityEvidence"];
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "recorded_at": components["schemas"]["Timestamp"];
      "revision": components["schemas"]["Revision"];
      "risk_score": components["schemas"]["NonnegativeDecimal"];
      "source_asset": string;
      "source_exchange": string;
      "total_cost": components["schemas"]["NonnegativeDecimal"];
      "warnings": Array<string>;
    };
    "RecoveryAnalysis": {
      "attempted": boolean;
      "disposition": string;
      "explanation": string;
      "quarantined": boolean;
      "recovery_loss": components["schemas"]["Decimal"];
      "succeeded": boolean;
    };
    "RegisteredResearchReport": {
      "benchmarks": Array<components["schemas"]["ResearchResultSlice"]>;
      "canonical_manifest": string;
      "capacity": Array<components["schemas"]["ResearchCapacityPoint"]>;
      "confidence_label": "local_tier_b" | "formal_tier_a" | "rejected";
      "created_at": components["schemas"]["Timestamp"];
      "disclaimer": string;
      "id": string;
      "manifest_hash": string;
      "platform_correctness": string;
      "research_generation_id": string;
      "run_references": Array<string>;
      "strategy_evidence": string;
      "stress": Array<components["schemas"]["ResearchResultSlice"]>;
      "viability": "undetermined" | "viable_for_more_research" | "rejected";
    };
    "ReplayEventInspection": {
      "canonical_balances": string;
      "canonical_decision": string;
      "canonical_event": string;
      "canonical_execution_events": string;
      "canonical_orders": string;
      "event_count": components["schemas"]["Revision"];
      "event_hash": string;
      "ordinal": components["schemas"]["Revision"];
    };
    "ReplayFaultKind": "disconnect" | "sequence_gap" | "latency" | "rejection" | "partial_fill" | "cancel_fill_race" | "unknown_state" | "storage_failure" | "restart_at_event";
    "ReplayFaultPage": {
      "items": Array<components["schemas"]["ReplayFaultResource"]>;
      "revision": components["schemas"]["Revision"];
      "simulation_only": true;
    };
    "ReplayFaultRequest": {
      "delay_nanos": components["schemas"]["Revision"];
      "expected_revision": components["schemas"]["Revision"];
      "fault": components["schemas"]["ReplayFaultKind"];
      "ordinal": components["schemas"]["Revision"];
      "reason": string;
      "repeatable"?: boolean;
    };
    "ReplayFaultResource": {
      "created_at": components["schemas"]["Timestamp"];
      "delay_nanos": components["schemas"]["Revision"];
      "fault": components["schemas"]["ReplayFaultKind"];
      "id": string;
      "ordinal": components["schemas"]["Revision"];
      "repeatable": boolean;
      "replay_id": string;
      "revision": components["schemas"]["Revision"];
      "simulation_only": true;
    };
    "ReplayJobRequest": components["schemas"]["OfflineJobRequest"] & {
      "first_ordinal"?: components["schemas"]["Revision"];
      "incident_id"?: string;
      "last_ordinal"?: components["schemas"]["Revision"];
      "speed"?: "original" | "accelerated" | "maximum";
    };
    "ReportExportRequest": {
      "format": "json" | "csv";
    };
    "ReportExportResource": {
      "content": string;
      "content_type": "application/json" | "text/csv";
      "created_at": components["schemas"]["Timestamp"];
      "format": "json" | "csv";
      "id": string;
      "payload_hash": string;
      "report_id": string;
      "revision": components["schemas"]["Revision"];
      "simulation_only": true;
    };
    "ResearchCapacityPoint": {
      "fill_rate": components["schemas"]["NonnegativeDecimal"];
      "net_return": components["schemas"]["Decimal"];
      "notional": components["schemas"]["NonnegativeDecimal"];
    };
    "ResearchResultSlice": {
      "max_drawdown": components["schemas"]["NonnegativeDecimal"];
      "name": string;
      "net_return": components["schemas"]["Decimal"];
      "trades": number;
    };
    "Revision": string;
    "RevisionCommandRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
    };
    "RiskContributor": {
      "limit": components["schemas"]["NonnegativeDecimal"];
      "name": string;
      "reason_code": string;
      "usage": components["schemas"]["NonnegativeDecimal"];
    };
    "RiskStatus": {
      "contributors": Array<components["schemas"]["RiskContributor"]>;
      "policy_version": components["schemas"]["Revision"];
      "reason_codes"?: Array<string>;
      "recovery_ready": boolean;
      "revision": components["schemas"]["Revision"];
      "state": "NORMAL" | "CAUTIOUS" | "PAUSED" | "LOCKED";
      "unresolved_critical"?: number;
      "updated_at": components["schemas"]["Timestamp"];
    };
    "SessionMe": {
      "reauthenticated_at": components["schemas"]["Timestamp"];
      "session_id": string;
      "session_revision": components["schemas"]["Revision"];
      "user": components["schemas"]["SessionUser"];
    };
    "SessionUser": {
      "email": string;
      "id": string;
      "permissions": Array<string>;
      "roles": Array<string>;
    };
    "ShadowSessionRequest": {
      "configuration_id": string;
      "portfolio_id": string;
      "strategy_version": "trend.v1a.1";
    };
    "ShadowSessionResource": {
      "accepted_decisions": number;
      "configuration_id": string;
      "created_at": components["schemas"]["Timestamp"];
      "decision_dataset_id": string;
      "entries_enabled": boolean;
      "failure_code"?: string;
      "id": string;
      "journal_transactions": number;
      "label": "PUBLIC-LIVE SHADOW / VIRTUAL";
      "model_namespace_id": string;
      "orders"?: Array<components["schemas"]["SimulatedOrder"]>;
      "public_only": true;
      "rejected_decisions": number;
      "revision": components["schemas"]["Revision"];
      "risk_state"?: "PAUSED" | "RESUMED" | "LOCKED";
      "simulation_only": true;
      "started_at"?: components["schemas"]["Timestamp"];
      "state": "QUEUED" | "RUNNING" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "FAILED";
      "stopped_at"?: components["schemas"]["Timestamp"];
      "strategy_version": string;
    };
    "SimulatedOrder": {
      "filled_quantity"?: components["schemas"]["NonnegativeDecimal"];
      "id": string;
      "instrument": string;
      "latency_ms"?: string;
      "limit_price": components["schemas"]["NonnegativeDecimal"];
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "side": "buy" | "sell";
      "simulated": true;
      "state": string;
    };
    "StrategyPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["StrategySummary"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "StrategySummary": {
      "confidence": "formal_tier_a" | "local_tier_b" | "insufficient" | "rejected";
      "created_at": components["schemas"]["Timestamp"];
      "disclaimer": string;
      "evidence_role": "champion" | "challenger" | "unassigned";
      "family": string;
      "id": string;
      "maturity": "EXPERIMENTAL" | "BACKTEST_VALIDATED" | "REPLAY_VALIDATED" | "SHADOW_VALIDATED" | "REJECTED";
      "name": string;
      "primary_metric"?: string;
      "revision": components["schemas"]["Revision"];
      "supported_modes": Array<"backtest" | "replay" | "shadow">;
      "version": string;
      "viability": "undetermined" | "viable_for_more_research" | "rejected";
    };
    "StreamEvent": {
      "causation_id": string;
      "correlation_id": string;
      "entity_revision": components["schemas"]["Revision"];
      "event_type": string;
      "id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "payload": Record<string, unknown>;
      "revision": components["schemas"]["Revision"];
      "schema_version": "axiom.stream.v1";
      "stream": "system" | "exchange" | "portfolio" | "risk" | "trend" | "job" | "shadow" | "incident" | "alert" | "order" | "fill" | "opportunity" | "strategy" | "inventory" | "rebalancing" | "research";
    };
    "SystemStatus": {
      "active_resource_id"?: string;
      "binance_state"?: string;
      "clock_drift_ms"?: string;
      "critical_incidents"?: number;
      "engine_state"?: string;
      "environment"?: string;
      "execution_mode"?: "backtest" | "replay" | "paper" | "shadow";
      "lifecycle_state": "STARTING" | "READY_PAUSED" | "RUNNING" | "DEGRADED" | "STOPPING";
      "phase": "A1" | "A11" | "B8";
      "real_trading_enabled": false;
      "release": "V1A" | "V1B";
      "revision"?: components["schemas"]["Revision"];
      "risk_state"?: "PAUSED" | "RESUMED" | "LOCKED";
      "role": string;
      "server_time"?: components["schemas"]["Timestamp"];
      "strategy_activation": "unavailable" | "trend.v1a.1";
    };
    "TimelineEvent": {
      "correlation_id": string;
      "event_type": string;
      "id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "redacted": boolean;
      "safe_detail"?: string;
    };
    "Timestamp": string;
    "TrendDecision": {
      "candle_view_id": string;
      "explanation": string;
      "id": string;
      "market_view_id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "outcome": "accepted" | "rejected";
      "reason_code": string;
      "revision": components["schemas"]["Revision"];
    };
    "TrendDecisionPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["TrendDecision"]>;
    };
    "TrendParameter": {
      "cadence": string;
      "id": string;
      "mutability": "immutable_per_run";
      "unit": string;
      "value": string;
    };
    "TrendStatus": {
      "evidence_maturity": "local_tier_b" | "formal_tier_a" | "insufficient" | "rejected";
      "health": "healthy" | "warming" | "paused" | "degraded" | "locked";
      "parameters": Array<components["schemas"]["TrendParameter"]>;
      "revision": components["schemas"]["Revision"];
      "timeframe": "4h";
      "version": "trend.v1a.1";
      "viability"?: "undetermined" | "viable_for_more_research" | "rejected";
    };
    "VersionResponse": {
      "version": string;
    };
  };
}

export interface operations {
  "getLiveness": { responses: { "200": components["schemas"]["HealthResponse"]; }; };
  "getReadiness": { responses: { "200": components["schemas"]["HealthResponse"]; "503": components["schemas"]["HealthResponse"]; }; };
  "loginSession": { header: { "Origin": string; }; requestBody: components["schemas"]["LoginRequest"]; responses: { "201": components["schemas"]["LoginResponse"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "logoutSession": { header: { "Origin": string; "X-CSRF-Token": string; }; responses: { "204": never; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "getCurrentSession": { responses: { "200": components["schemas"]["SessionMe"]; "401": components["schemas"]["Error"]; }; };
  "getVersion": { responses: { "200": components["schemas"]["VersionResponse"]; }; };
  "getBuildInformation": { responses: { "200": components["schemas"]["BuildInformation"]; }; };
  "getSystemStatus": { responses: { "200": components["schemas"]["SystemStatus"]; "401": components["schemas"]["Error"]; }; };
  "getDetailedHealth": { responses: { "200": components["schemas"]["DetailedHealthResponse"]; "401": components["schemas"]["Error"]; "503": components["schemas"]["DetailedHealthResponse"]; }; };
  "getBinanceHealth": { responses: { "200": components["schemas"]["BinanceHealth"]; "401": components["schemas"]["Error"]; }; };
  "listBinanceInstruments": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["InstrumentPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "listExchanges": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["ExchangePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "listOpportunities": { query: { "cursor"?: string; "page_size"?: number; "kind"?: "triangular" | "cross_exchange"; }; responses: { "200": components["schemas"]["OpportunityPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getOpportunity": { path: { "id": string; }; responses: { "200": components["schemas"]["OpportunityDetail"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listPortfolios": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["PortfolioPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getPortfolio": { path: { "id": string; }; responses: { "200": components["schemas"]["PortfolioDetail"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listPortfolioJournal": { path: { "id": string; }; query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["JournalPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listInventory": { query: { "cursor"?: string; "page_size"?: number; "exchange"?: string; "asset"?: string; "strategy"?: string; "portfolio"?: string; }; responses: { "200": components["schemas"]["InventoryPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getRiskStatus": { responses: { "200": components["schemas"]["RiskStatus"]; "401": components["schemas"]["Error"]; }; };
  "pauseRisk": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "resumeRisk": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getTrendStrategy": { responses: { "200": components["schemas"]["TrendStatus"]; "401": components["schemas"]["Error"]; }; };
  "listTrendDecisions": { query: { "cursor"?: string; "page_size"?: number; "outcome"?: "accepted" | "rejected"; }; responses: { "200": components["schemas"]["TrendDecisionPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "listStrategies": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["StrategyPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "listRebalancingRecommendations": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["RebalancingPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getRebalancingRecommendation": { path: { "id": string; }; responses: { "200": components["schemas"]["RebalancingDetail"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listChampionChallengerReports": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["ChampionChallengerPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "exportResearchReport": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReportExportRequest"]; responses: { "201": components["schemas"]["ReportExportResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; }; };
  "createBacktest": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["OfflineJobRequest"]; responses: { "202": components["schemas"]["JobResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getBacktest": { path: { "id": string; }; responses: { "200": components["schemas"]["JobResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "createReplay": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReplayJobRequest"]; responses: { "202": components["schemas"]["JobResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; }; };
  "getReplay": { path: { "id": string; }; query: { "event_ordinal"?: components["schemas"]["Revision"]; }; responses: { "200": components["schemas"]["JobResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "pauseReplay": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "resumeReplay": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "stepReplay": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "listReplayFaults": { path: { "id": string; }; responses: { "200": components["schemas"]["ReplayFaultPage"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "scheduleReplayFault": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReplayFaultRequest"]; responses: { "201": components["schemas"]["ReplayFaultResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "createShadowSession": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ShadowSessionRequest"]; responses: { "202": components["schemas"]["ShadowSessionResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "stopShadowSession": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "getShadowSession": { path: { "id": string; }; responses: { "200": components["schemas"]["ShadowSessionResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listIncidents": { query: { "cursor"?: string; "page_size"?: number; "state"?: "open" | "acknowledged" | "resolved"; }; responses: { "200": components["schemas"]["IncidentPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getIncident": { path: { "id": string; }; query: { "include_raw"?: boolean; }; responses: { "200": components["schemas"]["IncidentDetail"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listAuditEvents": { query: { "cursor"?: string; "page_size"?: number; "event_type"?: string; "include_detail"?: boolean; }; responses: { "200": components["schemas"]["AuditEventPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "streamEvents": { query: { "after_revision"?: components["schemas"]["Revision"]; }; header: { "Origin": string; "Last-Event-ID"?: components["schemas"]["Revision"]; }; responses: { "200": string; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; }; };
}
