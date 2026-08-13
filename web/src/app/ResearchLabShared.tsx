import { lazy, Suspense, type ReactNode } from "react";

import type { APIModel } from "../api/client";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import styles from "./Page.module.css";
import { RegisteredResearchReport } from "./RegisteredResearchReport";
const EvidenceChart = lazy(() =>
  import("../components/EvidenceChart").then((module) => ({
    default: module.EvidenceChart,
  })),
);

export function JobPanel({ job }: { readonly job: APIModel<"JobResource"> }) {
  return (
    <>
      <div className={styles.metrics}>
        <MetricCard
          label="Job state"
          value={job.state}
          tone={job.state === "SUCCEEDED" ? "good" : "neutral"}
        />
        <MetricCard label="Mode" value={job.mode_label} />
        <MetricCard label="Progress" value={job.progress ?? "—"} />
        <MetricCard label="Revision" value={job.revision} />
      </div>
      <section className={styles.card}>
        <h2>Run progress and identity</h2>
        <progress
          aria-label="Durable run progress"
          aria-valuetext={job.progress ?? job.state}
          max={1}
          value={job.state === "SUCCEEDED" ? 1 : undefined}
        />
        {job.input_manifest ? (
          <dl className={styles.facts}>
            {Object.entries(job.input_manifest).map(([name, value]) => (
              <div key={name}>
                <dt>{name.replaceAll("_", " ")}</dt>
                <dd>{String(value)}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <StatePanel
            state="partial"
            detail="This historical run has no projected input manifest. Its result remains visible, but comparison is incomplete."
          />
        )}
      </section>
      {job.result && (
        <section className={styles.card}>
          <h2>Authoritative result</h2>
          <dl className={styles.facts}>
            <div>
              <dt>Platform correctness</dt>
              <dd>{job.result.platform_correctness}</dd>
            </div>
            <div>
              <dt>Strategy evidence</dt>
              <dd>{job.result.strategy_evidence}</dd>
            </div>
            <div>
              <dt>Viability</dt>
              <dd>{job.result.viability}</dd>
            </div>
            <div>
              <dt>Reproducibility</dt>
              <dd>{job.result.reproducibility}</dd>
            </div>
            <div>
              <dt>Result hash</dt>
              <dd>{job.result.result_hash}</dd>
            </div>
            <div>
              <dt>Research report</dt>
              <dd>{job.result.report_id}</dd>
            </div>
            <div>
              <dt>Report hash</dt>
              <dd>{job.result.report_hash}</dd>
            </div>
            <div>
              <dt>Confidence</dt>
              <dd>{job.result.confidence_label}</dd>
            </div>
            <div>
              <dt>Research coverage</dt>
              <dd>{job.result.research_coverage}</dd>
            </div>
          </dl>
          {job.result.research_coverage === "single_run_incomplete" && (
            <StatePanel
              state="degraded"
              detail="Baseline metrics are complete. Walk-forward, confidence, neighborhood, capacity, stress, benchmark, and breakdown evidence are not established by this single run."
            />
          )}
          {job.result.metrics && (
            <>
              <dl className={styles.facts} aria-label="Exact run metrics">
                {Object.entries(job.result.metrics).map(([name, value]) => (
                  <div key={name}>
                    <dt>{name.replaceAll("_", " ")}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              </dl>
              <Suspense fallback={<StatePanel state="loading" />}>
                <EvidenceChart metrics={job.result.metrics} />
              </Suspense>
            </>
          )}
          <p role="note">{job.result.disclaimer}</p>
          <p role="note" className={styles.notice}>
            Platform correctness and strategy viability are separate evidence
            dimensions. This result does not prove profitability.
          </p>
        </section>
      )}
      {job.registered_report && (
        <RegisteredResearchReport report={job.registered_report} />
      )}
    </>
  );
}

export function Lab({
  title,
  eyebrow,
  description,
  children,
}: {
  readonly title: string;
  readonly eyebrow: string;
  readonly description: string;
  readonly children: ReactNode;
}) {
  return (
    <section className={styles.page}>
      <header className={styles.header}>
        <div>
          <span className={styles.eyebrow}>{eyebrow}</span>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
      </header>
      {children}
    </section>
  );
}
