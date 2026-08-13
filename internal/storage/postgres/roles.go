package postgres

import (
	"regexp"
)

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

var runtimeReadInsertTables = []string{
	"account_snapshots", "alert_acknowledgements", "alert_deliveries", "alerts", "asset_screening_versions", "assets", "audit_events",
	"api_entity_revisions", "authentication_failures", "owner_accounts",
	"evaluation_campaigns", "evaluation_campaign_stages", "evaluation_campaign_members",
	"evaluation_campaign_commands", "evaluation_data_audits", "evaluation_historical_imports",
	"evaluation_recorder_requests",
	"evaluation_campaign_events", "evaluation_campaign_reports", "evaluation_campaign_stress_results", "evaluation_data_audit_findings",
	"evaluation_historical_import_segments", "evaluation_campaign_datasets", "evaluation_campaign_dataset_members",
	"evaluation_campaign_metadata", "evaluation_campaign_candidate_locks", "evaluation_campaign_recording_segments",
	"evaluation_recorder_observations", "evaluation_recorder_instrument_observations",
	"evaluation_shadow_sessions", "evaluation_shadow_member_checkpoints", "evaluation_shadow_decisions",
	"sandbox_runtime_high_risk_audit_events", "sandbox_runtime_sandbox_authorizations", "sandbox_runtime_session_control_events",
	"sandbox_runtime_totp_replay_state", "sandbox_runtime_sandbox_sessions", "sandbox_runtime_sandbox_session_accounts", "sandbox_runtime_sandbox_arms",
	"sandbox_strategy_sessions", "sandbox_strategy_session_accounts",
	"sandbox_strategy_session_evaluations", "sandbox_strategy_decisions",
	"sandbox_runtime_submission_plans", "sandbox_runtime_plan_eligibility", "sandbox_runtime_plan_entry_safety",
	"sandbox_runtime_plan_account_snapshots", "sandbox_runtime_strategy_plan_decisions",
	"sandbox_runtime_sandbox_reservations", "sandbox_runtime_submission_outbox",
	"sandbox_runtime_daily_cap_counters", "sandbox_runtime_engine_commands", "sandbox_runtime_canary_evidence",
	"sandbox_runtime_engine_market_observations",
	"owner_console_export_artifacts", "owner_console_artifact_holds", "owner_console_artifact_access_events",
	"owner_console_qualification_runs",
	"owner_console_incident_events", "owner_console_incident_replay_inputs", "owner_console_incident_alert_links",
	"owner_console_incident_activity_links", "owner_console_incident_resolution_evidence",
	"owner_console_report_schedules", "owner_console_reports", "owner_console_alert_delivery_attempts",
	"owner_console_alert_escalations", "owner_console_alert_route_tests",
	"allocation_candidates", "allocation_reservations", "allocation_score_components",
	"command_requests", "configuration_activations", "configuration_versions", "consumer_cursors",
	"data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "decision_inputs", "decisions",
	"cross_market_view_headers", "cross_market_view_members", "dataset_exchange_coverage", "dataset_tier_a_members", "mean_reversion_decisions",
	"shadow_strategy_decision_evidence",
	"shadow_multileg_execution_evidence",
	"shadow_cross_exchange_inventory_initializations",
	"shadow_session_market_scopes",
	"shadow_session_activity_observations", "shadow_session_input_health_observations",
	"triangular_candidates", "triangular_candidate_legs", "triangular_simulation_outcomes",
	"triangular_opportunity_lifetimes", "triangular_journal_links",
	"cross_exchange_candidates", "cross_exchange_candidate_members",
	"cross_exchange_candidate_legs", "cross_exchange_inventory_snapshots",
	"cross_exchange_simulation_outcomes", "cross_exchange_simulation_legs",
	"cross_exchange_rebalancing_needs", "cross_exchange_journal_links",
	"rebalancing_fact_sets", "rebalancing_route_facts", "rebalancing_recommendations",
	"rebalancing_recommendation_steps", "rebalancing_checklist_steps",
	"research_promotion_experiment_preregistrations", "research_promotion_validation_suites", "research_promotion_champion_challenger_reports",
	"multi_exchange_console_replay_fault_schedule_states", "multi_exchange_console_replay_fault_schedules", "multi_exchange_console_report_exports",
	"exchange_capabilities", "exchanges", "execution_lease_epochs", "execution_leases", "execution_plan_legs",
	"execution_plans", "experiment_registrations", "fills", "inbox_events", "incidents", "instrument_metadata_versions",
	"instruments", "jobs", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"circuit_breaker_events", "fill_journal_postings", "liquidity_domains", "liquidity_reservations", "model_namespaces", "opportunities", "order_attempts", "order_events", "order_reduction_incidents", "orders", "outbox_events", "portfolio_ownership", "portfolios", "positions",
	"projection_revisions", "quarantined_scopes", "reconciliation_cases", "reconciliation_differences", "reconciliation_suspense", "recovery_attempts", "reservations",
	"public_clock_samples", "public_connection_events",
	"risk_evaluation_policies", "risk_evaluations", "risk_policies", "risk_policy_limits", "risk_state_events", "run_checkpoints", "run_results", "runs", "sessions", "startup_recovery_attempts", "startup_recovery_evidence", "strategy_definitions", "strategy_parameters",
	"experiment_final_test_consumptions", "research_generations", "research_reports", "run_canonical_outputs", "run_manifests", "shadow_sessions", "strategy_portfolios", "strategy_versions", "stream_connections", "trend_decisions", "users", "virtual_accounts", "virtual_balances",
}

