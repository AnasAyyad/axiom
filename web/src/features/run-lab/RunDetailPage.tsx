import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";

import { newIdempotencyKey, postAPI } from "../../api/client";
import {
  runEvidenceQuery,
  runOutputsQuery,
  runPortfolioProjectionQuery,
  runQuery,
  runRiskProjectionQuery,
} from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { ConfirmAction } from "../../components/ConfirmAction";
import { StatePanel } from "../../components/StatePanel";
import styles from "../shared/ConsoleSurface.module.css";
import { RunEvidenceTabs } from "./RunEvidenceTabs";

const outputViews = [
  ["timeline", "Timeline"],
  ["decisions", "Decisions"],
  ["orders", "Orders"],
  ["fills", "Fills"],
] as const;

type RunAction = "pause" | "resume" | "step" | "stop";
type RunTab =
  | "overview"
  | "timeline"
  | "decisions"
  | "orders"
  | "portfolio"
  | "risk"
  | "data"
  | "evidence";

const runTabs: ReadonlyArray<readonly [RunTab, string]> = [
  ["overview", "Overview"],
  ["timeline", "Timeline"],
  ["decisions", "Decisions"],
  ["orders", "Orders & Fills"],
  ["portfolio", "Portfolio & P&L"],
  ["risk", "Risk"],
  ["data", "Data & Models"],
  ["evidence", "Evidence"],
];

function actionDescription(action: RunAction) {
  switch (action) {
    case "pause":
      return "The worker will pause at its next safe event boundary.";
    case "resume":
      return "The recorded-data worker will continue from its durable checkpoint.";
    case "step":
      return "The recorded-data worker will process one deterministic event, then pause again.";
    case "stop":
      return "The public-data session will stop safely and reconcile any recorded work.";
  }
}

export function RunDetailPage() {
  const { id = "" } = useParams();
  const [activeTab, setActiveTab] = useState<RunTab>("overview");
  const queryClient = useQueryClient();
  const run = useQuery(runQuery(id));
  const portfolio = useQuery(runPortfolioProjectionQuery(id));
  const risk = useQuery(runRiskProjectionQuery(id));
  const evidence = useQuery(runEvidenceQuery(id));
  const outputs = useQueries({
    queries: outputViews.map(([view]) => runOutputsQuery(id, view)),
  });
  const control = useMutation({
    mutationFn: (action: RunAction) =>
      postAPI<"CommandAccepted">(
        `/api/v1/runs/${encodeURIComponent(id)}/${action}`,
        {
          expected_revision: run.data?.revision ?? "",
          reason: `owner requested ${action} from the run detail page`,
        },
        newIdempotencyKey(`run-${action}`),
      ),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["run", id] });
      await run.refetch();
    },
  });

  if (run.isLoading) return <StatePanel state="loading" />;
  if (run.isError || !run.data)
    return (
      <StatePanel
        state="error"
        detail="This run is unavailable or you no longer have a safe projection for it."
      />
    );
  const actions = run.data.available_actions as RunAction[];
  return (
    <Page
      title={run.data.friendly_name}
      eyebrow="Run detail"
      description="This page follows one immutable run through its recorded strategy, execution, portfolio, risk, and evidence views. Results are research or integration evidence, never proof of profitability."
    >
      <Link className={styles.linkButton} to="/run-lab">
        Back to run history
      </Link>
      <nav
        className={styles.tabs}
        aria-label="Run detail sections"
        role="tablist"
      >
        {runTabs.map(([tab, label]) => (
          <button
            aria-controls={`run-tab-${tab}`}
            aria-selected={activeTab === tab}
            id={`run-tab-control-${tab}`}
            key={tab}
            onClick={() => setActiveTab(tab)}
            role="tab"
            type="button"
          >
            {label}
          </button>
        ))}
      </nav>
      {activeTab === "overview" && (
        <section
          aria-labelledby="run-tab-control-overview"
          className={styles.card}
          id="run-tab-overview"
          role="tabpanel"
        >
          <h2 id="run-overview">Overview</h2>
          <dl className={styles.facts}>
            <div>
              <dt>State</dt>
              <dd>{run.data.state}</dd>
            </div>
            <div>
              <dt>Strategy</dt>
              <dd>{run.data.strategy_version}</dd>
            </div>
            <div>
              <dt>Environment</dt>
              <dd>{run.data.environment.replaceAll("_", " ")}</dd>
            </div>
          </dl>
          {run.data.waiting_reason && (
            <p className={styles.notice}>{run.data.waiting_reason}</p>
          )}
          {run.data.mode === "sandbox" && run.data.state !== "stopped" && (
            <section
              className={styles.section}
              aria-labelledby="sandbox-arm-workflow"
            >
              <h2 id="sandbox-arm-workflow">Owner arm and start</h2>
              <p>
                Preparing this run did not arm an account or create an order.
                Open Exchange Sandbox to review both engine and account states,
                create the short-lived arm, then reauthenticate to start this
                exact strategy session.
              </p>
              <Link className={styles.linkButton} to="/operations/sandbox">
                Review and arm Exchange Sandbox
              </Link>
            </section>
          )}
          {actions.length > 0 && (
            <section className={styles.section} aria-labelledby="run-controls">
              <h2 id="run-controls">Safe controls</h2>
              <p>
                Controls are shown only when this run’s current lifecycle can
                accept them. Each command is revision-checked, durable, and
                audited.
              </p>
              <div className={styles.actions}>
                {actions.map((action) => (
                  <ConfirmAction
                    key={action}
                    trigger={
                      <button
                        type="button"
                        className={
                          action === "stop" ? styles.danger : styles.secondary
                        }
                        disabled={control.isPending}
                      >
                        {action}
                      </button>
                    }
                    title={`${action} this run?`}
                    description={actionDescription(action)}
                    confirmLabel={action}
                    onConfirm={() => control.mutate(action)}
                  />
                ))}
              </div>
              {control.isError && (
                <p className={styles.error} role="alert">
                  The command was not accepted. Refresh the run and try again if
                  the current state still allows it.
                </p>
              )}
            </section>
          )}
        </section>
      )}
      <RunEvidenceTabs
        activeTab={activeTab}
        run={run.data}
        outputs={outputs}
        portfolio={portfolio.data}
        risk={risk.data}
        evidence={evidence.data}
      />
    </Page>
  );
}
