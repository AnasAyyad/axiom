import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";

import {
  getAPI,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../api/client";
import { ConfirmAction } from "../components/ConfirmAction";
import { StatePanel } from "../components/StatePanel";
import { JobPanel, Lab, RunForm } from "./ResearchLabShared";
import styles from "./Page.module.css";
import { emptyRun } from "./researchLabModel";

export function BacktestLab() {
  const { id } = useParams();
  const [form, setForm] = useState(emptyRun);
  const [jobID, setJobID] = useState(id ?? "");
  const create = useMutation({
    mutationFn: () =>
      postAPI<"JobResource">(
        "/api/v1/backtests",
        {
          configuration_id: form.configuration,
          dataset_id: form.dataset,
          research_generation_id: form.researchGeneration,
          strategy_version: form.strategy,
          root_seed_hash: form.seed,
        },
        newIdempotencyKey("backtest"),
      ),
    onSuccess: (job) => setJobID(job.id),
  });
  const job = useQuery({
    queryKey: ["backtest", jobID],
    queryFn: () => getAPI<"JobResource">(`/api/v1/backtests/${jobID}`),
    enabled: jobID !== "",
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      return state === "SUCCEEDED" || state === "FAILED" || state === "CANCELED"
        ? false
        : 2_000;
    },
  });
  return (
    <Lab
      title="Backtest Lab"
      eyebrow="Deterministic offline research"
      description="Create a durable Trend backtest from immutable configuration, dataset, strategy, and seed identities."
    >
      <RunForm
        form={form}
        setForm={setForm}
        label="Launch backtest"
        pending={create.isPending}
        submit={() => create.mutate()}
      />
      {create.isError && (
        <StatePanel
          state="error"
          detail="The server rejected the run definition or quota."
        />
      )}
      {job.data && <JobPanel job={job.data} />}
    </Lab>
  );
}

export function ReplayLab() {
  const { id } = useParams();
  const [search] = useSearchParams();
  const [form, setForm] = useState({
    ...emptyRun,
    dataset: search.get("dataset") ?? "",
  });
  const [jobID, setJobID] = useState(id ?? "");
  const [ordinalInput, setOrdinalInput] = useState("");
  const [inspectionOrdinal, setInspectionOrdinal] = useState("");
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
          speed: "maximum",
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
      <RunForm
        form={form}
        setForm={setForm}
        label="Create replay"
        pending={create.isPending}
        submit={() => create.mutate()}
      />
      {job.data && (
        <>
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
                      disabled={control.isPending}
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
          <JobPanel job={job.data} />
        </>
      )}
    </Lab>
  );
}

function ReplayEvidence({
  inspection,
}: {
  readonly inspection: NonNullable<
    APIModel<"JobResource">["replay_inspection"]
  >;
}) {
  const evidence = [
    ["Canonical event", inspection.canonical_event],
    ["Canonical decision", inspection.canonical_decision],
    ["Canonical orders", inspection.canonical_orders],
    ["Canonical execution events", inspection.canonical_execution_events],
    ["Canonical balances", inspection.canonical_balances],
  ] as const;
  return (
    <div>
      <dl className={styles.facts} aria-label="Replay event identity">
        <div>
          <dt>Selected ordinal</dt>
          <dd>{inspection.ordinal}</dd>
        </div>
        <div>
          <dt>Persisted event count</dt>
          <dd>{inspection.event_count}</dd>
        </div>
        <div>
          <dt>Canonical event hash</dt>
          <dd>{inspection.event_hash}</dd>
        </div>
      </dl>
      {evidence.map(([label, value]) => (
        <details key={label}>
          <summary>{label}</summary>
          <pre className={styles.canonical}>{value}</pre>
        </details>
      ))}
    </div>
  );
}
