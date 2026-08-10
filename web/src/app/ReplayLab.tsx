import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { sessionQuery } from "../api/queries";
import { ConfirmAction } from "../components/ConfirmAction";
import { StatePanel } from "../components/StatePanel";
import { LabRunTools, LabSafetyNote } from "../features/labs/LabRunTools";
import styles from "./Page.module.css";
import { ReplayEvidence } from "./ReplayEvidence";
import { ReplayFaultScheduler } from "./ReplayFaultScheduler";
import { JobPanel, Lab } from "./ResearchLabShared";

export function ReplayLab() {
  const { id } = useParams();
  const jobID = id ?? "";
  const [ordinalInput, setOrdinalInput] = useState("");
  const [inspectionOrdinal, setInspectionOrdinal] = useState("");
  const session = useQuery(sessionQuery);
  const ownerSessionReady = session.isSuccess;
  const job = useQuery({
    queryKey: ["replay", jobID, inspectionOrdinal],
    queryFn: () => {
      const selected =
        inspectionOrdinal === ""
          ? ""
          : `?event_ordinal=${encodeURIComponent(inspectionOrdinal)}`;
      return getAPI<"JobResource">(`/api/v1/replays/${jobID}${selected}`);
    },
    enabled: jobID !== "",
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      return state === "SUCCEEDED" || state === "FAILED" || state === "CANCELED"
        ? false
        : 250;
    },
  });
  const control = useMutation({
    mutationFn: (action: "pause" | "resume" | "step") =>
      postAPI<"CommandAccepted">(
        `/api/v1/replays/${jobID}/${action}`,
        {
          expected_revision: job.data?.revision,
          reason: `owner requested ${action}`,
        },
        newIdempotencyKey(`replay-${action}`),
      ),
    onSuccess: async () => {
      await job.refetch();
    },
  });
  return (
    <Lab
      title="Replay Lab"
      eyebrow="Exact event ordering"
      description="Inspect an existing recorded-data replay. New work is created from reviewed server choices."
    >
      <LabSafetyNote />
      {jobID === "" && (
        <section className={styles.card}>
          <h2>Start a reviewed replay</h2>
          <p>
            New replays use a server-approved strategy and qualified inputs. You
            never need to paste internal configuration, dataset, or model
            identifiers into the browser.
          </p>
          <Link className={styles.action} to="/run-lab">
            Choose a reviewed run
          </Link>
        </section>
      )}
      {job.data && (
        <>
          <ReplayFaultScheduler jobID={jobID} jobState={job.data.state} />
          <section className={styles.card}>
            <h2>Replay controls</h2>
            <div className={styles.actions}>
              {(["pause", "step", "resume"] as const).map((action) => (
                <ConfirmAction
                  key={action}
                  trigger={
                    <button
                      type="button"
                      className={styles.actionSecondary}
                      disabled={control.isPending || !ownerSessionReady}
                    >
                      {action}
                    </button>
                  }
                  title={`${action} deterministic replay?`}
                  description="The command is idempotent, durable, audited, and checked against the current revision."
                  confirmLabel={action}
                  onConfirm={() => control.mutate(action)}
                />
              ))}
            </div>
          </section>
          <section className={styles.card}>
            <h2>Exact event and decision inspection</h2>
            <form
              className={styles.form}
              onSubmit={(event) => {
                event.preventDefault();
                setInspectionOrdinal(ordinalInput);
              }}
            >
              <label>
                Event ordinal
                <input
                  inputMode="numeric"
                  pattern="[1-9][0-9]*"
                  placeholder="Newest event"
                  value={ordinalInput}
                  onChange={(event) => setOrdinalInput(event.target.value)}
                />
              </label>
              <button type="submit">Inspect persisted event</button>
            </form>
            {job.data.replay_inspection ? (
              <ReplayEvidence inspection={job.data.replay_inspection} />
            ) : (
              <StatePanel
                state={job.data.state === "RUNNING" ? "loading" : "empty"}
                detail="No persisted replay event is available at this ordinal yet."
              />
            )}
          </section>
          <section className={styles.card}>
            <h2>Durable replay checkpoints</h2>
            {job.data.checkpoints?.length ? (
              <ol className={styles.timeline}>
                {job.data.checkpoints.map((checkpoint) => (
                  <li key={checkpoint.revision}>
                    <button
                      type="button"
                      className={styles.rowButton}
                      onClick={() => {
                        setOrdinalInput(checkpoint.input_ordinal);
                        setInspectionOrdinal(checkpoint.input_ordinal);
                      }}
                    >
                      Ordinal {checkpoint.input_ordinal}
                      <span>checkpoint revision {checkpoint.revision}</span>
                    </button>
                  </li>
                ))}
              </ol>
            ) : (
              <StatePanel
                state={job.data.state === "RUNNING" ? "loading" : "empty"}
                detail="No durable checkpoint has been materialized yet."
              />
            )}
          </section>
          <JobPanel job={job.data} />
          <LabRunTools
            job={job.data}
            canControl={ownerSessionReady}
            canExport={ownerSessionReady}
            refresh={() => job.refetch()}
          />
        </>
      )}
    </Lab>
  );
}
