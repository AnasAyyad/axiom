import type { APIModel } from "../../api/client";
import { DataTable } from "../../components/DataTable";
import { StatePanel } from "../../components/StatePanel";
import styles from "../../app/Page.module.css";

interface ShadowSessionResultsProps {
  readonly session: APIModel<"ShadowSessionResource">;
}

export function ShadowSessionResults({ session }: ShadowSessionResultsProps) {
  return (
    <>
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
          detail={`No decisions are materialized yet. ${session.waiting_reason}`}
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
