import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router-dom";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { StatePanel } from "../components/StatePanel";
import { JobPanel, Lab, RunForm } from "./ResearchLabShared";
import { emptyRun } from "./researchLabModel";

export { ReplayLab } from "./ReplayLab";

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
