import type { APIModel } from "../api/client";
import { StatusBadge } from "../features/shared/StatusBadge";
import styles from "./Shell.module.css";

interface SafetyHeaderProps {
  readonly system?: APIModel<"SystemStatus"> | undefined;
  readonly binance?: APIModel<"BinanceHealth"> | undefined;
  readonly exchanges?: APIModel<"ExchangePage"> | undefined;
  readonly risk?: APIModel<"RiskStatus"> | undefined;
  readonly criticalAlerts?: number | undefined;
  readonly streamState: "live" | "reconnecting";
}

export function SafetyHeader({
  system,
  binance,
  exchanges,
  risk,
  criticalAlerts = 0,
  streamState,
}: SafetyHeaderProps) {
  const mode = system?.execution_mode ?? "shadow";
  const publicDataState = (exchangeID: string) =>
    exchanges?.items.find((exchange) => exchange.id === exchangeID)
      ?.websocket_state ?? "unavailable";
  const binancePublicData = publicDataState("binance");
  const bybitPublicData = publicDataState("bybit");
  const freshness =
    binance?.websocket_state === "healthy" &&
    binance.book_state === "healthy" &&
    binance.recorder_state === "healthy" &&
    binancePublicData === "healthy" &&
    bybitPublicData === "healthy"
      ? "fresh"
      : "attention required";
  return (
    <header
      className={styles.safetyHeader}
      aria-label="Persistent safety status"
      aria-live="polite"
    >
      <div className={styles.lockLabel}>
        <strong>REAL-MONEY TRADING IS NOT AVAILABLE</strong>
        <span>{mode.toUpperCase()} · VIRTUAL</span>
      </div>
      <dl tabIndex={0} aria-label="Safety status details">
        <div>
          <dt>Environment</dt>
          <dd>{system?.environment ?? "production_public"}</dd>
        </div>
        <div>
          <dt>Mode</dt>
          <dd>
            <StatusBadge value={mode} />
          </dd>
        </div>
        <div>
          <dt>Binance data</dt>
          <dd>
            <StatusBadge value={binancePublicData} />
          </dd>
        </div>
        <div>
          <dt>Bybit data</dt>
          <dd>
            <StatusBadge value={bybitPublicData} />
          </dd>
        </div>
        <div>
          <dt>Engine</dt>
          <dd>
            <StatusBadge
              value={
                system?.engine_state ?? system?.lifecycle_state ?? "unavailable"
              }
            />
          </dd>
        </div>
        <div>
          <dt>Risk</dt>
          <dd>
            <StatusBadge value={risk?.state ?? "unavailable"} />
          </dd>
        </div>
        <div>
          <dt>Critical alerts</dt>
          <dd>{String(criticalAlerts)}</dd>
        </div>
        <div>
          <dt>Data</dt>
          <dd>
            <StatusBadge value={freshness} />
          </dd>
        </div>
        <div>
          <dt>Updates</dt>
          <dd>
            <StatusBadge value={streamState} />
          </dd>
        </div>
      </dl>
    </header>
  );
}
