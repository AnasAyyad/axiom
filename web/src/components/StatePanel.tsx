import styles from "./UI.module.css";

interface StatePanelProps {
  readonly state:
    | "loading"
    | "empty"
    | "degraded"
    | "stale"
    | "paused"
    | "locked"
    | "reconnecting"
    | "forbidden"
    | "validation"
    | "partial"
    | "error";
  readonly detail?: string | undefined;
}

/** StatePanel makes non-happy operational states explicit and screen-reader visible. */
export function StatePanel({ state, detail }: StatePanelProps) {
  const labels: Record<StatePanelProps["state"], string> = {
    loading: "Loading authoritative state…",
    empty: "No durable records yet",
    degraded: "Service is degraded",
    stale: "Data is stale",
    paused: "Operations are paused",
    locked: "Safety lock is active",
    reconnecting: "Reconnecting to live updates…",
    forbidden: "You do not have permission to view this evidence",
    validation: "Review the highlighted values",
    partial: "Some authoritative data is unavailable",
    error: "Authoritative state is unavailable",
  };
  return (
    <section className={styles.statePanel} role="status" aria-live="polite">
      <strong>{labels[state]}</strong>
      {detail && <span>{detail}</span>}
    </section>
  );
}
