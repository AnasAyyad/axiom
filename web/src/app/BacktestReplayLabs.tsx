import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";

import { getAPI } from "../api/client";
import { sessionQuery } from "../api/queries";
import { LabRunTools, LabSafetyNote } from "../features/labs/LabRunTools";
import styles from "./Page.module.css";
import { JobPanel, Lab } from "./ResearchLabShared";

export { ReplayLab } from "./ReplayLab";

export function BacktestLab() {
  const { id } = useParams();
  const jobID = id ?? "";
  const session = useQuery(sessionQuery);
  const ownerSessionReady = session.isSuccess;
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
      description="Inspect existing deterministic research jobs. New work is created from reviewed server choices."
    >
      <LabSafetyNote />
      {jobID === "" && (
        <section className={styles.card}>
          <h2>Start a reviewed backtest</h2>
          <p>
            New backtests use the server-approved strategy, venue, instrument,
            and qualified-input selection. You never need to paste an internal
            configuration, dataset, or model identifier.
          </p>
          <Link className={styles.action} to="/run-lab">
            Choose a reviewed run
          </Link>
        </section>
      )}
      {job.data && (
        <>
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
