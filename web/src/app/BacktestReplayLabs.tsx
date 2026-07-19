import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
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
    refetchInterval: 2_000,
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
  const create = useMutation({
    mutationFn: () =>
      postAPI<"JobResource">(
        "/api/v1/replays",
        {
          configuration_id: form.configuration,
          dataset_id: form.dataset,
          strategy_version: form.strategy,
          root_seed_hash: form.seed,
          speed: "original",
          incident_id: search.get("incident") ?? undefined,
          first_ordinal: search.get("first") ?? undefined,
          last_ordinal: search.get("last") ?? undefined,
        },
        newIdempotencyKey("replay"),
      ),
    onSuccess: (job) => setJobID(job.id),
  });
  const job = useQuery({
    queryKey: ["replay", jobID],
    queryFn: () => getAPI<"JobResource">(`/api/v1/replays/${jobID}`),
    enabled: jobID !== "",
    refetchInterval: 2_000,
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
    onSuccess: () => void job.refetch(),
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
          <JobPanel job={job.data} />
          <section className={styles.card}>
            <h2>Replay controls</h2>
            <div className={styles.header}>
              {(["pause", "resume", "step"] as const).map((action) => (
                <ConfirmAction
                  key={action}
                  trigger={
                    <button className={styles.actionSecondary}>{action}</button>
                  }
                  title={`${action} deterministic replay?`}
                  description="The command is idempotent, durable, audited, and checked against the current revision."
                  confirmLabel={action}
                  onConfirm={() => control.mutate(action)}
                />
              ))}
            </div>
          </section>
        </>
      )}
    </Lab>
  );
}