var runtimeUpdateTables = []string{
	"alert_deliveries", "alerts", "allocation_candidates", "assets", "command_requests", "consumer_cursors", "dataset_manifests", "execution_lease_epochs",
	"execution_leases", "incidents", "jobs", "market_data_segments", "model_versions", "orders", "outbox_events",
	"liquidity_domains", "liquidity_reservations", "positions", "projection_revisions", "quarantined_scopes", "reconciliation_cases", "reservations", "runs", "sessions", "startup_recovery_attempts",
	"api_entity_revisions", "shadow_sessions", "stream_connections", "users", "virtual_balances",
	"multi_exchange_console_replay_fault_schedule_states",
	"evaluation_campaigns", "evaluation_campaign_stages", "evaluation_campaign_members",
	"evaluation_campaign_commands", "evaluation_data_audits", "evaluation_historical_imports",
	"evaluation_recorder_requests", "evaluation_shadow_sessions", "evaluation_shadow_member_checkpoints",
	"sandbox_runtime_sandbox_authorizations", "sandbox_runtime_totp_replay_state", "sandbox_runtime_sandbox_sessions", "sandbox_runtime_sandbox_arms",
	"sandbox_strategy_sessions",
	"sandbox_runtime_exchange_accounts", "sandbox_runtime_daily_cap_counters", "sandbox_runtime_engine_commands",
	"owner_console_strategy_controls", "owner_console_risk_controls", "owner_console_export_artifacts",
	"owner_console_artifact_holds", "owner_console_qualification_runs", "owner_console_report_schedules",
	"owner_console_reports", "owner_console_alert_routes", "owner_console_alert_route_tests",
}

var runtimeDeleteTables = []string{"execution_leases", "sessions"}

