import { useQuery } from "@tanstack/react-query";

import { getAPI } from "../api/client";
import { binanceQuery } from "../api/queries";
import { DataTable } from "../components/DataTable";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import { Page } from "./OperationalShared";
import styles from "./Page.module.css";

export { CommandCenterPage as CommandCenter } from "../features/command-center/CommandCenterPage";

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
