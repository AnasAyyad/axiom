import type { APIModel } from "../../api/client";
import { useEffect, useState } from "react";
import { ConfirmAction } from "../../components/ConfirmAction";
import { DataTable } from "../../components/DataTable";
import { MetricCard } from "../../components/MetricCard";
import { StatePanel } from "../../components/StatePanel";
import styles from "../../app/Page.module.css";
import { shadowEvaluationCountdown } from "./shadowActivityModel";
import { ShadowSessionResults } from "./ShadowSessionResults";

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
  const [clock, setClock] = useState(() => Date.now());
  useEffect(() => {
    if (!session.next_evaluation_at) return undefined;
    const timer = window.setInterval(() => setClock(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, [session.next_evaluation_at]);
  const countdown = session.next_evaluation_at
    ? shadowEvaluationCountdown(clock, session.next_evaluation_at)
    : "A time is not available; the trigger condition below remains authoritative.";
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
      <section className={styles.card} aria-live="polite">
        <h2>Why is nothing happening?</h2>
        <dl className={styles.facts}>
          <div>
            <dt>Current activity</dt>
            <dd>{session.activity_state.replaceAll("_", " ")}</dd>
          </div>
          <div>
            <dt>Exact reason</dt>
            <dd>{session.waiting_reason}</dd>
          </div>
          <div>
            <dt>Next expected evaluation</dt>
            <dd>
              {session.next_evaluation_at ?? "Trigger-based"} · {countdown}
            </dd>
          </div>
          <div>
            <dt>Trigger condition</dt>
            <dd>{session.trigger_condition}</dd>
          </div>
        </dl>
      </section>
      {session.input_health.length ? (
        <DataTable
          caption="Freshness for every required strategy input"
          rows={session.input_health.map((input) => ({
            id: `${input.exchange}-${input.instrument}`,
            ...input,
            exchange: shadowPublicExchangeName(input.exchange),
            state: input.state.replaceAll("_", " ").toLowerCase(),
            fresh: input.fresh ? "yes" : "no",
          }))}
          columns={[
            { key: "exchange", label: "Exchange" },
            { key: "instrument", label: "Instrument" },
            { key: "state", label: "State" },
            { key: "fresh", label: "Fresh" },
            { key: "age_milliseconds", label: "Age ms" },
            { key: "reason", label: "Reason" },
          ]}
        />
      ) : (
        <StatePanel
          state="loading"
          detail="The worker has not published the first exact required-input health set yet."
        />
      )}
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
      <ShadowSessionResults session={session} />
    </>
  );
}

function shadowPublicExchangeName(exchange: string): string {
  switch (exchange) {
    case "binance":
      return "Binance production public";
    case "bybit":
      return "Bybit production public";
    default:
      return "Unknown production-public source";
  }
}
