import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { sessionQuery } from "../api/queries";
import { DataTable } from "../components/DataTable";
import { StatePanel } from "../components/StatePanel";
import { LabSafetyNote } from "../features/labs/LabRunTools";
import { ShadowSessionEvidence } from "../features/labs/ShadowSessionEvidence";
import { compareShadowSessions } from "../features/labs/shadowModel";
import { hasAccess } from "../features/shared/access";
import { Field, Lab } from "./ResearchLabShared";
import styles from "./Page.module.css";

export function ShadowCenter() {
  const { id } = useParams();
  const [configuration, setConfiguration] = useState("");
  const [portfolio, setPortfolio] = useState("");
  const [strategy, setStrategy] = useState("trend.v1a.1");
  const [sessionID, setSessionID] = useState(id ?? "");
  const [compareID, setCompareID] = useState("");
  const currentUser = useQuery(sessionQuery);
  const canControl = currentUser.data
    ? hasAccess(currentUser.data.user, ["research.control"])
    : false;
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
  const history = useQuery({
    queryKey: ["shadow", "history"],
    queryFn: () =>
      getAPI<"ShadowSessionPage">("/api/v1/shadow-sessions?page_size=50"),
  });
  const comparison = useQuery({
    queryKey: ["shadow", "comparison", compareID],
    queryFn: () =>
      getAPI<"ShadowSessionResource">(
        `/api/v1/shadow-sessions/${encodeURIComponent(compareID)}`,
      ),
    enabled: compareID !== "",
  });
  const stop = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        `/api/v1/shadow-sessions/${sessionID}/stop`,
        {
          expected_revision: session.data?.revision,
          reason: "authorized researcher requested graceful stop",
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
        <label>
          Strategy version
          <select
            value={strategy}
            onChange={(event) => setStrategy(event.target.value)}
          >
            <option value="trend.v1a.1">Trend v1a.1</option>
          </select>
        </label>
        <button type="submit" disabled={create.isPending || !canControl}>
          Start virtual shadow
        </button>
      </form>
      <LabSafetyNote />
      {!canControl && (
        <StatePanel
          state="forbidden"
          detail="Your role can inspect shadow evidence but cannot start or stop a session."
        />
      )}
      {create.isError && (
        <StatePanel
          state="paused"
          detail="Safety prerequisites, identity, or one-session quota prevented start."
        />
      )}
      {session.data && (
        <ShadowSessionEvidence
          session={session.data}
          canControl={canControl}
          stopPending={stop.isPending}
          onStop={() => stop.mutate()}
        />
      )}
      <section className={styles.card}>
        <h2>Compare shadow sessions</h2>
        <form
          className={styles.form}
          onSubmit={(event) => event.preventDefault()}
        >
          <label>
            Comparison session ID
            <input
              value={compareID}
              onChange={(event) => setCompareID(event.target.value.trim())}
            />
          </label>
        </form>
        {session.data && comparison.data && (
          <DataTable
            caption={`Shadow comparison: ${session.data.id} and ${comparison.data.id}`}
            rows={compareShadowSessions(session.data, comparison.data)}
            columns={[
              { key: "field", label: "Evidence field" },
              { key: "left", label: session.data.id },
              { key: "right", label: comparison.data.id },
              { key: "changed", label: "Changed" },
            ]}
          />
        )}
      </section>
      <section className={styles.card}>
        <h2>Durable shadow history</h2>
        {history.data?.items.length ? (
          <ul className={styles.timeline}>
            {history.data.items.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  className={styles.rowButton}
                  onClick={() => setSessionID(item.id)}
                >
                  {item.id}
                  <span>
                    {item.state} · revision {item.revision}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <StatePanel
            state={history.isLoading ? "loading" : "empty"}
            detail="No public-data shadow sessions are visible."
          />
        )}
      </section>
    </Lab>
  );
}
