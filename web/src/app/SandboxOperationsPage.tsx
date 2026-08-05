import { useQuery } from "@tanstack/react-query";

import { APIError } from "../api/client";
import {
  sandboxQualificationQuery,
  sandboxOrdersQuery,
  sandboxOverviewQuery,
  sandboxReconciliationsQuery,
} from "../api/queries";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import { Page } from "./OperationalShared";
import { SandboxControls } from "./SandboxControls";
import { SandboxAccountGrid, SandboxOrderLedger } from "./SandboxAccountOrders";
import {
  SandboxQualificationPanel,
  SandboxReconciliationGrid,
  SandboxResetGrid,
} from "./SandboxReconciliationEvidence";
import { SandboxStrategySessions } from "./SandboxStrategySessions";
import styles from "./SandboxOperationsPage.module.css";

export function SandboxOperationsPage() {
  const overview = useQuery(sandboxOverviewQuery);
  const orders = useQuery(sandboxOrdersQuery);
  const reconciliations = useQuery(sandboxReconciliationsQuery);
  const qualification = useQuery(sandboxQualificationQuery);
  if (
    overview.isLoading ||
    orders.isLoading ||
    reconciliations.isLoading ||
    qualification.isLoading
  )
    return <StatePanel state="loading" />;
  const error =
    overview.error ??
    orders.error ??
    reconciliations.error ??
    qualification.error;
  if (error instanceof APIError && error.status === 403)
    return <StatePanel state="forbidden" detail={`Refusal: ${error.code}`} />;
  if (
    overview.isError ||
    orders.isError ||
    reconciliations.isError ||
    qualification.isError ||
    !overview.data ||
    !orders.data ||
    !reconciliations.data ||
    !qualification.data
  )
    return <StatePanel state="error" detail={errorCode(error)} />;
  const data = overview.data;
  const degraded = data.accounts.some(
    (account) => !account.engine_ready || account.state === "DEGRADED",
  );
  return (
    <Page
      title="Sandbox Operations"
      eyebrow="Controlled Testnet and Demo execution"
      description="Authoritative redacted Testnet and Demo state. Every value comes from the durable account, order, reconciliation, and qualification stores."
    >
      <section className={styles.environment} aria-label="Execution boundary">
        <strong>BINANCE SPOT TESTNET</strong>
        <strong>BYBIT DEMO</strong>
        <span>REAL TRADING DISABLED</span>
      </section>
      {data.stale && (
        <StatePanel
          state="stale"
          detail="Entry controls remain fail-closed until fresh engine evidence returns."
        />
      )}
      {!data.stale && degraded && (
        <StatePanel
          state="degraded"
          detail="Cancellation, query, and reconciliation remain available."
        />
      )}
      <div className={styles.metrics}>
        <MetricCard label="Risk state" value={data.risk_state} />
        <MetricCard
          label="Entry-ready engines"
          value={`${data.accounts.filter((item) => item.engine_ready).length}/${data.accounts.length}`}
        />
        <MetricCard
          label="UNKNOWN orders"
          value={String(
            orders.data.items.filter((item) => item.state === "UNKNOWN").length,
          )}
          tone={
            orders.data.items.some((item) => item.state === "UNKNOWN")
              ? "warn"
              : "good"
          }
        />
        <MetricCard
          label="Formal sandbox soak"
          value={
            qualification.data.formal_soak_pending
              ? "PENDING"
              : qualification.data.state
          }
          tone={qualification.data.formal_soak_pending ? "warn" : "good"}
        />
      </div>
      <SandboxControls
        accounts={data.accounts}
        orders={orders.data.items}
        reconciliations={reconciliations.data.items}
      />
      <SandboxStrategySessions sessions={data.strategy_sessions} />
      <SandboxAccountGrid accounts={data.accounts} />
      <SandboxOrderLedger orders={orders.data.items} />
      <SandboxReconciliationGrid reconciliations={reconciliations.data.items} />
      <SandboxResetGrid incidents={reconciliations.data.reset_incidents} />
      <SandboxQualificationPanel qualification={qualification.data} />
    </Page>
  );
}

function errorCode(value: unknown) {
  return value instanceof APIError
    ? `Backend refusal: ${value.code} · ${value.correlationID}`
    : "No unvalidated fallback state is shown.";
}
