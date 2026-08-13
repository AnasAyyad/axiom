// Code generated from api/openapi.yaml by scripts/generate-openapi-types.mjs.
// DO NOT EDIT.

export interface components {
  schemas: {
    "ActivityPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["ActivityResource"]>;
      "snapshot_revision": components["schemas"]["Revision"];
    };
    "ActivityResource": {
      "activity_revision": components["schemas"]["Revision"];
      "causation_id"?: string;
      "correlation_id": string;
      "details": Record<string, unknown>;
      "exchange_id"?: string;
      "id": string;
      "instrument_id"?: string;
      "links": Record<string, string>;
      "mode"?: "backtest" | "replay" | "paper" | "shadow" | "testnet" | "demo";
      "occurred_at": components["schemas"]["Timestamp"];
      "outcome": string;
      "reason": components["schemas"]["ReasonPresentation"];
      "side"?: "buy" | "sell";
      "source_id": string;
      "source_revision": string;
      "source_type": string;
      "strategy_id"?: string;
      "view": "decisions_orders" | "system_events";
    };
    "AlertDeliveryAttempt": {
      "attempt": number;
      "completed_at": components["schemas"]["Timestamp"];
      "id": string;
      "latency_ms"?: number;
      "reason_code"?: string;
      "sink_name": string;
      "started_at": components["schemas"]["Timestamp"];
      "state": "delivered" | "failed";
    };
    "AlertDetail": {
      "component": string;
      "correlation_id": string;
      "created_at": components["schemas"]["Timestamp"];
      "deliveries": Array<components["schemas"]["AlertDeliveryAttempt"]>;
      "escalations": Array<components["schemas"]["AlertEscalation"]>;
      "id": string;
      "incident_id"?: string;
      "last_seen_at": components["schemas"]["Timestamp"];
      "occurrences": number;
      "reason_code": string;
      "revision": components["schemas"]["Revision"];
      "severity": "info" | "warning" | "critical";
      "state": "open" | "acknowledged" | "resolved";
    };
    "AlertEscalation": {
      "actor_user_id": string;
      "escalated_at": components["schemas"]["Timestamp"];
      "id": string;
      "reason": string;
      "revision": components["schemas"]["Revision"];
    };
    "AlertRoute": {
      "enabled": boolean;
      "id": string;
      "last_test_state"?: "pending" | "delivered" | "failed";
      "last_tested_at"?: components["schemas"]["Timestamp"];
      "minimum_severity": "info" | "warning" | "critical";
      "revision": components["schemas"]["Revision"];
      "sink_name": string;
      "target_label"?: string;
    };
    "AlertRoutePage": {
      "items": Array<components["schemas"]["AlertRoute"]>;
      "revision": components["schemas"]["Revision"];
    };
    "AlertTestRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
    };
    "ArtifactHoldRequest": {
      "authorization_token": string;
      "expected_revision": components["schemas"]["Revision"];
      "hold_type": "incident" | "reproducibility";
      "reason": string;
      "reference_id": string;
    };
    "AuditEvent": {
      "actor": string;
      "category"?: "authentication" | "control" | "export" | "configuration" | "qualification" | "incident" | "alert" | "evidence_access" | "system";
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
    "AuditVerification": {
      "checked_events": number;
      "first_broken_sequence"?: number;
      "head_hash": string;
      "reason_code"?: string;
      "verdict": "valid" | "broken";
      "verified_at": components["schemas"]["Timestamp"];
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
    "ConfigurationActivationRequest": {
      "authorization_token": string;
      "configuration_id": string;
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
    };
    "DataAudit": {
      "completed_at"?: components["schemas"]["Timestamp"];
      "created_at": components["schemas"]["Timestamp"];
      "id": string;
      "reason_code"?: string;
      "state": "PENDING" | "RUNNING" | "COMPLETED" | "BLOCKED";
    };
    "DataCatalogueItem": {
      "coverage_end": components["schemas"]["Timestamp"];
      "coverage_start": components["schemas"]["Timestamp"];
      "coverage_types": Array<string>;
      "exchanges": Array<"binance" | "bybit">;
      "instruments": Array<string>;
      "known_gap_count": number;
      "manifest_hash": string;
      "name": string;
      "quality_tier"?: "unclassified" | "tier_a";
      "segment_count": number;
      "source": "recorded_public_data" | "approved_historical_data";
      "state": "building" | "ready" | "qualified" | "rejected";
      "supported_modes": Array<"backtest" | "replay" | "shadow">;
    };
    "DataCataloguePage": {
      "items": Array<components["schemas"]["DataCatalogueItem"]>;
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
      "blocking_prerequisites"?: Array<string>;
      "code": string;
      "correlation_id": string;
      "current_state"?: string;
      "detail"?: string;
      "field_details"?: Record<string, string>;
      "impact"?: string;
      "message": string;
      "required_state"?: string;
      "suggested_action"?: string;
      "summary"?: string;
    };
    "EvaluationCampaign": {
      "completed_stages": Array<"HISTORICAL_IMPORT" | "EXISTING_DATA_AUDIT" | "RECORDER_ROTATION" | "RECORDER_QUALIFICATION" | "BACKTEST_MATRIX" | "REPLAY_MATRIX" | "CANDIDATE_SELECTION" | "COMBINED_SHADOW" | "FINAL_REPORT">;
      "coverage"?: Array<components["schemas"]["EvaluationCoverage"]>;
      "created_at": components["schemas"]["Timestamp"];
      "current_stage"?: "HISTORICAL_IMPORT" | "EXISTING_DATA_AUDIT" | "RECORDER_ROTATION" | "RECORDER_QUALIFICATION" | "BACKTEST_MATRIX" | "REPLAY_MATRIX" | "CANDIDATE_SELECTION" | "COMBINED_SHADOW" | "FINAL_REPORT";
      "estimated_remaining_seconds"?: number;
      "feed_health"?: Array<components["schemas"]["EvaluationFeedHealth"]>;
      "historical_imports"?: Array<components["schemas"]["EvaluationHistoricalImportProgress"]>;
      "id": string;
      "matrix"?: Array<components["schemas"]["EvaluationMatrixMember"]>;
      "measured_bytes_per_hour"?: number;
      "preset": "balanced_full_v1";
      "reason_code"?: string;
      "recorded_bytes"?: number;
      "recording_last_valid_at"?: components["schemas"]["Timestamp"];
      "recording_limit_bytes"?: number;
      "revision": components["schemas"]["Revision"];
      "shadow"?: components["schemas"]["EvaluationShadowProgress"];
      "shadow_last_valid_at"?: components["schemas"]["Timestamp"];
      "shadow_reserved_bytes"?: number;
      "stages"?: Array<components["schemas"]["EvaluationStageProgress"]>;
      "state": "PENDING" | "RUNNING" | "PAUSED_RECOVERABLE" | "COMPLETED" | "PARTIAL" | "BLOCKED" | "CANCELED";
      "suggested_action"?: string;
      "updated_at": components["schemas"]["Timestamp"];
      "valid_recording_seconds"?: number;
      "valid_shadow_seconds"?: number;
      "wall_time_seconds"?: number;
    };
    "EvaluationCampaignCreateRequest": {
      "preset": "balanced_full_v1";
    };
    "EvaluationCampaignEvent": {
      "event_type": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "ordinal": components["schemas"]["Revision"];
      "reason_code"?: string;
      "stage"?: string;
      "summary"?: string;
    };
    "EvaluationCampaignEventPage": {
      "items": Array<components["schemas"]["EvaluationCampaignEvent"]>;
    };
    "EvaluationCampaignPage": {
      "items": Array<components["schemas"]["EvaluationCampaign"]>;
    };
    "EvaluationCampaignReport": {
      "content"?: Record<string, unknown>;
      "generated_at": components["schemas"]["Timestamp"];
      "reason_code"?: string;
      "report_hash"?: string;
      "state": "not_ready" | "final" | "partial";
      "summary"?: string;
      "verdict"?: "CONTINUE" | "IMPROVE" | "REJECT" | "BLOCKED";
    };
    "EvaluationCoverage": {
      "byte_count": number;
      "dataset_id": string;
      "duplicate_count": number;
      "eligibility": "eligible" | "ineligible" | "blocked";
      "exchange"?: string;
      "gap_count": number;
      "instrument"?: string;
      "reason_code": string;
      "segment_count": number;
    };
    "EvaluationFeedHealth": {
      "book_fresh": boolean;
      "clock_eligible": boolean;
      "decoder_error_count": number;
      "eligible": boolean;
      "exchange": "binance" | "bybit";
      "gap_count": number;
      "instrument": "BTCUSDT" | "ETHUSDT" | "ETHBTC";
      "latest_event_at": components["schemas"]["Timestamp"];
      "message_count": number;
      "queue_drop_count": number;
    };
    "EvaluationHistoricalImportProgress": {
      "byte_count": number;
      "checkpoint_time": components["schemas"]["Timestamp"];
      "exchange": "binance" | "bybit";
      "gap_count": number;
      "instrument": "BTC/USDT" | "ETH/USDT";
      "interval": "15m" | "1h" | "4h";
      "reason_code"?: string;
      "row_count": number;
      "state": "PENDING" | "RUNNING" | "COMPLETED" | "BLOCKED";
      "window_end": components["schemas"]["Timestamp"];
      "window_start": components["schemas"]["Timestamp"];
    };
    "EvaluationMatrixMember": {
      "capital_micros": number;
      "configuration": string;
      "cost_stress_bps": number;
      "id": string;
      "metrics"?: Record<string, unknown>;
      "mode": "backtest" | "replay" | "shadow" | "advisory";
      "reason_code"?: string;
      "repeat_ordinal": number;
      "result_hash"?: string;
      "state": "PENDING" | "QUEUED" | "RUNNING" | "SUCCEEDED" | "FAILED" | "EXCLUDED" | "CANCELED";
      "strategy": "trend-following" | "mean-reversion" | "triangular-arbitrage" | "cross-exchange-arbitrage" | "inventory-rebalancing";
      "verdict"?: "CONTINUE" | "IMPROVE" | "REJECT" | "BLOCKED";
    };
    "EvaluationShadowProgress": {
      "last_processed_ordinal": number;
      "member_ceiling_micros": number;
      "members": Array<components["schemas"]["EvaluationMatrixMember"]>;
      "protected_reserve_micros": number;
      "reason_code"?: string;
      "shared_capital_micros": number;
      "start_ordinal": number;
      "state": "PENDING" | "RUNNING" | "PAUSED_RECOVERABLE" | "COMPLETED" | "BLOCKED" | "CANCELED";
      "valid_seconds": number;
    };
    "EvaluationStageProgress": {
      "attempt": number;
      "completed_at"?: components["schemas"]["Timestamp"];
      "reason_code"?: string;
      "stage": "HISTORICAL_IMPORT" | "EXISTING_DATA_AUDIT" | "RECORDER_ROTATION" | "RECORDER_QUALIFICATION" | "BACKTEST_MATRIX" | "REPLAY_MATRIX" | "CANDIDATE_SELECTION" | "COMBINED_SHADOW" | "FINAL_REPORT";
      "started_at"?: components["schemas"]["Timestamp"];
      "state": "PENDING" | "RUNNING" | "PAUSED_RECOVERABLE" | "COMPLETED" | "BLOCKED" | "CANCELED";
      "updated_at": components["schemas"]["Timestamp"];
    };
    "EvidenceBundleRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "format": "txt" | "csv" | "json" | "jsonl";
      "reason": string;
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
    "ExportArtifact": {
      "command_id": string;
      "content"?: string;
      "content_hash": string;
      "content_type": string;
      "created_at": components["schemas"]["Timestamp"];
      "deleted": boolean;
      "expires_at": components["schemas"]["Timestamp"];
      "format": "txt" | "csv" | "json" | "jsonl";
      "held": boolean;
      "id": string;
      "job_id": string;
      "redaction_version": string;
      "resource_id": string;
      "resource_type": string;
      "revision": components["schemas"]["Revision"];
      "size_bytes": components["schemas"]["Revision"];
    };
    "ExportRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "format": "txt" | "csv" | "json" | "jsonl";
      "reason": string;
      "resource_id": string;
      "resource_type": "activity" | "report" | "incident" | "audit" | "qualification" | "lab_run";
    };
    "GuidedDemonstration": {
      "description": string;
      "expected_outcomes": Array<string>;
      "id": string;
      "strategy_id": string;
      "strategy_version": string;
      "synthetic": boolean;
      "title": string;
    };
    "GuidedDemonstrationEvent": {
      "balances": string;
      "decision": string;
      "execution_events": string;
      "orders": string;
      "ordinal": number;
    };
    "GuidedDemonstrationPage": {
      "items": Array<components["schemas"]["GuidedDemonstration"]>;
    };
    "GuidedDemonstrationResult": {
      "accepted": components["schemas"]["GuidedDemonstrationEvent"];
      "advisory_evidence"?: string;
      "advisory_only": boolean;
      "configuration_hash": string;
      "id": string;
      "metrics": string;
      "rejected": components["schemas"]["GuidedDemonstrationEvent"];
      "result_hash": string;
      "strategy_id": string;
      "strategy_version": string;
      "synthetic": boolean;
    };
    "HealthComponent": {
      "name": "postgres" | "authentication" | "outbox" | "public_binance" | "disk" | "recovery";
      "reason_code"?: "required_dependency_unavailable" | "bootstrap_required" | "stale" | "locked";
      "status": "ready" | "not_ready";
    };
    "HealthResponse": {
      "reason_code"?: string;
      "status": "live" | "ready" | "not_ready";
    };
    "HighRiskAuthorizationGrant": {
      "expires_at": components["schemas"]["Timestamp"];
      "purpose": "strategy_configuration" | "risk_control" | "qualification_start" | "configuration_activation" | "artifact_hold";
      "target_revision": components["schemas"]["Revision"];
      "token": string;
    };
    "HighRiskAuthorizationRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "password": string;
      "purpose": "strategy_configuration" | "risk_control" | "qualification_start" | "configuration_activation" | "artifact_hold";
      "reason": string;
      "totp": string;
    };
    "IncidentCreateRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "owner_user_id": string;
      "reason": string;
      "reason_code": string;
      "severity": "warning" | "error" | "critical";
    };
    "IncidentDetail": components["schemas"]["IncidentSummary"] & {
      "evidence_holds": Array<components["schemas"]["IncidentEvidenceHold"]>;
      "related_activity_ids": Array<string>;
      "related_alert_ids": Array<string>;
      "remediation_notes": Array<string>;
      "replay_window": {
      "dataset_id": string;
      "first_ordinal": components["schemas"]["Revision"];
      "last_ordinal": components["schemas"]["Revision"];
      "source_identity"?: string;
    };
      "resolution_evidence"?: string;
      "timeline": Array<components["schemas"]["TimelineEvent"]>;
    };
    "IncidentEvidenceHold": {
      "artifact_id": string;
      "created_at": components["schemas"]["Timestamp"];
      "hold_type": "incident" | "reproducibility";
      "id": string;
    };
    "IncidentPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["IncidentSummary"]>;
    };
    "IncidentSummary": {
      "id": string;
      "opened_at": components["schemas"]["Timestamp"];
      "owner_user_id": string;
      "reason_code": string;
      "resolved_at"?: components["schemas"]["Timestamp"];
      "revision": components["schemas"]["Revision"];
      "severity": "warning" | "error" | "critical";
      "state": "open" | "acknowledged" | "resolved";
      "updated_at": components["schemas"]["Timestamp"];
    };
    "IncidentTransitionRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "resolution_evidence"?: string;
      "state": "acknowledged" | "resolved";
    };
    "IncidentUpdateRequest": {
      "action": "assign_owner" | "add_remediation" | "link_alert" | "link_activity" | "link_replay";
      "dataset_id"?: string;
      "expected_revision": components["schemas"]["Revision"];
      "first_ordinal"?: components["schemas"]["Revision"];
      "last_ordinal"?: components["schemas"]["Revision"];
      "note"?: string;
      "owner_user_id"?: string;
      "reason": string;
      "reference_id"?: string;
      "source_identity"?: string;
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
      "checkpoints"?: Array<components["schemas"]["ReplayCheckpoint"]>;
      "created_at": components["schemas"]["Timestamp"];
      "cursor_ordinal"?: components["schemas"]["Revision"];
      "failure_code"?: string;
      "id": string;
      "input_manifest"?: components["schemas"]["LabInputManifest"];
      "kind": "backtest" | "replay";
      "lifecycle"?: components["schemas"]["LabLifecycleCapabilities"];
      "mode_label": "BACKTEST" | "REPLAY";
      "progress"?: components["schemas"]["NonnegativeDecimal"];
      "registered_report"?: components["schemas"]["RegisteredResearchReport"];
      "replay_inspection"?: components["schemas"]["ReplayEventInspection"];
      "reproduction_bundle"?: components["schemas"]["ReproductionBundle"];
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
    "LabInputManifest": {
      "configuration_id": string;
      "dataset_id": string;
      "first_ordinal"?: components["schemas"]["Revision"];
      "incident_id"?: string;
      "last_ordinal"?: components["schemas"]["Revision"];
      "research_generation_id": string;
      "root_seed_hash": string;
      "speed"?: "original" | "accelerated" | "maximum";
      "strategy_version": "trend-following@1.0.0" | "mean-reversion@1.0.0" | "triangular-arbitrage@1.0.0" | "cross-exchange-arbitrage@1.0.0" | "inventory-rebalancing@1.0.0";
    };
    "LabLifecycleCapabilities": {
      "cancel": boolean;
      "compare": boolean;
      "export": boolean;
      "pause": boolean;
      "reproduce": boolean;
      "resume": boolean;
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
      "strategy_version": "trend-following@1.0.0" | "mean-reversion@1.0.0" | "triangular-arbitrage@1.0.0" | "cross-exchange-arbitrage@1.0.0" | "inventory-rebalancing@1.0.0";
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
    "OwnerControlResource": {
      "attributes": Record<string, unknown>;
      "correlation_id": string;
      "id": string;
      "kind": string;
      "links": Record<string, string>;
      "occurred_at"?: components["schemas"]["Timestamp"];
      "reason"?: components["schemas"]["ReasonPresentation"];
      "revision": components["schemas"]["Revision"];
      "state": string;
    };
    "OwnerControlResourcePage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["OwnerControlResource"]>;
      "snapshot_revision": components["schemas"]["Revision"];
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
    "QualificationStartRequest": {
      "authorization_token": string;
      "configuration_hash": string;
      "expected_revision": components["schemas"]["Revision"];
      "image_digest"?: string;
      "qualification_id": string;
      "reason": string;
      "server_identity"?: string;
      "source_sha": string;
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
    "ReasonPresentation": {
      "code": string;
      "explanation": string;
      "severity": "info" | "warning" | "error" | "critical";
      "suggested_action": string;
      "summary": string;
      "unknown": boolean;
      "version": components["schemas"]["Revision"];
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
    "ReplayCheckpoint": {
      "created_at": components["schemas"]["Timestamp"];
      "deterministic_state_hash"?: string;
      "input_ordinal": components["schemas"]["Revision"];
      "model_namespace_id"?: string;
      "revision": components["schemas"]["Revision"];
      "state_hash": string;
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
    "ReportProvenance": {
      "confidence_tier": string;
      "maturity": string;
      "mode": "backtest" | "replay" | "paper" | "shadow" | "testnet" | "demo" | "mixed" | "operational";
      "model_provenance": Record<string, string>;
      "source_identity": string;
      "source_revision": components["schemas"]["Revision"];
      "valuation_basis": string;
    };
    "ReportRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "report_type": "strategy_results" | "decisions_orders" | "portfolios" | "inventory_pnl" | "risk" | "exchange_data_health" | "lab_runs" | "sandbox_qualifications" | "platform_readiness";
    };
    "ReportResource": {
      "content_hash"?: string;
      "created_at": components["schemas"]["Timestamp"];
      "failure_code"?: string;
      "generated_at"?: components["schemas"]["Timestamp"];
      "id": string;
      "job_id": string;
      "provenance": components["schemas"]["ReportProvenance"];
      "report_type": "strategy_results" | "decisions_orders" | "portfolios" | "inventory_pnl" | "risk" | "exchange_data_health" | "lab_runs" | "sandbox_qualifications" | "platform_readiness";
      "revision": components["schemas"]["Revision"];
      "schedule_id"?: string;
      "state": "QUEUED" | "RUNNING" | "SUCCEEDED" | "FAILED";
    };
    "ReportSchedule": {
      "created_at": components["schemas"]["Timestamp"];
      "frequency": "hourly" | "daily" | "weekly";
      "hour_utc"?: number;
      "id": string;
      "last_run_at"?: components["schemas"]["Timestamp"];
      "minute_utc": number;
      "next_run_at": components["schemas"]["Timestamp"];
      "report_type": "strategy_results" | "decisions_orders" | "portfolios" | "inventory_pnl" | "risk" | "exchange_data_health" | "lab_runs" | "sandbox_qualifications" | "platform_readiness";
      "revision": components["schemas"]["Revision"];
      "state": "active" | "paused";
      "updated_at": components["schemas"]["Timestamp"];
      "weekday_utc"?: number;
    };
    "ReportSchedulePage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["ReportSchedule"]>;
    };
    "ReportScheduleRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "frequency": "hourly" | "daily" | "weekly";
      "hour_utc"?: number;
      "minute_utc": number;
      "reason": string;
      "report_type": "strategy_results" | "decisions_orders" | "portfolios" | "inventory_pnl" | "risk" | "exchange_data_health" | "lab_runs" | "sandbox_qualifications" | "platform_readiness";
      "weekday_utc"?: number;
    };
    "ReportScheduleTransitionRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "state": "active" | "paused";
    };
    "ReproductionBundle": {
      "architecture": string;
      "canonical_manifest": string;
      "code_commit": string;
      "confidence_tier": "A" | "B" | "C" | "D";
      "configuration_hash": string;
      "dataset_manifest_hash": string;
      "dataset_revision": components["schemas"]["Revision"];
      "go_version": string;
      "input_hash": string;
      "manifest_hash": string;
      "model_namespace_id": string;
      "operating_system": string;
      "result_hash"?: string;
      "run_id": string;
      "source_commit": string;
      "starting_balance_hash": string;
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
    "RiskControlRequest": {
      "authorization_token"?: string;
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "state": "normal" | "paused" | "locked";
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
    "RunAction": "pause" | "resume" | "step" | "stop";
    "RunBlocker": {
      "code": string;
      "detail": string;
      "suggested_action": string;
      "summary": string;
    };
    "RunCatalog": {
      "blocker"?: components["schemas"]["RunBlocker"];
      "choices": Array<components["schemas"]["RunChoice"]>;
    };
    "RunChoice": {
      "cadence": string;
      "exchanges": Array<"binance" | "bybit">;
      "instrument": string;
      "mode": "demonstration" | "backtest" | "replay" | "shadow" | "sandbox" | "testnet" | "demo";
      "order_capable": boolean;
      "strategy_id": string;
      "strategy_name": string;
      "strategy_version": string;
      "warmup": string;
    };
    "RunCreateRequest": {
      "exchanges": Array<"binance" | "bybit">;
      "instrument": string;
      "mode": "backtest" | "replay" | "shadow" | "sandbox" | "testnet" | "demo" | "demonstration";
      "preset": "latest-qualified-inputs";
      "strategy_id": string;
      "strategy_version": string;
    };
    "RunEvidence": {
      "confidence_tier"?: "A" | "B" | "C" | "D";
      "configuration_hash"?: string;
      "dataset_manifest_hash"?: string;
      "manifest_hash"?: string;
      "model_namespace"?: string;
      "source_commit"?: string;
      "state": "recorded" | "not_recorded";
    };
    "RunOutput": {
      "canonical_payload": string;
      "content_hash": string;
      "kind": "event" | "decision" | "order" | "execution";
      "ordinal": components["schemas"]["Revision"];
    };
    "RunOutputPage": {
      "items": Array<components["schemas"]["RunOutput"]>;
    };
    "RunPage": {
      "items": Array<components["schemas"]["RunResource"]>;
    };
    "RunPortfolioFee": {
      "asset": string;
      "exchange": "binance" | "bybit";
      "fee": components["schemas"]["NonnegativeDecimal"];
      "instrument": string;
      "rebate": components["schemas"]["NonnegativeDecimal"];
    };
    "RunPortfolioPosition": {
      "exchange": "binance" | "bybit";
      "instrument": string;
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "realized_pnl": components["schemas"]["Decimal"];
      "total_cost": components["schemas"]["NonnegativeDecimal"];
      "updated_at": components["schemas"]["Timestamp"];
      "valuation_state": "complete" | "unvalued_fee_asset";
      "weighted_average_cost": components["schemas"]["NonnegativeDecimal"];
    };
    "RunPortfolioProjection": {
      "account_drawdown"?: components["schemas"]["NonnegativeDecimal"];
      "canonical_payload"?: string;
      "content_hash"?: string;
      "fees"?: Array<components["schemas"]["RunPortfolioFee"]>;
      "ordinal"?: components["schemas"]["Revision"];
      "positions"?: Array<components["schemas"]["RunPortfolioPosition"]>;
      "realized_pnl"?: components["schemas"]["Decimal"];
      "slippage"?: components["schemas"]["NonnegativeDecimal"];
      "state": "recorded" | "not_recorded";
      "summary"?: string;
      "total_pnl"?: components["schemas"]["Decimal"];
      "unrealized_pnl"?: components["schemas"]["Decimal"];
      "waiting_reason"?: string;
    };
    "RunResource": {
      "available_actions": Array<components["schemas"]["RunAction"]>;
      "created_at": components["schemas"]["Timestamp"];
      "environment": "recorded_data" | "production_public" | "binance_spot_testnet" | "bybit_demo" | "paired_exchange_sandbox";
      "exchanges"?: Array<"binance" | "bybit">;
      "friendly_name": string;
      "id": string;
      "instrument"?: string;
      "mode": "backtest" | "replay" | "shadow" | "sandbox";
      "next_evaluation_at"?: components["schemas"]["Timestamp"];
      "order_capable": boolean;
      "revision": components["schemas"]["Revision"];
      "state": string;
      "strategy_id": string;
      "strategy_version": string;
      "updated_at"?: components["schemas"]["Timestamp"];
      "waiting_reason"?: string;
    };
    "RunRiskObservation": {
      "account_drawdown": components["schemas"]["NonnegativeDecimal"];
      "asset_exposure": components["schemas"]["NonnegativeDecimal"];
      "combined_exposure": components["schemas"]["NonnegativeDecimal"];
      "evidence_hash": string;
      "exchange": "binance" | "bybit";
      "exchange_exposure": components["schemas"]["NonnegativeDecimal"];
      "health_blockers": Array<string>;
      "instrument": string;
      "observed_at": components["schemas"]["Timestamp"];
      "open_orders": number;
      "policy_version": components["schemas"]["Revision"];
      "quality_score": number;
      "reserve": components["schemas"]["NonnegativeDecimal"];
      "reserved_capital": components["schemas"]["NonnegativeDecimal"];
      "rolling_24_hour_loss": components["schemas"]["NonnegativeDecimal"];
      "slippage": components["schemas"]["NonnegativeDecimal"];
      "spread": components["schemas"]["NonnegativeDecimal"];
      "strategy_loss": components["schemas"]["NonnegativeDecimal"];
      "utc_day_loss": components["schemas"]["NonnegativeDecimal"];
    };
    "RunRiskProjection": {
      "blockers"?: Array<string>;
      "observations"?: Array<components["schemas"]["RunRiskObservation"]>;
      "state": "recorded" | "not_recorded";
      "status"?: "normal" | "waiting" | "blocked";
      "summary": string;
    };
    "RuntimeControlRequest": {
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "state": "running" | "paused";
    };
    "SandboxAccount": {
      "account_epoch": number;
      "active_arm"?: components["schemas"]["SandboxArm"];
      "audit_url": string;
      "cap_usage": components["schemas"]["SandboxCapUsage"];
      "credential_generation": number;
      "engine_ready": boolean;
      "environment": components["schemas"]["SandboxEnvironment"];
      "evidence_healthy": boolean;
      "exchange": components["schemas"]["SandboxExchange"];
      "id": string;
      "lease_held": boolean;
      "observed_at": components["schemas"]["Timestamp"];
      "private_stream_healthy": boolean;
      "reconciliation_clean": boolean;
      "revision": components["schemas"]["Revision"];
      "session_id"?: string;
      "session_revision"?: components["schemas"]["Revision"];
      "stale": boolean;
      "startup_cycle": number;
      "state": "LOCKED" | "READY_PAUSED" | "ARMED" | "DEGRADED" | "QUARANTINED";
    };
    "SandboxArm": {
      "account_ids": Array<string>;
      "audit_url": string;
      "created_at": components["schemas"]["Timestamp"];
      "expires_at": components["schemas"]["Timestamp"];
      "id": string;
      "revision": components["schemas"]["Revision"];
      "revoked_at"?: components["schemas"]["Timestamp"];
      "session_id": string;
      "state": "active" | "expired" | "revoked";
    };
    "SandboxArmRequest": {
      "account_ids": Array<string>;
      "authorization_token": string;
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
    };
    "SandboxAuthorizationGrant": {
      "expires_at": components["schemas"]["Timestamp"];
      "purpose": "sandbox_arm" | "risk_unlock";
      "token": string;
    };
    "SandboxAuthorizationRequest": {
      "password": string;
      "purpose": "sandbox_arm" | "risk_unlock";
      "reason": string;
      "totp": string;
    };
    "SandboxCapUsage": {
      "account_open": number;
      "account_open_limit": 1;
      "daily_limit": components["schemas"]["NonnegativeDecimal"];
      "daily_remaining": components["schemas"]["NonnegativeDecimal"];
      "daily_reserved": components["schemas"]["NonnegativeDecimal"];
      "global_open": number;
      "global_open_limit": 2;
      "per_order_limit": components["schemas"]["NonnegativeDecimal"];
      "utc_day": string;
    };
    "SandboxChaosSummary": {
      "failed": number;
      "last_observed_at": components["schemas"]["Timestamp"];
      "passed": number;
      "status": "not_run" | "passed" | "failed";
    };
    "SandboxDifference": {
      "asset"?: "USDT" | "BTC" | "ETH";
      "audit_url": string;
      "category": string;
      "classification": string;
      "critical": boolean;
      "id": string;
      "quantity"?: components["schemas"]["Decimal"];
      "recorded_at": components["schemas"]["Timestamp"];
      "state": "OPEN" | "RESOLVED" | "QUARANTINED" | "ADJUSTED";
    };
    "SandboxEnvironment": "spot_testnet" | "demo";
    "SandboxExchange": "binance" | "bybit";
    "SandboxFill": {
      "audit_url": string;
      "fee_asset": "USDT" | "BTC" | "ETH";
      "fee_quantity": components["schemas"]["NonnegativeDecimal"];
      "id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "order_id": string;
      "price": components["schemas"]["NonnegativeDecimal"];
      "quantity": components["schemas"]["NonnegativeDecimal"];
    };
    "SandboxOrder": {
      "account_id": string;
      "action": "ENTRY" | "EXIT" | "CANCEL" | "RECOVERY";
      "attempt": number;
      "audit_url": string;
      "created_at": components["schemas"]["Timestamp"];
      "environment": components["schemas"]["SandboxEnvironment"];
      "exchange": components["schemas"]["SandboxExchange"];
      "fills": Array<components["schemas"]["SandboxFill"]>;
      "id": string;
      "instrument": "BTCUSDT" | "ETHUSDT" | "ETHBTC";
      "limit_price": components["schemas"]["NonnegativeDecimal"];
      "notional": components["schemas"]["NonnegativeDecimal"];
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "recovery_status": "not_required" | "required" | "querying" | "reconciled";
      "revision": components["schemas"]["Revision"];
      "side": "buy" | "sell";
      "state": "APPROVED" | "SUBMITTING" | "ACKNOWLEDGED" | "PARTIALLY_FILLED" | "FILLED" | "CANCEL_PENDING" | "CANCELED" | "REJECTED" | "EXPIRED" | "UNKNOWN" | "RECOVERY_REQUIRED";
      "style": "LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY";
      "unknown_since"?: components["schemas"]["Timestamp"];
      "updated_at": components["schemas"]["Timestamp"];
    };
    "SandboxOrderPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["SandboxOrder"]>;
    };
    "SandboxOverview": {
      "accounts": Array<components["schemas"]["SandboxAccount"]>;
      "active_arms": Array<components["schemas"]["SandboxArm"]>;
      "audit_url": string;
      "environment_label": "BINANCE SPOT TESTNET + BYBIT DEMO / VIRTUAL";
      "observed_at": components["schemas"]["Timestamp"];
      "orders": Array<components["schemas"]["SandboxOrder"]>;
      "qualification": components["schemas"]["SandboxQualificationStatus"];
      "real_trading_enabled": false;
      "reconciliations": Array<components["schemas"]["SandboxReconciliation"]>;
      "reset_incidents": Array<components["schemas"]["SandboxResetIncident"]>;
      "risk_state": "NORMAL" | "CAUTIOUS" | "PAUSED" | "LOCKED";
      "stale": boolean;
      "strategy_sessions": Array<components["schemas"]["SandboxStrategySession"]>;
    };
    "SandboxQualificationStatus": {
      "audit_url": string;
      "build_hash"?: string;
      "chaos": components["schemas"]["SandboxChaosSummary"];
      "commit_sha"?: string;
      "configuration_hash"?: string;
      "ended_at"?: components["schemas"]["Timestamp"];
      "evidence_hash"?: string;
      "executable_hash"?: string;
      "failures": Array<string>;
      "formal_soak_pending": boolean;
      "id"?: string;
      "image_hash"?: string;
      "mode": "none" | "smoke" | "formal";
      "observed_duration_seconds": number;
      "profitability_evidence": false;
      "qualified": boolean;
      "recovery_incidents": Array<components["schemas"]["SandboxRecoveryIncident"]>;
      "required_duration_seconds": number;
      "slo": components["schemas"]["SandboxSLOSummary"];
      "started_at"?: components["schemas"]["Timestamp"];
      "state": "not_started" | "PENDING" | "RUNNING" | "SMOKE_PASSED" | "PASSED" | "FAILED";
    };
    "SandboxReconciliation": {
      "account_epoch": number;
      "account_id": string;
      "audit_url": string;
      "differences": Array<components["schemas"]["SandboxDifference"]>;
      "exchange": components["schemas"]["SandboxExchange"];
      "id": string;
      "quarantine_count": number;
      "reconciled_at": components["schemas"]["Timestamp"];
      "state": "clean" | "quarantined";
      "suspense_count": number;
    };
    "SandboxReconciliationPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["SandboxReconciliation"]>;
      "reset_incidents": Array<components["schemas"]["SandboxResetIncident"]>;
    };
    "SandboxRecoveryIncident": {
      "account_id": string;
      "cause_code": string;
      "clean_check_count": number;
      "deadline_at": components["schemas"]["Timestamp"];
      "detected_at": components["schemas"]["Timestamp"];
      "environment": "spot_testnet" | "demo";
      "evidence_hash": string;
      "exchange": "binance" | "bybit";
      "incident_source": "reconciliation" | "private_stream";
      "reason_category": string;
      "recovery_timestamp"?: components["schemas"]["Timestamp"];
      "state": "active" | "recovered" | "expired" | "repeated" | "unrecoverable";
    };
    "SandboxResetIncident": {
      "account_id": string;
      "adjustments": Array<{
      "asset": "USDT" | "BTC" | "ETH";
      "pnl_effect": false;
      "quantity": components["schemas"]["Decimal"];
      "recorded_at": components["schemas"]["Timestamp"];
    }>;
      "audit_url": string;
      "detected_at": components["schemas"]["Timestamp"];
      "exchange": components["schemas"]["SandboxExchange"];
      "id": string;
      "new_epoch": number;
      "prior_epoch": number;
      "resolved_at"?: components["schemas"]["Timestamp"];
      "state": "OPEN" | "RECONCILING" | "RESOLVED" | "QUARANTINED";
    };
    "SandboxSLOSummary": {
      "critical_alert_latency_ms": number;
      "double_posted_fills": number;
      "duplicate_creates": number;
      "lost_fills": number;
      "passing": boolean;
      "positive_memory_leak_trend": boolean;
      "reconciliation_mismatches": number;
      "reconnects": number;
      "recovery_duration_ms": number;
      "resident_memory_delta_bytes": number;
      "restarts": number;
      "samples": number;
      "suspense_items": number;
      "unknown_orders": number;
    };
    "SandboxStrategySession": {
      "audit_url": string;
      "created_at": components["schemas"]["Timestamp"];
      "display_name": string;
      "exchanges": Array<components["schemas"]["SandboxExchange"]>;
      "id": string;
      "instrument"?: "BTCUSDT" | "ETHUSDT";
      "revision": components["schemas"]["Revision"];
      "started_at"?: components["schemas"]["Timestamp"];
      "state": "prepared" | "running" | "blocked" | "stopped";
      "stopped_at"?: components["schemas"]["Timestamp"];
      "strategy_name": string;
      "waiting_reason"?: string;
    };
    "SandboxStrategySessionCreateRequest": {
      "exchanges": Array<components["schemas"]["SandboxExchange"]>;
      "instrument": "BTCUSDT" | "ETHUSDT";
      "preset": "latest-qualified-inputs";
      "reason": string;
      "strategy_id": "trend-following" | "mean-reversion" | "triangular-arbitrage" | "cross-exchange-arbitrage";
    };
    "SandboxStrategySessionStartRequest": {
      "authorization_token": string;
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
    };
    "SandboxTestOrderRequest": {
      "account_id": string;
      "arm_id": string;
      "exchange": components["schemas"]["SandboxExchange"];
      "expected_revision": components["schemas"]["Revision"];
      "instrument": "BTCUSDT" | "ETHUSDT";
      "limit_price": components["schemas"]["NonnegativeDecimal"];
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "reason": string;
      "session_id": string;
      "side": "buy";
      "style": "LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY";
    };
    "SandboxUnlockRequest": {
      "authorization_token": string;
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "reconciliation_id": string;
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
    };
    "ShadowBalance": {
      "asset": string;
      "available": components["schemas"]["NonnegativeDecimal"];
      "reserved": components["schemas"]["NonnegativeDecimal"];
      "revision": components["schemas"]["Revision"];
      "updated_at": components["schemas"]["Timestamp"];
    };
    "ShadowDataHealth": {
      "exchange": string;
      "fresh": boolean;
      "observed_at": components["schemas"]["Timestamp"];
      "reason": string;
      "state": "CONNECTING" | "SYNCING" | "SUBSCRIBED" | "HEALTHY" | "PAUSED" | "DISCONNECTED";
    };
    "ShadowDecisionSummary": {
      "id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "outcome": string;
      "reason_code": string;
      "risk_outcome": "approved" | "rejected" | "paused" | "locked" | "not_evaluated";
      "risk_reason_code": string;
    };
    "ShadowInputHealth": {
      "age_milliseconds": components["schemas"]["Revision"];
      "book_version": components["schemas"]["Revision"];
      "exchange": "binance" | "bybit";
      "fresh": boolean;
      "instrument": string;
      "observed_at": components["schemas"]["Timestamp"];
      "reason": string;
      "state": "CONNECTING" | "SYNCING" | "HEALTHY" | "STALE" | "PAUSED" | "DISCONNECTED" | "UNAVAILABLE";
    };
    "ShadowPnlAttribution": {
      "fee_expense": components["schemas"]["Decimal"];
      "latency": components["schemas"]["Decimal"];
      "realized_pnl": components["schemas"]["Decimal"];
      "slippage": components["schemas"]["Decimal"];
      "spread": components["schemas"]["Decimal"];
      "valuation_basis": "sealed_ledger_functional_value";
    };
    "ShadowPosition": {
      "instrument": string;
      "quantity": components["schemas"]["NonnegativeDecimal"];
      "realized_pnl": components["schemas"]["Decimal"];
      "revision": components["schemas"]["Revision"];
      "updated_at": components["schemas"]["Timestamp"];
      "weighted_average_cost": components["schemas"]["NonnegativeDecimal"];
    };
    "ShadowSessionPage": components["schemas"]["Page"] & {
      "items": Array<components["schemas"]["ShadowSessionSummary"]>;
    };
    "ShadowSessionRequest": {
      "configuration_id": string;
      "portfolio_id": string;
      "strategy_version": "trend-following@1.0.0";
    };
    "ShadowSessionResource": {
      "accepted_decisions": number;
      "activity_state": "preparing" | "warming_up" | "waiting" | "evaluating" | "running" | "paused" | "blocked" | "stopped";
      "balances"?: Array<components["schemas"]["ShadowBalance"]>;
      "configuration_id": string;
      "created_at": components["schemas"]["Timestamp"];
      "data_health"?: components["schemas"]["ShadowDataHealth"];
      "decision_dataset_id": string;
      "decisions"?: Array<components["schemas"]["ShadowDecisionSummary"]>;
      "entries_enabled": boolean;
      "exchange_id"?: string;
      "failure_code"?: string;
      "gap_model_id"?: string;
      "id": string;
      "input_health": Array<components["schemas"]["ShadowInputHealth"]>;
      "journal_transactions": number;
      "label": "PUBLIC-LIVE SHADOW / VIRTUAL";
      "model_namespace_id": string;
      "next_evaluation_at"?: components["schemas"]["Timestamp"];
      "orders"?: Array<components["schemas"]["SimulatedOrder"]>;
      "pnl_attribution"?: components["schemas"]["ShadowPnlAttribution"];
      "portfolio_id"?: string;
      "positions"?: Array<components["schemas"]["ShadowPosition"]>;
      "public_only": true;
      "rejected_decisions": number;
      "revision": components["schemas"]["Revision"];
      "risk_state"?: "PAUSED" | "RESUMED" | "LOCKED";
      "run_id"?: string;
      "simulation_only": true;
      "slippage_model_id"?: string;
      "started_at"?: components["schemas"]["Timestamp"];
      "state": "QUEUED" | "RUNNING" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "FAILED";
      "stopped_at"?: components["schemas"]["Timestamp"];
      "strategy_version": string;
      "trigger_condition": string;
      "waiting_reason": string;
      "waiting_reason_code": string;
    };
    "ShadowSessionSummary": {
      "configuration_id": string;
      "created_at": components["schemas"]["Timestamp"];
      "failure_code"?: string;
      "id": string;
      "public_only": true;
      "revision": components["schemas"]["Revision"];
      "simulation_only": true;
      "state": "QUEUED" | "RUNNING" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "FAILED";
      "stopped_at"?: components["schemas"]["Timestamp"];
      "strategy_version": "trend-following@1.0.0" | "mean-reversion@1.0.0";
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
    "StrategyConfigurationRequest": {
      "authorization_token": string;
      "configuration_id": string;
      "expected_revision": components["schemas"]["Revision"];
      "reason": string;
      "state": "enabled" | "disabled";
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
      "stream": "system" | "exchange" | "portfolio" | "risk" | "trend" | "job" | "shadow" | "incident" | "alert" | "order" | "fill" | "opportunity" | "strategy" | "inventory" | "rebalancing" | "research" | "sandbox" | "activity" | "qualification" | "export" | "configuration";
    };
    "SystemStatus": {
      "active_resource_id"?: string;
      "application_version": string;
      "binance_state"?: string;
      "build_commit": string;
      "clock_drift_ms"?: string;
      "configuration_identity": string;
      "critical_incidents"?: number;
      "engine_state"?: string;
      "environment"?: string;
      "execution_mode"?: "backtest" | "replay" | "paper" | "shadow";
      "lifecycle_state": "STARTING" | "READY_PAUSED" | "RUNNING" | "DEGRADED" | "STOPPING";
      "readiness_state": "ready" | "blocked" | "degraded";
      "real_trading_enabled": false;
      "revision"?: components["schemas"]["Revision"];
      "risk_state"?: "PAUSED" | "RESUMED" | "LOCKED";
      "server_time"?: components["schemas"]["Timestamp"];
      "strategy_activation": "unavailable" | "trend-following@1.0.0";
    };
    "TimelineEvent": {
      "actor"?: string;
      "correlation_id": string;
      "event_hash"?: string;
      "event_type": string;
      "id": string;
      "occurred_at": components["schemas"]["Timestamp"];
      "reason"?: string;
      "redacted": boolean;
      "reference_id"?: string;
      "reference_type"?: string;
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
      "version": "trend-following@1.0.0";
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
  "getRunCatalog": { responses: { "200": components["schemas"]["RunCatalog"]; "401": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listGuidedDemonstrations": { responses: { "200": components["schemas"]["GuidedDemonstrationPage"]; "401": components["schemas"]["Error"]; }; };
  "getGuidedDemonstration": { path: { "id": string; }; responses: { "200": components["schemas"]["GuidedDemonstrationResult"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listRuns": { responses: { "200": components["schemas"]["RunPage"]; "401": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createRun": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RunCreateRequest"]; responses: { "202": components["schemas"]["RunResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getRun": { path: { "id": string; }; responses: { "200": components["schemas"]["RunResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "pauseRun": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "resumeRun": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "stepRun": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "stopRun": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getRunTimeline": { path: { "id": string; }; responses: { "200": components["schemas"]["RunOutputPage"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRunDecisions": { path: { "id": string; }; responses: { "200": components["schemas"]["RunOutputPage"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRunOrders": { path: { "id": string; }; responses: { "200": components["schemas"]["RunOutputPage"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRunFills": { path: { "id": string; }; responses: { "200": components["schemas"]["RunOutputPage"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRunPortfolio": { path: { "id": string; }; responses: { "200": components["schemas"]["RunPortfolioProjection"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRunRisk": { path: { "id": string; }; responses: { "200": components["schemas"]["RunRiskProjection"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRunEvidence": { path: { "id": string; }; responses: { "200": components["schemas"]["RunEvidence"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listDataCatalogue": { responses: { "200": components["schemas"]["DataCataloguePage"]; "401": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listEvaluationCampaigns": { responses: { "200": components["schemas"]["EvaluationCampaignPage"]; "401": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createEvaluationCampaign": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["EvaluationCampaignCreateRequest"]; responses: { "202": components["schemas"]["EvaluationCampaign"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getEvaluationCampaign": { path: { "id": string; }; responses: { "200": components["schemas"]["EvaluationCampaign"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "cancelEvaluationCampaign": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "listEvaluationCampaignEvents": { path: { "id": string; }; responses: { "200": components["schemas"]["EvaluationCampaignEventPage"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getEvaluationCampaignReport": { path: { "id": string; }; responses: { "200": components["schemas"]["EvaluationCampaignReport"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "createDataAudit": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; responses: { "202": components["schemas"]["DataAudit"]; "401": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getDataAudit": { path: { "id": string; }; responses: { "200": components["schemas"]["DataAudit"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
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
  "listShadowSessions": { query: { "cursor"?: string; "page_size"?: number; "state"?: "QUEUED" | "RUNNING" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "FAILED"; }; responses: { "200": components["schemas"]["ShadowSessionPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "createShadowSession": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ShadowSessionRequest"]; responses: { "202": components["schemas"]["ShadowSessionResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "stopShadowSession": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "getShadowSession": { path: { "id": string; }; responses: { "200": components["schemas"]["ShadowSessionResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listIncidents": { query: { "cursor"?: string; "page_size"?: number; "state"?: "open" | "acknowledged" | "resolved"; }; responses: { "200": components["schemas"]["IncidentPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "createIncident": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["IncidentCreateRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getIncident": { path: { "id": string; }; query: { "include_raw"?: boolean; }; responses: { "200": components["schemas"]["IncidentDetail"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listAuditEvents": { query: { "cursor"?: string; "page_size"?: number; "event_type"?: string; "include_detail"?: boolean; }; responses: { "200": components["schemas"]["AuditEventPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "verifyAuditChain": { responses: { "200": components["schemas"]["AuditVerification"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getSandboxOverview": { responses: { "200": components["schemas"]["SandboxOverview"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listSandboxOrders": { query: { "cursor"?: string; "page_size"?: number; "exchange"?: components["schemas"]["SandboxExchange"]; "state"?: string; }; responses: { "200": components["schemas"]["SandboxOrderPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "createSandboxTestOrder": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["SandboxTestOrderRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listSandboxReconciliations": { query: { "cursor"?: string; "page_size"?: number; "exchange"?: components["schemas"]["SandboxExchange"]; }; responses: { "200": components["schemas"]["SandboxReconciliationPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "getSandboxQualificationStatus": { responses: { "200": components["schemas"]["SandboxQualificationStatus"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "createSandboxAuthorization": { header: { "Origin": string; "X-CSRF-Token": string; }; requestBody: components["schemas"]["SandboxAuthorizationRequest"]; responses: { "201": components["schemas"]["SandboxAuthorizationGrant"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createSandboxArm": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["SandboxArmRequest"]; responses: { "201": components["schemas"]["SandboxArm"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "createSandboxStrategySession": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["SandboxStrategySessionCreateRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "startSandboxStrategySession": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["SandboxStrategySessionStartRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "stopSandboxStrategySession": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "revokeSandboxArm": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "unlockSandboxAccount": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["SandboxUnlockRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "cancelSandboxOrder": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "querySandboxOrder": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "reconcileSandboxAccount": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createHighRiskAuthorization": { header: { "Origin": string; "X-CSRF-Token": string; }; requestBody: components["schemas"]["HighRiskAuthorizationRequest"]; responses: { "201": components["schemas"]["HighRiskAuthorizationGrant"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listAssets": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getStrategyDetail": { path: { "id": string; }; responses: { "200": components["schemas"]["OwnerControlResource"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listStrategyVersions": { path: { "id": string; }; query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "configureStrategy": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["StrategyConfigurationRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "controlStrategyRuntime": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RuntimeControlRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listRiskControls": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "controlRiskScope": { path: { "scope": "global" | "strategy" | "instrument" | "exchange" | "new_entries"; "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RiskControlRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listActivity": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "view"?: "decisions_orders" | "system_events"; "strategy"?: string; "instrument"?: string; "exchange"?: string; "side"?: "buy" | "sell"; "outcome"?: string; "reason"?: string; "mode"?: "backtest" | "replay" | "paper" | "shadow" | "testnet" | "demo"; "correlation_id"?: string; }; responses: { "200": components["schemas"]["ActivityPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getActivity": { path: { "id": string; }; responses: { "200": components["schemas"]["ActivityResource"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listOrders": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getOrder": { path: { "id": string; }; responses: { "200": components["schemas"]["OwnerControlResource"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listFills": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listAlerts": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getAlert": { path: { "id": string; }; responses: { "200": components["schemas"]["AlertDetail"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "acknowledgeAlert": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "escalateAlert": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listAlertRoutes": { responses: { "200": components["schemas"]["AlertRoutePage"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "testAlertRoute": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["AlertTestRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listReports": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createReport": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReportRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getReport": { path: { "id": string; }; responses: { "200": components["schemas"]["ReportResource"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listReportSchedules": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["ReportSchedulePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createReportSchedule": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReportScheduleRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "transitionReportSchedule": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReportScheduleTransitionRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createExport": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ExportRequest"]; responses: { "201": components["schemas"]["ExportArtifact"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getExport": { path: { "id": string; }; responses: { "200": components["schemas"]["ExportArtifact"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "deleteExport": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "holdExport": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ArtifactHoldRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "transitionIncident": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["IncidentTransitionRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "updateIncident": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["IncidentUpdateRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "createIncidentEvidenceBundle": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["EvidenceBundleRequest"]; responses: { "201": components["schemas"]["ExportArtifact"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listConfigurationRevisions": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "activateConfigurationRevision": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ConfigurationActivationRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listLabRuns": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "controlLabRun": { path: { "id": string; "action": "pause" | "resume" | "cancel" | "reproduce"; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "listQualifications": { query: { "cursor"?: string; "page_size"?: number; "from"?: components["schemas"]["Timestamp"]; "to"?: components["schemas"]["Timestamp"]; "state"?: string; }; responses: { "200": components["schemas"]["OwnerControlResourcePage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "startQualification": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["QualificationStartRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "abortQualification": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getCommand": { path: { "id": string; }; responses: { "200": components["schemas"]["OwnerControlResource"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "streamEvents": { query: { "after_revision"?: components["schemas"]["Revision"]; }; header: { "Origin": string; "Last-Event-ID"?: components["schemas"]["Revision"]; }; responses: { "200": string; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; }; };
}