var runtimeReadTables = []string{
	"schema_migrations", "triangular_arbitrage_claim_resources", "triangular_arbitrage_claim_groups", "triangular_arbitrage_claim_items",
	"cross_exchange_arbitrage_claim_resources", "cross_exchange_arbitrage_claim_groups", "cross_exchange_arbitrage_claim_items",
	"strategy_maturity_states", "strategy_maturity_commands", "strategy_maturity_events",
	"sandbox_runtime_exchange_accounts", "sandbox_runtime_account_epochs", "sandbox_runtime_credential_generations",
	"sandbox_runtime_authenticated_request_evidence",
	"sandbox_runtime_account_snapshots", "sandbox_runtime_private_inbox", "sandbox_runtime_exchange_fills",
	"sandbox_runtime_plan_account_snapshots",
	"sandbox_runtime_strategy_plan_decisions",
	"sandbox_runtime_exchange_metadata", "sandbox_runtime_reconciliation_differences",
	"sandbox_runtime_reconciliations", "sandbox_runtime_reset_incidents", "sandbox_runtime_external_adjustments",
	"sandbox_runtime_risk_unlocks", "sandbox_runtime_account_leases", "sandbox_runtime_engine_startup_evidence",
	"sandbox_runtime_engine_observations", "sandbox_runtime_engine_market_observations",
	"sandbox_runtime_engine_runtime_events", "sandbox_qualification_order_observations",
	"sandbox_strategy_sessions", "sandbox_strategy_session_accounts",
	"sandbox_strategy_session_evaluations", "sandbox_strategy_decisions",
	"sandbox_strategy_risk_observations",
	"sandbox_strategy_risk_valuations",
	"sandbox_accounting_transactions", "sandbox_accounting_entries",
	"sandbox_accounting_positions", "sandbox_accounting_position_fees",
	"sandbox_qualification_runs", "sandbox_qualification_accounts",
	"sandbox_qualification_samples", "sandbox_qualification_failures",
	"sandbox_qualification_chaos_events", "sandbox_qualification_recovery_events",
	"owner_console_reason_catalogue", "owner_console_activity_projection", "owner_console_activity_explanations",
	"owner_console_strategy_controls", "owner_console_risk_controls", "owner_console_qualification_catalogue",
	"owner_console_alert_routes", "owner_console_audit_chain", "owner_console_storage_pressure_state",
}

var recorderReadTables = []string{
	"assets", "configuration_versions", "exchanges", "instruments", "instrument_metadata_versions",
	"evaluation_campaigns", "evaluation_recorder_requests",
}

var recorderWriteTables = []string{
	"alert_deliveries", "alerts", "data_quality_events", "dataset_gaps", "dataset_manifests", "dataset_segments", "market_data_segments",
	"owner_console_storage_pressure_state",
}

var recorderAppendTables = []string{"audit_events", "dataset_exchange_coverage", "dataset_tier_a_members", "instrument_metadata_versions", "public_clock_samples", "public_connection_events", "owner_console_storage_pressure_events",
	"evaluation_campaign_recording_segments", "evaluation_recorder_observations", "evaluation_recorder_instrument_observations"}

// Shared alert services run in every process role. They may append immutable
// delivery evidence and update only the bounded route state used for delivery
// tests and validated webhook availability.
var processAlertAppendTables = []string{"owner_console_alert_delivery_attempts"}

var processAlertUpdateTables = []string{"owner_console_alert_routes", "owner_console_alert_route_tests"}

var sandboxRuntimeEngineReadWriteTables = []string{
	"sandbox_runtime_exchange_accounts", "sandbox_runtime_account_epochs", "sandbox_runtime_credential_generations",
	"sandbox_runtime_credential_rotations",
	"sandbox_runtime_sandbox_sessions", "sandbox_runtime_sandbox_session_accounts", "sandbox_runtime_sandbox_arms",
	"sandbox_runtime_authenticated_request_evidence",
	"sandbox_runtime_account_snapshots", "sandbox_runtime_daily_cap_counters", "sandbox_runtime_submission_plans",
	"sandbox_runtime_plan_eligibility", "sandbox_runtime_plan_entry_safety", "sandbox_runtime_plan_account_snapshots", "sandbox_runtime_strategy_plan_decisions", "sandbox_runtime_sandbox_reservations",
	"sandbox_runtime_submission_outbox", "sandbox_runtime_private_inbox",
	"sandbox_runtime_exchange_fills", "sandbox_runtime_exchange_metadata", "sandbox_runtime_reconciliation_differences",
	"sandbox_runtime_reconciliations", "sandbox_runtime_reset_incidents", "sandbox_runtime_external_adjustments",
	"sandbox_runtime_risk_unlocks", "sandbox_runtime_account_leases",
	"sandbox_runtime_engine_startup_evidence",
	"sandbox_runtime_engine_commands", "sandbox_runtime_engine_observations", "sandbox_runtime_engine_market_observations",
	"sandbox_accounting_positions", "sandbox_accounting_position_fees",
}

var sandboxRuntimeEngineReadOnlyTables = []string{
	"sessions", "users", "configuration_versions", "asset_screening_versions",
	"risk_policies", "risk_policy_limits", "owner_console_storage_pressure_state",
	"sandbox_strategy_sessions", "sandbox_strategy_session_accounts",
}

