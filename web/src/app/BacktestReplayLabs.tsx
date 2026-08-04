import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router-dom";

import { getAPI, newIdempotencyKey, postAPI } from "../api/client";
import { sessionQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import { GuidedRunForm } from "../features/labs/GuidedRunForm";
import { LabRunTools, LabSafetyNote } from "../features/labs/LabRunTools";
import { emptyLabRun } from "../features/labs/labModel";
import { hasAccess } from "../features/shared/access";
import { JobPanel, Lab } from "./ResearchLabShared";

export { ReplayLab } from "./ReplayLab";

export function BacktestLab() {
  const { id } = useParams();
  const [form, setForm] = useState(emptyLabRun);
  const [jobID, setJobID] = useState(id ?? "");
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
      <LabSafetyNote />
      <GuidedRunForm
        kind="backtest"
        form={form}
        setForm={setForm}
        pending={create.isPending}
        allowed={canControl}
        submit={() => create.mutate()}
      />
      {create.isError && (
        <StatePanel
          state="error"
          detail="The server rejected the run definition or quota."
        />
      )}
      {job.data && (
        <>
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
