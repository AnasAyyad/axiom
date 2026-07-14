// Code generated from api/openapi.yaml by scripts/generate-openapi-types.mjs.
// DO NOT EDIT.

export interface components {
  schemas: {
    "BuildInformation": {
      "built_at": string;
      "commit": string;
      "dirty": boolean;
      "go_version": string;
      "version": string;
    };
    "DetailedHealthResponse": {
      "components": Array<components["schemas"]["HealthComponent"]>;
      "lifecycle_state": "STARTING" | "READY_PAUSED" | "STOPPING";
      "real_trading_enabled": false;
      "role": string;
      "status": "ready" | "not_ready";
    };
    "HealthComponent": {
      "name": "postgres";
      "reason_code"?: "required_dependency_unavailable";
      "status": "ready" | "not_ready";
    };
    "HealthResponse": {
      "phase": "A1";
      "reason_code"?: string;
      "role": string;
      "status": "live" | "ready" | "not_ready";
    };
    "SystemStatus": {
      "lifecycle_state": "STARTING" | "READY_PAUSED" | "STOPPING";
      "phase": "A1";
      "real_trading_enabled": false;
      "release": "V1A";
      "role": string;
      "strategy_activation": "unavailable";
    };
    "VersionResponse": {
      "version": string;
    };
  };
}
