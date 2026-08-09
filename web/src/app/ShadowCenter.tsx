import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { sessionQuery } from "../api/queries";
import { DataTable } from "../components/DataTable";
import { StatePanel } from "../components/StatePanel";
import { LabSafetyNote } from "../features/labs/LabRunTools";
import { ShadowSessionEvidence } from "../features/labs/ShadowSessionEvidence";
import { compareShadowSessions } from "../features/labs/shadowModel";
import { Lab } from "./ResearchLabShared";
import styles from "./Page.module.css";

export function ShadowCenter() {
  const { id } = useParams();
  const [sessionID, setSessionID] = useState(id ?? "");
  const [compareID, setCompareID] = useState("");
  const currentUser = useQuery(sessionQuery);
  const ownerSessionReady = currentUser.isSuccess;
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
      description="Selected Binance or Bybit production-public data feeds only the simulation broker. No private credentials or external order path exists."
    >
      {sessionID === "" && (
        <section className={styles.card}>
          <h2>Start a reviewed shadow session</h2>
          <p>
            New public-data shadow sessions use the server-approved strategy,
            venue, instrument, portfolio, and risk selection. You never need to
            paste internal identifiers into the browser.
          </p>
          <Link className={styles.action} to="/run-lab">
            Choose a reviewed run
          </Link>
        </section>
      )}
      <LabSafetyNote />
      {!ownerSessionReady && (
        <StatePanel
          state="loading"
          detail="Confirming the owner session before lifecycle controls are available."
        />
      )}
      {session.data && (
        <ShadowSessionEvidence
          session={session.data}
          canControl={ownerSessionReady}
          stopPending={stop.isPending}
          onStop={() => stop.mutate()}
        />
      )}
      <section className={styles.card}>
        <h2>Compare shadow sessions</h2>
        <label className={styles.field}>
          Compare with
          <select
            value={compareID}
            onChange={(event) => setCompareID(event.target.value)}
          >
            <option value="">Choose an existing shadow session</option>
            {history.data?.items
              .filter((item) => item.id !== sessionID)
              .map((item) => (
                <option key={item.id} value={item.id}>
                  {item.state} shadow session · revision {item.revision}
                </option>
              ))}
          </select>
        </label>
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
