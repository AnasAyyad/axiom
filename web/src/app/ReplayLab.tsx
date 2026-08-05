import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams, useSearchParams } from "react-router";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { sessionQuery } from "../api/queries";
import { ConfirmAction } from "../components/ConfirmAction";
import { StatePanel } from "../components/StatePanel";
import { GuidedRunForm } from "../features/labs/GuidedRunForm";
import { LabRunTools, LabSafetyNote } from "../features/labs/LabRunTools";
import { emptyLabRun } from "../features/labs/labModel";
import { hasAccess } from "../features/shared/access";
import styles from "./Page.module.css";
import { ReplayEvidence } from "./ReplayEvidence";
import { ReplayFaultScheduler } from "./ReplayFaultScheduler";
import { JobPanel, Lab } from "./ResearchLabShared";

export function ReplayLab() {
  const { id } = useParams();
  const [search] = useSearchParams();
  const [form, setForm] = useState({
    ...emptyLabRun,
    dataset: search.get("dataset") ?? "",
  });
  const [jobID, setJobID] = useState(id ?? "");
  const [ordinalInput, setOrdinalInput] = useState("");
  const [inspectionOrdinal, setInspectionOrdinal] = useState("");
  const session = useQuery(sessionQuery);
  const canControl = session.data
    ? hasAccess(session.data.user, ["research.control"])
    : false;
  const canExport = session.data
    ? hasAccess(session.data.user, ["artifacts.read"])
    : false;
  const create = useMutation({
    mutationFn: () =>
      postAPI<"JobResource">(
        "/api/v1/replays",
        {
          configuration_id: form.configuration,
          dataset_id: form.dataset,
          research_generation_id: form.researchGeneration,
          strategy_version: form.strategy,
          root_seed_hash: form.seed,
          speed: form.speed,
          incident_id: search.get("incident") ?? undefined,
          first_ordinal: search.get("first") ?? undefined,
          last_ordinal: search.get("last") ?? undefined,
        },
        newIdempotencyKey("replay"),
      ),
    onSuccess: (job) => setJobID(job.id),
  });
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
      description="Reproduce recorded data, pause safely, or advance one deterministic event while retaining immutable identity."
    >
      <LabSafetyNote />
      <GuidedRunForm
        kind="replay"
        form={form}
        setForm={setForm}
        pending={create.isPending}
        allowed={canControl}
        submit={() => create.mutate()}
      />
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
                      disabled={control.isPending || !canControl}
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
            canControl={canControl}
            canExport={canExport}
            refresh={() => job.refetch()}
          />
        </>
      )}
    </Lab>
  );
}
