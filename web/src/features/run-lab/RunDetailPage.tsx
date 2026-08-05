import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
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
import styles from "../shared/D2.module.css";

const outputViews = [
  ["timeline", "Timeline"],
  ["decisions", "Decisions"],
  ["orders", "Orders"],
  ["fills", "Fills"],
] as const;

type RunAction = "pause" | "resume" | "step" | "stop";

function availableRunActions(run: {
  mode: string;
  state: string;
}): RunAction[] {
  if (run.mode === "replay") {
    if (run.state === "RUNNING") return ["pause"];
    if (run.state === "PAUSED") return ["resume", "step"];
    return [];
  }
  if (
    run.mode === "shadow" &&
    (run.state === "QUEUED" || run.state === "RUNNING" || run.state === "PAUSED")
  ) {
    return ["stop"];
  }
  return [];
}

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
  const actions = availableRunActions(run.data);
  return (
    <Page
      title={run.data.friendly_name}
      eyebrow="Run detail"
      description="This page follows one immutable run through its recorded strategy, execution, portfolio, risk, and evidence views. Results are research or integration evidence, never proof of profitability."
    >
      <Link className={styles.linkButton} to="/run-lab">
        Back to run history
      </Link>
      <section className={styles.card} aria-labelledby="run-overview">
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
      </section>
      {actions.length > 0 && (
        <section className={styles.card} aria-labelledby="run-controls">
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
                    className={action === "stop" ? styles.danger : styles.secondary}
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
      <section className={styles.section} aria-labelledby="run-records">
        <h2 id="run-records">Recorded workflow</h2>
        <p>
          Empty collections mean this run has not recorded that kind of event.
          They do not mean an event was inferred or skipped.
        </p>
        <div className={styles.cardGrid}>
          {outputViews.map(([view, label], index) => {
            const result = outputs[index];
            return (
              <article className={styles.card} key={view}>
                <h3>{label}</h3>
                {result?.isLoading && <p>Loading recorded evidence…</p>}
                {result?.isError && <p>Recorded evidence is unavailable.</p>}
                {result?.data && <p>{result.data.items.length} recorded item(s).</p>}
                {result?.data && result.data.items.length > 0 && (
                  <details>
                    <summary>Advanced immutable records</summary>
                    <ul>
                      {result.data.items.slice(0, 10).map((item) => (
                        <li key={`${item.kind}-${item.ordinal}`}>
                          Event {item.ordinal} · {item.content_hash.slice(0, 12)}…
                        </li>
                      ))}
                    </ul>
                  </details>
                )}
              </article>
            );
          })}
        </div>
      </section>
      <section className={styles.cardGrid} aria-label="Portfolio, risk, and evidence">
        <article className={styles.card}>
          <h2>Portfolio &amp; P&amp;L</h2>
          {portfolio.data?.state === "recorded" ? (
            <p>Latest reducer-owned portfolio snapshot recorded at event {portfolio.data.ordinal}.</p>
          ) : (
            <p>{portfolio.data?.waiting_reason ?? "Loading portfolio projection…"}</p>
          )}
        </article>
        <article className={styles.card}>
          <h2>Risk</h2>
          <p>{risk.data?.summary ?? "Loading risk evidence…"}</p>
        </article>
        <article className={styles.card}>
          <h2>Evidence</h2>
          {evidence.data?.state === "recorded" ? (
            <details>
              <summary>Advanced reproducibility identity</summary>
              <dl className={styles.facts}>
                <div><dt>Manifest</dt><dd>{evidence.data.manifest_hash}</dd></div>
                <div><dt>Source commit</dt><dd>{evidence.data.source_commit}</dd></div>
                <div><dt>Confidence tier</dt><dd>{evidence.data.confidence_tier}</dd></div>
              </dl>
            </details>
          ) : (
            <p>No immutable evidence manifest has been recorded for this run yet.</p>
          )}
        </article>
      </section>
    </Page>
  );
}