var sandboxRuntimeEngineRiskStateAppendTables = []string{"risk_state_events"}

var sandboxRuntimeEngineRiskStateUpdateTables = []string{"api_entity_revisions"}

var sandboxRuntimeEngineAlertReadWriteTables = []string{
	"alert_deliveries", "alerts",
}

var sandboxRuntimeEngineAlertAppendTables = []string{
	"audit_events",
}

var sandboxRuntimeEngineRuntimeAppendTables = []string{
	"sandbox_runtime_engine_runtime_events",
	"sandbox_strategy_session_evaluations", "sandbox_strategy_decisions",
	"sandbox_strategy_risk_observations",
	"sandbox_strategy_risk_valuations",
	"sandbox_accounting_transactions", "sandbox_accounting_entries",
}

var sandboxQualificationReadTables = []string{
	"alerts", "incidents", "sandbox_runtime_authenticated_request_evidence",
	"sandbox_runtime_daily_cap_counters", "sandbox_runtime_engine_observations", "sandbox_runtime_engine_market_observations",
	"sandbox_runtime_engine_runtime_events", "sandbox_qualification_order_observations",
	"sandbox_runtime_exchange_accounts", "sandbox_runtime_account_leases",
	"sandbox_runtime_reconciliation_differences",
	"sandbox_qualification_recovery_events",
}

var sandboxQualificationAppendTables = []string{
	"sandbox_qualification_runs", "sandbox_qualification_accounts",
	"sandbox_qualification_samples", "sandbox_qualification_failures",
	"sandbox_qualification_chaos_events", "sandbox_qualification_recovery_events",
}

