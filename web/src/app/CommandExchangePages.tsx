import { useQuery } from "@tanstack/react-query";

import { getAPI } from "../api/client";
import {
  binanceQuery,
  portfolioQuery,
  riskQuery,
  systemQuery,
} from "../api/queries";
import { DataTable } from "../components/DataTable";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import { Facts, Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function CommandCenter() {
  const system = useQuery(systemQuery);
  const binance = useQuery(binanceQuery);
  const portfolios = useQuery(portfolioQuery);
  const risk = useQuery(riskQuery);
  if ([system, binance, portfolios, risk].some((query) => query.isLoading))
    return <StatePanel state="loading" />;
  if (system.isError || binance.isError || portfolios.isError || risk.isError)
    return (
      <StatePanel
        state="degraded"
        detail="Cached values are hidden until an authoritative snapshot is available."
      />
    );
  const systemData = system.data!;
  const binanceData = binance.data!;
  const riskData = risk.data!;
  const portfolio = portfolios.data!.items[0];
  return (
    <Page
      title="Command Center"
      eyebrow="Live research operations"
      description="One fail-closed view of production-public data and virtual execution."
    >
      <div className={styles.metrics}>
        <MetricCard
          label="Execution mode"
          value={systemData.execution_mode ?? "shadow"}
          detail="VIRTUAL"
        />
        <MetricCard
          label="Risk state"
          value={riskData.state}
          tone={riskData.state === "NORMAL" ? "good" : "warn"}
        />
        <MetricCard
          label="Binance public feed"
          value={binanceData.websocket_state}
          tone={binanceData.websocket_state === "healthy" ? "good" : "warn"}
        />
        <MetricCard
          label="Virtual equity"
          value={portfolio?.equity ?? "—"}
          detail={
            portfolio
              ? `${portfolio.mode.toUpperCase()} · ${portfolio.label}`
              : "No portfolio"
          }
        />
      </div>
      <div className={styles.grid}>
        <Facts
          title="Safety posture"
          values={{
            "Real trading": "DISABLED",
            Lifecycle: systemData.lifecycle_state,
            "Strategy activation": systemData.strategy_activation,
            "Critical incidents": String(systemData.critical_incidents ?? 0),
          }}
        />
        <Facts
          title="Active research"
          values={{
            "Active resource": systemData.active_resource_id ?? "None",
            Engine: systemData.engine_state ?? "Not running",
            Revision: systemData.revision ?? "—",
            "Server time": systemData.server_time ?? "—",
          }}
        />
      </div>
    </Page>
  );
}

export function BinancePage() {
  const health = useQuery(binanceQuery);
  const instruments = useQuery({
    queryKey: ["instruments"],
    queryFn: () =>
      getAPI<"InstrumentPage">(
        "/api/v1/exchanges/binance/instruments?page_size=50",
      ),
  });
  if (health.isLoading || instruments.isLoading)
    return <StatePanel state="loading" />;
  if (health.isError || instruments.isError)
    return <StatePanel state="degraded" />;
  const healthData = health.data!;
  const instrumentData = instruments.data!;
  return (
    <Page
      title="Binance Connection"
      eyebrow="Production-public only"
      description="Public metadata, books, trades, candles, and recorder evidence. Private routes and credentials are absent."
    >
      {healthData.book_state !== "healthy" && (
        <StatePanel
          state={healthData.book_state === "stale" ? "stale" : "degraded"}
        />
      )}
      <div className={styles.metrics}>
        <MetricCard
          label="WebSocket"
          value={healthData.websocket_state}
          tone={healthData.websocket_state === "healthy" ? "good" : "warn"}
        />
        <MetricCard label="Book state" value={healthData.book_state} />
        <MetricCard label="Recorder" value={healthData.recorder_state} />
        <MetricCard
          label="Clock drift ms"
          value={healthData.clock_drift_ms ?? "unavailable"}
        />
      </div>
      <ul className={styles.tagList}>
        {healthData.capabilities?.map((capability) => (
          <li key={capability}>{capability}</li>
        ))}
      </ul>
      {instrumentData.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <DataTable
          caption="Spot instrument metadata"
          rows={instrumentData.items.map((item) => ({ ...item }))}
          columns={[
            { key: "symbol", label: "Symbol" },
            { key: "price_tick", label: "Price tick" },
            { key: "quantity_step", label: "Quantity step" },
            { key: "minimum_notional", label: "Minimum notional" },
            { key: "metadata_version", label: "Revision" },
          ]}
        />
      )}
    </Page>
  );
}
