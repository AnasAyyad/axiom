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
    "JobResource": {
      "created_at": components["schemas"]["Timestamp"];
      "cursor_ordinal"?: components["schemas"]["Revision"];
      "failure_code"?: string;
      "id": string;
      "kind": "backtest" | "replay";
      "mode_label": "BACKTEST" | "REPLAY";
      "progress"?: components["schemas"]["NonnegativeDecimal"];
      "result"?: components["schemas"]["JobResult"];
      "revision": components["schemas"]["Revision"];
      "state": "QUEUED" | "RUNNING" | "PAUSE_REQUESTED" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "SUCCEEDED" | "FAILED";
      "updated_at"?: components["schemas"]["Timestamp"];
    };
    "JobResult": {
      "metrics"?: Record<string, components["schemas"]["Decimal"]>;
      "platform_correctness": string;
      "reproducibility": string;
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
    "NonnegativeDecimal": string;
    "OfflineJobRequest": {
      "configuration_id": string;
      "dataset_id": string;
      "root_seed_hash": string;
      "strategy_version": "trend.v1a.1";
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
    "ReplayJobRequest": components["schemas"]["OfflineJobRequest"] & {
      "first_ordinal"?: components["schemas"]["Revision"];
      "incident_id"?: string;
      "last_ordinal"?: components["schemas"]["Revision"];
      "speed"?: "original" | "accelerated" | "maximum";
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
      "created_at": components["schemas"]["Timestamp"];
      "entries_enabled": boolean;
      "failure_code"?: string;
      "id": string;
      "label": "PUBLIC-LIVE SHADOW / VIRTUAL";
      "orders"?: Array<components["schemas"]["SimulatedOrder"]>;
      "public_only": true;
      "revision": components["schemas"]["Revision"];
      "risk_state"?: "PAUSED" | "RESUMED" | "LOCKED";
      "simulation_only": true;
      "started_at"?: components["schemas"]["Timestamp"];
      "state": "QUEUED" | "RUNNING" | "PAUSED" | "CANCEL_REQUESTED" | "CANCELED" | "FAILED";
      "stopped_at"?: components["schemas"]["Timestamp"];
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
      "stream": "system" | "exchange" | "portfolio" | "risk" | "trend" | "job" | "shadow" | "incident" | "alert" | "order" | "fill";
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
      "phase": "A1" | "A11";
      "real_trading_enabled": false;
      "release": "V1A";
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
  "listPortfolios": { query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["PortfolioPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getPortfolio": { path: { "id": string; }; responses: { "200": components["schemas"]["PortfolioDetail"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listPortfolioJournal": { path: { "id": string; }; query: { "cursor"?: string; "page_size"?: number; }; responses: { "200": components["schemas"]["JournalPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "getRiskStatus": { responses: { "200": components["schemas"]["RiskStatus"]; "401": components["schemas"]["Error"]; }; };
  "pauseRisk": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "resumeRisk": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getTrendStrategy": { responses: { "200": components["schemas"]["TrendStatus"]; "401": components["schemas"]["Error"]; }; };
  "listTrendDecisions": { query: { "cursor"?: string; "page_size"?: number; "outcome"?: "accepted" | "rejected"; }; responses: { "200": components["schemas"]["TrendDecisionPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "createBacktest": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["OfflineJobRequest"]; responses: { "202": components["schemas"]["JobResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "getBacktest": { path: { "id": string; }; responses: { "200": components["schemas"]["JobResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "createReplay": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ReplayJobRequest"]; responses: { "202": components["schemas"]["JobResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "429": components["schemas"]["Error"]; }; };
  "getReplay": { path: { "id": string; }; responses: { "200": components["schemas"]["JobResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "pauseReplay": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "resumeReplay": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "stepReplay": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "createShadowSession": { header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["ShadowSessionRequest"]; responses: { "202": components["schemas"]["ShadowSessionResource"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "503": components["schemas"]["Error"]; }; };
  "stopShadowSession": { path: { "id": string; }; header: { "Origin": string; "X-CSRF-Token": string; "Idempotency-Key": string; }; requestBody: components["schemas"]["RevisionCommandRequest"]; responses: { "202": components["schemas"]["CommandAccepted"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "412": components["schemas"]["Error"]; }; };
  "getShadowSession": { path: { "id": string; }; responses: { "200": components["schemas"]["ShadowSessionResource"]; "401": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listIncidents": { query: { "cursor"?: string; "page_size"?: number; "state"?: "open" | "acknowledged" | "resolved"; }; responses: { "200": components["schemas"]["IncidentPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; }; };
  "getIncident": { path: { "id": string; }; query: { "include_raw"?: boolean; }; responses: { "200": components["schemas"]["IncidentDetail"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "404": components["schemas"]["Error"]; }; };
  "listAuditEvents": { query: { "cursor"?: string; "page_size"?: number; "event_type"?: string; "include_detail"?: boolean; }; responses: { "200": components["schemas"]["AuditEventPage"]; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; }; };
  "streamEvents": { query: { "after_revision"?: components["schemas"]["Revision"]; }; header: { "Origin": string; "Last-Event-ID"?: components["schemas"]["Revision"]; }; responses: { "200": string; "400": components["schemas"]["Error"]; "401": components["schemas"]["Error"]; "403": components["schemas"]["Error"]; "409": components["schemas"]["Error"]; "410": components["schemas"]["Error"]; }; };
}