var readOnlyTables = []string{
	"account_snapshots", "alert_acknowledgements", "alert_deliveries", "alerts", "allocation_candidates", "allocation_reservations", "allocation_score_components", "asset_screening_versions", "assets", "audit_events",
	"configuration_activations", "configuration_versions", "consumer_cursors", "data_quality_events",
	"evaluation_campaigns", "evaluation_campaign_stages", "evaluation_campaign_members",
	"evaluation_campaign_events", "evaluation_campaign_reports", "evaluation_campaign_stress_results", "evaluation_data_audits",
	"evaluation_data_audit_findings", "evaluation_historical_imports", "evaluation_historical_import_segments",
	"evaluation_recorder_requests", "evaluation_campaign_datasets", "evaluation_campaign_dataset_members",
	"evaluation_campaign_metadata", "evaluation_campaign_candidate_locks", "evaluation_campaign_recording_segments", "evaluation_recorder_observations",
	"evaluation_recorder_instrument_observations", "evaluation_shadow_sessions",
	"evaluation_shadow_member_checkpoints", "evaluation_shadow_decisions",
	"dataset_gaps", "dataset_manifests", "dataset_segments", "decision_inputs", "decisions", "exchange_capabilities",
	"cross_market_view_headers", "cross_market_view_members", "dataset_exchange_coverage", "dataset_tier_a_members", "mean_reversion_decisions",
	"shadow_strategy_decision_evidence",
	"shadow_multileg_execution_evidence",
	"shadow_cross_exchange_inventory_initializations",
	"shadow_session_market_scopes",
	"shadow_session_activity_observations", "shadow_session_input_health_observations",
	"triangular_candidates", "triangular_candidate_legs", "triangular_simulation_outcomes",
	"triangular_opportunity_lifetimes", "triangular_journal_links",
	"triangular_arbitrage_claim_resources", "triangular_arbitrage_claim_groups", "triangular_arbitrage_claim_items",
	"cross_exchange_candidates", "cross_exchange_candidate_members",
	"cross_exchange_candidate_legs", "cross_exchange_inventory_snapshots",
	"cross_exchange_simulation_outcomes", "cross_exchange_simulation_legs",
	"cross_exchange_rebalancing_needs", "cross_exchange_journal_links",
	"cross_exchange_arbitrage_claim_resources", "cross_exchange_arbitrage_claim_groups", "cross_exchange_arbitrage_claim_items",
	"rebalancing_fact_sets", "rebalancing_route_facts", "rebalancing_recommendations",
	"rebalancing_recommendation_steps", "rebalancing_checklist_steps",
	"research_promotion_experiment_preregistrations", "research_promotion_validation_suites", "research_promotion_champion_challenger_reports",
	"multi_exchange_console_replay_fault_schedule_states", "multi_exchange_console_replay_fault_schedules", "multi_exchange_console_report_exports",
	"strategy_maturity_states", "strategy_maturity_commands", "strategy_maturity_events",
	"sandbox_runtime_exchange_accounts", "sandbox_runtime_account_epochs", "sandbox_runtime_credential_generations",
	"sandbox_runtime_sandbox_sessions", "sandbox_runtime_sandbox_session_accounts", "sandbox_runtime_sandbox_arms",
	"sandbox_strategy_sessions", "sandbox_strategy_session_accounts",
	"sandbox_strategy_session_evaluations", "sandbox_strategy_decisions",
	"sandbox_strategy_risk_observations",
	"sandbox_strategy_risk_valuations",
	"sandbox_accounting_transactions", "sandbox_accounting_entries",
	"sandbox_accounting_positions", "sandbox_accounting_position_fees",
	"sandbox_runtime_submission_plans", "sandbox_runtime_plan_entry_safety",
	"sandbox_runtime_plan_account_snapshots",
	"sandbox_runtime_strategy_plan_decisions",
	"sandbox_runtime_sandbox_reservations", "sandbox_runtime_submission_outbox", "sandbox_runtime_private_inbox",
	"sandbox_runtime_exchange_fills", "sandbox_runtime_reconciliations",
	"sandbox_runtime_reconciliation_differences", "sandbox_runtime_reset_incidents",
	"sandbox_runtime_external_adjustments", "sandbox_runtime_engine_startup_evidence",
	"sandbox_runtime_engine_commands", "sandbox_runtime_engine_observations", "sandbox_runtime_engine_market_observations",
	"sandbox_runtime_engine_runtime_events", "sandbox_qualification_order_observations",
	"sandbox_qualification_runs", "sandbox_qualification_accounts",
	"sandbox_qualification_samples", "sandbox_qualification_failures",
	"sandbox_qualification_chaos_events", "sandbox_qualification_recovery_events",
	"circuit_breaker_events", "exchanges", "execution_plan_legs", "execution_plans", "fill_journal_postings", "fills", "incidents", "instrument_metadata_versions",
	"instruments", "journal_transactions", "ledger_entries", "market_data_segments", "model_versions",
	"public_clock_samples", "public_connection_events",
	"liquidity_domains", "liquidity_reservations", "model_namespaces", "opportunities", "order_attempts", "order_events", "order_reduction_incidents", "orders", "portfolio_ownership", "portfolios", "positions",
	"projection_revisions", "quarantined_scopes", "reconciliation_cases", "reconciliation_differences", "reconciliation_suspense", "reservations", "risk_evaluation_policies", "risk_evaluations", "risk_policies", "risk_policy_limits", "risk_state_events",
	"experiment_final_test_consumptions", "research_generations", "research_reports", "run_canonical_outputs", "run_checkpoints", "run_manifests", "run_results", "runs", "shadow_sessions", "startup_recovery_attempts", "startup_recovery_evidence", "strategy_definitions", "strategy_parameters", "strategy_portfolios",
	"strategy_versions", "trend_decisions", "virtual_accounts", "virtual_balances",
	"owner_console_reason_catalogue", "owner_console_activity_projection", "owner_console_activity_explanations",
	"owner_console_strategy_controls", "owner_console_risk_controls", "owner_console_export_artifacts",
	"owner_console_artifact_holds", "owner_console_artifact_access_events", "owner_console_qualification_catalogue",
	"owner_console_qualification_runs", "owner_console_role_change_events",
	"owner_console_incident_events", "owner_console_incident_replay_inputs", "owner_console_incident_alert_links",
	"owner_console_incident_activity_links", "owner_console_incident_resolution_evidence",
	"owner_console_report_schedules", "owner_console_reports", "owner_console_alert_routes",
	"owner_console_alert_delivery_attempts", "owner_console_alert_escalations", "owner_console_alert_route_tests",
	"owner_console_audit_chain", "owner_console_storage_pressure_state", "owner_console_storage_pressure_events",
}
