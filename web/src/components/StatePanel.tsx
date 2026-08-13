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
    | "blocked"
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
    forbidden: "This evidence is not available for the current owner session",
    validation: "Review the highlighted values",
    partial: "Some authoritative data is unavailable",
    blocked: "A prerequisite is blocking this workflow",
    error: "Authoritative state is unavailable",
  };
  const marks: Record<StatePanelProps["state"], string> = {
    loading: "···",
    empty: "—",
    degraded: "!",
    stale: "!",
    paused: "Ⅱ",
    locked: "×",
    reconnecting: "↻",
    forbidden: "×",
    validation: "!",
    partial: "!",
    blocked: "!",
    error: "×",
  };
  return (
    <section
      className={styles.statePanel}
      data-state={state}
      role={state === "error" || state === "forbidden" ? "alert" : "status"}
      aria-live="polite"
    >
      <span className={styles.stateMark} aria-hidden="true">
        {marks[state]}
      </span>
      <span className={styles.stateCopy}>
        <strong>{labels[state]}</strong>
        {detail && <span>{detail}</span>}
      </span>
    </section>
  );
}
