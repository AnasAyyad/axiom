import { useQuery } from "@tanstack/react-query";

import {
  activityQuery,
  binanceQuery,
  exchangesQuery,
  portfolioQuery,
  riskQuery,
  strategiesQuery,
  systemQuery,
} from "../../api/queries";
import { Facts, Page } from "../../app/OperationalShared";
import pageStyles from "../../app/Page.module.css";
import { DataTable } from "../../components/DataTable";
import { MetricCard } from "../../components/MetricCard";
import { StatePanel } from "../../components/StatePanel";
import { emptyActivityFilters } from "../activity/activityModel";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "../shared/ConsoleSurface.module.css";

export function CommandCenterPage() {
  const system = useQuery(systemQuery);
  const binance = useQuery(binanceQuery);
  const portfolios = useQuery(portfolioQuery);
  const risk = useQuery(riskQuery);
  const exchanges = useQuery(exchangesQuery);
  const strategies = useQuery(strategiesQuery);
  const activity = useQuery(
    activityQuery("decisions_orders", emptyActivityFilters),
  );
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
      <div className={pageStyles.metrics}>
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
      <div className={pageStyles.grid}>
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
      {(exchanges.isError || strategies.isError || activity.isError) && (
        <StatePanel
          state="partial"
          detail="Core safety and portfolio state is current, but one or more monitoring summaries are partial."
        />
      )}
      <section className={styles.twoColumn}>
        <article className={styles.card}>
          <h2>Portfolio impact</h2>
          <dl className={styles.facts}>
            <div>
              <dt>Total virtual equity</dt>
              <dd>{portfolio?.equity ?? "Unavailable"}</dd>
            </div>
            <div>
              <dt>Available capital</dt>
              <dd>{portfolio?.available ?? "Unavailable"}</dd>
            </div>
            <div>
              <dt>Reserved capital</dt>
              <dd>{portfolio?.reserved ?? "Unavailable"}</dd>
            </div>
            <div>
              <dt>P&amp;L attribution</dt>
              <dd>Open Portfolio for authoritative detail</dd>
            </div>
          </dl>
        </article>
        <article className={styles.card}>
          <h2>Strategy readiness</h2>
          {strategies.isLoading ? (
            <StatePanel state="loading" />
          ) : strategies.data?.items.length ? (
            <ul>
              {strategies.data.items.map((strategy) => (
                <li key={strategy.id}>
                  <strong>{strategy.name}</strong> · v{strategy.version} ·{" "}
                  {strategy.maturity.replaceAll("_", " ")} ·{" "}
                  {strategy.evidence_role}
                </li>
              ))}
            </ul>
          ) : (
            <StatePanel
              state="empty"
              detail="No strategy summary is available."
            />
          )}
        </article>
      </section>
      {exchanges.data?.items.length ? (
        <DataTable
          caption="Exchange health and data quality"
          rows={exchanges.data.items.map((exchange) => ({
            id: exchange.id,
            exchange: exchange.name,
            connection: exchange.websocket_state,
            book: exchange.book_state,
            recorder: exchange.recorder_state,
            instruments: exchange.instruments,
            freshness: exchange.quality.freshness,
            confidence: exchange.quality.confidence,
          }))}
          columns={[
            { key: "exchange", label: "Exchange" },
            { key: "connection", label: "Connection" },
            { key: "book", label: "Book" },
            { key: "recorder", label: "Recorder" },
            { key: "instruments", label: "Instruments" },
            { key: "freshness", label: "Freshness" },
            { key: "confidence", label: "Confidence" },
          ]}
        />
      ) : null}
      <article className={styles.card}>
        <div className={styles.cardHeader}>
          <h2>Latest decisions and orders</h2>
          <StatusBadge value={activity.isError ? "partial" : "current"} />
        </div>
        {activity.isLoading ? (
          <StatePanel state="loading" />
        ) : activity.data?.items.length ? (
          <ol>
            {activity.data.items.slice(0, 6).map((item) => (
              <li key={item.id}>
                <strong>{item.reason.summary}</strong> — {item.outcome} ·{" "}
                {item.occurred_at}
              </li>
            ))}
          </ol>
        ) : (
          <StatePanel
            state={activity.isError ? "degraded" : "empty"}
            detail="Open Decisions & Orders for filters and complete correlation evidence."
          />
        )}
      </article>
    </Page>
  );
}
