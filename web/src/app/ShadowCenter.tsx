import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router-dom";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { ConfirmAction } from "../components/ConfirmAction";
import { DataTable } from "../components/DataTable";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import { Field, Lab } from "./ResearchLabShared";
import styles from "./Page.module.css";

export function ShadowCenter() {
  const { id } = useParams();
  const [configuration, setConfiguration] = useState("");
  const [portfolio, setPortfolio] = useState("");
  const [strategy, setStrategy] = useState("trend.v1a.1");
  const [sessionID, setSessionID] = useState(id ?? "");
  const create = useMutation({
    mutationFn: () =>
      postAPI<"ShadowSessionResource">(
        "/api/v1/shadow-sessions",
        {
          configuration_id: configuration,
          portfolio_id: portfolio,
          strategy_version: strategy,
        },
        newIdempotencyKey("shadow"),
      ),
    onSuccess: (session) => setSessionID(session.id),
  });
  const session = useQuery({
    queryKey: ["shadow", sessionID],
    queryFn: () =>
      getAPI<"ShadowSessionResource">(`/api/v1/shadow-sessions/${sessionID}`),
    enabled: sessionID !== "",
    refetchInterval: 2_000,
  });
  const stop = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        `/api/v1/shadow-sessions/${sessionID}/stop`,
        {
          expected_revision: session.data?.revision,
          reason: "owner requested graceful stop",
        },
        newIdempotencyKey("shadow-stop"),
      ),
    onSuccess: () => void session.refetch(),
  });
  return (
    <Lab
      title="Shadow Trading Center"
      eyebrow="Public-live · virtual execution"
      description="Binance production-public data feeds only the simulation broker. No private credentials or external order path exists."
    >
      <form
        className={`${styles.card} ${styles.form}`}
        onSubmit={(event) => {
          event.preventDefault();
          create.mutate();
        }}
      >
        <Field
          label="Configuration ID"
          value={configuration}
          set={setConfiguration}
        />
        <Field label="Portfolio ID" value={portfolio} set={setPortfolio} />
        <Field label="Strategy version" value={strategy} set={setStrategy} />
        <button type="submit" disabled={create.isPending}>
          Start virtual shadow
        </button>
      </form>
      {create.isError && (
        <StatePanel
          state="paused"
          detail="Safety prerequisites, identity, or one-session quota prevented start."
        />
      )}
      {session.data && (
        <>
          <div className={styles.metrics}>
            <MetricCard label="State" value={session.data.state} />
            <MetricCard
              label="Entries enabled"
              value={session.data.entries_enabled ? "yes" : "no"}
            />
            <MetricCard
              label="Public only"
              value={session.data.public_only ? "yes" : "no"}
              tone="good"
            />
            <MetricCard
              label="Simulation only"
              value={session.data.simulation_only ? "yes" : "no"}
              tone="good"
            />
          </div>
          <ConfirmAction
            trigger={
              <button className={styles.actionDanger}>
                Stop shadow session
              </button>
            }
            title="Stop this virtual shadow session?"
            description="New entries remain disabled and the engine performs a durable graceful stop."
            confirmLabel="Stop session"
            onConfirm={() => stop.mutate()}
          />
          {session.data.orders?.length ? (
            <DataTable
              caption="Simulated orders and fills"
              rows={session.data.orders.map((order) => ({ ...order }))}
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
      )}
    </Lab>
  );
}
