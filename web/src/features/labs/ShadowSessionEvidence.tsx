import type { APIModel } from "../../api/client";
import { ConfirmAction } from "../../components/ConfirmAction";
import { DataTable } from "../../components/DataTable";
import { MetricCard } from "../../components/MetricCard";
import { StatePanel } from "../../components/StatePanel";
import styles from "../../app/Page.module.css";

interface ShadowSessionEvidenceProps {
  readonly session: APIModel<"ShadowSessionResource">;
  readonly canControl: boolean;
  readonly stopPending: boolean;
  readonly onStop: () => void;
}

export function ShadowSessionEvidence({
  session,
  canControl,
  stopPending,
  onStop,
}: ShadowSessionEvidenceProps) {
  return (
    <>
      <div className={styles.metrics}>
        <MetricCard label="State" value={session.state} />
        <MetricCard
          label="Entries enabled"
          value={session.entries_enabled ? "yes" : "no"}
        />
        <MetricCard
          label="Public only"
          value={session.public_only ? "yes" : "no"}
          tone="good"
        />
        <MetricCard
          label="Simulation only"
          value={session.simulation_only ? "yes" : "no"}
          tone="good"
        />
        <MetricCard
          label="Decision grading"
          value={`${session.accepted_decisions} accepted · ${session.rejected_decisions} rejected`}
        />
        <MetricCard
          label="Journal impact"
          value={`${session.journal_transactions} transactions`}
        />
      </div>
      <section className={styles.card}>
        <h2>Immutable assumptions</h2>
        <dl className={styles.facts}>
          <div>
            <dt>Configuration</dt>
            <dd>{session.configuration_id}</dd>
          </div>
          <div>
            <dt>Strategy</dt>
            <dd>{session.strategy_version}</dd>
          </div>
          <div>
            <dt>Model namespace</dt>
            <dd>{session.model_namespace_id || "Pending claim"}</dd>
          </div>
          <div>
            <dt>Decision dataset</dt>
            <dd>{session.decision_dataset_id || "Pending flush"}</dd>
          </div>
          <div>
            <dt>Portfolio</dt>
            <dd>{session.portfolio_id ?? "Pending runtime"}</dd>
          </div>
          <div>
            <dt>Run identity</dt>
            <dd>{session.run_id ?? "Pending materialization"}</dd>
          </div>
          <div>
            <dt>Public exchange</dt>
            <dd>{session.exchange_id ?? "Production-public feed"}</dd>
          </div>
          <div>
            <dt>Slippage model</dt>
            <dd>{session.slippage_model_id ?? "Pending claim"}</dd>
          </div>
          <div>
            <dt>Gap model</dt>
            <dd>{session.gap_model_id ?? "Pending claim"}</dd>
          </div>
        </dl>
      </section>
      <ConfirmAction
        trigger={
          <button
            className={styles.actionDanger}
            disabled={
              !canControl ||
              stopPending ||
              session.state === "CANCELED" ||
              session.state === "FAILED"
            }
          >
            Stop shadow session
          </button>
        }
        title="Stop this virtual shadow session?"
        description="New entries remain disabled and the engine performs a durable graceful stop."
        confirmLabel="Stop session"
        onConfirm={onStop}
      />
      {session.data_health ? (
        <section className={styles.card}>
          <h2>Public-data health</h2>
          <dl className={styles.facts}>
            <div>
              <dt>Exchange</dt>
              <dd>{session.data_health.exchange}</dd>
            </div>
            <div>
              <dt>Connection state</dt>
              <dd>{session.data_health.state}</dd>
            </div>
            <div>
              <dt>Fresh</dt>
              <dd>{session.data_health.fresh ? "yes" : "no"}</dd>
            </div>
            <div>
              <dt>Reason</dt>
              <dd>{session.data_health.reason}</dd>
            </div>
            <div>
              <dt>Observed</dt>
              <dd>{session.data_health.observed_at}</dd>
            </div>
          </dl>
        </section>
      ) : (
        <StatePanel
          state="partial"
          detail="No public connection sample is linked to this session yet."
        />
      )}
      {session.pnl_attribution && (
        <section className={styles.card}>
          <h2>Sealed-ledger P&amp;L attribution</h2>
          <dl className={styles.facts}>
            {Object.entries(session.pnl_attribution).map(([key, value]) => (
              <div key={key}>
                <dt>{key.replaceAll("_", " ")}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
        </section>
      )}
      {session.decisions?.length ? (
        <DataTable
          caption="Recent decisions and risk actions"
          rows={session.decisions.map((decision) => ({ ...decision }))}
          columns={[
            { key: "outcome", label: "Decision" },
            { key: "reason_code", label: "Decision reason" },
            { key: "risk_outcome", label: "Risk action" },
            { key: "risk_reason_code", label: "Risk reason" },
            { key: "occurred_at", label: "Occurred" },
          ]}
        />
      ) : (
        <StatePanel
          state="empty"
          detail="No decisions are materialized for this shadow session."
        />
      )}
      {session.balances?.length ? (
        <DataTable
          caption="Virtual balances"
          rows={session.balances.map((balance) => ({
            id: balance.asset,
            ...balance,
          }))}
          columns={[
            { key: "asset", label: "Asset" },
            { key: "available", label: "Available" },
            { key: "reserved", label: "Reserved" },
            { key: "revision", label: "Revision" },
          ]}
        />
      ) : (
        <StatePanel
          state="empty"
          detail="No virtual balances are materialized yet."
        />
      )}
      {session.positions?.length ? (
        <DataTable
          caption="Owned virtual inventory"
          rows={session.positions.map((position) => ({
            id: position.instrument,
            ...position,
          }))}
          columns={[
            { key: "instrument", label: "Instrument" },
            { key: "quantity", label: "Owned quantity" },
            { key: "weighted_average_cost", label: "Average cost" },
            { key: "realized_pnl", label: "Realized P&L" },
          ]}
        />
      ) : (
        <StatePanel
          state="empty"
          detail="No owned virtual positions are open."
        />
      )}
      {session.orders?.length ? (
        <DataTable
          caption="Simulated orders and fills"
          rows={session.orders.map((order) => ({ ...order }))}
          columns={[
            { key: "instrument", label: "Instrument" },
            { key: "side", label: "Side" },
            { key: "quantity", label: "Quantity" },
            { key: "filled_quantity", label: "Filled" },
            { key: "state", label: "State" },
            { key: "latency_ms", label: "Latency ms" },
          ]}
        />
      ) : (
        <StatePanel
          state="empty"
          detail="No simulated orders in this session."
        />
      )}
    </>
  );
}
