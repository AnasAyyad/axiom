import { lazy, Suspense, type FormEvent, type ReactNode } from "react";

import type { APIModel } from "../api/client";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import styles from "./Page.module.css";
import { emptyRun } from "./researchLabModel";
const EvidenceChart = lazy(() =>
  import("../components/EvidenceChart").then((module) => ({
    default: module.EvidenceChart,
  })),
);

export function RunForm({
  form,
  setForm,
  label,
  pending,
  submit,
}: {
  readonly form: typeof emptyRun;
  readonly setForm: (value: typeof emptyRun) => void;
  readonly label: string;
  readonly pending: boolean;
  readonly submit: () => void;
}) {
  function handle(event: FormEvent) {
    event.preventDefault();
    submit();
  }
  return (
    <form className={`${styles.card} ${styles.form}`} onSubmit={handle}>
      <Field
        label="Configuration ID"
        value={form.configuration}
        set={(configuration) => setForm({ ...form, configuration })}
      />
      <Field
        label="Dataset ID"
        value={form.dataset}
        set={(dataset) => setForm({ ...form, dataset })}
      />
      <Field
        label="Strategy version"
        value={form.strategy}
        set={(strategy) => setForm({ ...form, strategy })}
      />
      <Field
        label="Root seed hash"
        value={form.seed}
        set={(seed) => setForm({ ...form, seed })}
      />
      <button type="submit" disabled={pending}>
        {pending ? "Persisting…" : label}
      </button>
    </form>
  );
}

export function Field({
  label,
  value,
  set,
}: {
  readonly label: string;
  readonly value: string;
  readonly set: (value: string) => void;
}) {
  return (
    <label>
      {label}
      <input
        required
        value={value}
        onChange={(event) => set(event.target.value)}
      />
    </label>
  );
}

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
          </dl>
          {job.result.metrics && (
            <Suspense fallback={<StatePanel state="loading" />}>
              <EvidenceChart metrics={job.result.metrics} />
            </Suspense>
          )}
        </section>
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
