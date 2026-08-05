import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";

import {
  runEvidenceQuery,
  runOutputsQuery,
  runPortfolioProjectionQuery,
  runQuery,
  runRiskProjectionQuery,
} from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import styles from "../shared/D2.module.css";

const outputViews = [
  ["timeline", "Timeline"],
  ["decisions", "Decisions"],
  ["orders", "Orders"],
  ["fills", "Fills"],
] as const;

export function RunDetailPage() {
  const { id = "" } = useParams();
  const run = useQuery(runQuery(id));
  const portfolio = useQuery(runPortfolioProjectionQuery(id));
  const risk = useQuery(runRiskProjectionQuery(id));
  const evidence = useQuery(runEvidenceQuery(id));
  const outputs = useQueries({
    queries: outputViews.map(([view]) => runOutputsQuery(id, view)),
  });

  if (run.isLoading) return <StatePanel state="loading" />;
  if (run.isError || !run.data)
    return (
      <StatePanel
        state="error"
        detail="This run is unavailable or you no longer have a safe projection for it."
      />
    );
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
