import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { portfolioQuery, riskQuery } from "../api/queries";
import { ConfirmAction } from "../components/ConfirmAction";
import { DataTable } from "../components/DataTable";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import { Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function PortfolioPage() {
  const { id: routeID } = useParams();
  const portfolios = useQuery(portfolioQuery);
  const firstID = routeID ?? portfolios.data?.items[0]?.id;
  const detail = useQuery({
    queryKey: ["portfolio", firstID],
    queryFn: () => getAPI<"PortfolioDetail">(`/api/v1/portfolios/${firstID}`),
    enabled: firstID !== undefined,
  });
  const journal = useQuery({
    queryKey: ["journal", firstID],
    queryFn: () =>
      getAPI<"JournalPage">(
        `/api/v1/portfolios/${firstID}/journal?page_size=50`,
      ),
    enabled: firstID !== undefined,
  });
  if (
    portfolios.isLoading ||
    (firstID && (detail.isLoading || journal.isLoading))
  )
    return <StatePanel state="loading" />;
  if (portfolios.isError || detail.isError || journal.isError)
    return <StatePanel state="error" />;
  if (!firstID || !detail.data || !journal.data)
    return (
      <Page
        title="Portfolio"
        eyebrow="Virtual ledger"
        description="Balances and positions are sourced from the immutable virtual journal."
      >
        <StatePanel state="empty" />
      </Page>
    );
  return (
    <Page
      title="Portfolio"
      eyebrow={`${detail.data.mode.toUpperCase()} · ${detail.data.label}`}
      description="Exact server-side decimals; the browser performs no valuation or P&L calculation."
    >
      <div className={styles.metrics}>
        <MetricCard label="Virtual equity" value={detail.data.equity} />
        <MetricCard label="Available" value={detail.data.available} />
        <MetricCard label="Reserved" value={detail.data.reserved} />
        <MetricCard label="Revision" value={detail.data.revision} />
      </div>
      <DataTable
        caption="Virtual balances"
        rows={detail.data.balances.map((item) => ({ id: item.asset, ...item }))}
        columns={[
          { key: "asset", label: "Asset" },
          { key: "available", label: "Available" },
          { key: "reserved", label: "Reserved" },
        ]}
      />
      {journal.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No journal postings for this virtual portfolio."
        />
      ) : (
        <DataTable
          caption="Immutable journal lines"
          rows={journal.data.items.map((item) => ({ ...item }))}
          columns={[
            { key: "occurred_at", label: "UTC time" },
            { key: "asset", label: "Asset" },
            { key: "direction", label: "Direction" },
            { key: "quantity", label: "Quantity" },
            { key: "transaction_id", label: "Transaction" },
          ]}
        />
      )}
    </Page>
  );
}

export function RiskPage() {
  const risk = useQuery(riskQuery);
  const client = useQueryClient();
  const command = useMutation({
    mutationFn: (action: "pause" | "resume") =>
      postAPI<"CommandAccepted">(
        `/api/v1/risk/${action}`,
        {
          expected_revision: risk.data?.revision,
          reason: `owner requested ${action}`,
        },
        newIdempotencyKey(`risk-${action}`),
      ),
    onSuccess: () => void client.invalidateQueries({ queryKey: ["risk"] }),
  });
  if (risk.isLoading) return <StatePanel state="loading" />;
  if (risk.isError)
    return (
      <StatePanel
        state="locked"
        detail="Risk state could not be established."
      />
    );
  const riskData = risk.data!;
  return (
    <Page
      title="Risk Center"
      eyebrow="Policy-gated recovery"
      description="Pause and resume are durable, audited commands. Resume cannot bypass reconciliation, quarantine, or critical incidents."
    >
      {riskData.state !== "NORMAL" && (
        <StatePanel
          state={riskData.state === "LOCKED" ? "locked" : "paused"}
          detail={riskData.reason_codes?.join(", ") ?? ""}
        />
      )}
      <div className={styles.metrics}>
        <MetricCard
          label="Effective state"
          value={riskData.state}
          tone={riskData.state === "NORMAL" ? "good" : "warn"}
        />
        <MetricCard label="Policy version" value={riskData.policy_version} />
        <MetricCard
          label="Recovery ready"
          value={riskData.recovery_ready ? "yes" : "no"}
        />
        <MetricCard
          label="Critical blockers"
          value={String(riskData.unresolved_critical ?? 0)}
        />
      </div>
      <div className={styles.card}>
        <h2>Manual controls</h2>
        <div className={styles.header}>
          <ConfirmAction
            trigger={
              <button className={styles.actionDanger}>Pause risk</button>
            }
            title="Pause all new entries?"
            description="This writes a durable audited command. Existing virtual exposure is not fabricated away."
            confirmLabel="Pause"
            onConfirm={() => command.mutate("pause")}
          />
          <ConfirmAction
            trigger={<button className={styles.action}>Resume risk</button>}
            title="Request policy-gated resume?"
            description="Recent authentication and every recovery prerequisite are checked again by the server."
            confirmLabel="Request resume"
            onConfirm={() => command.mutate("resume")}
          />
        </div>
      </div>
    </Page>
  );
}
